// Package relay is the server behind cmd/relay. It pairs two players in a
// room identified by a short code and forwards each player's binary messages
// to the other unchanged, without reading them. It knows nothing about games.
//
// The routes, the wire contract, the limits, and the deployment notes are in
// specs/relay-m16.md. Connections use the WebSocket subset of netlink/ws.
package relay

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/krapht-foundation/goje/netlink/ws"
)

// Message types the relay sends, defined by specs/relay-m16.md. The netlink
// client decodes the same values.
const (
	msgWelcome    = 0x01
	msgPeerJoined = 0x02
	msgPeerLeft   = 0x03
	msgData       = 0x04
)

// codeAlphabet leaves out letters and digits that look alike: I, L, O, 0, 1.
const (
	codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLen      = 6
)

// Config holds the relay's limits. A zero field takes the default given in
// its comment.
type Config struct {
	// MaxRooms bounds open rooms of both versions, including rooms waiting
	// for a guest; 256.
	MaxRooms int
	// Origins is the allow-list for browser Origin headers; empty allows all.
	Origins []string
	// Idle closes a paired room after this long without a message; 30 s.
	Idle time.Duration
	// JoinWindow closes a room that no guest joined within it; 2 min.
	JoinWindow time.Duration
	// Rate is each connection's sustained limit in messages per second, with
	// bursts of up to one second's worth; 240.
	Rate int
	// MaxMessage is the largest message payload a client may send, in bytes;
	// 2048. A v2 message adds a 9-byte header to it.
	MaxMessage int
	// Rejoin holds a v2 player's slot for this long after its transport is
	// lost; 30 s.
	Rejoin time.Duration
	// MaxPending bounds /v2/resume connections that have not yet presented
	// their credential; 64.
	MaxPending int
	// ViewQueue bounds the messages waiting for a spectator; a spectator
	// that falls this far behind is disconnected; 64.
	ViewQueue int
	// Logger receives room events, never message contents; slog.Default().
	Logger *slog.Logger
}

