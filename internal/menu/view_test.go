package menu

import (
	"context"
	"errors"
	"testing"

	"github.com/Laraakaa/laraka-vpn/internal/ipc"
	"go.uber.org/zap"
)

// fakeController is a test double implementing the local Controller interface.
type fakeController struct {
	state       ipc.State
	message     string
	connectErr  error
	disconnErr  error
	refreshErr  error
	connects    int
	disconnects int
	refreshes   int
}

func (f *fakeController) Connect(ctx context.Context) error {
	f.connects++
	return f.connectErr
}

func (f *fakeController) Disconnect(ctx context.Context) error {
	f.disconnects++
	return f.disconnErr
}

func (f *fakeController) Refresh(ctx context.Context) error {
	f.refreshes++
	return f.refreshErr
}

func (f *fakeController) State() ipc.State { return f.state }
func (f *fakeController) Message() string  { return f.message }

func newTestActions(c Controller) actions {
	return actions{ctrl: c, log: zap.NewNop()}
}

func TestActionsConnectInvokesController(t *testing.T) {
	f := &fakeController{}
	newTestActions(f).connect(context.Background())
	if f.connects != 1 {
		t.Fatalf("connects = %d, want 1", f.connects)
	}
}

func TestActionsConnectSwallowsError(t *testing.T) {
	f := &fakeController{connectErr: errors.New("boom")}
	// Must not panic; error is logged, not propagated.
	newTestActions(f).connect(context.Background())
	if f.connects != 1 {
		t.Fatalf("connects = %d, want 1", f.connects)
	}
}

func TestActionsDisconnectInvokesController(t *testing.T) {
	f := &fakeController{}
	newTestActions(f).disconnect(context.Background())
	if f.disconnects != 1 {
		t.Fatalf("disconnects = %d, want 1", f.disconnects)
	}
}

func TestActionsRefreshInvokesController(t *testing.T) {
	f := &fakeController{refreshErr: errors.New("transient")}
	newTestActions(f).refresh(context.Background())
	if f.refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", f.refreshes)
	}
}

func TestViewForStatusAndTooltip(t *testing.T) {
	v := viewFor(ipc.StateConnected, "")
	if v.status != "● Connected" {
		t.Errorf("status = %q, want %q", v.status, "● Connected")
	}
	if v.barTitle != "● VPN" {
		t.Errorf("barTitle = %q, want %q", v.barTitle, "● VPN")
	}
	if v.tooltip != "Laraka VPN - Connected" {
		t.Errorf("tooltip = %q, want %q", v.tooltip, "Laraka VPN - Connected")
	}
}

func TestViewForTooltipIncludesMessage(t *testing.T) {
	v := viewFor(ipc.StateAuthFailed, "keychain not authorized")
	want := "Laraka VPN - Authentication failed: keychain not authorized"
	if v.tooltip != want {
		t.Errorf("tooltip = %q, want %q", v.tooltip, want)
	}
}

func TestViewForEmptyStateIsUnknown(t *testing.T) {
	v := viewFor(ipc.State(""), "")
	if v.status != "○ Unknown" {
		t.Errorf("status = %q, want %q", v.status, "○ Unknown")
	}
	if v.barTitle != "○ VPN" {
		t.Errorf("barTitle = %q, want %q", v.barTitle, "○ VPN")
	}
}

func TestStateSymbol(t *testing.T) {
	cases := []struct {
		state ipc.State
		want  string
	}{
		{ipc.StateConnected, "●"},
		{ipc.StateDegraded, "◐"},
		{ipc.StateAuthenticating, "⋯"},
		{ipc.StateConnecting, "⋯"},
		{ipc.StateAuthFailed, "✕"},
		{ipc.StateSessionRejected, "✕"},
		{ipc.StateIdle, "○"},
		{ipc.StateDisconnected, "○"},
		{ipc.StateUnknown, "○"},
		{ipc.State(""), "○"},
	}
	for _, tc := range cases {
		if got := stateSymbol(tc.state); got != tc.want {
			t.Errorf("stateSymbol(%s) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestConnectDisconnectEnablement(t *testing.T) {
	cases := []struct {
		state          ipc.State
		wantConnect    bool
		wantDisconnect bool
	}{
		{ipc.StateIdle, true, false},
		{ipc.StateDisconnected, true, false},
		{ipc.StateAuthFailed, true, true},
		{ipc.StateSessionRejected, true, true},
		{ipc.StateAuthenticating, false, true},
		{ipc.StateConnecting, false, true},
		{ipc.StateConnected, false, true},
		{ipc.StateDegraded, false, true},
		{ipc.StateUnknown, true, true},
	}
	for _, tc := range cases {
		v := viewFor(tc.state, "")
		if v.connectEnabled != tc.wantConnect {
			t.Errorf("state %s: connectEnabled = %v, want %v", tc.state, v.connectEnabled, tc.wantConnect)
		}
		if v.disconnectEnabled != tc.wantDisconnect {
			t.Errorf("state %s: disconnectEnabled = %v, want %v", tc.state, v.disconnectEnabled, tc.wantDisconnect)
		}
	}
}

func TestHumanStateAllKnown(t *testing.T) {
	cases := map[ipc.State]string{
		ipc.StateIdle:            "Idle",
		ipc.StateAuthenticating:  "Authenticating",
		ipc.StateAuthFailed:      "Authentication failed",
		ipc.StateConnecting:      "Connecting",
		ipc.StateConnected:       "Connected",
		ipc.StateDegraded:        "Degraded",
		ipc.StateSessionRejected: "Session rejected",
		ipc.StateDisconnected:    "Disconnected",
		ipc.StateUnknown:         "Unknown",
	}
	for state, want := range cases {
		if got := humanState(state); got != want {
			t.Errorf("humanState(%s) = %q, want %q", state, got, want)
		}
	}
}

func TestHumanStateUnrecognizedFallsBackToRaw(t *testing.T) {
	if got := humanState(ipc.State("weird")); got != "weird" {
		t.Errorf("humanState(weird) = %q, want %q", got, "weird")
	}
}
