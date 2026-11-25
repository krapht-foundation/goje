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
	"github.com/krapht-foundation/goje/examples/skirmish"
	"github.com/krapht-foundation/goje/netlink"
)

// Online play is the cooperative outpost of RFC 0017: two players command the six
// scouts between them, the guards stay under the simulation's control, and every
// tick runs on both machines only when both players' commands for it have
// arrived. This file holds the lobby, the lockstep gate of a match, and the rules
// for what a player may command. See specs/lockstep-m16.md and specs/outpost-m14.md.
const (
	onlineGame = "outpost"
	// onlineDelay is the lockstep delay. A command issued during tick t is applied at
	// tick t+onlineDelay+1, six ticks or 100 ms at 60 ticks per second, because the
	// frame for the first tick that can take it is sent a tick later.
	onlineDelay = 5
	// onlineHashInterval is how often the players compare state hashes.
	onlineHashInterval = 60
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
	waiting() bool
}

// onlineSync gates one player's copy of a match. It implements outpostSync.
type onlineSync struct {
	lock   *lockstep.Session
	local  int
	cmds   []skirmish.Command
	buf    []byte
	waited int // Consecutive ticks that could not run.
	// fault changes a hash before it is sent. It is the package's onlineHashFault.
	fault func(tick, hash uint64) uint64
}

func newOnlineSync(lock *lockstep.Session, local int) *onlineSync {
	return &onlineSync{
		lock: lock, local: local,
		cmds:  make([]skirmish.Command, 0, 8),
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
		return false, nil
	}
	o.waited = 0
	for player, payload := range payloads {
		cmds, err := skirmish.DecodeCommands(o.cmds[:0], payload)
		if err != nil {
			return false, &onlineEnd{fmt.Errorf("player %d sent bad commands: %w", player, err)}
		}
		o.cmds = cmds
		for i := range cmds {
			if err := checkOwnership(s, player, &cmds[i]); err != nil {
				return false, &onlineEnd{err}
			}
			s.sim.Apply(cmds[i])
		}
	}
	return true, nil
}

// checkOwnership rejects a command that names a unit its sender cannot command:
// one that does not exist, is not a scout, or belongs to the other player. A scout
// that has fallen since the command was issued is allowed, because the sender
// could not have known.
func checkOwnership(s *outpostSession, player int, c *skirmish.Command) error {
	for _, id := range c.IDs {
		u, ok := s.sim.Unit(id)
		if !ok || u.Team != skirmish.Scouts || onlineOwner(id) != player {
			return fmt.Errorf("player %d commanded unit %d, which is not theirs", player, id)
		}
	}
	return nil
}

// after sends the hash of the tick that ran, to be compared at checkpoints.
func (o *onlineSync) after(hash uint64) error {
	tick := o.lock.Tick() + 1
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
	if err := l.conn.Err(); err != nil {
		l.err = err
		return false, err
	}
	return l.conn.PeerJoined(), nil
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
