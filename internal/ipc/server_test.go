package ipc

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestAllowUID(t *testing.T) {
	const allowed = uint32(501)
	authz := AllowUID(allowed)

	t.Run("root_always_rejected", func(t *testing.T) {
		err := authz(PeerCred{UID: 0})
		if !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("uid0 err = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("wrong_uid_rejected", func(t *testing.T) {
		err := authz(PeerCred{UID: allowed + 1})
		if !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("wrong uid err = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("correct_uid_allowed", func(t *testing.T) {
		if err := authz(PeerCred{UID: allowed}); err != nil {
			t.Fatalf("correct uid err = %v, want nil", err)
		}
	})

	t.Run("root_rejected_even_if_allowed_is_root", func(t *testing.T) {
		// Defense in depth: configuring allowed=0 must still reject uid0.
		if err := AllowUID(0)(PeerCred{UID: 0}); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("uid0 with allowed=0 err = %v, want ErrNotAuthorized", err)
		}
	})
}

// shortSocketPath returns a socket path under a short temp dir, falling back to
// /tmp when the default t.TempDir() (under /var/folders/...) would exceed the
// 103-byte sun_path limit on macOS.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.sock")
	if len(p) <= maxSocketPath {
		return p
	}
	dir, err := os.MkdirTemp("/tmp", "ipc")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

// TestServerClientRoundTrip exercises the real socket path: a server that
// authorizes the current uid, a handler that echoes a Response, and a client
// that completes a Do round-trip.
func TestServerClientRoundTrip(t *testing.T) {
	sock := shortSocketPath(t)
	srv, err := Listen(ServerConfig{
		SocketPath: sock,
		Authorize:  AllowUID(uint32(os.Getuid())),
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Serve(HandlerFunc(func(c *Conn, _ PeerCred) {
			req, err := c.ReadRequest()
			if err != nil {
				return
			}
			_ = c.WriteResponse(Response{State: StateConnected, Detail: string(req.Command)})
		}))
	}()

	cli := NewClient(sock)
	resp, err := cli.Do(Request{Command: CmdStatus})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.State != StateConnected || resp.Detail != string(CmdStatus) {
		t.Fatalf("resp = %+v", resp)
	}

	srv.Close()
	wg.Wait()
}

// TestSocketOwnerUIDChowns verifies that a non-nil SocketOwnerUID chowns the
// socket inode. Chowning to a different uid needs root, so when running as the
// current uid we assert the no-op case; the cross-uid case is root-only.
func TestSocketOwnerUIDChowns(t *testing.T) {
	sock := shortSocketPath(t)
	want := uint32(os.Getuid())
	srv, err := Listen(ServerConfig{
		SocketPath:     sock,
		SocketOwnerUID: &want,
		Authorize:      AllowUID(want),
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	var st syscall.Stat_t
	if err := syscall.Stat(sock, &st); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Uid != want {
		t.Fatalf("socket uid = %d, want %d", st.Uid, want)
	}
}

// TestServerAuthorizeDenialClosesConn confirms that when Authorize denies a
// peer, the server closes the connection without serving it, so the client's
// read fails.
func TestServerAuthorizeDenialClosesConn(t *testing.T) {
	sock := shortSocketPath(t)
	srv, err := Listen(ServerConfig{
		SocketPath: sock,
		Authorize:  func(PeerCred) error { return ErrNotAuthorized },
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Serve(HandlerFunc(func(c *Conn, _ PeerCred) {
			// Should never be reached for a denied peer.
			_ = c.WriteResponse(Response{State: StateConnected})
		}))
	}()

	cli := NewClient(sock)
	if _, err := cli.Do(Request{Command: CmdStatus}); err == nil {
		t.Fatal("Do succeeded, want error from closed connection")
	}

	srv.Close()
	wg.Wait()
}
