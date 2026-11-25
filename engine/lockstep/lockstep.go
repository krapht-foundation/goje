// Package lockstep keeps two to four copies of a deterministic simulation in
// step over a reliable, ordered message link. Each player submits an opaque
// payload for every tick, the payloads are scheduled a fixed number of ticks
// ahead, and the simulation advances only when every player's payload for the
// current tick has arrived. Players exchange state hashes at checkpoints, so a
// copy that diverges stops the match with the tick at which it was found.
//
// The package does no I/O and knows no game rules. The game decides what a
// payload holds and how to apply it, and a Link, such as netlink.Conn or the
// in-memory MemoryNet, carries the messages. The protocol is defined in
// specs/lockstep-m16.md, and docs/online.md shows how a scene uses it.
package lockstep

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// MaxPayload is the largest payload of one player for one tick, in bytes.
	MaxPayload = 1024
	// MaxPlayers is the most players a session holds.
	MaxPlayers = 4
	// Version is the protocol version that Hello carries.
	Version = 1
	// MaxDelay is the largest Config.Delay.
	MaxDelay = 30
	// maxGame is the longest game name in bytes.
	maxGame = 32
	// frameHeader is the size of a Frame before its payload: type, player,
	// tick, and payload length.
	frameHeader = 1 + 1 + 8 + 2
	// maxMessage is the largest legal message, a Frame with a full payload.
	maxMessage = frameHeader + MaxPayload
	hashSize   = 1 + 1 + 8 + 8
	byeSize    = 1 + 1
)

// Message types, the first byte of every message.
const (
	msgHello byte = 1 + iota
	msgFrame
	msgHash
	msgBye
)

var (
	// ErrPayloadTooLarge reports that Submit would make the pending payload
	// longer than MaxPayload.
	ErrPayloadTooLarge = errors.New("lockstep: payload larger than MaxPayload")
	// ErrStalled reports that Ready found a player's payload missing on
	// Config.StallLimit consecutive calls.
	ErrStalled = errors.New("lockstep: a player's frames stopped arriving")
	// ErrPeerLeft reports that another player sent Bye.
	ErrPeerLeft = errors.New("lockstep: a player left the match")
	errNotReady = errors.New("lockstep: Done called before Ready returned every payload")
)

// DesyncError reports that a player's state hash for a checkpoint tick differs
// from the local one, so the two copies of the simulation have diverged.
type DesyncError struct {
	Tick, Local, Remote uint64
	Player              int
}

func (e *DesyncError) Error() string {
	return fmt.Sprintf("lockstep: desync at tick %d: local hash %016x, player %d hash %016x",
		e.Tick, e.Local, e.Player, e.Remote)
}

// ProtocolError reports a message that breaks the protocol: malformed, out of
// order, out of range, or disagreeing with the local Hello. Player is the
// sender's claimed index, or -1 when the message names none.
type ProtocolError struct {
	Player int
	Reason string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("lockstep: player %d: %s", e.Player, e.Reason)
}

// Link carries messages between the players of a session. It must be reliable
// and ordered, and a message sent once must reach every other player, as a
// relay or a broadcast does.
type Link interface {
	// Send sends one message and must not retain msg after it returns.
	Send(msg []byte) error
	// Recv returns the next message without blocking, or ok false when none
	// has arrived. The message is valid only until the next Recv.
	Recv() (msg []byte, ok bool, err error)
}

// Config describes a session. Every player must use the same Game, Content,
// Players, Delay, and HashInterval; Hello checks the first three.
type Config struct {
	// Game names the game, 1 to 32 bytes.
	Game string
	// Content identifies the match content, such as replay.ContentHash.
	Content uint64
	// Players is the number of players, 2 to MaxPlayers.
	Players int
	// Local is this player's index, below Players.
	Local int
	// Delay is the number of ticks, 0 to MaxDelay, between the tick during
	// which a payload is submitted and the tick before the one it is applied
	// at: a payload submitted during tick t is applied at tick t+Delay+1.
	Delay int
	// HashInterval is the number of ticks between checkpoints, at least 1.
	HashInterval int
	// StallLimit is the number of consecutive Ready calls without every
	// payload after which the session ends with ErrStalled, at least 1.
	StallLimit int
}

