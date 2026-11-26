package relay

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/krapht-foundation/goje/netlink/ws"
)

// Message types of the versioned room mode, defined by specs/relay-m18.md.
// Data (0x04) and PeerLeft (0x03) keep their v1 values; v2 Data carries an
// epoch. The netlink client decodes the same values.
const (
	msgWelcome2  = 0x05
	msgEpoch     = 0x06
	msgPaused    = 0x07
	msgWatcher   = 0x08
	msgView      = 0x09
	msgEnd       = 0x0A
	msgResume    = 0x0B
	msgKeepAlive = 0x0D
)

const (
	// tokenLen is the length of a resume credential.
	tokenLen = 32
	// headerLen is a v2 Data or View message's type byte and 8-byte epoch or
	// spectator id.
	headerLen = 9
	// keepAliveTimeout closes a v2 connection that sent nothing, not even a
	// keepalive, for this long. Clients send one every 5 seconds.
	keepAliveTimeout = 15 * time.Second
	// watcherSlot is the spectator's slot in its Welcome.
	watcherSlot = 2
)

// room2 is a v2 room: two player slots that survive a transport loss, and one
// spectator seat. Fields other than out and last are guarded by Server.mu.
type room2 struct {
	code, view string
	// out[i] serializes writes to player i with the check of the epoch they
	// belong to, so a message from an old epoch never follows an Epoch
	// message. Lock order: out[0], out[1], then Server.mu.
	out [2]sync.Mutex
	// last is the time of the last forwarded Data, as time since the server
	// started.
	last atomic.Int64

	opened, closed bool
	// joined means the guest slot has been claimed by /v2/join.
	joined bool
	conns  [2]*ws.Conn
	tokens [2][tokenLen]byte
	// held[i] means player i lost its transport at lost[i] and its slot is
	// reserved for a resume.
	held     [2]bool
	lost     [2]time.Duration
	openedAt time.Duration
	// epoch counts pairings; live means both players are connected in it.
	epoch uint64
	live  bool
	// watching means the spectator seat is taken or being upgraded.
	watching bool
	watcher  *watcher
	watchGen uint64
	timer    *time.Timer
}

// watcher is a connected spectator. Its writer goroutine drains q, so a
// slow spectator never blocks a player; q is closed exactly once, by
// whoever detaches the watcher from its room under Server.mu.
type watcher struct {
	id      uint64
	c       *ws.Conn
	q       chan []byte
	evicted bool
}

type tokenRef struct {
	rm   *room2
	slot int
}

// note is a message to write to a connection once Server.mu is released.
type note struct {
	c   *ws.Conn
	msg []byte
}

func (s *Server) now() time.Duration { return time.Since(s.start) }

func (s *Server) wsOptions2() ws.Options {
	return ws.Options{
		Limit: headerLen + s.cfg.MaxMessage, ReadTimeout: keepAliveTimeout,
		Origins: s.cfg.Origins,
	}
}

// update runs f under Server.mu with both player write locks held, then
// writes the messages f returned in order. A failed write closes that
// connection, and its reader then treats the player as lost.
func (s *Server) update(rm *room2, f func() []note) {
	rm.out[0].Lock()
	defer rm.out[0].Unlock()
	rm.out[1].Lock()
	defer rm.out[1].Unlock()
	s.mu.Lock()
	notes := f()
	s.mu.Unlock()
	for _, n := range notes {
		if err := n.c.WriteMessage(n.msg); err != nil {
			closeQuietly(n.c)
		}
	}
}

