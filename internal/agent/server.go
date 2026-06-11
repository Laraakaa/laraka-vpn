package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Laraakaa/laraka-vpn/internal/ipc"
)

// DefaultAgentSocket returns the per-user CLI<->AGENT socket path. It lives
// under the user's home directory so it is naturally scoped to one user, and
// the server pins the parent directory's owner to the running UID (§4, §10a).
func DefaultAgentSocket() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".laraka-vpn", "agent.sock")
}

// Orchestrator is the subset of *Controller the agent IPC server drives. It is
// declared as an interface so the server can be unit-tested with a fake.
type Orchestrator interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Refresh(ctx context.Context) error
	State() ipc.State
	Message() string
}

// Server serves the per-user CLI<->AGENT socket, translating user intent
// (connect / disconnect / status) into Controller calls. It carries no
// credential material: this protocol never sees the cookie (§2, message.go).
type Server struct {
	ctrl       Orchestrator
	socketPath string
	uid        uint32
	baseCtx    context.Context
}

// NewServer returns a Server that will listen on socketPath and dispatch to
// ctrl. The socket is authorized to exactly the current UID.
func NewServer(ctrl Orchestrator, socketPath string) *Server {
	return &Server{
		ctrl:       ctrl,
		socketPath: socketPath,
		uid:        uint32(os.Getuid()),
	}
}

// Run binds the user socket and serves requests until ctx is cancelled. The
// socket directory is created 0700 and pinned to the running UID; only that
// same UID may connect (root is always rejected, per ipc.AllowUID).
func (s *Server) Run(ctx context.Context) error {
	s.baseCtx = ctx

	uid := s.uid
	srv, err := ipc.Listen(ipc.ServerConfig{
		SocketPath:         s.socketPath,
		DirMode:            0o700, // per-user private directory
		SocketMode:         0o600,
		RequireDirOwnerUID: &uid, // refuse if the dir is not ours
		Authorize:          ipc.AllowUID(uid),
	})
	if err != nil {
		return fmt.Errorf("agent: listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	return srv.Serve(s)
}

// Handle implements ipc.Handler. The server invokes it once per connection in
// its own goroutine and closes the conn when it returns.
func (s *Server) Handle(c *ipc.Conn, _ ipc.PeerCred) {
	req, err := c.ReadRequest()
	if err != nil {
		return
	}
	_ = c.WriteResponse(s.dispatch(req))
}

// dispatch maps a CLI request to a Controller action and a Response.
//
// Connect is fire-and-forget: the keychain-signing --authenticate phase can
// block on a Mobile ID push for far longer than a single framed round trip
// (ipc.DefaultDeadline), so the agent starts it asynchronously and the CLI
// observes progress via subsequent status calls. Disconnect and status are
// fast helper round trips and run synchronously.
func (s *Server) dispatch(req ipc.Request) ipc.Response {
	ctx := s.context()
	switch req.Command {
	case ipc.CmdConnect:
		go func() { _ = s.ctrl.Connect(ctx) }()
		return ipc.Response{State: s.ctrl.State(), Detail: "connect requested"}
	case ipc.CmdDisconnect:
		if err := s.ctrl.Disconnect(ctx); err != nil {
			return s.errorResponse(err)
		}
		return s.stateResponse()
	case ipc.CmdStatus:
		if err := s.ctrl.Refresh(ctx); err != nil {
			return s.errorResponse(err)
		}
		return s.stateResponse()
	default:
		return ipc.Response{State: s.ctrl.State(), Error: "agent: unknown command"}
	}
}

// stateResponse reports the controller's current state and message.
func (s *Server) stateResponse() ipc.Response {
	return ipc.Response{State: s.ctrl.State(), Detail: s.ctrl.Message()}
}

// errorResponse reports a failure while still surfacing the last-known state so
// the CLI can show something useful.
func (s *Server) errorResponse(err error) ipc.Response {
	return ipc.Response{State: s.ctrl.State(), Error: err.Error(), Detail: s.ctrl.Message()}
}

// context returns the server's base context, falling back to Background when
// the server is used without Run (e.g. in tests).
func (s *Server) context() context.Context {
	if s.baseCtx != nil {
		return s.baseCtx
	}
	return context.Background()
}