// checkpoint holds the hashes known for one checkpoint tick until every
// player's hash has been compared.
type checkpoint struct {
	tick   uint64 // 0 marks a free slot; checkpoints are at least HashInterval.
	local  uint64
	remote [MaxPlayers]uint64
	have   uint8 // bit p set when player p's hash is known, including Local.
}

// Session is one player's view of a lockstep match. It is driven from one
// goroutine, normally the scene's Update. See the package comment for its
// place and specs/lockstep-m16.md for its rules.
type Session struct {
	cfg  Config
	link Link
	// window is the number of ticks of frames and checkpoints held at once.
	// A peer can be at most Delay+1 ticks ahead of the local tick and, while
	// the local tick waits for it, at most Delay+1 behind, so 2·Delay+2 slots
	// hold every tick that is not yet resolved; see the spec's ring sizes.
	window   int
	tick     uint64
	agreed   bool
	helloed  uint8              // bit p set when player p's Hello was received, including Local.
	next     [MaxPlayers]uint64 // next frame tick expected from each player
	nextHash [MaxPlayers]uint64 // next checkpoint tick expected from each player
	frames   [MaxPlayers][][]byte
	ready    [][]byte
	pending  []byte
	checks   []checkpoint
	msg      []byte
	stalls   int
	err      error
}

// New validates cfg, creates a session, and sends the local Hello on link.
// The session allocates everything it needs here.
func New(cfg Config, link Link) (*Session, error) {
	switch {
	case cfg.Players < 2 || cfg.Players > MaxPlayers:
		return nil, fmt.Errorf("lockstep: %d players, want 2 to %d", cfg.Players, MaxPlayers)
	case cfg.Local < 0 || cfg.Local >= cfg.Players:
		return nil, fmt.Errorf("lockstep: local player %d of %d", cfg.Local, cfg.Players)
	case cfg.Delay < 0 || cfg.Delay > MaxDelay:
		return nil, fmt.Errorf("lockstep: delay %d, want 0 to %d", cfg.Delay, MaxDelay)
	case cfg.HashInterval < 1:
		return nil, fmt.Errorf("lockstep: hash interval %d, want at least 1", cfg.HashInterval)
	case cfg.StallLimit < 1:
		return nil, fmt.Errorf("lockstep: stall limit %d, want at least 1", cfg.StallLimit)
	case cfg.Game == "" || len(cfg.Game) > maxGame:
		return nil, fmt.Errorf("lockstep: game name of %d bytes, want 1 to %d", len(cfg.Game), maxGame)
	}
	s := &Session{
		cfg:     cfg,
		link:    link,
		window:  2*cfg.Delay + 2,
		helloed: 1 << cfg.Local,
		ready:   make([][]byte, cfg.Players),
		pending: make([]byte, 0, MaxPayload),
		msg:     make([]byte, 0, maxMessage),
	}
	s.checks = make([]checkpoint, s.window)
	for p := range cfg.Players {
		s.next[p] = uint64(cfg.Delay)                // #nosec G115 -- Delay is 0..MaxDelay.
		s.nextHash[p] = uint64(cfg.HashInterval)     // #nosec G115 -- HashInterval is positive.
		backing := make([]byte, s.window*MaxPayload) // one block per player
		s.frames[p] = make([][]byte, s.window)
		for i := range s.frames[p] {
			s.frames[p][i] = backing[i*MaxPayload : i*MaxPayload : (i+1)*MaxPayload]
		}
	}
	m := []byte{msgHello, Version, byte(len(cfg.Game))} // #nosec G115 -- At most maxGame.
	m = append(m, cfg.Game...)
	m = binary.LittleEndian.AppendUint64(m, cfg.Content)
	m = append(m, byte(cfg.Players), byte(cfg.Local)) // #nosec G115 -- Both below MaxPlayers.
	if err := link.Send(m); err != nil {
		return nil, fmt.Errorf("lockstep: send hello: %w", err)
	}
	return s, nil
}

