package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Laraakaa/laraka-vpn/internal/config"
	"github.com/Laraakaa/laraka-vpn/internal/ipc"
)

// DefaultHelperSocket is where the privileged helper listens. It mirrors the
// ipc.helper_socket default in config.LoadRootConfig; the agent only ever sends
// the helper an opaque cookie + host, never routing or cert authority (§2).
const DefaultHelperSocket = "/var/run/laraka-vpn/helper.sock"

const (
	defaultBackoffBase = 2 * time.Second
	defaultBackoffMax  = 2 * time.Minute
)

// Sentinel guards returned by beginAuth. They are internal: callers map them to
// either a no-op (already active / cooling down) or a surfaced error.
var (
	errAlreadyActive = errors.New("agent: already connected")
	errInProgress    = errors.New("agent: authentication already in progress")
	errCooldown      = errors.New("agent: in backoff cooldown")
	errTerminal      = errors.New("agent: authentication blocked pending keychain approval")
)

// authRunner is the subset of Authenticator the controller depends on (injected
// for tests).
type authRunner interface {
	Authenticate(ctx context.Context) (*AuthResult, error)
}

// tunnelClient is the subset of ipc.Client the controller depends on (injected
// for tests).
type tunnelClient interface {
	DoTunnel(req ipc.TunnelRequest) (ipc.TunnelResponse, error)
}

// Controller is the agent's 9-state machine (§7). It serializes a single
// authentication at a time, hands the cookie to the helper, and reconciles the
// agent's view with the helper-reported tunnel state. A keychain-authorization
// failure is terminal: autonomous reauth stops until the user reconnects
// manually (after approving the GUI prompt).
type Controller struct {
	auth   authRunner
	helper tunnelClient
	now    func() time.Time

	mu            sync.Mutex
	state         ipc.State
	message       string
	authInFlight  bool
	terminal      bool
	attempt       int
	nextAttemptAt time.Time
}

// NewController wires a Controller to the real authenticator and helper client.
func NewController(cfg *config.UserConfig) *Controller {
	return &Controller{
		auth:   NewAuthenticator(cfg),
		helper: ipc.NewClient(DefaultHelperSocket),
		now:    time.Now,
		state:  ipc.StateIdle,
	}
}

// State returns the current agent state.
func (c *Controller) State() ipc.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Message returns the latest human-readable status/guidance (may be empty).
func (c *Controller) Message() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.message
}

// Connect runs a user-initiated connect. Being manual, it bypasses the backoff
// cooldown and clears any terminal block (the user may have just approved the
// keychain prompt), but it still refuses to run two authentications at once.
func (c *Controller) Connect(ctx context.Context) error {
	if err := c.beginAuth(true); err != nil {
		if errors.Is(err, errAlreadyActive) {
			return nil
		}
		return err
	}
	return c.runAuth(ctx)
}

// Disconnect tears down the tunnel via the helper and resets retry/terminal
// state.
func (c *Controller) Disconnect(_ context.Context) error {
	resp, err := c.helper.DoTunnel(ipc.TunnelRequest{Command: ipc.CmdDisconnect})

	c.mu.Lock()
	defer c.mu.Unlock()
	c.attempt = 0
	c.nextAttemptAt = time.Time{}
	c.terminal = false
	if err != nil {
		c.message = fmt.Sprintf("helper unreachable: %v", err)
		return fmt.Errorf("agent: contacting helper: %w", err)
	}
	c.state = normalizeState(resp.State, ipc.StateDisconnected)
	c.message = ""
	return nil
}

// Refresh polls the helper for the tunnel state and reconciles. A transient
// Degraded is left to openconnect to self-heal; an unexpected drop becomes
// SessionRejected and, after a backoff window, triggers an autonomous reauth
// (unless blocked by a terminal keychain failure).
func (c *Controller) Refresh(ctx context.Context) error {
	resp, err := c.helper.DoTunnel(ipc.TunnelRequest{Command: ipc.CmdStatus})
	if err != nil {
		return fmt.Errorf("agent: helper status: %w", err)
	}

	c.mu.Lock()
	if c.authInFlight {
		c.mu.Unlock()
		return nil
	}
	prev := c.state
	next, action := reconcile(prev, normalizeState(resp.State, ipc.StateUnknown))
	c.state = next
	switch {
	case next == ipc.StateConnected:
		c.attempt = 0
		c.nextAttemptAt = time.Time{}
		c.message = ""
	case next == ipc.StateDisconnected:
		c.message = ""
	case action == actionReauth && prev != ipc.StateSessionRejected:
		// First observation of the drop: start the backoff clock.
		c.scheduleBackoffLocked()
		c.message = "session dropped; will re-authenticate"
	}
	readyToReauth := next == ipc.StateSessionRejected &&
		prev == ipc.StateSessionRejected &&
		!c.terminal &&
		!c.now().Before(c.nextAttemptAt)
	c.mu.Unlock()

	if readyToReauth {
		return c.reauth(ctx)
	}
	return nil
}

