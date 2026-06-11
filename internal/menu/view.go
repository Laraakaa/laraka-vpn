// Package menu implements the user-session menu-bar UI for the VPN agent.
//
// It is intentionally decoupled from the concrete agent orchestrator: the menu
// drives a small local Controller interface, so the rendering and click-wiring
// logic can be unit-tested without a live GUI session or a real
// openconnect/helper stack. The systray-specific glue lives in menu.go behind a
// darwin/linux build tag; everything in this file is pure and portable.
package menu

import (
	"context"

	"github.com/Laraakaa/laraka-vpn/internal/ipc"
	"go.uber.org/zap"
)

// Controller is the subset of the agent orchestrator that the menu drives.
// It is declared locally (rather than importing the agent package) so the menu
// package stays testable and free of GUI/openconnect dependencies. The concrete
// *agent.Controller satisfies this interface structurally.
type Controller interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Refresh(ctx context.Context) error
	State() ipc.State
	Message() string
}

// actions wraps the controller with a logger and exposes the click/refresh
// handlers. It holds no GUI state, so it is fully unit-testable.
type actions struct {
	ctrl Controller
	log  *zap.Logger
}

func (a actions) connect(ctx context.Context) {
	if err := a.ctrl.Connect(ctx); err != nil {
		a.log.Warn("menu: connect failed", zap.Error(err))
	}
}

func (a actions) disconnect(ctx context.Context) {
	if err := a.ctrl.Disconnect(ctx); err != nil {
		a.log.Warn("menu: disconnect failed", zap.Error(err))
	}
}

func (a actions) refresh(ctx context.Context) {
	if err := a.ctrl.Refresh(ctx); err != nil {
		a.log.Debug("menu: refresh failed", zap.Error(err))
	}
}

// view is the desired visual state of the menu, derived purely from the
// controller's reported state. Keeping this a plain value makes the
// state->display mapping trivially testable.
type view struct {
	// barTitle is the compact string shown permanently in the macOS menu bar
	// (e.g. "● VPN" or "○ VPN"). It is set via systray.SetTitle on every
	// render so the connection state is always visible without opening the menu.
	barTitle          string
	status            string
	tooltip           string
	connectEnabled    bool
	disconnectEnabled bool
}

// viewFor maps a controller state (and optional human message) onto the menu's
// visual state.
func viewFor(state ipc.State, message string) view {
	hs := humanState(state)
	tooltip := "Laraka VPN - " + hs
	if message != "" {
		tooltip = "Laraka VPN - " + hs + ": " + message
	}
	sym := stateSymbol(state)
	return view{
		barTitle:          sym + " VPN",
		status:            sym + " " + hs,
		tooltip:           tooltip,
		connectEnabled:    connectEnabled(state),
		disconnectEnabled: disconnectEnabled(state),
	}
}

// stateSymbol returns a single Unicode indicator glyph for the given state so
// the menu-bar title and status item convey connection health at a glance.
//
//	● – tunnel is up (connected)
//	◐ – tunnel alive but degraded / reconnecting
//	⋯ – transient in-progress states (authenticating, connecting)
//	✕ – terminal error (auth_failed, session_rejected)
//	○ – no active tunnel (idle, disconnected, unknown)
func stateSymbol(s ipc.State) string {
	switch s {
	case ipc.StateConnected:
		return "●"
	case ipc.StateDegraded:
		return "◐"
	case ipc.StateAuthenticating, ipc.StateConnecting:
		return "⋯"
	case ipc.StateAuthFailed, ipc.StateSessionRejected:
		return "✕"
	default:
		return "○"
	}
}

// humanState renders an ipc.State as friendly lower-case text for the menu.
func humanState(s ipc.State) string {
	switch s {
	case ipc.StateIdle:
		return "idle"
	case ipc.StateAuthenticating:
		return "authenticating"
	case ipc.StateAuthFailed:
		return "authentication failed"
	case ipc.StateConnecting:
		return "connecting"
	case ipc.StateConnected:
		return "connected"
	case ipc.StateDegraded:
		return "degraded"
	case ipc.StateSessionRejected:
		return "session rejected"
	case ipc.StateDisconnected:
		return "disconnected"
	case ipc.StateUnknown:
		return "unknown"
	default:
		if s == "" {
			return "unknown"
		}
		return string(s)
	}
}

// connectEnabled reports whether the Connect item should be clickable. Connect
// is disabled while a tunnel is already up or actively coming up.
func connectEnabled(s ipc.State) bool {
	switch s {
	case ipc.StateConnected, ipc.StateConnecting, ipc.StateAuthenticating, ipc.StateDegraded:
		return false
	default:
		return true
	}
}

// disconnectEnabled reports whether the Disconnect item should be clickable.
// Disconnect is disabled only when there is definitively nothing to tear down.
func disconnectEnabled(s ipc.State) bool {
	switch s {
	case ipc.StateIdle, ipc.StateDisconnected:
		return false
	default:
		return true
	}
}
