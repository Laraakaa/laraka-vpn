package agent

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Laraakaa/laraka-vpn/internal/config"
	"github.com/Laraakaa/laraka-vpn/internal/ipc"
)

// --- test doubles -----------------------------------------------------------

// fakeAuth is an injectable authRunner. fn receives the 1-based call count so a
// single fake can change behaviour between attempts.
type fakeAuth struct {
	calls int
	fn    func(call int) (*AuthResult, error)
}

func (f *fakeAuth) Authenticate(_ context.Context) (*AuthResult, error) {
	f.calls++
	return f.fn(f.calls)
}

// okResult returns a fresh successful AuthResult. A new cookie slice is
// allocated each call because the controller zeroes it after handoff.
func okResult(_ int) (*AuthResult, error) {
	return &AuthResult{Cookie: []byte("cookievalue"), Host: "gw.example.com", Fingerprint: "fp"}, nil
}

// fakeHelper is an injectable tunnelClient. byCmd lets a single fake answer the
// status poll and the connect handoff differently; resp is the fallback.
type fakeHelper struct {
	calls     int
	err       error
	resp      ipc.TunnelResponse
	byCmd     map[ipc.Command]ipc.TunnelResponse
	lastReq   ipc.TunnelRequest
	gotCookie []byte // snapshot of the cookie before the caller zeroes it
	cmds      []ipc.Command
}

func (f *fakeHelper) DoTunnel(req ipc.TunnelRequest) (ipc.TunnelResponse, error) {
	f.calls++
	f.lastReq = req
	f.cmds = append(f.cmds, req.Command)
	f.gotCookie = append([]byte(nil), req.Cookie...)
	if f.err != nil {
		return ipc.TunnelResponse{}, f.err
	}
	if f.byCmd != nil {
		if r, ok := f.byCmd[req.Command]; ok {
			return r, nil
		}
	}
	return f.resp, nil
}

// fakeClock is a controllable monotonic clock.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestController(a authRunner, h tunnelClient, clk *fakeClock) *Controller {
	return &Controller{auth: a, helper: h, now: clk.now, state: ipc.StateIdle}
}

// --- constructor ------------------------------------------------------------

func TestNewControllerInitialState(t *testing.T) {
	c := NewController(&config.UserConfig{})
	if got := c.State(); got != ipc.StateIdle {
		t.Errorf("initial State() = %q, want %q", got, ipc.StateIdle)
	}
}

// --- Connect ----------------------------------------------------------------

func TestConnectSuccess(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateConnecting}}
	c := newTestController(auth, helper, clk)

	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: unexpected error: %v", err)
	}
	if got := c.State(); got != ipc.StateConnecting {
		t.Errorf("State() = %q, want %q", got, ipc.StateConnecting)
	}
	if helper.lastReq.Command != ipc.CmdConnect {
		t.Errorf("helper command = %q, want %q", helper.lastReq.Command, ipc.CmdConnect)
	}
	if !bytes.Equal(helper.gotCookie, []byte("cookievalue")) {
		t.Errorf("cookie delivered = %q, want %q", helper.gotCookie, "cookievalue")
	}
	if helper.lastReq.Host != "gw.example.com" {
		t.Errorf("host delivered = %q, want %q", helper.lastReq.Host, "gw.example.com")
	}
	// Cookie must be zeroed by the controller after handoff (shared backing array).
	for i, b := range helper.lastReq.Cookie {
		if b != 0 {
			t.Errorf("cookie byte %d = %d, want 0 (controller must zero after handoff)", i, b)
		}
	}
}

func TestConnectKeychainTerminal(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: func(int) (*AuthResult, error) { return nil, ErrKeychainNotAuthorized }}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateConnecting}}
	c := newTestController(auth, helper, clk)

	err := c.Connect(context.Background())
	if !errors.Is(err, ErrKeychainNotAuthorized) {
		t.Fatalf("Connect err = %v, want ErrKeychainNotAuthorized", err)
	}
	if got := c.State(); got != ipc.StateAuthFailed {
		t.Errorf("State() = %q, want %q", got, ipc.StateAuthFailed)
	}
	if !c.terminal {
		t.Error("terminal = false, want true after keychain failure")
	}
	if helper.calls != 0 {
		t.Errorf("helper.calls = %d, want 0 (helper must not be contacted on auth failure)", helper.calls)
	}
	// An autonomous reauth must be a silent no-op while terminal.
	c.nextAttemptAt = time.Time{} // cooldown not the blocker
	if err := c.reauth(context.Background()); err != nil {
		t.Errorf("reauth while terminal = %v, want nil no-op", err)
	}
	if auth.calls != 1 {
		t.Errorf("auth.calls = %d, want 1 (terminal must block autonomous reauth)", auth.calls)
	}
}

