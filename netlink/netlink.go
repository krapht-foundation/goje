// Package netlink connects a game to a relay (cmd/relay) for online lockstep
// play. Dial joins or opens a room and returns a Conn whose Send and Recv
// never block, so a scene can poll it from Update. Conn has the shape of
// engine/lockstep's Link.
//
// Natively the connection uses the WebSocket subset in netlink/ws; in the
// browser it uses the page's WebSocket. Both share the bounded queues and the
// decoding of relay messages in this file. The relay's wire contract is in
// specs/relay-m16.md, and its versioned room mode, with a spectator seat and
// resumable player slots, in specs/relay-m18.md.
package netlink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MaxMessage is the largest message Send accepts, in bytes. It matches the
// relay's default limit.
const MaxMessage = 2048

// Queue sizes. Each slot keeps its buffer, so a queue holds at most its size
// times maxFrame bytes and allocates nothing once warm.
const (
	sendQueue = 64
	recvQueue = 256
	// viewQueue bounds the send queue's spectator views, leaving the rest
	// for the game's messages and controls.
	viewQueue = sendQueue / 4
)

// Relay message types, defined by specs/relay-m16.md and, from 0x05,
// specs/relay-m18.md.
const (
	msgWelcome    = 0x01
	msgPeerJoined = 0x02
	msgPeerLeft   = 0x03
	msgData       = 0x04
	msgWelcome2   = 0x05
	msgEpoch      = 0x06
	msgPaused     = 0x07
	msgWatcher    = 0x08
	msgView       = 0x09
	msgEnd        = 0x0A
	msgResume     = 0x0B
	msgKeepAlive  = 0x0D
)

const (
	// welcomeLen is the type byte, the slot byte, and a six-character code:
	// a v1 Welcome, or a v2 Welcome to a spectator.
	welcomeLen = 8
	// welcome2Len adds the spectator code and the resume token for a player.
	welcome2Len = welcomeLen + 6 + tokenLen
	tokenLen    = 32
	// headerLen is a v2 Data or View message's type byte and 8-byte epoch or
	// spectator id.
	headerLen = 9
	// maxFrame is the largest message on the wire in either version.
	maxFrame = headerLen + MaxMessage
)

// Keepalive timing of v2 connections: each side hears from the other at
// least every keepAliveEvery, so silence for keepAliveTimeout is a lost
// transport even when TCP has not noticed.
const (
	keepAliveEvery   = 5 * time.Second
	keepAliveTimeout = 15 * time.Second
)

// watchSlot is a spectator's slot.
const watchSlot = 2

// codeAlphabet is the relay's room code alphabet.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

var (
	// ErrPeerLeft reports that the room ended: the other player left on
	// purpose or broke a limit, a player ended the room, or the relay expired
	// or shut it down. It is final; resuming cannot help.
	ErrPeerLeft = errors.New("netlink: the other player left")
	// ErrLost reports that the transport to the relay failed: a network
	// error, a closed socket, or keepalive silence. On a v2 player connection
	// the slot is held for 30 seconds, so the game may call Resume with
	// ResumeToken. Errors that wrap it also wrap the underlying cause.
	ErrLost = errors.New("netlink: connection to the relay lost")
	// ErrClosed reports use of a Conn after Close.
	ErrClosed = errors.New("netlink: connection closed")
	// ErrQueueFull reports a Send while 64 messages are still waiting to be
	// written. Only that Send fails.
	ErrQueueFull = errors.New("netlink: send queue full")
	// ErrRecvFull ends a connection whose game stopped calling Recv while 256
	// messages waited.
	ErrRecvFull = errors.New("netlink: receive queue full")
	// ErrProtocol reports a message from the relay that breaks its contract.
	ErrProtocol = errors.New("netlink: protocol error")
	// ErrEpochChanged reports a SendEpoch or RecvEpoch for an epoch that is no
	// longer the connection's live epoch. The connection itself is fine.
	ErrEpochChanged = errors.New("netlink: epoch changed")
)

// closeLost is the WebSocket close status a connection sends when it gives
// up on a relay it lost, so the relay holds the slot (specs/relay-m18.md).
const closeLost = 4000

