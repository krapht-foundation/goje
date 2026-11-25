// Package netlink connects a game to a relay (cmd/relay) for online lockstep
// play. Dial joins or opens a room and returns a Conn whose Send and Recv
// never block, so a scene can poll it from Update. Conn has the shape of
// engine/lockstep's Link.
//
// Natively the connection uses the WebSocket subset in netlink/ws; in the
// browser it uses the page's WebSocket. Both share the bounded queues and the
// decoding of relay messages in this file. The relay's wire contract is in
// specs/relay-m16.md.
package netlink

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// MaxMessage is the largest message Send accepts, in bytes. It matches the
// relay's default limit.
const MaxMessage = 2048

// Queue sizes. Each slot keeps its buffer, so a queue holds at most its size
// times MaxMessage bytes and allocates nothing once warm.
const (
	sendQueue = 64
	recvQueue = 256
)

// Relay message types, defined by specs/relay-m16.md.
const (
	msgWelcome    = 0x01
	msgPeerJoined = 0x02
	msgPeerLeft   = 0x03
	msgData       = 0x04
)

// welcomeLen is the type byte, the slot byte, and the six-character room code.
const welcomeLen = 8

// codeAlphabet is the relay's room code alphabet.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

var (
	// ErrPeerLeft reports that the other player disconnected and the relay
	// closed the room.
	ErrPeerLeft = errors.New("netlink: the other player left")
	// ErrClosed reports use of a Conn after Close.
	ErrClosed = errors.New("netlink: connection closed")
	// ErrQueueFull reports a Send while 64 messages are still waiting to be
	// written.
	ErrQueueFull = errors.New("netlink: send queue full")
	// ErrProtocol reports a message from the relay that breaks its contract.
	ErrProtocol = errors.New("netlink: protocol error")
)

// transport is the platform's connection under a Conn.
type transport interface {
	// write sends one message; it may block until the message is written.
	write(msg []byte) error
	// close ends the connection; it is called once.
	close()
}

// Conn is a connection to a relay room. Its methods are safe to call from any
// goroutine. A writer goroutine drains the send queue; the platform's reader
// fills the receive queue through receive.
type Conn struct {
	slot   int
	code   string
	tr     transport
	wake   chan struct{}
	joined atomic.Bool

	mu      sync.Mutex
	in, out ring
	// holding means the front of in was returned by Recv and is still the
	// caller's until the next Recv.
	holding bool
	err     error
	closing bool
}

func newConn(tr transport, slot int, code string) *Conn {
	c := &Conn{
		slot: slot, code: code, tr: tr, wake: make(chan struct{}, 1),
		in: newRing(recvQueue), out: newRing(sendQueue),
	}
	if slot == 1 {
		c.joined.Store(true)
	}
	return c
}

// Slot returns 0 for the host, who opened the room, and 1 for the guest.
func (c *Conn) Slot() int { return c.slot }

// Code returns the room code.
func (c *Conn) Code() string { return c.code }

// PeerJoined reports whether the other player is in the room. It is always
// true for the guest.
func (c *Conn) PeerJoined() bool { return c.joined.Load() }

// Err returns the error that ended the connection, or nil while it is open. It
// does not touch the receive queue, so a lobby can watch the connection without
// taking the messages of a match that is about to start.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateErr()
}

// Send queues a copy of msg, 1 to MaxMessage bytes, for the writer and returns
// at once. It does not retain msg. It returns ErrQueueFull when 64 messages
// are waiting, ErrClosed after Close, and the connection's error after it
// failed. The relay drops messages the host sends before the guest joins.
func (c *Conn) Send(msg []byte) error {
	if len(msg) == 0 || len(msg) > MaxMessage {
		return fmt.Errorf("netlink: message of %d bytes, want 1 to %d", len(msg), MaxMessage)
	}
	c.mu.Lock()
	err := c.stateErr()
	if err == nil && !c.out.push(msg) {
		err = ErrQueueFull
	}
	c.mu.Unlock()
	if err == nil {
		c.signal()
	}
	return err
}

