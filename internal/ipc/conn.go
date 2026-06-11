package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// MaxFrameBytes caps a single NDJSON frame. The protocol payloads are tiny
// (a command plus at most one cookie), so a small cap defends the server
// against a local same-UID peer trying to exhaust memory with one giant line
// (plan §10a). The Swisscom cookie is a few hundred bytes; 64 KiB is generous.
const MaxFrameBytes = 64 * 1024

// DefaultDeadline bounds a single read or write so a peer cannot connect and
// then stall the server indefinitely (plan §10a).
const DefaultDeadline = 30 * time.Second

// ErrFrameTooLarge is returned when an incoming frame exceeds MaxFrameBytes.
var ErrFrameTooLarge = fmt.Errorf("ipc: frame exceeds %d bytes", MaxFrameBytes)

// Conn is one framed connection. Each message is a single line of JSON
// terminated by '\n' (NDJSON). Conn is not safe for concurrent use by
// multiple goroutines; use one Conn per goroutine.
type Conn struct {
	c       net.Conn
	r       *bufio.Reader
	maxRead int
}

// NewConn wraps a net.Conn with the framing layer.
func NewConn(c net.Conn) *Conn {
	return &Conn{
		c:       c,
		r:       bufio.NewReaderSize(c, 4096),
		maxRead: MaxFrameBytes,
	}
}

// Close closes the underlying connection.
func (f *Conn) Close() error { return f.c.Close() }

// readLine reads one '\n'-terminated frame, enforcing MaxFrameBytes. It reads
// incrementally so an oversized frame is rejected without buffering more than
// the cap. The returned slice excludes the trailing newline.
func (f *Conn) readLine() ([]byte, error) {
	var buf []byte
	for {
		chunk, err := f.r.ReadSlice('\n')
		// Account for bytes seen so far against the cap before anything else.
		if len(buf)+len(chunk) > f.maxRead {
			return nil, ErrFrameTooLarge
		}
		if err == nil {
			buf = append(buf, chunk...)
			// Strip the trailing '\n' (and optional '\r').
			n := len(buf) - 1
			if n > 0 && buf[n-1] == '\r' {
				n--
			}
			return buf[:n], nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			// Partial line larger than bufio's buffer; keep accumulating up to
			// the cap, then continue reading the rest of this line.
			buf = append(buf, chunk...)
			continue
		}
		return nil, err
	}
}

// readMessage applies a read deadline, reads one frame, and unmarshals it into
// v. The deadline guards the whole read of a single frame.
func (f *Conn) readMessage(v any, deadline time.Duration) error {
	if err := f.c.SetReadDeadline(time.Now().Add(deadline)); err != nil {
		return err
	}
	line, err := f.readLine()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(line, v); err != nil {
		return fmt.Errorf("ipc: decode: %w", err)
	}
	return nil
}

// writeMessage applies a write deadline, marshals v, enforces the frame cap,
// and writes it as a single NDJSON line.
func (f *Conn) writeMessage(v any, deadline time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("ipc: encode: %w", err)
	}
	if len(b)+1 > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	b = append(b, '\n')
	if err := f.c.SetWriteDeadline(time.Now().Add(deadline)); err != nil {
		return err
	}
	_, err = f.c.Write(b)
	return err
}

// --- Typed convenience wrappers (one per direction/protocol) ---

// WriteRequest sends a CLI -> AGENT request.
func (f *Conn) WriteRequest(r Request) error { return f.writeMessage(r, DefaultDeadline) }

// ReadRequest reads a CLI -> AGENT request.
func (f *Conn) ReadRequest() (Request, error) {
	var r Request
	err := f.readMessage(&r, DefaultDeadline)
	return r, err
}

// WriteResponse sends an AGENT -> CLI response.
func (f *Conn) WriteResponse(r Response) error { return f.writeMessage(r, DefaultDeadline) }

// ReadResponse reads an AGENT -> CLI response.
func (f *Conn) ReadResponse() (Response, error) {
	var r Response
	err := f.readMessage(&r, DefaultDeadline)
	return r, err
}

// WriteTunnelRequest sends an AGENT -> HELPER request (trust boundary).
func (f *Conn) WriteTunnelRequest(r TunnelRequest) error { return f.writeMessage(r, DefaultDeadline) }

// ReadTunnelRequest reads an AGENT -> HELPER request (trust boundary).
func (f *Conn) ReadTunnelRequest() (TunnelRequest, error) {
	var r TunnelRequest
	err := f.readMessage(&r, DefaultDeadline)
	return r, err
}

// WriteTunnelResponse sends a HELPER -> AGENT response.
func (f *Conn) WriteTunnelResponse(r TunnelResponse) error { return f.writeMessage(r, DefaultDeadline) }

// ReadTunnelResponse reads a HELPER -> AGENT response.
func (f *Conn) ReadTunnelResponse() (TunnelResponse, error) {
	var r TunnelResponse
	err := f.readMessage(&r, DefaultDeadline)
	return r, err
}
