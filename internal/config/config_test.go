package config

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		allowIP   bool
		want      string
		wantError bool
	}{
		{name: "simple", raw: "vpn.example.com", want: "vpn.example.com"},
		{name: "uppercase lowered", raw: "VPN.Example.COM", want: "vpn.example.com"},
		{name: "trailing dot trimmed", raw: "vpn.example.com.", want: "vpn.example.com"},
		{name: "empty", raw: "", wantError: true},
		{name: "url scheme", raw: "https://vpn.example.com", wantError: true},
		{name: "with port", raw: "vpn.example.com:443", wantError: true},
		{name: "with path", raw: "vpn.example.com/login", wantError: true},
		{name: "userinfo", raw: "user@vpn.example.com", wantError: true},
		{name: "control char newline", raw: "vpn.example.com\n", wantError: true},
		{name: "control char tab", raw: "vpn\t.example.com", wantError: true},
		{name: "del char", raw: "vpn.example.com\x7f", wantError: true},
		{name: "leading hyphen label", raw: "-vpn.example.com", wantError: true},
		{name: "trailing hyphen label", raw: "vpn-.example.com", wantError: true},
		{name: "empty label", raw: "vpn..example.com", wantError: true},
		{name: "ip literal rejected by default", raw: "10.0.0.1", wantError: true},
		{name: "ip literal allowed when configured", raw: "10.0.0.1", allowIP: true, want: "10.0.0.1"},
		{name: "double trailing dot", raw: "vpn.example.com..", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeHost(tt.raw, tt.allowIP)
			if tt.wantError {
				if err == nil {
					t.Fatalf("NormalizeHost(%q) = %q, want error", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeHost(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeHost(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNewHostAllowlistEmpty(t *testing.T) {
	if _, err := NewHostAllowlist(nil, false); err == nil {
		t.Fatal("NewHostAllowlist(nil) = nil error, want error (must never default to any-host)")
	}
	if _, err := NewHostAllowlist([]string{"", "  "}, false); err == nil {
		t.Fatal("NewHostAllowlist(blanks) = nil error, want error")
	}
}

func TestHostAllowlistExact(t *testing.T) {
	a, err := NewHostAllowlist([]string{"vpn.example.com"}, false)
	if err != nil {
		t.Fatalf("NewHostAllowlist: %v", err)
	}
	got, err := a.Validate("VPN.example.com.")
	if err != nil {
		t.Fatalf("Validate exact match unexpected error: %v", err)
	}
	if got != "vpn.example.com" {
		t.Fatalf("Validate normalized = %q, want vpn.example.com", got)
	}
	if _, err := a.Validate("evil.com"); err == nil {
		t.Fatal("Validate(evil.com) = nil error, want not-in-allowlist")
	}
}

func TestHostAllowlistSuffix(t *testing.T) {
	a, err := NewHostAllowlist([]string{".example.com"}, false)
	if err != nil {
		t.Fatalf("NewHostAllowlist: %v", err)
	}
	// subdomain matches suffix
	if _, err := a.Validate("gw1.example.com"); err != nil {
		t.Fatalf("Validate(gw1.example.com) unexpected error: %v", err)
	}
	// apex matches suffix
	if _, err := a.Validate("example.com"); err != nil {
		t.Fatalf("Validate(example.com) apex unexpected error: %v", err)
	}
	// the classic suffix-confusion attack must NOT match
	if _, err := a.Validate("notexample.com"); err == nil {
		t.Fatal("Validate(notexample.com) = nil error, want reject (suffix confusion)")
	}
	if _, err := a.Validate("example.com.evil.com"); err == nil {
		t.Fatal("Validate(example.com.evil.com) = nil error, want reject")
	}
}

func TestSliceArg(t *testing.T) {
	c := &RootConfig{Slices: []string{"10.0.0.0/8", "192.168.0.0/16"}}
	got := c.SliceArg()
	want := "vpn-slice 10.0.0.0/8 192.168.0.0/16"
	if got != want {
		t.Fatalf("SliceArg() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "vpn-slice ") {
		t.Fatalf("SliceArg() = %q, want vpn-slice prefix", got)
	}
}

func TestRootConfigValidate(t *testing.T) {
	base := func() *RootConfig {
		return &RootConfig{
			ServerCert:      "pin-sha256:gnKCGJ6tmhP2eTEjY8ZRT1HSHMzWmAMB0K2VLxBJZVY=",
			Slices:          []string{"10.0.0.0/8"},
			OpenconnectPath: "/opt/homebrew/bin/openconnect",
			VPNSlicePath:    "/opt/homebrew/bin/vpn-slice",
			HelperSocket:    "/var/run/laraka-vpn/helper.sock",
		}
	}
	if err := base().validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := base()
	bad.ServerCert = "sha256:nope"
	if err := bad.validate(); err == nil {
		t.Fatal("validate() accepted server_cert without pin-sha256: prefix")
	}
	bad = base()
	bad.Slices = nil
	if err := bad.validate(); err == nil {
		t.Fatal("validate() accepted empty slices")
	}
	bad = base()
	bad.OpenconnectPath = "openconnect"
	if err := bad.validate(); err == nil {
		t.Fatal("validate() accepted relative openconnect path")
	}
}

func TestUserConfigValidate(t *testing.T) {
	base := func() *UserConfig {
		return &UserConfig{
			PKCS11URI:  "pkcs11:token=Keychain;id=%A3",
			Profile:    "/opt/cisco/anyconnect/profile/SWISSCOM-CERTRAS_client_profile.xml",
			ServerArg:  "Swisscom Secure RAS - Mobile ID",
			ServerCert: "pin-sha256:gnKCGJ6tmhP2eTEjY8ZRT1HSHMzWmAMB0K2VLxBJZVY=",
		}
	}
	if err := base().validate(); err != nil {
		t.Fatalf("valid user config rejected: %v", err)
	}
	bad := base()
	bad.PKCS11URI = "https://nope"
	if err := bad.validate(); err == nil {
		t.Fatal("validate() accepted pkcs11_uri without pkcs11: prefix")
	}
	bad = base()
	bad.ServerArg = ""
	if err := bad.validate(); err == nil {
		t.Fatal("validate() accepted empty server_arg")
	}
}

func TestLoadRootConfigMissing(t *testing.T) {
	// A directory with no vpn-daemon.yaml must error, not silently succeed.
	_, err := LoadRootConfig(t.TempDir(), false)
	if err == nil {
		t.Fatal("LoadRootConfig(empty dir) = nil error, want config-not-found")
	}
	// sanity: error should not be a nil-deref style panic surrogate
	if errors.Is(err, nil) {
		t.Fatal("unexpected nil error wrap")
	}
}