// transport is the platform's connection under a Conn.
type transport interface {
	// write sends one message; it may block until the message is written.
	write(msg []byte) error
	// close ends the connection; it is called once. lost asks for close
	// status closeLost instead of a normal close, so a v2 relay holds the
	// slot instead of ending the room.
	close(lost bool)
}

// welcome is a decoded Welcome.
type welcome struct {
	slot       int
	code, view string
	token      [tokenLen]byte
	v2         bool
}

// Conn is a connection to a relay room. Its methods are safe to call from any
// goroutine. A writer goroutine drains the send queue; the platform's reader
// fills the receive queue through receive.
type Conn struct {
	welcome
	tr      transport
	wake    chan struct{}
	joined  atomic.Bool
	watcher atomic.Uint64
	// lastRecv is when the relay last sent anything, in Unix nanoseconds.
	lastRecv atomic.Int64

	mu      sync.Mutex
	in, out ring
	// holding means the front of in was returned by Recv and is still the
	// caller's until the next Recv.
	holding bool
	// views counts queued SendView messages.
	views int
	// epoch is the last epoch the relay started; live means it still runs.
	epoch   uint64
	live    bool
	err     error
	closing bool
}

func newConn(tr transport, w welcome) *Conn {
	c := &Conn{
		welcome: w, tr: tr, wake: make(chan struct{}, 1),
		in: newRing(recvQueue), out: newRing(sendQueue),
	}
	if !w.v2 && w.slot == 1 {
		c.joined.Store(true)
	}
	c.lastRecv.Store(time.Now().UnixNano())
	return c
}

// Slot returns 0 for the host, who opened the room, 1 for the guest, and 2
// for a spectator.
func (c *Conn) Slot() int { return c.slot }

// Code returns the room's invitation code, or "" for a spectator.
func (c *Conn) Code() string { return c.code }

// ViewCode returns the room's spectator code, or "" for a v1 room.
func (c *Conn) ViewCode() string { return c.view }

// ResumeToken returns the credential that Resume presents to take this
// player's slot back after ErrLost. It is zero for v1 rooms and spectators.
// It stays the same for the whole room and is a secret: keep it in memory
// and never log it.
func (c *Conn) ResumeToken() [32]byte { return c.token }

// PeerJoined reports whether the other player is in the room. It is always
// true for a v1 guest; on v2 it becomes true with the first epoch.
func (c *Conn) PeerJoined() bool { return c.joined.Load() }

// Epoch returns the v2 epoch in which both players are connected: 1 for the
// first pairing, one more for each pairing restored after a transport loss.
// It is 0 before pairing, while paused, after the connection ended, and on
// v1 rooms and spectators.
func (c *Conn) Epoch() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live && c.stateErr() == nil {
		return c.epoch
	}
	return 0
}

// Watcher returns the id of the room's current spectator, or 0 when there is
// none. Players learn it from the relay; SendView needs it.
func (c *Conn) Watcher() uint64 { return c.watcher.Load() }

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
// failed. The relay drops messages the host sends before the guest joins. On
// v2, msg belongs to the current epoch and is dropped with it; while Epoch is
// 0 it is dropped at once.
func (c *Conn) Send(msg []byte) error { return c.send(msg, 0) }

// SendEpoch is Send for a message that must belong to epoch, which is not 0.
// It returns ErrEpochChanged, and queues nothing, when epoch is not the live
// epoch, so a lockstep session never sends into an epoch it did not start.
func (c *Conn) SendEpoch(epoch uint64, msg []byte) error {
	if epoch == 0 {
		return errors.New("netlink: SendEpoch for epoch 0")
	}
	return c.send(msg, epoch)
}

