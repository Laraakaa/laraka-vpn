package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Laraakaa/laraka-vpn/internal/ipc"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// fakeOrchestrator implements Orchestrator for server dispatch tests.
type fakeOrchestrator struct {
	mu          sync.Mutex
	state       ipc.State
	message     string
	connectErr  error
	disconnErr  error
	refreshErr  error
	connects    int
	disconnects int
	refreshes   int
	connectWait chan struct{} // if non-nil, Connect blocks until closed
}

func (f *fakeOrchestrator) Connect(context.Context) error {
	f.mu.Lock()
	f.connects++
	wait := f.connectWait
	err := f.connectErr
	f.mu.Unlock()
	if wait != nil {
		<-wait
	}
	return err
}

func (f *fakeOrchestrator) Disconnect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnects++
	return f.disconnErr
}

func (f *fakeOrchestrator) Refresh(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	return f.refreshErr
}

func (f *fakeOrchestrator) State() ipc.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeOrchestrator) Message() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.message
}

func (f *fakeOrchestrator) counts() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects, f.disconnects, f.refreshes
}

func newTestServer(o *fakeOrchestrator) *Server {
	return &Server{ctrl: o, socketPath: "/unused/in/dispatch/tests.sock", uid: 1000}
}

func TestDispatchConnectIsAsync(t *testing.T) {
	o := &fakeOrchestrator{state: ipc.StateIdle, connectWait: make(chan struct{})}
	s := newTestServer(o)

	// Connect blocks in the orchestrator; dispatch must still return promptly.
	resp := s.dispatch(ipc.Request{Command: ipc.CmdConnect})
	if resp.Error != "" {
		t.Fatalf("connect dispatch returned error: %q", resp.Error)
	}
	if resp.State != ipc.StateIdle {
		t.Fatalf("state = %q, want idle", resp.State)
	}
	if resp.Detail != "connect requested" {
		t.Fatalf("detail = %q, want %q", resp.Detail, "connect requested")
	}
	// Let the background Connect finish.
	close(o.connectWait)
	// Drain: give the goroutine a chance and assert it ran exactly once.
	waitFor(t, func() bool {
		c, _, _ := o.counts()
		return c == 1
	})
}

func TestDispatchDisconnectSync(t *testing.T) {
	o := &fakeOrchestrator{state: ipc.StateConnected}
	s := newTestServer(o)

	resp := s.dispatch(ipc.Request{Command: ipc.CmdDisconnect})
	if _, d, _ := o.counts(); d != 1 {
		t.Fatalf("disconnects = %d, want 1", d)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}

func TestDispatchDisconnectError(t *testing.T) {
	o := &fakeOrchestrator{state: ipc.StateConnected, disconnErr: context.DeadlineExceeded}
	s := newTestServer(o)

	resp := s.dispatch(ipc.Request{Command: ipc.CmdDisconnect})
	if resp.Error == "" {
		t.Fatal("expected error in response")
	}
	if resp.State != ipc.StateConnected {
		t.Fatalf("state = %q, want last-known connected", resp.State)
	}
}

func TestDispatchStatusRefreshes(t *testing.T) {
	o := &fakeOrchestrator{state: ipc.StateConnected, message: "all good"}
	s := newTestServer(o)

	resp := s.dispatch(ipc.Request{Command: ipc.CmdStatus})
	if _, _, r := o.counts(); r != 1 {
		t.Fatalf("refreshes = %d, want 1", r)
	}
	if resp.State != ipc.StateConnected {
		t.Fatalf("state = %q, want connected", resp.State)
	}
	if resp.Detail != "all good" {
		t.Fatalf("detail = %q, want message passthrough", resp.Detail)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	o := &fakeOrchestrator{state: ipc.StateIdle}
	s := newTestServer(o)

	resp := s.dispatch(ipc.Request{Command: ipc.Command("bogus")})
	if resp.Error == "" {
		t.Fatal("expected error for unknown command")
	}
	if c, d, r := o.counts(); c != 0 || d != 0 || r != 0 {
		t.Fatalf("unknown command should not invoke controller, got %d/%d/%d", c, d, r)
	}
}

func TestServeRoundTrip(t *testing.T) {
	// End-to-end over a real Unix socket: Listen, Serve, dial with ipc.Client.
	dir := t.TempDir()
	sockPath := dir + "/agent.sock"
	o := &fakeOrchestrator{state: ipc.StateConnected, message: "up"}
	uid := uint32(os.Getuid())
	srv := &Server{ctrl: o, socketPath: sockPath, uid: uid}

	ln, err := ipc.Listen(ipc.ServerConfig{
		SocketPath: sockPath,
		DirMode:    0o700,
		SocketMode: 0o600,
		Authorize:  ipc.AllowUID(uid),
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = ln.Serve(srv) }()

	client := ipc.NewClient(sockPath)
	resp, err := client.Do(ipc.Request{Command: ipc.CmdStatus})
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	if resp.State != ipc.StateConnected {
		t.Fatalf("state = %q, want connected", resp.State)
	}
	if resp.Detail != "up" {
		t.Fatalf("detail = %q, want %q", resp.Detail, "up")
	}
}

func TestDefaultAgentSocketAbsolute(t *testing.T) {
	p := DefaultAgentSocket()
	if p == "" {
		t.Fatal("DefaultAgentSocket returned empty")
	}
	if !filepath.IsAbs(p) {
		t.Fatalf("DefaultAgentSocket = %q, want absolute", p)
	}
}
