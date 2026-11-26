package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/lockstep"
	"github.com/krapht-foundation/goje/engine/matchlog"
	"github.com/krapht-foundation/goje/netlink"
)

const (
	onlineReconnectLimit = 30 * time.Second
	onlineRebuildLimit   = 2 * time.Minute
	// A silent WebSocket can need 15 seconds to be declared lost by the relay.
	// Do not let lockstep's timeout defeat the transport's hold and restore.
	onlineLiveStall = 1200
)

func isTransportChange(err error) bool {
	return errors.Is(err, netlink.ErrLost) || errors.Is(err, netlink.ErrEpochChanged)
}

func onlineLock(header matchlog.Header, slot int, link lockstep.Link) (*lockstep.Session, error) {
	return lockstep.New(lockstep.Config{
		Game: onlineGame, Content: header.Content, Players: header.Players, Local: slot,
		Delay: onlineDelay, HashInterval: onlineHashInterval, StallLimit: onlineLiveStall,
	}, link)
}

// A small local cache avoids replaying the whole history after a late drop.
// Snapshots never cross the link, and the saved hash and the replay's final
// hash are checked independently.
type recoverySnapshot struct {
	tick int
	data []byte
}

// The match remains playable if the log fills. Only rejoin and new history
// stop; an existing watcher can still drain the verified prefix.
func (o *onlineSync) canRecover() bool { return o.link != nil && o.logErr == nil }

func (o *onlineSync) keepCheckpoint(s *outpostSession) error {
	if !o.canRecover() || o.recorder.Ticks() == 0 {
		return nil
	}
	tick := o.recorder.Ticks()
	if tick%matchlog.Interval != 0 {
		return nil
	}
	entry := &o.saved[(tick/matchlog.Interval)%len(o.saved)]
	if entry.tick == tick {
		return nil
	}
	data, err := s.sim.Snapshot()
	if err != nil {
		return err
	}
	*entry = recoverySnapshot{tick, data}
	return nil
}

func (o *onlineSync) snapshotBefore(tick int) recoverySnapshot {
	var best recoverySnapshot
	for _, saved := range o.saved {
		if saved.tick > best.tick && saved.tick < tick {
			best = saved
		}
	}
	return best
}

type outpostRecovery struct {
	deadline time.Time
	cancel   context.CancelFunc
	dial     chan onlineDialed
	link     *outpostLink
	offered  bool
	choice   onlineProof
	play     *matchWatch
	match    *engine.Runtime
	session  *outpostSession
	ready    bool
	remote   bool
}

func (r *outpostRecovery) dropEpoch() {
	if r.match != nil {
		noteClose("stop the canceled rebuild", r.match.Stop())
	}
	r.link, r.play, r.match, r.session = nil, nil, nil, nil
	r.offered, r.ready, r.remote = false, false, false
}

func (r *outpostRecovery) close() {
	r.dropEpoch()
	if r.dial != nil {
		r.cancel()
		go func() {
			if d := <-r.dial; d.conn != nil {
				noteClose("end the canceled reconnect", d.conn.EndRoom())
				noteClose("close the canceled reconnect", d.conn.Close())
			}
		}()
	}
}

func (r *outpostRecovery) startDial(url string, token [32]byte) {
	ctx, cancel := context.WithDeadline(context.Background(), r.deadline)
	r.cancel = cancel
	r.dial = make(chan onlineDialed, 1)
	go func(out chan<- onlineDialed) {
		for {
			attempt, done := context.WithTimeout(ctx, 2*time.Second)
			c, err := netlink.Resume(attempt, url, token)
			done()
			if c != nil || errors.Is(err, netlink.ErrPeerLeft) || errors.Is(err, netlink.ErrProtocol) {
				out <- onlineDialed{c, err}
				return
			}
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				out <- onlineDialed{err: errors.New("rejoin timed out")}
				return
			case <-timer.C:
			}
		}
	}(r.dial)
}

func (a *outpostApp) beginRecovery() {
	if a.recovery != nil {
		return
	}
	if a.osync.logErr != nil {
		a.endOnline(fmt.Errorf("cannot rejoin without a complete match history: %w", a.osync.logErr))
		return
	}
	a.recovery = &outpostRecovery{deadline: time.Now().Add(onlineReconnectLimit)}
	a.onlineMenu = false
	slog.Info("Online match paused for rejoin", slog.Int("player", a.osync.local))
}

func (a *outpostApp) updateRecovery(in engine.Input) {
	if in.Pressed.Has(engine.KeyEscape) || a.start || a.menuButton.Update(in) {
		a.toTitle()
		return
	}
	if err := a.stepRecovery(); err != nil {
		if isTransportChange(err) {
			return
		}
		a.endOnline(err)
	}
}