func (c *Config) setDefaults() {
	if c.MaxRooms <= 0 {
		c.MaxRooms = 256
	}
	if c.Idle <= 0 {
		c.Idle = 30 * time.Second
	}
	if c.JoinWindow <= 0 {
		c.JoinWindow = 2 * time.Minute
	}
	if c.Rate <= 0 {
		c.Rate = 240
	}
	if c.MaxMessage <= 0 {
		c.MaxMessage = 2048
	}
	if c.Rejoin <= 0 {
		c.Rejoin = 30 * time.Second
	}
	if c.MaxPending <= 0 {
		c.MaxPending = 64
	}
	if c.ViewQueue <= 0 {
		c.ViewQueue = 64
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Server is the relay's HTTP handler. Use New to create one.
type Server struct {
	cfg   Config
	mux   *http.ServeMux
	start time.Time

	mu     sync.Mutex
	rooms  map[string]*room
	rooms2 map[string]*room2 // v2 rooms by invitation code
	views  map[string]*room2 // v2 rooms by spectator code
	tokens map[[tokenLen]byte]tokenRef
	// pending counts resume connections not yet authenticated.
	pending int
	closed  bool
}

// room is one match's meeting point. Fields other than the atomics are
// guarded by Server.mu.
type room struct {
	code string
	// conns[0] is the host and conns[1] the guest; each is set once, and the
	// guest before paired becomes true.
	conns  [2]*ws.Conn
	guest  bool // The guest slot is taken or being upgraded.
	closed bool
	timer  *time.Timer
	paired atomic.Bool
	// last is the time of the last forwarded message, as time since the
	// server started so the comparison is monotonic.
	last atomic.Int64
}

// New returns a relay with cfg's limits.
func New(cfg Config) *Server {
	cfg.setDefaults()
	s := &Server{
		cfg: cfg, mux: http.NewServeMux(), start: time.Now(), rooms: map[string]*room{},
		rooms2: map[string]*room2{}, views: map[string]*room2{}, tokens: map[[tokenLen]byte]tokenRef{},
	}
	s.mux.HandleFunc("GET /new", s.handleNew)
	s.mux.HandleFunc("GET /join", s.handleJoin)
	s.mux.HandleFunc("GET /v2/new", s.handleNew2)
	s.mux.HandleFunc("GET /v2/join", s.handleJoin2)
	s.mux.HandleFunc("GET /v2/watch", s.handleWatch)
	s.mux.HandleFunc("GET /v2/resume", s.handleResume)
	return s
}

// ServeHTTP routes GET /new, GET /join?room=CODE, and the v2 routes of
// specs/relay-m18.md.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Close closes every room and refuses new ones. The connections' goroutines
// return soon after. http.Server.Shutdown does not track upgraded
// connections, so a graceful shutdown calls both.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	rooms := make([]*room, 0, len(s.rooms))
	for _, rm := range s.rooms {
		rooms = append(rooms, rm)
	}
	rooms2 := make([]*room2, 0, len(s.rooms2))
	for _, rm := range s.rooms2 {
		rooms2 = append(rooms2, rm)
	}
	s.mu.Unlock()
	for _, rm := range rooms {
		s.closeRoom(rm, -1, "shutdown")
	}
	for _, rm := range rooms2 {
		s.closeRoom2(rm, "shutdown")
	}
}

func (s *Server) wsOptions() ws.Options {
	// No read deadline: the room's timer closes idle and unjoined rooms.
	return ws.Options{Limit: s.cfg.MaxMessage, Origins: s.cfg.Origins}
}

func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.closed || len(s.rooms)+len(s.rooms2) >= s.cfg.MaxRooms {
		s.mu.Unlock()
		http.Error(w, "the relay is full", http.StatusServiceUnavailable)
		return
	}
	code, err := s.newCodeLocked()
	if err != nil {
		s.mu.Unlock()
		s.cfg.Logger.Error("room code", "err", err)
		http.Error(w, "no room code available", http.StatusServiceUnavailable)
		return
	}
	// The room is reserved before the upgrade so the room count stays bounded,
	// but it cannot be joined until its host has a connection.
	rm := &room{code: code}
	s.rooms[code] = rm
	s.mu.Unlock()

	c, err := ws.Accept(w, r, s.wsOptions())
	if err != nil {
		s.cfg.Logger.Info("upgrade rejected", "route", "new", "err", err)
		s.closeRoom(rm, -1, "upgrade failed")
		return
	}
	s.mu.Lock()
	if rm.closed {
		s.mu.Unlock()
		closeQuietly(c)
		return
	}
	// The Welcome is written under the lock so that a guest, who can only
	// learn the code from it, never finds the room without its host. A few
	// bytes on a fresh connection fit in the socket buffer, so the write does
	// not block other rooms.
	if err := c.WriteMessage(append([]byte{msgWelcome, 0}, code...)); err != nil {
		s.mu.Unlock()
		closeQuietly(c)
		s.closeRoom(rm, -1, "welcome failed")
		return
	}
	rm.conns[0] = c
	rm.timer = time.AfterFunc(s.cfg.JoinWindow, func() { s.expire(rm) })
	s.mu.Unlock()
	s.cfg.Logger.Info("room opened", "room", code)
	s.serve(rm, 0)
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(r.URL.Query().Get("room"))
	s.mu.Lock()
	rm := s.rooms[code]
	if rm == nil || rm.conns[0] == nil {
		s.mu.Unlock()
		http.Error(w, "no such room", http.StatusNotFound)
		return
	}
	if rm.guest {
		s.mu.Unlock()
		http.Error(w, "the room is full", http.StatusConflict)
		return
	}
	rm.guest = true
	s.mu.Unlock()

	c, err := ws.Accept(w, r, s.wsOptions())
	s.mu.Lock()
	if err != nil {
		rm.guest = false
		s.mu.Unlock()
		s.cfg.Logger.Info("upgrade rejected", "route", "join", "err", err)
		return
	}
	if rm.closed {
		s.mu.Unlock()
		closeQuietly(c)
		return
	}
	rm.conns[1] = c
	s.mu.Unlock()

	// The host's messages are forwarded only once paired is set, so the guest
	// receives its Welcome first. paired is set before PeerJoined goes out,
	// or a host that answers PeerJoined at once could have its first message
	// dropped as sent before pairing. The guest's messages are read only
	// after PeerJoined is written, so the host learns of the guest first.
	if err := c.WriteMessage(append([]byte{msgWelcome, 1}, code...)); err != nil {
		s.closeRoom(rm, -1, "welcome failed")
		return
	}
	rm.last.Store(int64(time.Since(s.start)))
	rm.paired.Store(true)
	if err := rm.conns[0].WriteMessage([]byte{msgPeerJoined}); err != nil {
		s.closeRoom(rm, -1, "host unreachable")
		return
	}
	s.mu.Lock()
	if !rm.closed {
		rm.timer.Reset(s.cfg.Idle)
	}
	s.mu.Unlock()
	s.cfg.Logger.Info("room paired", "room", code)
	s.serve(rm, 1)
}