// send queues a player message. pinned is the epoch the caller requires, or
// 0 for the current one.
func (c *Conn) send(msg []byte, pinned uint64) error {
	if err := checkSize(msg); err != nil {
		return err
	}
	if c.slot == watchSlot {
		return errors.New("netlink: a spectator cannot send")
	}
	c.mu.Lock()
	err := c.stateErr()
	if err == nil {
		switch {
		case !c.v2:
			if !c.out.push(nil, msg) {
				err = ErrQueueFull
			}
		case pinned != 0 && (!c.live || pinned != c.epoch):
			err = ErrEpochChanged
		case c.live:
			var hdr [headerLen]byte
			hdr[0] = msgData
			binary.BigEndian.PutUint64(hdr[1:], c.epoch)
			if !c.out.push(hdr[:], msg) {
				err = ErrQueueFull
			}
		}
	}
	c.mu.Unlock()
	if err == nil {
		c.signal()
	}
	return err
}

// SendView queues msg, 1 to MaxMessage bytes, for the spectator with id
// watcher. Only a v2 host may send views. The relay drops it if that
// spectator has gone, so a view never reaches a later spectator. Views share
// the send queue with Send and are not tied to an epoch, but at most
// viewQueue of them wait at once; beyond that SendView returns ErrQueueFull,
// so views never take the room the game's messages need.
func (c *Conn) SendView(watcher uint64, msg []byte) error {
	if err := checkSize(msg); err != nil {
		return err
	}
	if !c.v2 || c.slot != 0 || watcher == 0 {
		return errors.New("netlink: SendView needs a v2 host and a spectator id")
	}
	var hdr [headerLen]byte
	hdr[0] = msgView
	binary.BigEndian.PutUint64(hdr[1:], watcher)
	return c.push(hdr[:], msg)
}

// EndRoom queues the end of a v2 room. Close afterwards writes it, along
// with everything queued before it, so a spectator receives the views sent
// before the end. The relay then ends the room for everyone, and every
// connection still open, this one included, reports ErrPeerLeft.
func (c *Conn) EndRoom() error {
	if !c.v2 || c.slot == watchSlot {
		return errors.New("netlink: EndRoom needs a v2 player")
	}
	return c.push([]byte{msgEnd}, nil)
}

func (c *Conn) push(hdr, msg []byte) error {
	view := hdr[0] == msgView
	c.mu.Lock()
	err := c.stateErr()
	switch {
	case err != nil:
	case view && c.views >= viewQueue, !c.out.push(hdr, msg):
		err = ErrQueueFull
	case view:
		c.views++
	}
	c.mu.Unlock()
	if err == nil {
		c.signal()
	}
	return err
}

func checkSize(msg []byte) error {
	if len(msg) == 0 || len(msg) > MaxMessage {
		return fmt.Errorf("netlink: message of %d bytes, want 1 to %d", len(msg), MaxMessage)
	}
	return nil
}

// Recv returns the next message from the other player, or for a spectator
// the next view, or ok false when none has arrived. The message is valid
// until the next call to Recv or RecvEpoch. Once the queue is empty it
// returns ErrPeerLeft after the room ended, ErrClosed after Close, or the
// connection's error. On v2 it returns only messages of the current epoch:
// the queue is emptied when an epoch pauses and when the next one starts.
func (c *Conn) Recv() (msg []byte, ok bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.release()
	if c.closing {
		return nil, false, ErrClosed
	}
	if c.in.n > 0 {
		c.holding = true
		return c.in.front(), true, nil
	}
	return nil, false, c.err
}

// RecvEpoch is Recv for a caller that runs epoch, which is not 0. It returns
// ErrEpochChanged, or the connection's error once it has failed, as soon as
// epoch is not the live epoch, so a lockstep session never takes a message
// of a later epoch.
func (c *Conn) RecvEpoch(epoch uint64) (msg []byte, ok bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.release()
	switch {
	case c.closing:
		return nil, false, ErrClosed
	case epoch == 0 || !c.live || c.epoch != epoch:
		if c.err != nil {
			return nil, false, c.err
		}
		return nil, false, ErrEpochChanged
	case c.in.n > 0:
		c.holding = true
		return c.in.front(), true, nil
	}
	return nil, false, c.err
}

// release frees the message the caller held.
func (c *Conn) release() {
	if c.holding {
		c.in.pop()
		c.holding = false
	}
}

