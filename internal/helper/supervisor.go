package helper

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Laraakaa/laraka-vpn/internal/config"
	"github.com/Laraakaa/laraka-vpn/internal/ipc"
)

// teardownGrace is how long the process group has to exit after SIGTERM before
// it is SIGKILLed.
const teardownGrace = 5 * time.Second

// Supervisor is the privileged HELPER. It owns at most one openconnect tunnel
// at a time, drives it from a cookie supplied over the privileged socket, and
// reports state. It never touches the keychain and never accepts routing or
// execution parameters from the agent: everything root executes comes from the
// root-owned config (§2, §10c).
type Supervisor struct {
	cfg *config.RootConfig

	mu         sync.Mutex
	cmd        *exec.Cmd
	pgid       int
	done       chan struct{} // closed by supervise() once the child is reaped
	state      ipc.State
	assignedIP string
	host       string
}

// NewSupervisor returns a Supervisor bound to the given root config.
func NewSupervisor(cfg *config.RootConfig) *Supervisor {
	return &Supervisor{cfg: cfg, state: ipc.StateIdle}
}

// Run reconciles any prior tunnel state, then listens on the privileged socket
// and serves tunnel requests until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) error {
	if err := s.reconcile(); err != nil {
		return err
	}

	zero := uint32(0)
	owner := s.cfg.AllowedUID
	srv, err := ipc.Listen(ipc.ServerConfig{
		SocketPath:         s.cfg.HelperSocket,
		DirMode:            0o755, // /var/run/laraka-vpn is root:wheel 0755 (§10a)
		SocketMode:         0o600,
		SocketOwnerUID:     &owner, // chown to agent uid so it can connect() (§4)
		RequireDirOwnerUID: &zero,  // pin the socket dir to root
		Authorize:          ipc.AllowUID(s.cfg.AllowedUID),
	})
	if err != nil {
		return fmt.Errorf("helper: listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	return srv.Serve(s)
}

// Handle implements ipc.Handler. The server invokes it once per connection in
// its own goroutine and closes the conn when it returns.
func (s *Supervisor) Handle(c *ipc.Conn, _ ipc.PeerCred) {
	req, err := c.ReadTunnelRequest()
	if err != nil {
		return
	}
	// Wipe the cookie's backing array once we are done with this request. The
	// cookie is consumed synchronously by connect (written to the child's
	// stdin) before this defer runs, so the helper never retains it (§6).
	defer req.Zero()

	var resp ipc.TunnelResponse
	switch req.Command {
	case ipc.CmdConnect:
		resp = s.connect(req.Cookie, req.Host)
	case ipc.CmdDisconnect:
		resp = s.disconnect()
	case ipc.CmdStatus:
		resp = s.status()
	default:
		resp = ipc.TunnelResponse{State: ipc.StateUnknown, Error: "helper: unknown command"}
	}
	_ = c.WriteTunnelResponse(resp)
}

// connect starts a single openconnect tunnel from the supplied cookie. It
// refuses to start a second tunnel and validates the host against the root
// allowlist before spawning anything.
func (s *Supervisor) connect(cookie []byte, host string) ipc.TunnelResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil {
		return ipc.TunnelResponse{State: s.state, Error: "helper: a tunnel is already active"}
	}

	// Validate the agent-supplied host against the root allowlist BEFORE we
	// spawn anything (§10b trust boundary).
	validatedHost, err := s.cfg.Allowlist.Validate(host)
	if err != nil {
		return ipc.TunnelResponse{State: ipc.StateDisconnected, Error: fmt.Sprintf("helper: host rejected: %v", err)}
	}

	// Absolute path only; no $PATH resolution (§10c). All argv comes from root
	// config. The cookie never appears in argv or env — only on stdin.
	cmd := exec.Command(
		s.cfg.OpenconnectPath,
		"--protocol=anyconnect",
		"--cookie-on-stdin",
		"--servercert="+s.cfg.ServerCert,
		"-s", s.cfg.SliceArg(),
		validatedHost,
	)
	// Own a fresh process group so we can signal the whole tree (openconnect
	// plus the vpn-slice child it spawns).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Minimal env: only enough PATH for openconnect to exec vpn-slice. Do not
	// inherit the parent (root) environment (§10c).
	cmd.Env = []string{"PATH=" + filepath.Dir(s.cfg.VPNSlicePath)}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ipc.TunnelResponse{State: ipc.StateDisconnected, Error: fmt.Sprintf("helper: stdin pipe: %v", err)}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ipc.TunnelResponse{State: ipc.StateDisconnected, Error: fmt.Sprintf("helper: stdout pipe: %v", err)}
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout for the scanner (organ A)

	if err := cmd.Start(); err != nil {
		return ipc.TunnelResponse{State: ipc.StateDisconnected, Error: fmt.Sprintf("helper: start openconnect: %v", err)}
	}

	// Feed the cookie, then close stdin immediately so openconnect proceeds and
	// nothing lingers in a pipe we hold.
	if _, werr := stdin.Write(cookie); werr != nil {
		_ = stdin.Close()
		pgid, _ := syscall.Getpgid(cmd.Process.Pid)
		terminateGroup(pgid, nil)
		_ = cmd.Wait()
		return ipc.TunnelResponse{State: ipc.StateDisconnected, Error: fmt.Sprintf("helper: write cookie: %v", werr)}
	}
	_ = stdin.Close()

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		pgid = cmd.Process.Pid
	}

	done := make(chan struct{})
	s.cmd = cmd
	s.pgid = pgid
	s.done = done
	s.host = validatedHost
	s.assignedIP = ""
	s.state = ipc.StateConnecting
	s.writeStateLocked()

	go s.supervise(cmd, stdout, done)

	return ipc.TunnelResponse{State: ipc.StateConnecting, Detail: validatedHost}
}

