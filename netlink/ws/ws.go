// Package ws is the subset of the WebSocket protocol (RFC 6455) that Goje's
// relay and native clients speak: the opening handshake, single unfragmented
// binary frames of bounded size, ping, pong, and the close handshake. It
// rejects everything else, including text frames, fragmentation, and
// extensions. It runs over a net.Conn and starts no goroutines.
//
// The package is a trust boundary: every frame is checked against the limits
// before its payload is read or any memory is allocated for it. The subset and
// its limits are specified in specs/websocket-m16.md. The netlink package and
// cmd/relay use it; the browser uses its own WebSocket instead.
package ws

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Default limits used when the corresponding Options field is zero.
const (
	// DefaultLimit is the largest data payload a Conn accepts, in bytes.
	DefaultLimit = 2048
	// DefaultWriteTimeout bounds each frame write and the opening handshake.
	DefaultWriteTimeout = 30 * time.Second
)

// maxControl is the largest payload of a control frame (RFC 6455 section 5.5).
const maxControl = 125

// closeTimeout bounds the courtesy close frame that Close sends, so closing a
// connection to a peer that has stopped reading never waits for long.
const closeTimeout = time.Second

// Frame opcodes of RFC 6455 section 5.2.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// Close status codes of RFC 6455 section 7.4.1.
const (
	closeNormal    = 1000
	closeGoingAway = 1001
	closeProtocol  = 1002
	closeTooBig    = 1009
)

var (
	// ErrProtocol reports a frame or handshake outside the supported subset.
	ErrProtocol = errors.New("ws: protocol error")
	// ErrTooBig reports a data frame larger than the connection's limit.
	ErrTooBig = errors.New("ws: message too big")
	// ErrClosed reports that the peer closed the connection with a close frame.
	ErrClosed = errors.New("ws: closed by peer")
)

// Options configures a connection. The zero value uses the defaults.
type Options struct {
	// Limit is the largest data payload accepted, in bytes. Zero means
	// DefaultLimit. It does not limit writes.
	Limit int
	// ReadTimeout is the deadline for each ReadMessage call, measured from its
	// start. Zero means no read deadline: the owner bounds idle connections
	// some other way, for example by closing them from a timer.
	ReadTimeout time.Duration
	// WriteTimeout is the deadline for each frame write and for the opening
	// handshake. Zero means DefaultWriteTimeout.
	WriteTimeout time.Duration
	// Origins is the server's allow-list for the Origin header. Empty allows
	// every origin. A request without an Origin header comes from a program
	// rather than a browser page and is always allowed, because only browsers
	// send the header and an origin check cannot stop other programs.
	Origins []string
}

func (o Options) limit() int {
	if o.Limit > 0 {
		return o.Limit
	}
	return DefaultLimit
}

func (o Options) writeTimeout() time.Duration {
	if o.WriteTimeout > 0 {
		return o.WriteTimeout
	}
	return DefaultWriteTimeout
}

// Conn is an open WebSocket connection. One goroutine may call ReadMessage
// while another calls WriteMessage; writes, including the pongs and close
// frames that ReadMessage sends, are serialized. Close may be called from any
// goroutine at any time. After ReadMessage returns an error the connection is
// closed.
type Conn struct {
	nc net.Conn
	br *bufio.Reader
	// client connections mask what they send and require unmasked frames;
	// server connections do the opposite.
	client       bool
	limit        int
	readTimeout  time.Duration
	writeTimeout time.Duration

	// hdr and ctrl belong to the reader; they are fields so reading a frame
	// does not allocate.
	hdr  [14]byte
	ctrl [maxControl]byte

	wmu  sync.Mutex
	wbuf []byte
	key  [4]byte // The mask key of the frame being written, kept here so that it is not allocated.

	closeOnce sync.Once
	closeErr  error
}

func newConn(nc net.Conn, br *bufio.Reader, client bool, opts Options) *Conn {
	return &Conn{
		nc:           nc,
		br:           br,
		client:       client,
		limit:        opts.limit(),
		readTimeout:  opts.ReadTimeout,
		writeTimeout: opts.writeTimeout(),
	}
}