// Tick returns the tick whose payloads Ready returns next.
func (s *Session) Tick() uint64 { return s.tick }

// Running reports whether every player's Hello has been received and agreed.
func (s *Session) Running() bool { return s.agreed }

// Submit appends b to the local payload of the next frame that Done sends. If
// the payload would exceed MaxPayload, nothing is appended and Submit returns
// ErrPayloadTooLarge.
func (s *Session) Submit(b []byte) error {
	if len(s.pending)+len(b) > MaxPayload {
		return ErrPayloadTooLarge
	}
	s.pending = append(s.pending, b...)
	return nil
}

// Poll reads every message that has arrived on the link. It returns the error
// that ended the session, if any, and the same error on every later call.
func (s *Session) Poll() error {
	for s.err == nil {
		msg, ok, err := s.link.Recv()
		if err != nil {
			s.err = fmt.Errorf("lockstep: receive: %w", err)
			break
		}
		if !ok {
			break
		}
		s.err = s.handle(msg)
	}
	return s.err
}

// Ready returns every player's payload for Tick, indexed by player, when all
// of them are present. The slices are valid until Done. While the session is
// running, StallLimit consecutive calls without every payload end it with
// ErrStalled, which the next Poll or Done returns.
func (s *Session) Ready() ([][]byte, bool) {
	if s.err != nil || !s.agreed {
		return nil, false
	}
	for p := range s.cfg.Players {
		if s.next[p] <= s.tick {
			s.stalls++
			if s.stalls >= s.cfg.StallLimit {
				s.err = ErrStalled
			}
			return nil, false
		}
	}
	s.stalls = 0
	for p := range s.cfg.Players {
		if s.tick < uint64(s.cfg.Delay) { // #nosec G115 -- Delay is 0..MaxDelay.
			s.ready[p] = nil // the first Delay ticks are agreed empty
		} else {
			s.ready[p] = s.frames[p][s.slot(s.tick)]
		}
	}
	return s.ready, true
}

// Done records that the payloads of Tick were applied and the simulation was
// stepped, with hash as the resulting state hash. It advances Tick; when the
// new tick is a checkpoint it sends hash and compares it with the other
// players' hashes, and then it sends the pending payload as the local frame
// for Tick+Delay and clears it.
func (s *Session) Done(hash uint64) error {
	if s.err != nil {
		return s.err
	}
	for p := range s.cfg.Players {
		if !s.agreed || s.next[p] <= s.tick {
			return errNotReady
		}
	}
	s.tick++
	if s.tick%uint64(s.cfg.HashInterval) == 0 { // #nosec G115 -- HashInterval is positive.
		m := s.msg[:0]
		m = append(m, msgHash, byte(s.cfg.Local)) // #nosec G115 -- Local < MaxPlayers.
		m = binary.LittleEndian.AppendUint64(m, s.tick)
		m = binary.LittleEndian.AppendUint64(m, hash)
		if err := s.link.Send(m); err != nil {
			s.err = fmt.Errorf("lockstep: send hash: %w", err)
			return s.err
		}
		if s.err = s.check(s.cfg.Local, s.tick, hash); s.err != nil {
			return s.err
		}
	}
	s.err = s.sendFrame()
	return s.err
}

// Leave tells the other players that this one is leaving by sending Bye.
func (s *Session) Leave() error {
	// #nosec G115 -- Local < MaxPlayers.
	if err := s.link.Send(append(s.msg[:0], msgBye, byte(s.cfg.Local))); err != nil {
		return fmt.Errorf("lockstep: send bye: %w", err)
	}
	return nil
}

