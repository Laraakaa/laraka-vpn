package agent

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseAuthOutput(t *testing.T) {
	const out = "CONNECT_URL='https://gw.example.com'\n" +
		"COOKIE='abc123cookievalue'\n" +
		"HOST='gw.example.com'\n" +
		"FINGERPRINT='pin-sha256:gnKCGJ6tmhP2eTEjY8ZRT1HSHMzWmAMB0K2VLxBJZVY='\n"

	res, err := parseAuthOutput(strings.NewReader(out))
	if err != nil {
		t.Fatalf("parseAuthOutput: unexpected error: %v", err)
	}
	if !bytes.Equal(res.Cookie, []byte("abc123cookievalue")) {
		t.Errorf("Cookie = %q, want %q", res.Cookie, "abc123cookievalue")
	}
	if res.Host != "gw.example.com" {
		t.Errorf("Host = %q, want %q", res.Host, "gw.example.com")
	}
	if res.Fingerprint != "pin-sha256:gnKCGJ6tmhP2eTEjY8ZRT1HSHMzWmAMB0K2VLxBJZVY=" {
		t.Errorf("Fingerprint = %q, want pin-sha256:...", res.Fingerprint)
	}
}

func TestParseAuthOutputUnquotedAndDoubleQuoted(t *testing.T) {
	const out = "COOKIE=rawcookie\n" +
		"HOST=\"gw2.example.com\"\n"

	res, err := parseAuthOutput(strings.NewReader(out))
	if err != nil {
		t.Fatalf("parseAuthOutput: unexpected error: %v", err)
	}
	if !bytes.Equal(res.Cookie, []byte("rawcookie")) {
		t.Errorf("Cookie = %q, want %q", res.Cookie, "rawcookie")
	}
	if res.Host != "gw2.example.com" {
		t.Errorf("Host = %q, want %q", res.Host, "gw2.example.com")
	}
}

func TestParseAuthOutputCRLF(t *testing.T) {
	const out = "COOKIE='crlfcookie'\r\nHOST='gw.example.com'\r\n"

	res, err := parseAuthOutput(strings.NewReader(out))
	if err != nil {
		t.Fatalf("parseAuthOutput: unexpected error: %v", err)
	}
	if !bytes.Equal(res.Cookie, []byte("crlfcookie")) {
		t.Errorf("Cookie = %q, want %q (CR not stripped?)", res.Cookie, "crlfcookie")
	}
	if res.Host != "gw.example.com" {
		t.Errorf("Host = %q, want %q (CR not stripped?)", res.Host, "gw.example.com")
	}
}

func TestParseAuthOutputMissingCookie(t *testing.T) {
	const out = "HOST='gw.example.com'\nFINGERPRINT='pin-sha256:x'\n"

	if _, err := parseAuthOutput(strings.NewReader(out)); err == nil {
		t.Fatal("parseAuthOutput: expected error for missing COOKIE, got nil")
	}
}

func TestParseAuthOutputMissingHost(t *testing.T) {
	const out = "COOKIE='abc'\nFINGERPRINT='pin-sha256:x'\n"

	if _, err := parseAuthOutput(strings.NewReader(out)); err == nil {
		t.Fatal("parseAuthOutput: expected error for missing HOST, got nil")
	}
}

func TestParseAuthOutputIgnoresNoiseAndBlankKeys(t *testing.T) {
	const out = "Please enter your username.\n" +
		"= leadingequals should be skipped\n" +
		"COOKIE='good'\n" +
		"HOST='gw.example.com'\n" +
		"Connected as 10.0.0.1\n"

	res, err := parseAuthOutput(strings.NewReader(out))
	if err != nil {
		t.Fatalf("parseAuthOutput: unexpected error: %v", err)
	}
	if !bytes.Equal(res.Cookie, []byte("good")) {
		t.Errorf("Cookie = %q, want %q", res.Cookie, "good")
	}
}

// TestParseAuthOutputCookieIsCopy verifies the cookie is copied out of the
// scanner's reusable buffer (a string conversion or sub-slice would alias it).
func TestParseAuthOutputCookieIsCopy(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("COOKIE='copyme'\n")
	b.WriteString("HOST='gw.example.com'\n")
	// Pad with a long trailing line to force the scanner to reuse/overwrite
	// its buffer region after the COOKIE line was read.
	b.WriteString("PADDING=" + strings.Repeat("z", 8192) + "\n")

	res, err := parseAuthOutput(&b)
	if err != nil {
		t.Fatalf("parseAuthOutput: unexpected error: %v", err)
	}
	if !bytes.Equal(res.Cookie, []byte("copyme")) {
		t.Errorf("Cookie = %q, want %q (buffer aliasing?)", res.Cookie, "copyme")
	}
}

func TestClassifyAuthErrorKeychain(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
	}{
		{"errcode", "POST failed: error -25308 while signing"},
		{"errsec", "SecKeyRawSign failed: errSecInteractionNotAllowed"},
		{"phrase", "The user interaction is not allowed."},
		{"shortphrase", "interaction not allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyAuthError(tc.stderr, errors.New("exit status 1"))
			if !errors.Is(err, ErrKeychainNotAuthorized) {
				t.Errorf("classifyAuthError(%q) = %v, want wrapping ErrKeychainNotAuthorized", tc.stderr, err)
			}
		})
	}
}

func TestClassifyAuthErrorGeneric(t *testing.T) {
	err := classifyAuthError("Connection refused by server", errors.New("exit status 1"))
	if err == nil {
		t.Fatal("classifyAuthError: expected error, got nil")
	}
	if errors.Is(err, ErrKeychainNotAuthorized) {
		t.Errorf("classifyAuthError(generic) = %v, must NOT wrap ErrKeychainNotAuthorized", err)
	}
}

func TestClassifyAuthErrorNilRunErr(t *testing.T) {
	// A parse failure with clean stderr: still an error, not the keychain one.
	err := classifyAuthError("", nil)
	if err == nil {
		t.Fatal("classifyAuthError: expected error, got nil")
	}
	if errors.Is(err, ErrKeychainNotAuthorized) {
		t.Errorf("classifyAuthError(empty) = %v, must NOT wrap ErrKeychainNotAuthorized", err)
	}
}

func TestAuthResultZero(t *testing.T) {
	cookie := []byte("secretcookie")
	res := &AuthResult{Cookie: cookie, Host: "gw.example.com"}
	res.Zero()
	if res.Cookie != nil {
		t.Errorf("Cookie = %v, want nil after Zero", res.Cookie)
	}
	for i, c := range cookie {
		if c != 0 {
			t.Errorf("backing array byte %d = %d, want 0 (not wiped)", i, c)
		}
	}
}

func TestAuthResultZeroNilSafe(t *testing.T) {
	var res *AuthResult
	res.Zero() // must not panic

	empty := &AuthResult{}
	empty.Zero()
	if empty.Cookie != nil {
		t.Errorf("Cookie = %v, want nil", empty.Cookie)
	}
}