func TestConnectGenericFailureSchedulesBackoff(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: func(int) (*AuthResult, error) { return nil, errors.New("connection refused") }}
	helper := &fakeHelper{}
	c := newTestController(auth, helper, clk)

	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("Connect: expected error, got nil")
	}
	if got := c.State(); got != ipc.StateAuthFailed {
		t.Errorf("State() = %q, want %q", got, ipc.StateAuthFailed)
	}
	if c.terminal {
		t.Error("terminal = true, want false for generic failure")
	}
	if c.attempt != 1 {
		t.Errorf("attempt = %d, want 1", c.attempt)
	}
	want := clk.t.Add(defaultBackoffBase)
	if !c.nextAttemptAt.Equal(want) {
		t.Errorf("nextAttemptAt = %v, want %v", c.nextAttemptAt, want)
	}
}

func TestConnectAlreadyConnectedNoOp(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{}
	c := newTestController(auth, helper, clk)
	c.state = ipc.StateConnected

	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: unexpected error: %v", err)
	}
	if auth.calls != 0 {
		t.Errorf("auth.calls = %d, want 0 (already connected)", auth.calls)
	}
	if got := c.State(); got != ipc.StateConnected {
		t.Errorf("State() = %q, want %q", got, ipc.StateConnected)
	}
}

func TestConnectClearsTerminalAndRetries(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: func(call int) (*AuthResult, error) {
		if call == 1 {
			return nil, ErrKeychainNotAuthorized
		}
		return okResult(call)
	}}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateConnecting}}
	c := newTestController(auth, helper, clk)

	// First manual connect hits the keychain block.
	_ = c.Connect(context.Background())
	if !c.terminal {
		t.Fatal("expected terminal after first keychain failure")
	}
	// User approved the GUI prompt and reconnects manually: terminal clears.
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect: unexpected error: %v", err)
	}
	if c.terminal {
		t.Error("terminal = true, want false after manual retry")
	}
	if auth.calls != 2 {
		t.Errorf("auth.calls = %d, want 2", auth.calls)
	}
	if got := c.State(); got != ipc.StateConnecting {
		t.Errorf("State() = %q, want %q", got, ipc.StateConnecting)
	}
}

// --- handToHelper failure modes ---------------------------------------------

func TestConnectHelperUnreachable(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{err: errors.New("dial: connection refused")}
	c := newTestController(auth, helper, clk)

	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("Connect: expected error when helper unreachable")
	}
	if got := c.State(); got != ipc.StateDisconnected {
		t.Errorf("State() = %q, want %q", got, ipc.StateDisconnected)
	}
	if c.attempt != 1 {
		t.Errorf("attempt = %d, want 1 (backoff scheduled)", c.attempt)
	}
	// Even on helper failure, the cookie must have been zeroed.
	for i, b := range helper.lastReq.Cookie {
		if b != 0 {
			t.Errorf("cookie byte %d = %d, want 0", i, b)
		}
	}
}

func TestConnectHelperRejectsCookie(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateSessionRejected, Error: "bad cookie"}}
	c := newTestController(auth, helper, clk)

	if err := c.Connect(context.Background()); err == nil {
		t.Fatal("Connect: expected error when helper rejects cookie")
	}
	if got := c.State(); got != ipc.StateSessionRejected {
		t.Errorf("State() = %q, want %q", got, ipc.StateSessionRejected)
	}
	if c.message != "bad cookie" {
		t.Errorf("message = %q, want %q", c.message, "bad cookie")
	}
	if c.attempt != 1 {
		t.Errorf("attempt = %d, want 1", c.attempt)
	}
}

// --- Disconnect -------------------------------------------------------------

