package ipc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// ErrNotAuthorized is returned by an Authorize policy when a connected peer is
// denied. The server closes denied connections without replying (fail closed,
// plan §4).
var ErrNotAuthorized = errors.New("ipc: peer not authorized")

// maxSocketPath is the conservative sun_path limit on macOS (104 incl. NUL).
const maxSocketPath = 103

// Handler processes one accepted, authorized connection. The server closes the
// Conn after Handle returns; the handler must not retain it.
type Handler interface {
	Handle(*Conn, PeerCred)
}

// HandlerFunc adapts an ordinary function to a Handler.
type HandlerFunc func(*Conn, PeerCred)

// Handle calls f.
func (f HandlerFunc) Handle(c *Conn, p PeerCred) { f(c, p) }

// ServerConfig configures a Unix-domain socket server. The same type backs
// both the per-user CLI<->AGENT socket and the root-owned AGENT<->HELPER
// socket; only the paths, modes and Authorize policy differ.
type ServerConfig struct {
	// SocketPath is the absolute path of the socket to create.
	SocketPath string
	// DirMode is the permission mode enforced on the parent directory
	// (e.g. 0o755 for the root-owned privileged dir, 0o700 for a per-user dir).
	// Defaults to 0o700 when zero.
	DirMode os.FileMode
	// SocketMode is the permission mode enforced on the socket inode.
	// Defaults to 0o600 when zero.
	SocketMode os.FileMode
	// RequireDirOwnerUID, when non-nil, requires the parent directory to be
	// owned by this UID; the server refuses to start otherwise. Use 0 to pin
	// the privileged directory to root.
	RequireDirOwnerUID *uint32
	// Authorize decides whether an accepted peer may proceed. Required.
	Authorize func(PeerCred) error
}

// Server is a running Unix-domain socket server.
type Server struct {
	cfg  ServerConfig
	ln   *net.UnixListener
	lock *os.File
}

// Listen prepares the parent directory, takes a singleton lock, removes any
// stale socket, and binds the socket with the configured permissions. The
// returned Server is ready for Serve.
func Listen(cfg ServerConfig) (*Server, error) {
	if cfg.Authorize == nil {
		return nil, errors.New("ipc: ServerConfig.Authorize is required")
	}
	if !filepath.IsAbs(cfg.SocketPath) {
		return nil, fmt.Errorf("ipc: socket path must be absolute: %q", cfg.SocketPath)
	}
	if len(cfg.SocketPath) > maxSocketPath {
		return nil, fmt.Errorf("ipc: socket path too long (%d > %d): %q",
			len(cfg.SocketPath), maxSocketPath, cfg.SocketPath)
	}
	if cfg.DirMode == 0 {
		cfg.DirMode = 0o700
	}
	if cfg.SocketMode == 0 {
		cfg.SocketMode = 0o600
	}

	dir := filepath.Dir(cfg.SocketPath)
	if err := secureDir(dir, cfg.DirMode, cfg.RequireDirOwnerUID); err != nil {
		return nil, err
	}

	// Singleton lock: a held flock means another instance owns this socket.
	// Holding it also lets us treat any leftover socket as definitively stale.
	lock, err := acquireLock(cfg.SocketPath + ".lock")
	if err != nil {
		return nil, err
	}
	if err := removeStaleSocket(cfg.SocketPath); err != nil {
		_ = lock.Close()
		return nil, err
	}
	ln, err := listenUnix(cfg.SocketPath, cfg.SocketMode)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	return &Server{cfg: cfg, ln: ln, lock: lock}, nil
}

// Serve accepts connections until Close is called, dispatching each authorized
// peer to h. Unauthorized peers are closed without a reply.
func (s *Server) Serve(h Handler) error {
	for {
		uc, err := s.ln.AcceptUnix()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.serveConn(uc, h)
	}
}

func (s *Server) serveConn(uc *net.UnixConn, h Handler) {
	defer func() { _ = uc.Close() }()
	cred, err := peerCred(uc)
	if err != nil {
		return
	}
	if err := s.cfg.Authorize(cred); err != nil {
		return
	}
	h.Handle(NewConn(uc), cred)
}

// Close stops accepting, releases the singleton lock, and removes the socket.
func (s *Server) Close() error {
	err := s.ln.Close()
	if s.lock != nil {
		_ = s.lock.Close() // releases the flock
	}
	_ = os.Remove(s.cfg.SocketPath)
	return err
}

// AllowUID returns an Authorize policy that permits exactly one UID and always
// rejects root (uid 0), per plan §4. The kernel-reported peer UID cannot be
// forged by a local process, so this is the primary authorization gate.
func AllowUID(allowed uint32) func(PeerCred) error {
	return func(p PeerCred) error {
		if p.UID == 0 {
			return fmt.Errorf("%w: root (uid 0) is never permitted", ErrNotAuthorized)
		}
		if p.UID != allowed {
			return fmt.Errorf("%w: uid %d (want %d)", ErrNotAuthorized, p.UID, allowed)
		}
		return nil
	}
}

// secureDir creates dir (if needed), asserts its mode, and validates that it is
// a real directory (not a symlink) optionally owned by a required UID.
func secureDir(dir string, mode os.FileMode, requireOwner *uint32) error {
	if err := os.MkdirAll(dir, mode); err != nil {
		return fmt.Errorf("ipc: mkdir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		return fmt.Errorf("ipc: chmod %s: %w", dir, err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("ipc: lstat %s: %w", dir, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("ipc: %s is a symlink", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("ipc: %s is not a directory", dir)
	}
	if requireOwner != nil {
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("ipc: cannot read owner of %s", dir)
		}
		if st.Uid != *requireOwner {
			return fmt.Errorf("ipc: %s owned by uid %d, want %d", dir, st.Uid, *requireOwner)
		}
	}
	return nil
}

// acquireLock opens (creating if needed) a lock file and takes a non-blocking
// exclusive flock. The returned file must stay open for the server's lifetime;
// closing it releases the lock.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("ipc: open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("ipc: another instance holds %s: %w", path, err)
	}
	return f, nil
}

// removeStaleSocket removes a leftover socket at path. It refuses to remove a
// symlink or any non-socket file, so a planted symlink cannot redirect the
// removal elsewhere.
func removeStaleSocket(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("ipc: lstat %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("ipc: refusing to remove symlink at socket path %s", path)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("ipc: refusing to remove non-socket at socket path %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("ipc: remove stale socket %s: %w", path, err)
	}
	return nil
}

// listenUnix binds a Unix socket at path with mode. umask narrows the inode at
// creation time; the explicit chmod is authoritative and closes the brief
// window between bind and chmod.
func listenUnix(path string, mode os.FileMode) (*net.UnixListener, error) {
	umask := (^int(mode)) & 0o777
	old := syscall.Umask(umask)
	defer syscall.Umask(old)

	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("ipc: listen %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("ipc: chmod socket %s: %w", path, err)
	}
	return ln, nil
}