// Recv returns the next message from the other player, or ok false when none
// has arrived. The message is valid until the next call to Recv. Once the
// queue is empty it returns ErrPeerLeft after the other player left, ErrClosed
// after Close, or the connection's error.
func (c *Conn) Recv() (msg []byte, ok bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holding {
		c.in.pop()
		c.holding = false
	}
	if c.closing {
		return nil, false, ErrClosed
	}
	if c.in.n > 0 {
		c.holding = true
		return c.in.front(), true, nil
	}
	return nil, false, c.err
}

// Close ends the connection. Messages already queued by Send are still
// written, within the write timeout, before the connection closes, so a
// final goodbye is not lost. Close does not block and is safe to repeat.
func (c *Conn) Close() error {
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	c.signal()
	return nil
}

func (c *Conn) stateErr() error {
	if c.closing {
		return ErrClosed
	}
	return c.err
}

func (c *Conn) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// fail records the first error that ends the connection and stops the
// writer, which closes the transport.
func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	c.signal()
}

// writeLoop writes queued messages until the connection fails or is closed
// and drained, then closes the transport. Each platform runs it on its own
// goroutine.
func (c *Conn) writeLoop() {
	defer c.tr.close()
	for {
		c.mu.Lock()
		for c.out.n == 0 || c.err != nil {
			if c.err != nil || c.closing {
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
			<-c.wake
			c.mu.Lock()
		}
		// The front slot stays in the queue while it is written, so Send cannot
		// reuse its buffer.
		msg := c.out.front()
		c.mu.Unlock()
		err := c.tr.write(msg)
		c.mu.Lock()
		c.out.pop()
		c.mu.Unlock()
		if err != nil {
			c.fail(err)
			return
		}
	}
}

// receive handles one message from the relay after the Welcome. It returns
// an error, which ends the connection, for anything outside the contract and
// when the receive queue is full because the game has stopped polling.
func (c *Conn) receive(msg []byte) error {
	if len(msg) == 0 {
		return fmt.Errorf("%w: empty message", ErrProtocol)
	}
	switch {
	case msg[0] == msgData && len(msg) > 1:
		c.joined.Store(true)
		c.mu.Lock()
		ok := c.in.push(msg[1:])
		c.mu.Unlock()
		if !ok {
			return errors.New("netlink: receive queue full")
		}
	case msg[0] == msgPeerJoined && len(msg) == 1:
		c.joined.Store(true)
	case msg[0] == msgPeerLeft && len(msg) == 1:
		c.fail(ErrPeerLeft)
	default:
		return fmt.Errorf("%w: unexpected message type %d of %d bytes", ErrProtocol, msg[0],
			len(msg))
	}
	return nil
}

// parseWelcome decodes the relay's first message.
func parseWelcome(msg []byte) (slot int, code string, err error) {
	if len(msg) != welcomeLen || msg[0] != msgWelcome || msg[1] > 1 {
		return 0, "", fmt.Errorf("%w: bad welcome", ErrProtocol)
	}
	for _, b := range msg[2:] {
		if strings.IndexByte(codeAlphabet, b) < 0 {
			return 0, "", fmt.Errorf("%w: bad room code in welcome", ErrProtocol)
		}
	}
	return int(msg[1]), string(msg[2:]), nil
}

// ring is a bounded FIFO of messages. Each slot keeps its buffer between
// uses, so pushing allocates only until every slot has grown to the largest
// message it has held.
type ring struct {
	slots   [][]byte
	head, n int
}

func newRing(size int) ring { return ring{slots: make([][]byte, size)} }

// push copies msg into the next free slot and reports whether there was one.
func (r *ring) push(msg []byte) bool {
	if r.n == len(r.slots) {
		return false
	}
	i := (r.head + r.n) % len(r.slots)
	r.slots[i] = append(r.slots[i][:0], msg...)
	r.n++
	return true
}

// front returns the oldest message; the ring must not be empty.
func (r *ring) front() []byte { return r.slots[r.head] }

// pop frees the oldest slot; the ring must not be empty.
func (r *ring) pop() {
	r.head = (r.head + 1) % len(r.slots)
	r.n--
}