func TestDisconnectResets(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateDisconnected}}
	c := newTestController(&fakeAuth{fn: okResult}, helper, clk)
	c.state = ipc.StateConnected
	c.attempt = 3
	c.terminal = true
	c.nextAttemptAt = clk.t.Add(time.Minute)

	if err := c.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect: unexpected error: %v", err)
	}
	if got := c.State(); got != ipc.StateDisconnected {
		t.Errorf("State() = %q, want %q", got, ipc.StateDisconnected)
	}
	if c.attempt != 0 || c.terminal || !c.nextAttemptAt.IsZero() {
		t.Errorf("reset failed: attempt=%d terminal=%v nextAttemptAt=%v", c.attempt, c.terminal, c.nextAttemptAt)
	}
	if helper.lastReq.Command != ipc.CmdDisconnect {
		t.Errorf("helper command = %q, want %q", helper.lastReq.Command, ipc.CmdDisconnect)
	}
}

func TestDisconnectHelperErrorStillResets(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	helper := &fakeHelper{err: errors.New("dial fail")}
	c := newTestController(&fakeAuth{fn: okResult}, helper, clk)
	c.attempt = 5
	c.terminal = true

	if err := c.Disconnect(context.Background()); err == nil {
		t.Fatal("Disconnect: expected error")
	}
	if c.attempt != 0 || c.terminal {
		t.Errorf("reset failed on helper error: attempt=%d terminal=%v", c.attempt, c.terminal)
	}
}

// --- Refresh ----------------------------------------------------------------

func TestRefreshConnectedResetsBackoff(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateConnected}}
	c := newTestController(&fakeAuth{fn: okResult}, helper, clk)
	c.state = ipc.StateConnecting
	c.attempt = 3
	c.nextAttemptAt = clk.t.Add(time.Minute)

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if got := c.State(); got != ipc.StateConnected {
		t.Errorf("State() = %q, want %q", got, ipc.StateConnected)
	}
	if c.attempt != 0 || !c.nextAttemptAt.IsZero() {
		t.Errorf("backoff not reset: attempt=%d nextAttemptAt=%v", c.attempt, c.nextAttemptAt)
	}
}

func TestRefreshDropSchedulesBackoffNoImmediateReauth(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateDisconnected}}
	c := newTestController(auth, helper, clk)
	c.state = ipc.StateConnected

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if got := c.State(); got != ipc.StateSessionRejected {
		t.Errorf("State() = %q, want %q", got, ipc.StateSessionRejected)
	}
	if c.attempt != 1 {
		t.Errorf("attempt = %d, want 1", c.attempt)
	}
	if auth.calls != 0 {
		t.Errorf("auth.calls = %d, want 0 (no reauth on first drop observation)", auth.calls)
	}
}

func TestRefreshStickyReauthAfterCooldown(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{byCmd: map[ipc.Command]ipc.TunnelResponse{
		ipc.CmdStatus:  {State: ipc.StateDisconnected},
		ipc.CmdConnect: {State: ipc.StateConnecting},
	}}
	c := newTestController(auth, helper, clk)
	c.state = ipc.StateSessionRejected
	c.attempt = 1
	c.nextAttemptAt = clk.t.Add(-time.Second) // cooldown already elapsed

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if auth.calls != 1 {
		t.Errorf("auth.calls = %d, want 1 (sticky reauth should fire)", auth.calls)
	}
	if got := c.State(); got != ipc.StateConnecting {
		t.Errorf("State() = %q, want %q", got, ipc.StateConnecting)
	}
}

func TestRefreshStickyReauthBlockedByCooldown(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateDisconnected}}
	c := newTestController(auth, helper, clk)
	c.state = ipc.StateSessionRejected
	c.attempt = 2
	c.nextAttemptAt = clk.t.Add(time.Minute) // still cooling down

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if auth.calls != 0 {
		t.Errorf("auth.calls = %d, want 0 (cooldown must block reauth)", auth.calls)
	}
	if got := c.State(); got != ipc.StateSessionRejected {
		t.Errorf("State() = %q, want %q", got, ipc.StateSessionRejected)
	}
}

func TestRefreshTerminalBlocksReauth(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateDisconnected}}
	c := newTestController(auth, helper, clk)
	c.state = ipc.StateSessionRejected
	c.terminal = true
	c.nextAttemptAt = clk.t.Add(-time.Second) // cooldown elapsed, but terminal blocks

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if auth.calls != 0 {
		t.Errorf("auth.calls = %d, want 0 (terminal must block reauth)", auth.calls)
	}
}

