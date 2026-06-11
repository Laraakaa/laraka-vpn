// Package ipc implements the local Unix-domain socket transport that connects
// the three laraka-vpn roles:
//
//	CLI  <-- user socket -->  AGENT  <-- privileged socket -->  HELPER
//
// Two protocols share one framing layer (length-capped NDJSON, see conn.go):
//
//   - CLI <-> AGENT carries user intent only (connect / disconnect / status).
//     See Request / Response.
//   - AGENT <-> HELPER (the trust boundary) carries ONLY an opaque cookie
//     (bytes) plus an allowlist-validated host. See TunnelRequest /
//     TunnelResponse. No certificate, key, route list, script, env or argv
//     ever crosses this boundary.
//
// Cookie hygiene (plan §6): the cookie is carried as []byte, never as a
// long-lived string, must never be logged, and should be zeroed by the holder
// as soon as it has been handed to the openconnect child. Callers MUST NOT log
// raw frames.
package ipc

// Command is the verb of a request on either protocol.
type Command string

const (
	// CmdConnect requests that a tunnel be established.
	CmdConnect Command = "connect"
	// CmdDisconnect requests that the active tunnel be torn down.
	CmdDisconnect Command = "disconnect"
	// CmdStatus requests the current state without changing it.
	CmdStatus Command = "status"
)

// Valid reports whether c is a known command.
func (c Command) Valid() bool {
	switch c {
	case CmdConnect, CmdDisconnect, CmdStatus:
		return true
	default:
		return false
	}
}

// State is the shared VPN state vocabulary (plan §7). The agent owns the
// authoritative state machine; the helper and CLI use these names to report
// and display state so all three roles agree on terminology.
type State string

const (
	// StateIdle: no tunnel, no cookie held.
	StateIdle State = "idle"
	// StateAuthenticating: agent is running `openconnect --authenticate`.
	StateAuthenticating State = "authenticating"
	// StateAuthFailed: authentication failed; no helper connect attempted.
	StateAuthFailed State = "auth_failed"
	// StateConnecting: helper accepted a cookie and spawned openconnect.
	StateConnecting State = "connecting"
	// StateConnected: tunnel established (helper observed success).
	StateConnected State = "connected"
	// StateDegraded: openconnect still alive but reconnecting; let it self-heal.
	StateDegraded State = "degraded"
	// StateSessionRejected: cookie/session rejected; agent must reauth.
	StateSessionRejected State = "session_rejected"
	// StateDisconnected: requested stop or child exited cleanly.
	StateDisconnected State = "disconnected"
	// StateUnknown: helper restarted or lost child state; reconcile required.
	StateUnknown State = "unknown"
)

// Request is a CLI -> AGENT message. It carries user intent only; it never
// carries any credential material.
type Request struct {
	Command Command `json:"command"`
}

// Response is an AGENT -> CLI message reporting the current state.
type Response struct {
	State  State  `json:"state"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// TunnelRequest is an AGENT -> HELPER message. This is the trust boundary
// payload. For CmdConnect it carries the opaque session cookie and the
// already-validated gateway host; for other commands those fields are empty.
//
// The cookie is []byte (not string) so the holder can zero it after use; call
// Zero once the cookie has been written to the openconnect child's stdin.
type TunnelRequest struct {
	Command Command `json:"command"`
	Cookie  []byte  `json:"cookie,omitempty"`
	Host    string  `json:"host,omitempty"`
}

// Zero best-effort wipes the cookie bytes in place. It is safe to call on a
// request whose cookie is nil or already zeroed.
func (r *TunnelRequest) Zero() {
	for i := range r.Cookie {
		r.Cookie[i] = 0
	}
	r.Cookie = nil
}

// TunnelResponse is a HELPER -> AGENT message reporting tunnel state and, on
// failure, a classified reason the agent's state machine can act on.
type TunnelResponse struct {
	State  State  `json:"state"`
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}
