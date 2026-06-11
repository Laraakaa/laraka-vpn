package config

import (
	"fmt"
	"net"
	"strings"
)

// maxHostnameLen is the maximum length of a DNS name (RFC 1035 presentation
// form, excluding the trailing dot).
const maxHostnameLen = 253

// NormalizeHost canonicalizes raw into a comparable bare hostname, or returns
// an error if raw is not a plain hostname.
//
// This is deliberately strict (§10b). The result decides whether the root
// helper will dial a host on the agent's behalf, so anything ambiguous — a
// URL, userinfo, an embedded path, a port, control characters — is rejected
// rather than guessed. IP literals are rejected unless allowIPLiterals is set.
//
// On success it returns the host lowercased with any single trailing dot
// removed.
func NormalizeHost(raw string, allowIPLiterals bool) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("config: empty host")
	}
	// Reject NUL and any control character (includes CR, LF, TAB, DEL).
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x20 || raw[i] == 0x7f {
			return "", fmt.Errorf("config: host contains a control character")
		}
	}
	// A bare hostname must not look like a URL or carry userinfo, a path, a
	// query/fragment, or whitespace.
	if strings.Contains(raw, "://") {
		return "", fmt.Errorf("config: host %q looks like a URL", raw)
	}
	if strings.ContainsAny(raw, " \t/\\@?#%") {
		return "", fmt.Errorf("config: host %q contains forbidden characters", raw)
	}

	h := strings.ToLower(raw)
	// Trim a single trailing dot (the FQDN root) before comparison.
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return "", fmt.Errorf("config: host is only a dot")
	}

	// IP literals: only permitted when explicitly configured.
	if ip := net.ParseIP(h); ip != nil {
		if !allowIPLiterals {
			return "", fmt.Errorf("config: host %q is an IP literal (not allowed)", raw)
		}
		return h, nil
	}
	// Anything left containing a colon or bracket is a port or an IPv6
	// literal in some other shape — reject (IPv6 must parse above bare).
	if strings.ContainsAny(h, ":[]") {
		return "", fmt.Errorf("config: host %q contains a port or bracket", raw)
	}

	if len(h) > maxHostnameLen {
		return "", fmt.Errorf("config: host too long (%d > %d)", len(h), maxHostnameLen)
	}
	for _, label := range strings.Split(h, ".") {
		if err := validateLabel(label); err != nil {
			return "", fmt.Errorf("config: host %q: %w", raw, err)
		}
	}
	return h, nil
}

// validateLabel enforces the LDH (letter/digit/hyphen) rule for a single DNS
// label.
func validateLabel(l string) error {
	if len(l) == 0 {
		return fmt.Errorf("empty label")
	}
	if len(l) > 63 {
		return fmt.Errorf("label too long (%d > 63)", len(l))
	}
	if l[0] == '-' || l[len(l)-1] == '-' {
		return fmt.Errorf("label %q has a leading or trailing hyphen", l)
	}
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return fmt.Errorf("label %q has an invalid character", l)
		}
	}
	return nil
}

// HostAllowlist is a normalized set of permitted hosts.
//
// Entries are either exact hostnames ("vpn.example.com") or suffix patterns
// that begin with a dot (".example.com"). A suffix pattern matches that domain
// apex ("example.com") and any subdomain ("vpn.example.com"), but never a
// sibling that merely shares the trailing text ("notexample.com").
type HostAllowlist struct {
	exact    map[string]struct{}
	suffixes []string // each begins with "."
	allowIP  bool
}

// NewHostAllowlist builds an allowlist from raw config entries, normalizing and
// validating each one. An empty allowlist is an error: the agent⇒helper trust
// boundary must never default to "any host".
func NewHostAllowlist(entries []string, allowIPLiterals bool) (*HostAllowlist, error) {
	al := &HostAllowlist{exact: make(map[string]struct{}), allowIP: allowIPLiterals}
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if strings.HasPrefix(e, ".") {
			norm, err := NormalizeHost(strings.TrimPrefix(e, "."), allowIPLiterals)
			if err != nil {
				return nil, fmt.Errorf("config: invalid allowlist suffix %q: %w", raw, err)
			}
			al.suffixes = append(al.suffixes, "."+norm)
		} else {
			norm, err := NormalizeHost(e, allowIPLiterals)
			if err != nil {
				return nil, fmt.Errorf("config: invalid allowlist host %q: %w", raw, err)
			}
			al.exact[norm] = struct{}{}
		}
	}
	if len(al.exact) == 0 && len(al.suffixes) == 0 {
		return nil, fmt.Errorf("config: host allowlist is empty")
	}
	return al, nil
}

// Validate normalizes raw and reports whether it is permitted by the allowlist.
// On success it returns the normalized host that the helper should dial.
func (a *HostAllowlist) Validate(raw string) (string, error) {
	norm, err := NormalizeHost(raw, a.allowIP)
	if err != nil {
		return "", err
	}
	if _, ok := a.exact[norm]; ok {
		return norm, nil
	}
	for _, suffix := range a.suffixes {
		apex := suffix[1:] // strip the leading dot
		if norm == apex || strings.HasSuffix(norm, suffix) {
			return norm, nil
		}
	}
	return "", fmt.Errorf("config: host %q is not in the allowlist", norm)
}

// Len reports the number of entries (exact + suffix) in the allowlist.
func (a *HostAllowlist) Len() int {
	return len(a.exact) + len(a.suffixes)
}