// ReadMessage returns the next binary message. The payload is read into buf
// when its capacity is large enough and into a new slice of the payload's size
// otherwise, which is never larger than the connection's limit. Pings are
// answered and pongs are skipped while waiting. A close frame is answered and
// returns an error wrapping ErrClosed. Any error closes the connection.
func (c *Conn) ReadMessage(buf []byte) ([]byte, error) {
	if c.readTimeout > 0 {
		if err := c.nc.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
			return nil, c.fail(fmt.Errorf("ws: set read deadline: %w", err))
		}
	}
	for {
		op, n, err := c.readHeader()
		if err != nil {
			return nil, c.fail(err)
		}
		if op == opBinary {
			if cap(buf) < n {
				buf = make([]byte, n)
			}
			buf = buf[:n]
			if err := c.readPayload(buf); err != nil {
				return nil, c.fail(err)
			}
			return buf, nil
		}
		payload := c.ctrl[:n]
		if err := c.readPayload(payload); err != nil {
			return nil, c.fail(err)
		}
		switch op {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return nil, c.fail(err)
			}
		case opClose:
			return nil, c.closed(payload)
		}
		// A pong needs no answer.
	}
}

// readHeader reads and validates one frame header, including the mask key,
// and returns the opcode and payload length. It rejects every frame outside
// the subset before the payload is read.
func (c *Conn) readHeader() (op byte, n int, err error) {
	h := c.hdr[:]
	if _, err := io.ReadFull(c.br, h[:2]); err != nil {
		return 0, 0, fmt.Errorf("ws: read frame header: %w", err)
	}
	fin, rsv, op := h[0]&0x80 != 0, h[0]&0x70, h[0]&0x0F
	masked, length := h[1]&0x80 != 0, uint64(h[1]&0x7F)
	switch {
	case rsv != 0:
		return 0, 0, fmt.Errorf("%w: reserved bits set", ErrProtocol)
	case op == opContinuation || op == opText:
		return 0, 0, fmt.Errorf("%w: opcode %d is not supported", ErrProtocol, op)
	case op != opBinary && op != opClose && op != opPing && op != opPong:
		return 0, 0, fmt.Errorf("%w: unknown opcode %d", ErrProtocol, op)
	case !fin:
		return 0, 0, fmt.Errorf("%w: fragmented frame", ErrProtocol)
	case masked == c.client:
		return 0, 0, fmt.Errorf("%w: wrong masking for this side", ErrProtocol)
	}
	switch length {
	case 126:
		if _, err := io.ReadFull(c.br, h[2:4]); err != nil {
			return 0, 0, fmt.Errorf("ws: read frame length: %w", err)
		}
		length = uint64(binary.BigEndian.Uint16(h[2:4]))
		if length < 126 {
			return 0, 0, fmt.Errorf("%w: length not minimally encoded", ErrProtocol)
		}
	case 127:
		if _, err := io.ReadFull(c.br, h[2:10]); err != nil {
			return 0, 0, fmt.Errorf("ws: read frame length: %w", err)
		}
		length = binary.BigEndian.Uint64(h[2:10])
		if length>>63 != 0 || length <= 0xFFFF {
			return 0, 0, fmt.Errorf("%w: invalid 64-bit length", ErrProtocol)
		}
	}
	if op&0x8 != 0 {
		if length > maxControl {
			return 0, 0, fmt.Errorf("%w: control frame of %d bytes", ErrProtocol, length)
		}
		if op == opClose && length == 1 {
			return 0, 0, fmt.Errorf("%w: one-byte close payload", ErrProtocol)
		}
	} else if length > uint64(c.limit) { // #nosec G115 -- the limit is positive.
		return 0, 0, fmt.Errorf("%w: %d bytes, limit %d", ErrTooBig, length, c.limit)
	}
	if masked {
		if _, err := io.ReadFull(c.br, h[10:14]); err != nil {
			return 0, 0, fmt.Errorf("ws: read mask key: %w", err)
		}
	}
	return op, int(length), nil // #nosec G115 -- length is at most the limit.
}