func (s *Server) handleNew2(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.closed || len(s.rooms)+len(s.rooms2) >= s.cfg.MaxRooms {
		s.mu.Unlock()
		http.Error(w, "the relay is full", http.StatusServiceUnavailable)
		return
	}
	rm := &room2{}
	code, err := s.newCodeLocked()
	if err == nil {
		rm.code = code
		s.rooms2[code] = rm // Reserved first, so the view code cannot equal it.
		rm.view, err = s.newCodeLocked()
		if err != nil {
			delete(s.rooms2, code)
		}
	}
	if err != nil {
		s.mu.Unlock()
		s.cfg.Logger.Error("room code", "err", err)
		http.Error(w, "no room code available", http.StatusServiceUnavailable)
		return
	}
	s.views[rm.view] = rm
	s.mu.Unlock()

	c, err := ws.Accept(w, r, s.wsOptions2())
	if err != nil {
		s.cfg.Logger.Info("upgrade rejected", "route", "v2/new", "err", err)
		s.closeRoom2(rm, "upgrade failed")
		return
	}
	s.mu.Lock()
	if rm.closed {
		s.mu.Unlock()
		closeQuietly(c)
		return
	}
	// As in v1, the Welcome is written under the lock: nobody can reach the
	// room before it is opened, and a few bytes fit in the socket buffer.
	rm.tokens[0] = newToken()
	if err := c.WriteMessage(welcome2(rm, 0)); err != nil {
		s.mu.Unlock()
		closeQuietly(c)
		s.closeRoom2(rm, "welcome failed")
		return
	}
	s.tokens[rm.tokens[0]] = tokenRef{rm, 0}
	rm.conns[0] = c
	rm.opened = true
	rm.openedAt = s.now()
	rm.timer = time.AfterFunc(s.cfg.JoinWindow, func() { s.expire2(rm) })
	s.mu.Unlock()
	s.cfg.Logger.Info("room opened", "room", rm.code, "version", 2)
	s.serve2(rm, 0, c)
}

func (s *Server) handleJoin2(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(r.URL.Query().Get("room"))
	s.mu.Lock()
	rm := s.rooms2[code]
	if rm == nil || !rm.opened {
		s.mu.Unlock()
		http.Error(w, "no such room", http.StatusNotFound)
		return
	}
	if rm.joined {
		s.mu.Unlock()
		http.Error(w, "the room is full", http.StatusConflict)
		return
	}
	rm.joined = true
	s.mu.Unlock()

	c, err := ws.Accept(w, r, s.wsOptions2())
	if err != nil {
		s.mu.Lock()
		rm.joined = false
		s.mu.Unlock()
		s.cfg.Logger.Info("upgrade rejected", "route", "v2/join", "err", err)
		return
	}
	ok := false
	s.update(rm, func() []note {
		if rm.closed {
			return nil
		}
		ok = true
		rm.tokens[1] = newToken()
		s.tokens[rm.tokens[1]] = tokenRef{rm, 1}
		return s.attachLocked(rm, 1, c)
	})
	if !ok {
		sendLeftAndClose(c)
		return
	}
	s.cfg.Logger.Info("guest joined", "room", code)
	s.serve2(rm, 1, c)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.closed || s.pending >= s.cfg.MaxPending {
		s.mu.Unlock()
		http.Error(w, "the relay is busy", http.StatusServiceUnavailable)
		return
	}
	s.pending++
	s.mu.Unlock()

	c, err := ws.Accept(w, r, s.wsOptions2())
	if err != nil {
		s.mu.Lock()
		s.pending--
		s.mu.Unlock()
		s.cfg.Logger.Info("upgrade rejected", "route", "v2/resume", "err", err)
		return
	}
	// The read deadline bounds how long an unauthenticated connection waits.
	msg, err := c.ReadMessage(make([]byte, 1+tokenLen))
	s.mu.Lock()
	s.pending--
	var ref tokenRef
	if err == nil && len(msg) == 1+tokenLen && msg[0] == msgResume {
		ref = s.tokens[[tokenLen]byte(msg[1:])]
	}
	s.mu.Unlock()
	switch {
	case err != nil:
		s.cfg.Logger.Info("resume rejected", "err", err)
		return
	case len(msg) != 1+tokenLen || msg[0] != msgResume:
		s.cfg.Logger.Info("resume rejected", "err", "malformed credential")
		closeQuietly(c)
		return
	case ref.rm == nil:
		// Unknown, or its room has ended; never logged, as it is a credential.
		s.cfg.Logger.Info("resume rejected", "err", "unknown credential")
		sendLeftAndClose(c)
		return
	}
	rm, slot := ref.rm, ref.slot
	ended, accepted := false, false
	s.update(rm, func() []note {
		switch {
		case rm.closed:
			ended = true
		case rm.conns[slot] == nil: // A connected player is never replaced.
			accepted = true
			return s.attachLocked(rm, slot, c)
		}
		return nil
	})
	switch {
	case ended:
		sendLeftAndClose(c)
	case !accepted:
		s.cfg.Logger.Info("resume rejected", "room", rm.code, "slot", slot, "err", "slot connected")
		closeQuietly(c)
	default:
		s.cfg.Logger.Info("player resumed", "room", rm.code, "slot", slot)
		s.serve2(rm, slot, c)
	}
}