// Close ends the connection. Messages already queued by Send are still
// written, within the write timeout, before the connection closes, so a
// final goodbye is not lost. Close does not block and is safe to repeat. On
// v2 a player whose Close reaches the relay ends the room, as EndRoom does;
// a connection already lost does not, and its slot stays held.
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

// lost wraps a transport error so that it matches ErrLost.
func lost(err error) error { return fmt.Errorf("%w: %w", ErrLost, err) }

// writeLoop writes queued messages until the connection fails or is closed
// and drained, then closes the transport. Each platform runs it on its own
// goroutine. On v2 it also sends keepalives while idle and fails the
// connection when the relay has been silent too long.
func (c *Conn) writeLoop() {
	defer func() {
		c.mu.Lock()
		lost := errors.Is(c.err, ErrLost)
		c.mu.Unlock()
		c.tr.close(lost)
	}()
	var tick <-chan time.Time
	if c.v2 {
		t := time.NewTicker(keepAliveEvery)
		defer t.Stop()
		tick = t.C
	}
	for {
		c.mu.Lock()
		for c.out.n == 0 || c.err != nil {
			if c.err != nil || c.closing {
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
			select {
			case <-c.wake:
			case <-tick:
				if err := c.keepAlive(); err != nil {
					c.fail(err)
					return
				}
			}
			c.mu.Lock()
		}
		// The front slot stays in the queue while it is written, so Send cannot
		// reuse its buffer.
		msg := c.out.front()
		if c.staleLocked(msg) {
			c.out.pop()
			c.mu.Unlock()
			continue
		}
		c.mu.Unlock()
		err := c.tr.write(msg)
		c.mu.Lock()
		if c.v2 && msg[0] == msgView {
			c.views--
		}
		c.out.pop()
		c.mu.Unlock()
		if err != nil {
			c.fail(lost(err))
			return
		}
	}
}

// staleLocked reports whether msg is v2 Data of an epoch that has ended. The
// relay drops such messages too; dropping them here saves the bandwidth.
func (c *Conn) staleLocked(msg []byte) bool {
	return c.v2 && msg[0] == msgData &&
		(!c.live || binary.BigEndian.Uint64(msg[1:headerLen]) != c.epoch)
}

func (c *Conn) keepAlive() error {
	if silent := time.Since(time.Unix(0, c.lastRecv.Load())); silent > keepAliveTimeout {
		return fmt.Errorf("%w: the relay was silent for %v", ErrLost, silent.Round(time.Second))
	}
	if err := c.tr.write([]byte{msgKeepAlive}); err != nil {
		return lost(err)
	}
	return nil
}

// receive handles one message from the relay after the Welcome. It returns
// an error, which ends the connection, for anything outside the contract and
// when the receive queue is full because the game has stopped polling.
func (c *Conn) receive(msg []byte) error {
	if len(msg) == 0 {
		return fmt.Errorf("%w: empty message", ErrProtocol)
	}
	if c.v2 {
		return c.receive2(msg)
	}
	switch {
	case msg[0] == msgData && len(msg) > 1:
		c.joined.Store(true)
		if err := c.queue(msg[1:]); err != nil {
			return err
		}
	case msg[0] == msgPeerJoined && len(msg) == 1:
		c.joined.Store(true)
	case msg[0] == msgPeerLeft && len(msg) == 1:
		c.fail(ErrPeerLeft)
	default:
		return unexpected(msg)
	}
	return nil
}

func (c *Conn) receive2(msg []byte) error {
	c.lastRecv.Store(time.Now().UnixNano())
	player := c.slot != watchSlot
	switch {
	case player && msg[0] == msgData && len(msg) > headerLen:
		epoch := binary.BigEndian.Uint64(msg[1:headerLen])
		c.mu.Lock()
		ok := !c.live || epoch != c.epoch || c.in.push(nil, msg[headerLen:])
		c.mu.Unlock()
		if !ok {
			return ErrRecvFull
		}
	case !player && msg[0] == msgView && len(msg) > 1:
		return c.queue(msg[1:])
	case player && msg[0] == msgEpoch && len(msg) == headerLen:
		epoch := binary.BigEndian.Uint64(msg[1:])
		c.mu.Lock()
		if epoch <= c.epoch {
			c.mu.Unlock()
			return fmt.Errorf("%w: epoch %d after %d", ErrProtocol, epoch, c.epoch)
		}
		c.epoch, c.live = epoch, true
		c.clearInLocked()
		c.mu.Unlock()
		c.joined.Store(true)
	case player && msg[0] == msgPaused && len(msg) == 1:
		c.mu.Lock()
		c.live = false
		c.clearInLocked()
		c.mu.Unlock()
	case player && msg[0] == msgWatcher && len(msg) == headerLen:
		c.watcher.Store(binary.BigEndian.Uint64(msg[1:]))
	case msg[0] == msgPeerLeft && len(msg) == 1:
		c.fail(ErrPeerLeft)
	case msg[0] == msgKeepAlive && len(msg) == 1:
	default:
		return unexpected(msg)
	}
	return nil
}

func (c *Conn) queue(msg []byte) error {
	c.mu.Lock()
	ok := c.in.push(nil, msg)
	c.mu.Unlock()
	if !ok {
		return ErrRecvFull
	}
	return nil
}

// clearInLocked drops every queued message of an ended epoch except the one
// the caller still holds, which the next Recv frees.
func (c *Conn) clearInLocked() {
	c.in.n = 0
	if c.holding {
		c.in.n = 1
	}
}

func unexpected(msg []byte) error {
	return fmt.Errorf("%w: unexpected message type %d of %d bytes", ErrProtocol, msg[0], len(msg))
}

// parseWelcome decodes the relay's first message.
func parseWelcome(msg []byte) (welcome, error) {
	var w welcome
	var codes []byte
	switch {
	case len(msg) == welcomeLen && msg[0] == msgWelcome && msg[1] <= 1:
		w = welcome{slot: int(msg[1]), code: string(msg[2:])}
		codes = msg[2:]
	case len(msg) == welcome2Len && msg[0] == msgWelcome2 && msg[1] <= 1:
		w = welcome{
			slot: int(msg[1]), code: string(msg[2:8]), view: string(msg[8:14]),
			token: [tokenLen]byte(msg[14:]), v2: true,
		}
		codes = msg[2:14]
	case len(msg) == welcomeLen && msg[0] == msgWelcome2 && msg[1] == watchSlot:
		w = welcome{slot: watchSlot, view: string(msg[2:]), v2: true}
		codes = msg[2:]
	default:
		return w, fmt.Errorf("%w: bad welcome", ErrProtocol)
	}
	for _, b := range codes {
		if strings.IndexByte(codeAlphabet, b) < 0 {
			return w, fmt.Errorf("%w: bad room code in welcome", ErrProtocol)
		}
	}
	return w, nil
}

// resumeMessage is the first message on /v2/resume.
func resumeMessage(token [32]byte) []byte { return append([]byte{msgResume}, token[:]...) }

// parseFirst decodes the relay's first message on a connection: a Welcome,
// or PeerLeft for a room that has ended or a resume token the relay does
// not know. A resume must be answered by a v2 player's Welcome.
func parseFirst(msg []byte, resume bool) (welcome, error) {
	if len(msg) == 1 && msg[0] == msgPeerLeft {
		return welcome{}, ErrPeerLeft
	}
	w, err := parseWelcome(msg)
	if err == nil && resume && (!w.v2 || w.slot == watchSlot) {
		err = fmt.Errorf("%w: resume answered with a slot %d welcome", ErrProtocol, w.slot)
	}
	return w, err
}

// ring is a bounded FIFO of messages. Each slot keeps its buffer between
// uses, so pushing allocates only until every slot has grown to the largest
// message it has held.
type ring struct {
	slots   [][]byte
	head, n int
}

func newRing(size int) ring { return ring{slots: make([][]byte, size)} }

// push copies hdr followed by msg into the next free slot and reports
// whether there was one.
func (r *ring) push(hdr, msg []byte) bool {
	if r.n == len(r.slots) {
		return false
	}
	i := (r.head + r.n) % len(r.slots)
	r.slots[i] = append(append(r.slots[i][:0], hdr...), msg...)
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
