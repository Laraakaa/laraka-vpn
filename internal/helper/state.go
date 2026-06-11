package helper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Laraakaa/laraka-vpn/internal/ipc"
)

// stateFileName is the root-owned file (alongside the socket) that records the
// currently supervised tunnel so a restarted helper can reconcile rather than
// spawn a second tunnel (§8).
const stateFileName = "state.json"

// persistedState is the on-disk reconciliation record. It is written 0600 and
// owned by root.
type persistedState struct {
	PID       int       `json:"pid"`
	StartTime time.Time `json:"start_time"`
	Exe       string    `json:"exe"`
	State     ipc.State `json:"state"`
	Host      string    `json:"host,omitempty"`
}

// stateFilePath is the path to the reconciliation record, kept in the same
// root-owned directory as the privileged socket.
func (s *Supervisor) stateFilePath() string {
	return filepath.Join(filepath.Dir(s.cfg.HelperSocket), stateFileName)
}

// writeStateLocked persists the current tunnel record. Caller must hold s.mu.
// Failures are non-fatal to the live tunnel but are surfaced to stderr by the
// caller's logging; here we simply best-effort write.
func (s *Supervisor) writeStateLocked() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	rec := persistedState{
		PID:       s.cmd.Process.Pid,
		StartTime: time.Now(),
		Exe:       s.cfg.OpenconnectPath,
		State:     s.state,
		Host:      s.host,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	// Write to a temp file in the same dir then rename for atomicity; ensure
	// 0600 root-owned.
	tmp := s.stateFilePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.stateFilePath())
}

// clearStateLocked removes the reconciliation record. Caller must hold s.mu.
func (s *Supervisor) clearStateLocked() {
	_ = os.Remove(s.stateFilePath())
}

// reconcile inspects any persisted state at startup and fails closed if a
// previously supervised tunnel is still alive. A helper that restarted while a
// tunnel was running must not spawn a competing tunnel; the operator (or
// launchd) should let the existing one continue or kill it explicitly (§8).
func (s *Supervisor) reconcile() error {
	path := s.stateFilePath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // clean start
		}
		return fmt.Errorf("helper: reading state file %s: %w", path, err)
	}

	var rec persistedState
	if err := json.Unmarshal(b, &rec); err != nil {
		// Corrupt record: remove it and start clean rather than wedging.
		_ = os.Remove(path)
		return nil
	}

	if rec.PID > 0 && processAlive(rec.PID) {
		// Fail closed: something is still running under the recorded PID.
		return fmt.Errorf(
			"helper: refusing to start; a prior tunnel (pid %d, host %q) appears to still be running — kill it or remove %s",
			rec.PID, rec.Host, path,
		)
	}

	// Recorded process is gone; clear the stale record and start clean.
	_ = os.Remove(path)
	return nil
}

// processAlive reports whether a process with the given pid currently exists.
// Signal 0 performs error checking without sending a signal: nil means the
// process exists, ESRCH means it does not, EPERM means it exists but is owned
// by another user (still "alive" for our purposes).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}