// attachLocked seats c in slot and returns its Welcome, the spectator's
// presence, and the start of a new epoch when both players are now present.
func (s *Server) attachLocked(rm *room2, slot int, c *ws.Conn) []note {
	rm.conns[slot] = c
	rm.held[slot] = false
	notes := []note{{c, welcome2(rm, slot)}}
	if w := rm.watcher; w != nil {
		notes = append(notes, note{c, idMessage(msgWatcher, w.id)})
	}
	if rm.conns[0] != nil && rm.conns[1] != nil {
		rm.epoch++
		rm.live = true
		rm.last.Store(int64(s.now()))
		m := idMessage(msgEpoch, rm.epoch)
		notes = append(notes, note{rm.conns[0], m}, note{rm.conns[1], m})
		s.cfg.Logger.Info("room paired", "room", rm.code, "epoch", rm.epoch)
	}
	s.rearmLocked(rm)
	return notes
}

// serve2 reads one player's messages until the connection ends, then either
// ends the room or holds the player's slot for a resume.
func (s *Server) serve2(rm *room2, slot int, c *ws.Conn) {
	buf := make([]byte, headerLen+s.cfg.MaxMessage)
	b := newBucket(s.cfg.Rate)
	terminal, reason := true, ""
read:
	for {
		msg, err := c.ReadMessage(buf)
		if err != nil {
			terminal, reason = closedOnPurpose(err), err.Error()
			break
		}
		if !b.take() {
			reason = "rate limit"
			break
		}
		switch {
		case msg[0] == msgData && len(msg) > headerLen:
			s.forward(rm, 1-slot, msg)
		case msg[0] == msgView && len(msg) > headerLen:
			if slot == 0 {
				s.view(rm, binary.BigEndian.Uint64(msg[1:headerLen]), msg[headerLen:])
			}
		case msg[0] == msgKeepAlive && len(msg) == 1:
			rm.out[slot].Lock()
			err := c.WriteMessage(msg)
			rm.out[slot].Unlock()
			if err != nil {
				closeQuietly(c)
			}
		case msg[0] == msgEnd && len(msg) == 1:
			reason = "ended"
			break read
		default:
			reason = "protocol violation"
			break read
		}
	}
	if terminal {
		s.closeRoom2(rm, reason)
		return
	}
	held := false
	s.update(rm, func() []note {
		if rm.closed || rm.conns[slot] != c {
			return nil
		}
		held = true
		rm.conns[slot] = nil
		rm.held[slot] = true
		rm.lost[slot] = s.now()
		var notes []note
		if rm.live {
			rm.live = false
			if o := rm.conns[1-slot]; o != nil {
				notes = append(notes, note{o, []byte{msgPaused}})
			}
		}
		s.rearmLocked(rm)
		return notes
	})
	if held {
		s.cfg.Logger.Info("player lost", "room", rm.code, "slot", slot, "reason", reason)
	}
}

// closedOnPurpose reports whether a read error ends the room rather than
// reserving the slot: a normal close frame (1000), which netlink sends on
// Close, or a frame outside the WebSocket subset. Other close codes, such as
// 1001 from a client whose read failed and 4000 from netlink after it lost
// the relay, and network errors and timeouts are transport loss.
func closedOnPurpose(err error) bool {
	if ce, ok := errors.AsType[*ws.CloseError](err); ok {
		return ce.Code == 1000
	}
	return errors.Is(err, ws.ErrTooBig) || errors.Is(err, ws.ErrProtocol)
}