// readPayload reads a payload whose header readHeader just accepted and
// removes the client's mask.
func (c *Conn) readPayload(p []byte) error {
	if _, err := io.ReadFull(c.br, p); err != nil {
		return fmt.Errorf("ws: read payload: %w", err)
	}
	if !c.client {
		mask(p, [4]byte(c.hdr[10:14]))
	}
	return nil
}

// closed answers a close frame by echoing its status code and closes the
// connection.
func (c *Conn) closed(payload []byte) error {
	code := uint16(closeNormal)
	if len(payload) >= 2 {
		code = binary.BigEndian.Uint16(payload)
	}
	if err := c.closeWith(code); err != nil {
		return fmt.Errorf("%w (status %d): %w", ErrClosed, code, err)
	}
	return fmt.Errorf("%w (status %d)", ErrClosed, code)
}

// fail closes the connection after a read error, telling the peer why when
// the error is the peer's fault, and returns the error.
func (c *Conn) fail(err error) error {
	code := uint16(closeGoingAway)
	switch {
	case errors.Is(err, ErrTooBig):
		code = closeTooBig
	case errors.Is(err, ErrProtocol):
		code = closeProtocol
	}
	return errors.Join(err, c.closeWith(code))
}

// WriteMessage sends msg as one binary frame. It does not retain msg.
func (c *Conn) WriteMessage(msg []byte) error {
	return c.writeFrame(opBinary, msg)
}

// writeFrame encodes a whole frame into the reused write buffer and sends it
// with one Write, so a frame is never interleaved with another.
func (c *Conn) writeFrame(op byte, p []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.writeFrameLocked(op, p, c.writeTimeout)
}

func (c *Conn) writeFrameLocked(op byte, p []byte, timeout time.Duration) error {
	var maskBit byte
	if c.client {
		maskBit = 0x80
	}
	b := c.wbuf[:0]
	b = append(b, 0x80|op)
	switch n := len(p); {
	case n < 126:
		b = append(b, maskBit|byte(n))
	case n <= 0xFFFF:
		b = append(b, maskBit|126)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, maskBit|127)
		b = binary.BigEndian.AppendUint64(b, uint64(n))
	}
	start := len(b)
	if c.client {
		// The key lives in the connection because a local array that crypto/rand reads
		// into is moved to the heap on Linux, one allocation for every frame.
		if _, err := rand.Read(c.key[:]); err != nil {
			return fmt.Errorf("ws: mask key: %w", err)
		}
		b = append(b, c.key[:]...)
		start += 4
		b = append(b, p...)
		mask(b[start:], c.key)
	} else {
		b = append(b, p...)
	}
	c.wbuf = b
	if err := c.nc.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("ws: set write deadline: %w", err)
	}
	if _, err := c.nc.Write(b); err != nil {
		return fmt.Errorf("ws: write frame: %w", err)
	}
	return nil
}

// Close sends a normal close frame, without waiting for the answer, and closes
// the connection. It is safe to call more than once and from any goroutine.
func (c *Conn) Close() error {
	return c.closeWith(closeNormal)
}

func (c *Conn) closeWith(code uint16) error {
	c.closeOnce.Do(func() {
		// The close frame is a courtesy. When another goroutine is blocked in a
		// write, skipping it lets the network close below unblock that write
		// at once instead of waiting for its deadline.
		if c.wmu.TryLock() {
			status := binary.BigEndian.AppendUint16(nil, code)
			//nolint:errcheck // The peer may be gone; the close below ends the connection anyway.
			_ = c.writeFrameLocked(opClose, status, closeTimeout)
			c.wmu.Unlock()
		}
		if err := c.nc.Close(); err != nil {
			c.closeErr = fmt.Errorf("ws: close: %w", err)
		}
	})
	return c.closeErr
}

// mask applies the masking of RFC 6455 section 5.3, which is its own inverse.
func mask(p []byte, key [4]byte) {
	for i := range p {
		p[i] ^= key[i&3]
	}
}