func TestRefreshDegradedSelfHeals(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateDegraded}}
	c := newTestController(auth, helper, clk)
	c.state = ipc.StateConnected

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if got := c.State(); got != ipc.StateDegraded {
		t.Errorf("State() = %q, want %q", got, ipc.StateDegraded)
	}
	if c.attempt != 0 {
		t.Errorf("attempt = %d, want 0 (degraded must not schedule backoff)", c.attempt)
	}
	if auth.calls != 0 {
		t.Errorf("auth.calls = %d, want 0", auth.calls)
	}
}

func TestRefreshSkipsWhenAuthInFlight(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	auth := &fakeAuth{fn: okResult}
	helper := &fakeHelper{resp: ipc.TunnelResponse{State: ipc.StateConnected}}
	c := newTestController(auth, helper, clk)
	c.state = ipc.StateAuthenticating
	c.authInFlight = true

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: unexpected error: %v", err)
	}
	if got := c.State(); got != ipc.StateAuthenticating {
		t.Errorf("State() = %q, want %q (must not reconcile while auth in flight)", got, ipc.StateAuthenticating)
	}
}

// --- pure helpers -----------------------------------------------------------

func TestReconcile(t *testing.T) {
	cases := []struct {
		current, helper ipc.State
		wantState       ipc.State
		wantAction      reconcileAction
	}{
		{ipc.StateConnected, ipc.StateConnected, ipc.StateConnected, actionNone},
		{ipc.StateConnected, ipc.StateDegraded, ipc.StateDegraded, actionNone},
		{ipc.StateConnecting, ipc.StateConnecting, ipc.StateConnecting, actionNone},
		{ipc.StateConnected, ipc.StateDisconnected, ipc.StateSessionRejected, actionReauth},
		{ipc.StateDegraded, ipc.StateDisconnected, ipc.StateSessionRejected, actionReauth},
		{ipc.StateConnecting, ipc.StateDisconnected, ipc.StateSessionRejected, actionReauth},
		{ipc.StateConnected, ipc.StateIdle, ipc.StateSessionRejected, actionReauth},
		{ipc.StateSessionRejected, ipc.StateDisconnected, ipc.StateSessionRejected, actionReauth},
		{ipc.StateIdle, ipc.StateDisconnected, ipc.StateDisconnected, actionNone},
		{ipc.StateDisconnected, ipc.StateIdle, ipc.StateDisconnected, actionNone},
		{ipc.StateIdle, ipc.StateUnknown, ipc.StateUnknown, actionNone},
		{ipc.StateConnected, ipc.StateAuthenticating, ipc.StateUnknown, actionNone},
	}
	for _, tc := range cases {
		gotState, gotAction := reconcile(tc.current, tc.helper)
		if gotState != tc.wantState || gotAction != tc.wantAction {
			t.Errorf("reconcile(%q,%q) = (%q,%v), want (%q,%v)",
				tc.current, tc.helper, gotState, gotAction, tc.wantState, tc.wantAction)
		}
	}
}

func TestNextBackoff(t *testing.T) {
	base, max := 2*time.Second, 2*time.Minute
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 32 * time.Second},
		{6, 64 * time.Second},
		{7, 2 * time.Minute},  // 128s capped to 120s
		{20, 2 * time.Minute}, // far past cap
	}
	for _, tc := range cases {
		if got := nextBackoff(tc.attempt, base, max); got != tc.want {
			t.Errorf("nextBackoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
	// base larger than max must still be capped.
	if got := nextBackoff(1, 5*time.Minute, 2*time.Minute); got != 2*time.Minute {
		t.Errorf("nextBackoff(1, 5m, 2m) = %v, want 2m", got)
	}
}

func TestNormalizeState(t *testing.T) {
	if got := normalizeState("", ipc.StateConnecting); got != ipc.StateConnecting {
		t.Errorf("normalizeState(empty) = %q, want %q", got, ipc.StateConnecting)
	}
	if got := normalizeState(ipc.StateConnected, ipc.StateConnecting); got != ipc.StateConnected {
		t.Errorf("normalizeState(connected) = %q, want %q", got, ipc.StateConnected)
	}
}

// --- serialization guard ----------------------------------------------------

func TestBeginAuthSerializes(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c := newTestController(&fakeAuth{fn: okResult}, &fakeHelper{}, clk)

	if err := c.beginAuth(true); err != nil {
		t.Fatalf("first beginAuth: %v", err)
	}
	if err := c.beginAuth(true); !errors.Is(err, errInProgress) {
		t.Errorf("second beginAuth = %v, want errInProgress", err)
	}
}