// forward writes a Data message to player to when it belongs to the current
// live epoch and drops it otherwise.
func (s *Server) forward(rm *room2, to int, msg []byte) {
	epoch := binary.BigEndian.Uint64(msg[1:headerLen])
	rm.out[to].Lock()
	defer rm.out[to].Unlock()
	s.mu.Lock()
	c := rm.conns[to]
	ok := rm.live && epoch == rm.epoch && c != nil
	s.mu.Unlock()
	if !ok {
		return
	}
	rm.last.Store(int64(s.now()))
	if err := c.WriteMessage(msg); err != nil {
		closeQuietly(c)
	}
}

// view queues a host's view payload for the spectator with that id. A full
// queue evicts the spectator rather than wait for it.
func (s *Server) view(rm *room2, id uint64, payload []byte) {
	s.mu.Lock()
	w := rm.watcher
	evict := w != nil && w.id == id && !w.enqueueLocked(append([]byte{msgView}, payload...))
	s.mu.Unlock()
	if evict {
		closeQuietly(w.c)
	}
}

// enqueueLocked queues msg without blocking and reports whether it fit. A
// spectator that did not fit is evicted and receives nothing more.
func (w *watcher) enqueueLocked(msg []byte) bool {
	if w.evicted {
		return true
	}
	select {
	case w.q <- msg:
		return true
	default:
		w.evicted = true
		return false
	}
}

// write drains the spectator's queue until it is closed, then closes the
// connection.
func (w *watcher) write() {
	ok := true
	for msg := range w.q {
		if ok && w.c.WriteMessage(msg) != nil {
			ok = false
			closeQuietly(w.c)
		}
	}
	closeQuietly(w.c)
}

func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	view := strings.ToUpper(r.URL.Query().Get("room"))
	s.mu.Lock()
	rm := s.views[view]
	if rm == nil || !rm.opened {
		s.mu.Unlock()
		http.Error(w, "no such room", http.StatusNotFound)
		return
	}
	if rm.watching {
		s.mu.Unlock()
		http.Error(w, "the room already has a spectator", http.StatusConflict)
		return
	}
	rm.watching = true
	s.mu.Unlock()

	c, err := ws.Accept(w, r, s.wsOptions2())
	if err != nil {
		s.mu.Lock()
		rm.watching = false
		s.mu.Unlock()
		s.cfg.Logger.Info("upgrade rejected", "route", "v2/watch", "err", err)
		return
	}
	wt := &watcher{c: c, q: make(chan []byte, s.cfg.ViewQueue)}
	ok := false
	s.update(rm, func() []note {
		if rm.closed {
			return nil
		}
		ok = true
		rm.watchGen++
		wt.id = rm.watchGen
		rm.watcher = wt
		wt.q <- append([]byte{msgWelcome2, watcherSlot}, rm.view...)
		return s.toPlayersLocked(rm, idMessage(msgWatcher, wt.id))
	})
	if !ok {
		sendLeftAndClose(c)
		return
	}
	go wt.write()
	s.cfg.Logger.Info("spectator joined", "room", rm.code)

	buf := make([]byte, 1)
	b := newBucket(s.cfg.Rate)
	reason := "read-only spectator sent a message"
	for {
		msg, err := c.ReadMessage(buf)
		if err != nil {
			reason = err.Error()
			break
		}
		if !b.take() {
			reason = "rate limit"
			break
		}
		if len(msg) != 1 || msg[0] != msgKeepAlive {
			break
		}
		s.mu.Lock()
		evict := rm.watcher == wt && !wt.enqueueLocked([]byte{msgKeepAlive})
		s.mu.Unlock()
		if evict {
			closeQuietly(c)
		}
	}
	detached := false
	s.update(rm, func() []note {
		if rm.watcher != wt {
			return nil
		}
		detached = true
		rm.watcher = nil
		rm.watching = false
		close(wt.q)
		return s.toPlayersLocked(rm, idMessage(msgWatcher, 0))
	})
	if detached {
		// The room still runs; a spectator that broke the contract is dropped
		// even though its queue still holds messages.
		closeQuietly(c)
		s.cfg.Logger.Info("spectator left", "room", rm.code, "reason", reason)
	}
}

