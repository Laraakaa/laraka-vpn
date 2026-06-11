package ipc

import (
	"fmt"
	"net"
)

// Client dials a laraka-vpn Unix-domain socket and performs one framed
// request/response round trip per call. It is the replacement for the former
// ZeroMQ daemon client; each call uses a fresh short-lived connection, which
// keeps the protocol stateless and avoids a long-lived shared socket.
type Client struct {
	path string
}

// NewClient returns a Client for the socket at path.
func NewClient(path string) *Client {
	return &Client{path: path}
}

// dial opens a framed connection to the server with a bounded timeout.
func (c *Client) dial() (*Conn, error) {
	d := net.Dialer{Timeout: DefaultDeadline}
	conn, err := d.Dial("unix", c.path)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial %s: %w", c.path, err)
	}
	return NewConn(conn), nil
}

// Do performs one CLI -> AGENT request/response round trip.
func (c *Client) Do(req Request) (Response, error) {
	conn, err := c.dial()
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.WriteRequest(req); err != nil {
		return Response{}, err
	}
	return conn.ReadResponse()
}

// DoTunnel performs one AGENT -> HELPER request/response round trip across the
// trust boundary.
//
// Cookie ownership: the caller owns req.Cookie and is responsible for zeroing
// it (req.Zero) once the round trip completes; this method does not retain or
// wipe the cookie so the caller can decide the exact lifetime (plan §6).
func (c *Client) DoTunnel(req TunnelRequest) (TunnelResponse, error) {
	conn, err := c.dial()
	if err != nil {
		return TunnelResponse{}, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.WriteTunnelRequest(req); err != nil {
		return TunnelResponse{}, err
	}
	return conn.ReadTunnelResponse()
}