// serve reads one player's messages and forwards them to the other until
// either connection fails or a limit is broken, then closes the room. It runs
// on the request's goroutine.
func (s *Server) serve(rm *room, slot int) {
	c := rm.conns[slot]
	// The forwarded message is the data type followed by the payload, so the
	// payload is read directly after the type byte.
	buf := make([]byte, 1+s.cfg.MaxMessage)
	buf[0] = msgData
	rate := float64(s.cfg.Rate)
	tokens, last := rate, time.Now()
	leaver, reason := slot, "left"
	for {
		msg, err := c.ReadMessage(buf[1:])
		if err != nil {
			reason = err.Error()
			break
		}
		if len(msg) == 0 {
			reason = "empty message"
			break
		}
		now := time.Now()
		tokens = min(rate, tokens+now.Sub(last).Seconds()*rate)
		last = now
		if tokens < 1 {
			reason = "rate limit"
			break
		}
		tokens--
		// Messages sent before the guest has joined have no recipient.
		if !rm.paired.Load() {
			continue
		}
		rm.last.Store(int64(time.Since(s.start)))
		if err := rm.conns[1-slot].WriteMessage(buf[:1+len(msg)]); err != nil {
			leaver, reason = 1-slot, err.Error()
			break
		}
	}
	s.closeRoom(rm, leaver, reason)
}

// expire runs from the room's timer. It closes a room that was not joined in
// time or has been idle, and otherwise rearms the timer for the rest of the
// idle period.
func (s *Server) expire(rm *room) {
	s.mu.Lock()
	if rm.closed {
		s.mu.Unlock()
		return
	}
	if rm.paired.Load() {
		idle := time.Since(s.start) - time.Duration(rm.last.Load())
		if idle < s.cfg.Idle {
			rm.timer.Reset(s.cfg.Idle - idle)
			s.mu.Unlock()
			return
		}
	}
	s.mu.Unlock()
	s.closeRoom(rm, -1, "expired")
}

// closeRoom removes a room and closes both connections. When a player left a
// paired room, the other player is told first. leaver is -1 when the relay
// closes the room itself.
func (s *Server) closeRoom(rm *room, leaver int, reason string) {
	s.mu.Lock()
	if rm.closed {
		s.mu.Unlock()
		return
	}
	rm.closed = true
	delete(s.rooms, rm.code)
	if rm.timer != nil {
		rm.timer.Stop()
	}
	conns := rm.conns
	s.mu.Unlock()
	if leaver >= 0 && rm.paired.Load() {
		//nolint:errcheck // The room is closing; a peer that cannot be told is closed below.
		_ = conns[1-leaver].WriteMessage([]byte{msgPeerLeft})
	}
	for _, c := range conns {
		if c != nil {
			closeQuietly(c)
		}
	}
	s.cfg.Logger.Info("room closed", "room", rm.code, "reason", reason)
}

// newCodeLocked returns an unused room code. The caller holds s.mu.
func (s *Server) newCodeLocked() (string, error) {
	// With 31⁶ codes and at most a few hundred rooms, a collision is rare,
	// so a few attempts suffice.
	for range 8 {
		var b [codeLen]byte
		for i := range b {
			b[i] = codeAlphabet[randomIndex(len(codeAlphabet))]
		}
		// v1 codes, v2 invitation codes, and v2 spectator codes never collide.
		if code := string(b[:]); s.rooms[code] == nil && s.rooms2[code] == nil && s.views[code] == nil {
			return code, nil
		}
	}
	return "", fmt.Errorf("relay: no unused room code after 8 attempts with %d rooms",
		len(s.rooms)+len(s.rooms2))
}

// randomIndex returns a uniform random index below n, which is at most 256.
func randomIndex(n int) int {
	// Bytes at or above the largest multiple of n are rejected so that every
	// index is equally likely.
	limit := 256 - 256%n
	for {
		var b [1]byte
		_, _ = rand.Read(b[:]) // It never returns an error since Go 1.24.
		if int(b[0]) < limit {
			return int(b[0]) % n
		}
	}
}

func closeQuietly(c *ws.Conn) {
	//nolint:errcheck // The connection is being discarded; there is nothing to recover.
	_ = c.Close()
}