func (a *outpostApp) stepRecovery() error {
	r, o := a.recovery, a.osync
	if time.Now().After(r.deadline) {
		return errors.New("rejoin timed out")
	}
	if r.dial != nil {
		select {
		case result := <-r.dial:
			r.cancel()
			r.dial = nil
			if result.err != nil {
				return result.err
			}
			if result.conn.Slot() != o.local || result.conn.Code() != a.conn.Code() {
				noteClose("close a wrong rejoin slot", result.conn.Close())
				return errors.New("relay returned the wrong rejoin slot")
			}
			a.conn = result.conn
		default:
			return nil
		}
	}
	epoch := a.conn.Epoch()
	err := a.conn.Err()
	if err != nil || epoch == 0 {
		if r.link != nil {
			r.dropEpoch()
			r.deadline = time.Now().Add(onlineReconnectLimit)
		}
		if errors.Is(err, netlink.ErrLost) {
			r.startDial(a.world.online.relay+"/v2/resume", a.conn.ResumeToken())
			return nil
		}
		return err
	}
	if r.link == nil || r.link.epoch != epoch {
		r.dropEpoch()
		r.link = &outpostLink{conn: a.conn, epoch: epoch}
		r.deadline = time.Now().Add(onlineRebuildLimit)
	}
	if rebuildErr := a.advanceCheckpoint(r, o); rebuildErr != nil {
		return rebuildErr
	}
	if !r.ready || !r.remote {
		return nil
	}
	tick := r.choice.tick
	r.link.peer = r.choice
	lock, err := onlineLock(a.world.matchHeader(o.setup), o.local, r.link)
	if err != nil {
		return err
	}
	if !o.recorder.Rewind(tick) {
		return errors.New("could not rewind to the agreed checkpoint")
	}
	for i, saved := range o.saved {
		if saved.tick > tick {
			o.saved[i] = recoverySnapshot{}
		}
	}
	if err := a.match.Stop(); err != nil {
		return err
	}
	r.session.camera, r.session.zoomLevel = a.session.camera, a.session.zoomLevel
	r.session.sync, r.session.issuer = o, o.issue
	o.link, o.lock = r.link, lock
	o.base, o.proved, o.shared = tick, tick, tick
	o.waited, o.finalSent = 0, false
	o.interrupted = false
	a.match, a.session, a.recovery = r.match, r.session, nil
	a.say("Rejoined. Later orders discarded.")
	slog.Info(
		"Online match rejoined",
		slog.Int("player", o.local),
		slog.Int("tick", tick),
		slog.Uint64("epoch", epoch),
	)
	return nil
}

func (o *onlineSync) proofAt(tick int) (onlineProof, bool) {
	if tick == 0 {
		return onlineProof{hash: o.initial}, true
	}
	hash, ok := o.recorder.Checkpoint(tick)
	return onlineProof{tick, hash}, ok
}

func (a *outpostApp) advanceCheckpoint(r *outpostRecovery, o *onlineSync) error {
	if !r.offered {
		p, ok := o.proofAt(o.proved)
		if !ok {
			return errors.New("missing the most recent local proof")
		}
		if err := p.send(r.link, resumeOffer); err != nil {
			return err
		}
		r.offered = true
	}
	if r.play == nil {
		p, ok, err := r.nextProof(resumeOffer)
		if err != nil || !ok {
			return err
		}
		if p.tick < o.shared || p.tick%matchlog.Interval != 0 {
			return errors.New("peer offered an invalid rejoin checkpoint")
		}
		if local, have := o.proofAt(p.tick); p.tick <= o.proved && (!have || local != p) {
			return errors.New("peer offered a different rejoin hash")
		}
		tick := min(o.proved, p.tick)
		r.choice, _ = o.proofAt(tick)
		saved := o.snapshotBefore(tick)
		data, ok := o.recorder.Segment(saved.tick, tick)
		if !ok {
			return errors.New("could not read history for rejoin")
		}
		log, err := matchlog.Decode(data)
		if err != nil {
			return err
		}
		r.play = &matchWatch{
			reader: log.Reader(), cmds: newMatchCommands(), ticks: log.Ticks(), base: saved.tick,
		}
		r.match, err = engine.Start(func() engine.Scene {
			s := a.world.onlineSession(o.setup, o.local)
			s.sync = r.play
			r.session = s
			return s
		})
		if err != nil {
			return err
		}
		if saved.data != nil {
			if err := r.session.sim.Restore(saved.data); err != nil {
				return err
			}
			hash, ok := o.recorder.Checkpoint(saved.tick)
			if !ok || r.session.sim.Trace() != hash {
				return errors.New("local checkpoint cache has a different hash")
			}
		}
	}
	if !r.remote {
		p, ok, err := r.nextProof(resumeReady)
		if err != nil {
			return err
		}
		if ok && p != r.choice {
			return errors.New("peer rebuilt a different rejoin state")
		}
		r.remote = ok
	}
	if r.ready {
		return nil
	}
	for i := 0; i < viewStepBudget && !r.play.done(); i++ {
		if err := r.match.Step(engine.Input{}); err != nil {
			return err
		}
	}
	if r.play.done() {
		if r.session.sim.Trace() != r.choice.hash {
			return errors.New("could not rebuild the verified checkpoint")
		}
		if err := r.choice.send(r.link, resumeReady); err != nil {
			return err
		}
		r.ready = true
	}
	return nil
}

func (r *outpostRecovery) nextProof(kind byte) (onlineProof, bool, error) {
	msg, ok, err := r.link.conn.RecvEpoch(r.link.epoch)
	if err != nil || !ok {
		return onlineProof{}, false, err
	}
	if len(msg) == 0 || msg[0] != kind {
		return onlineProof{}, false, errors.New("unexpected message during rejoin")
	}
	p, err := decodeOnlineProof(msg)
	return p, true, err
}
