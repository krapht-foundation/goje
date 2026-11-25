package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/krapht-foundation/goje/engine/lockstep"
	"github.com/krapht-foundation/goje/engine/matchlog"
	"github.com/krapht-foundation/goje/examples/skirmish"
	"github.com/krapht-foundation/goje/netlink"
)

// Every copy of an online match waits for both players' commands before it
// runs a tick. The lobby chooses how those players divide control; the
// lockstep package itself knows nothing about teams or orders.
const (
	onlineGame = "outpost"
	// onlineDelay is the lockstep delay. A command issued during tick t is applied at
	// tick t+onlineDelay+1, six ticks or 100 ms at 60 ticks per second, because the
	// frame for the first tick that can take it is sent a tick later.
	onlineDelay = 5
	// onlineHashInterval is how often the players compare state hashes.
	onlineHashInterval = matchlog.Interval
	// onlineStallLimit is how many consecutive ticks the match may wait for the
	// other player before it ends, ten seconds.
	onlineStallLimit = 600
	// onlineWaitShown is how many ticks of waiting pass before the screen says so.
	onlineWaitShown = 10
	onlineDialLimit = 10 * time.Second
	onlineRoomLen   = 6
	onlineRoomChars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
)

// onlineConfig says where the relay is and which room to join. The relay is a
// ws:// or wss:// URL without a path; both fields are empty when not configured.
type onlineConfig struct {
	relay, room string
}

// cleanOnlineConfig drops what cannot be used, so that a bad launch parameter
// leaves the game without online play instead of with a broken menu entry.
func cleanOnlineConfig(c onlineConfig) onlineConfig {
	u, err := url.Parse(c.relay)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return onlineConfig{}
	}
	c.relay = strings.TrimSuffix(c.relay, "/")
	if len(c.room) != onlineRoomLen || strings.Trim(c.room, onlineRoomChars) != "" {
		c.room = ""
	}
	return c
}

// onlineOwner is the player who commands a scout: the odd IDs belong to the
// first player and the even IDs to the second.
func onlineOwner(id uint64) int { return int((id + 1) % 2) }

// matchSetup precedes the lockstep Hello. The relay forwards it unchanged.
type matchSetup struct {
	mode    matchlog.Mode
	tactics bool
}

const setupMessage = 0x53

func decodeMatchSetup(msg []byte) (matchSetup, error) {
	if len(msg) != 3 || msg[0] != setupMessage || msg[1] > byte(matchlog.Versus) ||
		msg[2] > 1 || msg[1] == byte(matchlog.Versus) && msg[2] != 0 {
		return matchSetup{}, &lockstep.ProtocolError{Player: 0, Reason: "invalid match setup"}
	}
	return matchSetup{mode: matchlog.Mode(msg[1]), tactics: msg[2] == 1}, nil
}

func playerTeam(player int) skirmish.Team {
	if player == 1 {
		return skirmish.Guards
	}
	return skirmish.Scouts
}

func (w *outpostWorld) onlineSession(setup matchSetup, local int) *outpostSession {
	s := w.newSession(nil)
	s.tactics = setup.tactics
	if setup.mode == matchlog.Versus {
		s.board, s.versus, s.fogTeam = w.versusBoard, true, playerTeam(local)
	} else {
		s.owns = func(id uint64) bool { return onlineOwner(id) == local }
		s.foreign = func(id uint64) bool { return onlineOwner(id) != local }
	}
	return s
}

// onlineHashFault lets a smoke build change a state hash before it is compared,
// to prove that a divergence is caught. It is nil outside such builds.
var onlineHashFault func(tick, hash uint64) uint64

// onlineEnd is the error that ends an online match. It carries the reason that
// the end screen shows, and it never ends the program.
type onlineEnd struct{ err error }

func (e *onlineEnd) Error() string { return "online match ended: " + e.err.Error() }
func (e *onlineEnd) Unwrap() error { return e.err }

// outpostSync is what an online match gives the session: it decides whether the
// tick may run and applies every player's commands first, then takes the hash of
// the tick that ran.
type outpostSync interface {
	before(s *outpostSession) (run bool, err error)
	after(hash uint64) error
}