// slot returns the ring index of a tick's frame.
func (s *Session) slot(tick uint64) int {
	return int(tick % uint64(s.window)) // #nosec G115 -- window is at most 2·MaxDelay+2.
}

// sendFrame sends the pending payload as the local frame for the next local
// frame tick, keeps a copy in the local ring, and clears the payload.
func (s *Session) sendFrame() error {
	l := s.cfg.Local
	tick := s.next[l]
	n := uint16(len(s.pending)) // #nosec G115 -- At most MaxPayload.
	m := s.msg[:0]
	m = append(m, msgFrame, byte(l)) // #nosec G115 -- Local < MaxPlayers.
	m = binary.LittleEndian.AppendUint64(m, tick)
	m = binary.LittleEndian.AppendUint16(m, n)
	m = append(m, s.pending...)
	if err := s.link.Send(m); err != nil {
		return fmt.Errorf("lockstep: send frame: %w", err)
	}
	i := s.slot(tick)
	s.frames[l][i] = append(s.frames[l][i][:0], s.pending...)
	s.pending = s.pending[:0]
	s.next[l]++
	return nil
}

// handle decodes and applies one message from the link. Every field is
// checked against its limit before it is used, because the bytes come from
// another machine.
func (s *Session) handle(msg []byte) error {
	if len(msg) < 2 {
		return &ProtocolError{Player: -1, Reason: fmt.Sprintf("message of %d bytes", len(msg))}
	}
	if len(msg) > maxMessage {
		return &ProtocolError{Player: -1, Reason: fmt.Sprintf("message of %d bytes", len(msg))}
	}
	if msg[0] == msgHello {
		return s.hello(msg)
	}
	p := int(msg[1])
	if p >= s.cfg.Players || p == s.cfg.Local || s.helloed&(1<<p) == 0 {
		return &ProtocolError{Player: p, Reason: "message from a player without a Hello"}
	}
	switch msg[0] {
	case msgFrame:
		return s.frame(p, msg)
	case msgHash:
		if len(msg) != hashSize {
			return &ProtocolError{Player: p, Reason: "malformed Hash"}
		}
		tick := binary.LittleEndian.Uint64(msg[2:])
		// A peer sends the hash of tick c only after it applied tick c-1,
		// which needs the local frame for c-1, so c is at most
		// Tick+Delay+1; this bounds the checkpoint ring.
		if tick != s.nextHash[p] ||
			tick > s.tick+uint64(s.cfg.Delay)+1 { // #nosec G115 -- Delay is 0..MaxDelay.
			return &ProtocolError{Player: p, Reason: fmt.Sprintf("Hash for tick %d", tick)}
		}
		s.nextHash[p] += uint64(s.cfg.HashInterval) // #nosec G115 -- HashInterval is positive.
		return s.check(p, tick, binary.LittleEndian.Uint64(msg[10:]))
	case msgBye:
		if len(msg) != byeSize {
			return &ProtocolError{Player: p, Reason: "malformed Bye"}
		}
		return fmt.Errorf("lockstep: player %d: %w", p, ErrPeerLeft)
	default:
		return &ProtocolError{Player: p, Reason: fmt.Sprintf("unknown message type %d", msg[0])}
	}
}

