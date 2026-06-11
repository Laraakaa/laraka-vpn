// Package agent implements the Aqua-session side of the two-process design:
// the menu UI and the authentication orchestrator. The agent is the ONLY
// component that touches the login keychain. It runs
// "openconnect --authenticate" with the PKCS#11 keychain identity, captures the
// resulting cookie/host/fingerprint, and hands the opaque cookie to the
// privileged helper over the IPC trust boundary (§2, §6, §12).
package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Laraakaa/laraka-vpn/internal/config"
)

// ErrKeychainNotAuthorized is returned when openconnect could not sign with the
// keychain identity because user interaction was not permitted
// (errSecInteractionNotAllowed / -25308). This is a terminal condition: the
// state machine must STOP and surface guidance rather than retry, since
// retrying without a GUI approval only produces a prompt storm (§7).
var ErrKeychainNotAuthorized = errors.New("agent: keychain access not authorized")

// AuthResult is the output of a successful --authenticate run. The cookie is
// held as raw bytes and never converted to a string so it does not linger in
// the interned-string heap or get accidentally logged (§6).
type AuthResult struct {
	// Cookie is the opaque session cookie to hand to the helper. Treat as
	// secret: never log, never place in argv/env. Zero it after use.
	Cookie []byte

	// Host is the cookie-auth gateway the helper must dial. It may differ from
	// the profile host (load balancer); the helper re-validates it against its
	// own allowlist before dialing (§10b).
	Host string

	// Fingerprint is the server certificate hash reported by --authenticate
	// (informational; the helper pins via its own --servercert config).
	Fingerprint string
}

// Zero wipes the cookie's backing array and drops the reference. Safe on nil.
func (r *AuthResult) Zero() {
	if r == nil {
		return
	}
	for i := range r.Cookie {
		r.Cookie[i] = 0
	}
	r.Cookie = nil
}

// Authenticator runs the keychain-signing authentication phase.
type Authenticator struct {
	cfg *config.UserConfig
}

// NewAuthenticator returns an Authenticator bound to the user config.
func NewAuthenticator(cfg *config.UserConfig) *Authenticator {
	return &Authenticator{cfg: cfg}
}

// Authenticate runs "openconnect --authenticate" with the PKCS#11 identity and
// returns the captured cookie/host/fingerprint. The invocation is verbatim per
// §12; openconnect v9.12 infers -k from -c. On failure the error is classified:
// a keychain-authorization failure wraps ErrKeychainNotAuthorized so the caller
// can stop instead of retrying.
func (a *Authenticator) Authenticate(ctx context.Context) (*AuthResult, error) {
	args := []string{
		"--protocol=anyconnect",
		"--os=mac-intel",
		"--xmlconfig=" + a.cfg.Profile,
		"--servercert=" + a.cfg.ServerCert,
		"-c", a.cfg.PKCS11URI,
		"--authenticate",
		"--non-inter",
		a.cfg.ServerArg,
	}
	// Absolute path only; never resolved through $PATH (§10c).
	cmd := exec.CommandContext(ctx, a.cfg.OpenconnectPath, args...)

	// The agent is the trusted user-session component and must reach the login
	// keychain through the native-pkcs11 module (discovered via p11-kit), which
	// depends on the user session environment. Unlike the root helper (whose
	// child runs with a minimal env at the cookie trust boundary), inheriting
	// the session env here preserves the proven keychain-signing path. The
	// cookie never transits argv or env.
	cmd.Env = os.Environ()

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("agent: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("agent: starting openconnect: %w", err)
	}

	// Read stdout to EOF before Wait (documented os/exec ordering).
	result, parseErr := parseAuthOutput(stdout)
	waitErr := cmd.Wait()

	if waitErr != nil {
		// Process failed: stdout result (if any) is untrustworthy. Wipe it and
		// classify using stderr diagnostics.
		result.Zero()
		return nil, classifyAuthError(stderr.String(), waitErr)
	}
	if parseErr != nil {
		return nil, classifyAuthError(stderr.String(), parseErr)
	}
	return result, nil
}

// parseAuthOutput parses the shell-style KEY='VALUE' assignments that
// "openconnect --authenticate" writes to stdout (COOKIE, HOST, CONNECT_URL,
// FINGERPRINT, ...). It is pure and unit-testable without launching openconnect.
// The cookie is extracted as bytes and copied out of the scanner buffer; it is
// never converted to a string (§6).
func parseAuthOutput(r io.Reader) (*AuthResult, error) {
	res := &AuthResult{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSuffix(sc.Bytes(), []byte("\r"))
		eq := bytes.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := bytes.TrimSpace(line[:eq])
		val := stripQuotes(line[eq+1:])
		switch string(key) {
		case "COOKIE":
			// Fresh allocation: val aliases the scanner buffer, which is reused
			// on the next Scan. Copy so the cookie survives and stays bytes.
			res.Cookie = append([]byte(nil), val...)
		case "HOST":
			res.Host = string(val)
		case "FINGERPRINT":
			res.Fingerprint = string(val)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("agent: reading authenticate output: %w", err)
	}
	if len(res.Cookie) == 0 {
		return nil, errors.New("agent: no COOKIE in authenticate output")
	}
	if res.Host == "" {
		return nil, errors.New("agent: no HOST in authenticate output")
	}
	return res, nil
}

// stripQuotes trims surrounding whitespace and one matching pair of single or
// double quotes from a value.
func stripQuotes(b []byte) []byte {
	b = bytes.TrimSpace(b)
	if len(b) >= 2 {
		q := b[0]
		if (q == '\'' || q == '"') && b[len(b)-1] == q {
			return b[1 : len(b)-1]
		}
	}
	return b
}

// classifyAuthError inspects openconnect's stderr to distinguish a terminal
// keychain-authorization failure (which must stop the state machine) from a
// generic authentication failure (which may be retried with backoff).
func classifyAuthError(output string, runErr error) error {
	low := strings.ToLower(output)
	for _, marker := range []string{
		"-25308",
		"errsecinteractionnotallowed",
		"interaction is not allowed",
		"interaction not allowed",
		"user interaction is not allowed",
	} {
		if strings.Contains(low, marker) {
			return fmt.Errorf("%w; open the app once from the GUI and approve the keychain prompt", ErrKeychainNotAuthorized)
		}
	}
	if runErr != nil {
		return fmt.Errorf("agent: authenticate failed: %w", runErr)
	}
	return errors.New("agent: authenticate failed")
}