// supervise scans the child's merged output for state transitions and reaps the
// process when it exits. It is the sole caller of cmd.Wait for this child.
func (s *Supervisor) supervise(cmd *exec.Cmd, stdout io.Reader, done chan struct{}) {
	scanOutput(stdout, func(ev scanEvent) {
		s.mu.Lock()
		if s.cmd == cmd { // ignore output from a superseded tunnel
			s.state = ev.State
			if ev.State == ipc.StateConnected {
				s.assignedIP = ev.Detail
			}
			s.writeStateLocked()
		}
		s.mu.Unlock()
	})

	// stdout closed => the child is exiting. Reap it, then signal the escalation
	// timer (if any) that there is nothing left to kill.
	_ = cmd.Wait()
	close(done)

	s.mu.Lock()
	if s.cmd == cmd {
		s.cmd = nil
		s.pgid = 0
		s.done = nil
		s.assignedIP = ""
		s.host = ""
		s.state = ipc.StateDisconnected
		s.clearStateLocked()
	}
	s.mu.Unlock()
}

// disconnect tears down the active tunnel. It signals the process group and
// returns immediately; supervise performs the actual reap.
func (s *Supervisor) disconnect() ipc.TunnelResponse {
	s.mu.Lock()
	if s.cmd == nil {
		s.state = ipc.StateDisconnected
		s.mu.Unlock()
		return ipc.TunnelResponse{State: ipc.StateDisconnected}
	}
	pgid := s.pgid
	done := s.done
	s.state = ipc.StateDisconnected
	s.mu.Unlock()

	terminateGroup(pgid, done)
	return ipc.TunnelResponse{State: ipc.StateDisconnected}
}

// status reports the current tunnel state.
func (s *Supervisor) status() ipc.TunnelResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ipc.TunnelResponse{State: s.state, Detail: s.assignedIP}
}

// terminateGroup SIGTERMs the process group, then escalates to SIGKILL after
// teardownGrace unless done is closed first (meaning the child was already
// reaped). Guarding on done avoids signalling a reused process group id.
func terminateGroup(pgid int, done <-chan struct{}) {
	if pgid <= 0 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	go func() {
		if done == nil {
			time.Sleep(teardownGrace)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			return
		}
		select {
		case <-done:
		case <-time.After(teardownGrace):
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}()
}