// hello checks a Hello against the local configuration and starts the match
// when every player's Hello has agreed.
func (s *Session) hello(msg []byte) error {
	// Fixed fields: type, version, name length, content, players, player.
	if len(msg) < 3 || len(msg) != 3+int(msg[2])+8+2 {
		return &ProtocolError{Player: -1, Reason: "malformed Hello"}
	}
	n := int(msg[2])
	p := int(msg[len(msg)-1])
	switch {
	case msg[1] != Version:
		return &ProtocolError{
			Player: p,
			Reason: fmt.Sprintf("protocol version %d, want %d", msg[1], Version),
		}
	case string(msg[3:3+n]) != s.cfg.Game:
		return &ProtocolError{Player: p, Reason: "different game"}
	case binary.LittleEndian.Uint64(msg[3+n:]) != s.cfg.Content:
		return &ProtocolError{Player: p, Reason: "different content"}
	case int(msg[len(msg)-2]) != s.cfg.Players:
		return &ProtocolError{
			Player: p,
			Reason: fmt.Sprintf("%d players, want %d", msg[len(msg)-2], s.cfg.Players),
		}
	case p >= s.cfg.Players || s.helloed&(1<<p) != 0:
		return &ProtocolError{Player: p, Reason: "player index out of range or taken"}
	}
	s.helloed |= 1 << p
	if s.helloed != 1<<s.cfg.Players-1 {
		return nil
	}
	// Ticks 0..Delay-1 are empty for everyone, so the first frame is for
	// tick Delay and is sent as soon as the match starts.
	s.agreed = true
	return s.sendFrame()
}

// frame stores a peer's frame for its next expected tick.
func (s *Session) frame(p int, msg []byte) error {
	if len(msg) < frameHeader {
		return &ProtocolError{Player: p, Reason: "malformed Frame"}
	}
	tick := binary.LittleEndian.Uint64(msg[2:])
	n := int(binary.LittleEndian.Uint16(msg[10:]))
	if len(msg) != frameHeader+n {
		return &ProtocolError{Player: p, Reason: "malformed Frame"}
	}
	delay := uint64(s.cfg.Delay) // #nosec G115 -- Delay is 0..MaxDelay.
	// The ring holds ticks Tick..Tick+window-1. A peer sends the frame for
	// tick x only after its Hash for tick x-Delay, so requiring that Hash
	// keeps the checkpoint ring from overflowing too.
	switch {
	case tick != s.next[p]:
		return &ProtocolError{
			Player: p,
			Reason: fmt.Sprintf("Frame for tick %d, want %d", tick, s.next[p]),
		}
	case tick-s.tick >= uint64(s.window): // #nosec G115 -- window is small.
		return &ProtocolError{
			Player: p,
			Reason: fmt.Sprintf("Frame for tick %d is too far ahead", tick),
		}
	case s.nextHash[p]+delay <= tick:
		return &ProtocolError{Player: p, Reason: fmt.Sprintf("Frame for tick %d before its Hash", tick)}
	}
	i := s.slot(tick)
	s.frames[p][i] = append(s.frames[p][i][:0], msg[frameHeader:]...)
	s.next[p]++
	return nil
}

// check records player p's hash for a checkpoint tick and compares it with the
// local hash once both are known. A slot is freed when every player's hash
// for its tick is known.
func (s *Session) check(p int, tick, hash uint64) error {
	c := &s.checks[int(tick/uint64(s.cfg.HashInterval)%uint64(s.window))] // #nosec G115 -- Small values.
	if c.tick != tick {
		if c.tick != 0 {
			// The frame and Hash bounds make this unreachable for any
			// sequence of messages; it is kept so a bug cannot corrupt a
			// comparison silently.
			return &ProtocolError{Player: p, Reason: fmt.Sprintf("checkpoint %d overflows", tick)}
		}
		*c = checkpoint{tick: tick}
	}
	c.have |= 1 << p
	if p == s.cfg.Local {
		c.local = hash
	} else {
		c.remote[p] = hash
	}
	if c.have&(1<<s.cfg.Local) != 0 {
		for q := range s.cfg.Players {
			if q != s.cfg.Local && c.have&(1<<q) != 0 && c.remote[q] != c.local {
				return &DesyncError{Tick: tick, Local: c.local, Remote: c.remote[q], Player: q}
			}
		}
	}
	if c.have == 1<<s.cfg.Players-1 {
		c.tick = 0
	}
	return nil
}
