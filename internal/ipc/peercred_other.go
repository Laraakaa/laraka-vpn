//go:build !darwin

package ipc

import (
	"errors"
	"net"
)

// PeerCred is the identity of the process on the other end of a Unix-domain
// socket connection. On non-darwin platforms this is a stub: laraka-vpn is a
// macOS product and peer-credential extraction is only implemented for darwin.
type PeerCred struct {
	UID uint32
	GID uint32
	PID int
}

// errUnsupported is returned by the non-darwin stub.
var errUnsupported = errors.New("ipc: peer credentials are only supported on darwin")

// peerCred is unimplemented off darwin so the package builds for tooling on
// other platforms; it always fails closed.
func peerCred(_ *net.UnixConn) (PeerCred, error) {
	return PeerCred{}, errUnsupported
}