func (s *Server) toPlayersLocked(rm *room2, msg []byte) []note {
	var notes []note
	for _, c := range rm.conns {
		if c != nil {
			notes = append(notes, note{c, msg})
		}
	}
	return notes
}

// expire2 runs from the room's timer. It ends a room past its deadline and
// otherwise rearms the timer.
func (s *Server) expire2(rm *room2) {
	s.mu.Lock()
	if rm.closed {
		s.mu.Unlock()
		return
	}
	if d := s.deadlineLocked(rm) - s.now(); d > 0 {
		rm.timer.Reset(d)
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.closeRoom2(rm, "expired")
}

// deadlineLocked is when the room expires: the join window before the first
// pairing, the idle period while live, and the earliest held slot's resume
// window.
func (s *Server) deadlineLocked(rm *room2) time.Duration {
	d := time.Duration(math.MaxInt64)
	switch {
	case rm.epoch == 0:
		d = rm.openedAt + s.cfg.JoinWindow
	case rm.live:
		d = time.Duration(rm.last.Load()) + s.cfg.Idle
	}
	for i, held := range rm.held {
		if held {
			d = min(d, rm.lost[i]+s.cfg.Rejoin)
		}
	}
	return d
}

func (s *Server) rearmLocked(rm *room2) {
	if rm.timer != nil {
		rm.timer.Reset(max(0, s.deadlineLocked(rm)-s.now()))
	}
}

// closeRoom2 ends a room: every connected player and the spectator receive
// PeerLeft, the spectator after the views already queued for it, and all
// credentials stop working.
func (s *Server) closeRoom2(rm *room2, reason string) {
	rm.out[0].Lock()
	defer rm.out[0].Unlock()
	rm.out[1].Lock()
	defer rm.out[1].Unlock()
	s.mu.Lock()
	if rm.closed {
		s.mu.Unlock()
		return
	}
	rm.closed = true
	delete(s.rooms2, rm.code)
	delete(s.views, rm.view)
	for _, t := range rm.tokens {
		if t != ([tokenLen]byte{}) {
			delete(s.tokens, t)
		}
	}
	if rm.timer != nil {
		rm.timer.Stop()
	}
	conns := rm.conns
	w := rm.watcher
	rm.watcher = nil
	evict := false
	if w != nil {
		evict = !w.enqueueLocked([]byte{msgPeerLeft})
		close(w.q)
	}
	s.mu.Unlock()
	if evict {
		closeQuietly(w.c)
	}
	for _, c := range conns {
		if c != nil {
			sendLeftAndClose(c)
		}
	}
	s.cfg.Logger.Info("room closed", "room", rm.code, "reason", reason)
}

func sendLeftAndClose(c *ws.Conn) {
	//nolint:errcheck // The connection is closed next either way.
	_ = c.WriteMessage([]byte{msgPeerLeft})
	closeQuietly(c)
}

func welcome2(rm *room2, slot int) []byte {
	m := append([]byte{msgWelcome2, "\x00\x01"[slot]}, rm.code...)
	m = append(m, rm.view...)
	return append(m, rm.tokens[slot][:]...)
}

func idMessage(typ byte, v uint64) []byte {
	return binary.BigEndian.AppendUint64([]byte{typ}, v)
}

func newToken() (t [tokenLen]byte) {
	_, _ = rand.Read(t[:]) // It never returns an error since Go 1.24.
	return t
}

// bucket is a connection's token-bucket rate limit, holding one second's
// worth of messages.
type bucket struct {
	rate, tokens float64
	last         time.Time
}

func newBucket(rate int) bucket {
	return bucket{rate: float64(rate), tokens: float64(rate), last: time.Now()}
}

// take spends one token and reports whether there was one.
func (b *bucket) take() bool {
	now := time.Now()
	b.tokens = min(b.rate, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
