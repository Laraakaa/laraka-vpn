//go:build darwin

package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// PeerCred is the identity of the process on the other end of a Unix-domain
// socket connection, as reported by the kernel.
type PeerCred struct {
	UID uint32
	GID uint32
	PID int
}

// peerCred extracts the peer's credentials from a *net.UnixConn using the
// macOS LOCAL_PEERCRED / LOCAL_PEERPID socket options. This is the Go
// equivalent of getpeereid(2): the kernel reports the credentials of the peer
// at connect time, which a local process cannot forge.
//
// PID is best-effort and inherently race-prone (PIDs are reused); it is only
// used as input to an optional code-signature check, never as the sole basis
// for authorization (plan §4).
func peerCred(uc *net.UnixConn) (PeerCred, error) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return PeerCred{}, fmt.Errorf("ipc: syscallconn: %w", err)
	}

	var (
		cred   PeerCred
		xucred *unix.Xucred
		xErr   error
		pid    int
		pidErr error
	)
	ctrlErr := raw.Control(func(fd uintptr) {
		xucred, xErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		pid, pidErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	})
	if ctrlErr != nil {
		return PeerCred{}, fmt.Errorf("ipc: control: %w", ctrlErr)
	}
	if xErr != nil {
		return PeerCred{}, fmt.Errorf("ipc: LOCAL_PEERCRED: %w", xErr)
	}
	cred.UID = xucred.Uid
	if xucred.Ngroups > 0 {
		cred.GID = xucred.Groups[0]
	}
	// A missing PID is non-fatal: the UID gate stands on its own.
	if pidErr == nil {
		cred.PID = pid
	}
	return cred, nil
}