// onlineSync gates one player's copy of a match. It implements outpostSync.
type onlineSync struct {
	lock     *lockstep.Session
	local    int
	cmds     matchCommands
	buf      []byte
	waited   int // Consecutive ticks that could not run.
	recorder *matchlog.Recorder
	payloads [][]byte // Borrowed until Done advances the lockstep ring.
	logErr   error
	// fault changes a hash before it is sent. It is the package's onlineHashFault.
	fault func(tick, hash uint64) uint64
}

func newOnlineSync(lock *lockstep.Session, local int) *onlineSync {
	return &onlineSync{
		lock: lock, local: local,
		cmds:  newMatchCommands(),
		buf:   make([]byte, 0, skirmish.MaxCommandBytes),
		fault: onlineHashFault,
	}
}

// issue sends a local command to the other players, and to this player's own
// simulation, at the tick it is scheduled for. A command that does not fit in
// the frame is dropped; the frame holds more than a player can issue in a tick.
func (o *onlineSync) issue(c skirmish.Command) int {
	o.buf = c.AppendTo(o.buf[:0])
	if err := o.lock.Submit(o.buf); err != nil {
		return 0
	}
	return len(c.IDs)
}

// before applies the commands of every player for the tick, in player order,
// when all of them have arrived.
func (o *onlineSync) before(s *outpostSession) (bool, error) {
	if err := o.lock.Poll(); err != nil {
		return false, &onlineEnd{err}
	}
	payloads, ready := o.lock.Ready()
	if !ready {
		o.waited++
		if !o.lock.Running() && o.waited >= onlineStallLimit {
			return false, &onlineEnd{lockstep.ErrStalled}
		}
		return false, nil
	}
	o.waited = 0
	if err := o.cmds.apply(s, payloads); err != nil {
		return false, &onlineEnd{err}
	}
	o.payloads = payloads
	return true, nil
}

type matchCommands [2][]skirmish.Command

func newMatchCommands() matchCommands {
	return matchCommands{make([]skirmish.Command, 0, 8), make([]skirmish.Command, 0, 8)}
}

func (m *matchCommands) apply(s *outpostSession, payloads [][]byte) error {
	// Validate the whole tick before applying any orders, including when reading
	// a log. A bad command must not leave a half-applied tick behind.
	for player, payload := range payloads {
		cmds, err := skirmish.DecodeCommands(m[player][:0], payload)
		if err != nil {
			return &lockstep.ProtocolError{Player: player, Reason: "bad commands: " + err.Error()}
		}
		m[player] = cmds
		for i := range cmds {
			if err := checkOwnership(s, player, &cmds[i]); err != nil {
				return err
			}
		}
	}
	for _, cmds := range m {
		for _, cmd := range cmds {
			s.sim.Apply(cmd)
		}
	}
	return nil
}

// checkOwnership rejects IDs the player does not own. Fallen units are allowed
// because they may have died after an honest player issued the command.
func checkOwnership(s *outpostSession, player int, c *skirmish.Command) error {
	for _, id := range c.IDs {
		u, ok := s.sim.Unit(id)
		owns := ok && u.Team == skirmish.Scouts && onlineOwner(id) == player
		if s.versus {
			owns = ok && u.Team == playerTeam(player)
		}
		if !owns {
			return &lockstep.ProtocolError{
				Player: player,
				Reason: fmt.Sprintf("player %d commanded unit %d, which is not theirs", player, id),
			}
		}
	}
	return nil
}

// after sends the hash of the tick that ran, to be compared at checkpoints.
func (o *onlineSync) after(hash uint64) error {
	tick := o.lock.Tick() + 1
	if o.recorder != nil && o.logErr == nil {
		o.logErr = o.recorder.Append(o.payloads, hash)
	}
	o.payloads = nil
	if o.fault != nil {
		hash = o.fault(tick, hash)
	}
	if err := o.lock.Done(hash); err != nil {
		return &onlineEnd{err}
	}
	if tick%onlineHashInterval == 0 {
		slog.Info(
			"Online sync",
			slog.Uint64("tick", tick),
			slog.String("hash", strconv.FormatUint(hash, 16)),
		)
	}
	return nil
}