// reauth is the autonomous (non-manual) authentication path. It respects the
// cooldown and terminal guards; those guards collapse to a silent no-op.
func (c *Controller) reauth(ctx context.Context) error {
	if err := c.beginAuth(false); err != nil {
		if errors.Is(err, errAlreadyActive) || errors.Is(err, errInProgress) ||
			errors.Is(err, errCooldown) || errors.Is(err, errTerminal) {
			return nil
		}
		return err
	}
	return c.runAuth(ctx)
}

// beginAuth performs the guarded state transition into Authenticating. A manual
// attempt bypasses cooldown/terminal and resets the retry counter.
func (c *Controller) beginAuth(manual bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authInFlight {
		return errInProgress
	}
	if c.state == ipc.StateConnected || c.state == ipc.StateConnecting {
		return errAlreadyActive
	}
	if !manual {
		if c.terminal {
			return errTerminal
		}
		if c.now().Before(c.nextAttemptAt) {
			return errCooldown
		}
	}
	c.authInFlight = true
	c.state = ipc.StateAuthenticating
	c.message = ""
	if manual {
		c.terminal = false
		c.attempt = 0
		c.nextAttemptAt = time.Time{}
	}
	return nil
}

// runAuth executes the authentication and hands off to the helper. It always
// clears the in-flight flag.
func (c *Controller) runAuth(ctx context.Context) error {
	defer c.endAuth()
	result, err := c.auth.Authenticate(ctx)
	if err != nil {
		c.failAuth(err)
		return err
	}
	return c.handToHelper(result)
}

func (c *Controller) endAuth() {
	c.mu.Lock()
	c.authInFlight = false
	c.mu.Unlock()
}

// failAuth records an authentication failure. A keychain-authorization failure
// is terminal (stop autonomous retries, surface guidance); everything else
// schedules a bounded backoff.
func (c *Controller) failAuth(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = ipc.StateAuthFailed
	if errors.Is(err, ErrKeychainNotAuthorized) {
		c.terminal = true
		c.message = "Keychain access not authorized; open the app once from the GUI and approve."
		return
	}
	c.message = err.Error()
	c.scheduleBackoffLocked()
}

// handToHelper sends the opaque cookie to the helper, then wipes it. The cookie
// is never retained by the helper client (§6); the agent owns and zeroes it.
func (c *Controller) handToHelper(result *AuthResult) error {
	req := ipc.TunnelRequest{Command: ipc.CmdConnect, Cookie: result.Cookie, Host: result.Host}
	resp, err := c.helper.DoTunnel(req)
	req.Zero()
	result.Zero()

	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.state = ipc.StateDisconnected
		c.message = fmt.Sprintf("helper unreachable: %v", err)
		c.scheduleBackoffLocked()
		return fmt.Errorf("agent: contacting helper: %w", err)
	}
	if resp.Error != "" {
		c.state = normalizeState(resp.State, ipc.StateDisconnected)
		c.message = resp.Error
		c.scheduleBackoffLocked()
		return fmt.Errorf("agent: helper rejected tunnel: %s", resp.Error)
	}
	c.state = normalizeState(resp.State, ipc.StateConnecting)
	c.attempt = 0
	c.nextAttemptAt = time.Time{}
	c.message = ""
	return nil
}

func (c *Controller) scheduleBackoffLocked() {
	c.attempt++
	c.nextAttemptAt = c.now().Add(nextBackoff(c.attempt, defaultBackoffBase, defaultBackoffMax))
}

// reconcileAction is what Refresh should do after mapping the helper state.
type reconcileAction int

const (
	actionNone reconcileAction = iota
	actionReauth
)

// reconcile maps the helper-reported tunnel state to the agent's next state and
// the action to take. It is pure. An unexpected disconnect while the agent
// believed the tunnel was up becomes SessionRejected with a reauth action;
// Degraded is left untouched for openconnect to self-heal.
func reconcile(current, helper ipc.State) (ipc.State, reconcileAction) {
	switch helper {
	case ipc.StateConnected:
		return ipc.StateConnected, actionNone
	case ipc.StateDegraded:
		return ipc.StateDegraded, actionNone
	case ipc.StateConnecting:
		return ipc.StateConnecting, actionNone
	case ipc.StateDisconnected, ipc.StateIdle:
		if current == ipc.StateConnected || current == ipc.StateDegraded || current == ipc.StateConnecting {
			return ipc.StateSessionRejected, actionReauth
		}
		if current == ipc.StateSessionRejected {
			// Keep SessionRejected sticky so the reauth window can elapse.
			return ipc.StateSessionRejected, actionReauth
		}
		return ipc.StateDisconnected, actionNone
	default:
		return ipc.StateUnknown, actionNone
	}
}

// nextBackoff returns base*2^(attempt-1), capped at max. attempt is 1-based.
func nextBackoff(attempt int, base, max time.Duration) time.Duration {
	if attempt <= 1 {
		if base > max {
			return max
		}
		return base
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= max {
			return max
		}
	}
	return d
}

// normalizeState returns fallback when s is empty, else s.
func normalizeState(s, fallback ipc.State) ipc.State {
	if s == "" {
		return fallback
	}
	return s
}
