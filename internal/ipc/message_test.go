package ipc

import (
	"bytes"
	"testing"
)

func TestCommandValid(t *testing.T) {
	cases := []struct {
		cmd  Command
		want bool
	}{
		{CmdConnect, true},
		{CmdDisconnect, true},
		{CmdStatus, true},
		{Command("bogus"), false},
		{Command(""), false},
	}
	for _, c := range cases {
		if got := c.cmd.Valid(); got != c.want {
			t.Errorf("Command(%q).Valid() = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// TestTunnelRequestZero verifies Zero wipes the cookie's backing array and nils
// the slice, so a captured reference to the old backing array reads as zeros.
func TestTunnelRequestZero(t *testing.T) {
	cookie := []byte("super-secret-cookie")
	backing := cookie // same backing array
	req := TunnelRequest{Command: CmdConnect, Cookie: cookie, Host: "h"}

	req.Zero()

	if req.Cookie != nil {
		t.Fatalf("Cookie not niled after Zero: %v", req.Cookie)
	}
	for i, b := range backing {
		if b != 0 {
			t.Fatalf("backing[%d] = %d, want 0 (cookie not wiped)", i, b)
		}
	}
}

// TestTunnelRequestZeroNilSafe ensures Zero on an empty/nil cookie is a no-op
// and does not panic.
func TestTunnelRequestZeroNilSafe(t *testing.T) {
	var req TunnelRequest
	req.Zero() // must not panic
	if req.Cookie != nil {
		t.Fatalf("Cookie = %v, want nil", req.Cookie)
	}
}

// TestTunnelRequestZeroIndependentCopy is a sanity check that wiping does not
// rely on aliasing surprises: a separate copy of the bytes is unaffected.
func TestTunnelRequestZeroIndependentCopy(t *testing.T) {
	cookie := []byte("abc")
	independent := bytes.Clone(cookie)
	req := TunnelRequest{Cookie: cookie}
	req.Zero()
	if !bytes.Equal(independent, []byte("abc")) {
		t.Fatalf("independent copy mutated: %v", independent)
	}
}