// waiting reports whether the match has been held up long enough to say so.
func (o *onlineSync) waiting() bool { return o.waited >= onlineWaitShown }

// onlineLobby is a connection being made, and then waiting for the other player.
type onlineLobby struct {
	host   bool
	cancel context.CancelFunc
	result chan onlineDialed
	conn   *netlink.Conn
	err    error
	setup  matchSetup
	waited int
	// announced is true once the room code has been logged.
	announced bool
}

type onlineDialed struct {
	conn *netlink.Conn
	err  error
}

// startLobby dials the relay in the background and returns at once, so the scene
// keeps running while the connection is made.
func startLobby(cfg onlineConfig, host bool) *onlineLobby {
	ctx, cancel := context.WithTimeout(context.Background(), onlineDialLimit)
	target := cfg.relay + "/new"
	if !host {
		target = cfg.relay + "/join?room=" + url.QueryEscape(cfg.room)
	}
	l := &onlineLobby{host: host, cancel: cancel, result: make(chan onlineDialed, 1)}
	go func() {
		conn, err := netlink.Dial(ctx, target)
		l.result <- onlineDialed{conn, err}
	}()
	return l
}

// poll reports whether the other player is present. It returns the first
// failure of the connection.
func (l *onlineLobby) poll() (ready bool, err error) {
	if l.err != nil {
		return false, l.err
	}
	if l.conn == nil {
		select {
		case d := <-l.result:
			l.cancel()
			if d.err != nil {
				l.err = d.err
				return false, l.err
			}
			l.conn = d.conn
		default:
			return false, nil
		}
	}
	if err = l.conn.Err(); err != nil {
		l.err = err
		return false, err
	}
	if !l.conn.PeerJoined() {
		return false, nil
	}
	if l.host {
		msg := [...]byte{setupMessage, byte(l.setup.mode), boolByte(l.setup.tactics)}
		err = l.conn.Send(msg[:])
		return err == nil, err
	}
	msg, ok, err := l.conn.Recv()
	if err != nil {
		return false, err
	}
	if !ok {
		l.waited++
		if l.waited >= onlineStallLimit {
			return false, lockstep.ErrStalled
		}
		return false, nil
	}
	l.setup, err = decodeMatchSetup(msg)
	return err == nil, err
}

// close abandons the lobby. The connection is kept when the match takes it over.
func (l *onlineLobby) close() {
	l.cancel()
	if l.conn == nil {
		// A dial that is still running closes its connection when it finishes.
		go func() {
			if d := <-l.result; d.conn != nil {
				noteClose("close the abandoned connection", d.conn.Close())
			}
		}()
		return
	}
	noteClose("close the lobby's connection", l.conn.Close())
}

// noteClose logs the failure to end an online connection or match. It is routine
// when the other side has already gone, so it is not an error of the game.
func noteClose(what string, err error) {
	if err != nil {
		slog.Debug(
			"Online cleanup failed",
			slog.String("step", what),
			slog.String("error", err.Error()),
		)
	}
}

// describeOnlineEnd turns the reason a match ended into the lines of the end screen.
func describeOnlineEnd(err error) []string {
	if desync, ok := errors.AsType[*lockstep.DesyncError](err); ok {
		return []string{
			"The two games no longer agree.",
			"Tick " + strconv.FormatUint(desync.Tick, 10) + ": yours " +
				strconv.FormatUint(desync.Local, 16),
			"theirs " + strconv.FormatUint(desync.Remote, 16),
		}
	}
	if protocol, ok := errors.AsType[*lockstep.ProtocolError](err); ok {
		return []string{"The other player sent invalid data.", clipLine(protocol.Reason)}
	}
	switch {
	case errors.Is(err, lockstep.ErrPeerLeft), errors.Is(err, netlink.ErrPeerLeft):
		return []string{"The other player left."}
	case errors.Is(err, lockstep.ErrStalled):
		return []string{"The other player stopped responding."}
	}
	return []string{"The connection was lost.", clipLine(err.Error())}
}

// clipLine shortens a line to fit the 320-pixel screen at the built-in font's width.
func clipLine(s string) string {
	const longest = 48
	if len(s) > longest {
		return s[:longest-3] + "..."
	}
	return s
}
