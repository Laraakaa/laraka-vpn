package ipc

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeAddr is a no-op net.Addr for fakeConn.
type fakeAddr struct{}

func (fakeAddr) Network() string { return "fake" }
func (fakeAddr) String() string  { return "fake" }

// fakeConn is a minimal net.Conn backed by an io.Reader/Writer with no-op
// deadlines, used to drive the framing layer deterministically without a real
// socket or the synchronous blocking of net.Pipe.
type fakeConn struct {
	r *bytes.Reader
	w *bytes.Buffer
}

func (f *fakeConn) Read(b []byte) (int, error) {
	if f.r == nil {
		return 0, nil
	}
	return f.r.Read(b)
}

func (f *fakeConn) Write(b []byte) (int, error) {
	if f.w == nil {
		return len(b), nil
	}
	return f.w.Write(b)
}

func (f *fakeConn) Close() error                       { return nil }
func (f *fakeConn) LocalAddr() net.Addr                { return fakeAddr{} }
func (f *fakeConn) RemoteAddr() net.Addr               { return fakeAddr{} }
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

// TestRoundTripTypedMessages verifies every typed wrapper encodes and decodes
// across a net.Pipe, including a binary cookie that must survive intact.
func TestRoundTripTypedMessages(t *testing.T) {
	t.Run("request", func(t *testing.T) {
		c1, c2 := net.Pipe()
		defer c1.Close()
		defer c2.Close()
		a, b := NewConn(c1), NewConn(c2)
		errc := make(chan error, 1)
		go func() { errc <- a.WriteRequest(Request{Command: CmdConnect}) }()
		got, err := b.ReadRequest()
		if err != nil {
			t.Fatalf("ReadRequest: %v", err)
		}
		if err := <-errc; err != nil {
			t.Fatalf("WriteRequest: %v", err)
		}
		if got.Command != CmdConnect {
			t.Fatalf("command = %q, want %q", got.Command, CmdConnect)
		}
	})

	t.Run("response", func(t *testing.T) {
		c1, c2 := net.Pipe()
		defer c1.Close()
		defer c2.Close()
		a, b := NewConn(c1), NewConn(c2)
		want := Response{State: StateConnected, Error: "", Detail: "1.2.3.4"}
		errc := make(chan error, 1)
		go func() { errc <- a.WriteResponse(want) }()
		got, err := b.ReadResponse()
		if err != nil {
			t.Fatalf("ReadResponse: %v", err)
		}
		if err := <-errc; err != nil {
			t.Fatalf("WriteResponse: %v", err)
		}
		if got != want {
			t.Fatalf("response = %+v, want %+v", got, want)
		}
	})

	t.Run("tunnel_request_cookie", func(t *testing.T) {
		c1, c2 := net.Pipe()
		defer c1.Close()
		defer c2.Close()
		a, b := NewConn(c1), NewConn(c2)
		cookie := []byte{0x00, 0x01, 0xfe, 0xff, 'A', '\n', 'B'}
		want := TunnelRequest{Command: CmdConnect, Cookie: cookie, Host: "vpn.example.com"}
		errc := make(chan error, 1)
		go func() { errc <- a.WriteTunnelRequest(want) }()
		got, err := b.ReadTunnelRequest()
		if err != nil {
			t.Fatalf("ReadTunnelRequest: %v", err)
		}
		if err := <-errc; err != nil {
			t.Fatalf("WriteTunnelRequest: %v", err)
		}
		if got.Command != CmdConnect || got.Host != "vpn.example.com" {
			t.Fatalf("got %+v", got)
		}
		if !bytes.Equal(got.Cookie, cookie) {
			t.Fatalf("cookie = %v, want %v", got.Cookie, cookie)
		}
	})

	t.Run("tunnel_response", func(t *testing.T) {
		c1, c2 := net.Pipe()
		defer c1.Close()
		defer c2.Close()
		a, b := NewConn(c1), NewConn(c2)
		want := TunnelResponse{State: StateDegraded, Detail: "reconnecting"}
		errc := make(chan error, 1)
		go func() { errc <- a.WriteTunnelResponse(want) }()
		got, err := b.ReadTunnelResponse()
		if err != nil {
			t.Fatalf("ReadTunnelResponse: %v", err)
		}
		if err := <-errc; err != nil {
			t.Fatalf("WriteTunnelResponse: %v", err)
		}
		if got != want {
			t.Fatalf("tunnel response = %+v, want %+v", got, want)
		}
	})
}

// TestWriteFrameTooLarge ensures the writer rejects an oversized frame before
// touching the wire.
func TestWriteFrameTooLarge(t *testing.T) {
	fc := &fakeConn{w: &bytes.Buffer{}}
	conn := NewConn(fc)
	huge := Response{State: StateConnected, Detail: strings.Repeat("x", MaxFrameBytes)}
	if err := conn.WriteResponse(huge); err != ErrFrameTooLarge {
		t.Fatalf("WriteResponse err = %v, want ErrFrameTooLarge", err)
	}
	if fc.w.Len() != 0 {
		t.Fatalf("wrote %d bytes to wire, want 0", fc.w.Len())
	}
}

// TestReadFrameTooLarge ensures the reader rejects an oversized inbound frame
// without buffering past the cap. The stream is a single line longer than the
// cap with no early newline.
func TestReadFrameTooLarge(t *testing.T) {
	payload := append(bytes.Repeat([]byte("a"), MaxFrameBytes+10), '\n')
	fc := &fakeConn{r: bytes.NewReader(payload)}
	conn := NewConn(fc)
	if _, err := conn.ReadResponse(); err != ErrFrameTooLarge {
		t.Fatalf("ReadResponse err = %v, want ErrFrameTooLarge", err)
	}
}

// TestReadFrameAtCap confirms a frame just under the cap still decodes, so the
// boundary check is not off-by-one in the rejecting direction.
func TestReadFrameAtCap(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	a, b := NewConn(c1), NewConn(c2)
	// Build a Response whose JSON encoding is comfortably under MaxFrameBytes.
	detail := strings.Repeat("y", MaxFrameBytes-1024)
	want := Response{State: StateConnected, Detail: detail}
	errc := make(chan error, 1)
	go func() { errc <- a.WriteResponse(want) }()
	got, err := b.ReadResponse()
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	if got.Detail != detail {
		t.Fatalf("detail truncated: got %d bytes, want %d", len(got.Detail), len(detail))
	}
}
