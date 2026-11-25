package main

import (
	"encoding/binary"
	"errors"
	"log/slog"
	"strconv"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/matchlog"
	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/netlink"
)

const (
	viewBegin byte = 0x60
	viewChunk byte = 0x61
	// At 60 updates/s two chunks leave room for the player's normal frames under
	// the relay's default message-rate limit, even during a full catch-up.
	viewSendBudget = 2
	viewReadBudget = 24
	// Historical ticks run without user input or sound. Bound their work per
	// update so a long catch-up still draws and responds to Leave.
	viewStepBudget = 32
	viewQueueLimit = 256
)

// viewSender writes one independent match log segment at a time. Once a log
// has been committed, a watcher can never see it retract after a reconnect.
type viewSender struct {
	id           uint64
	through, end int
	data         []byte
	offset       int
	begun        bool
}

func (v *viewSender) send(conn *netlink.Conn, log *matchlog.Recorder, committed int) {
	id := conn.Watcher()
	if id != v.id {
		*v = viewSender{id: id}
	}
	if id == 0 || committed <= v.through {
		return
	}
	if v.data == nil {
		v.end = committed
		if v.through != 0 {
			v.end = min(v.end, v.through+matchlog.Interval)
		}
		var ok bool
		v.data, ok = log.Segment(v.through, v.end)
		if !ok {
			return
		}
	}
	if !v.begun {
		var msg [9]byte
		msg[0] = viewBegin
		binary.LittleEndian.PutUint32(
			msg[1:],
			uint32(v.through),
		) // #nosec G115 -- bounded by matchlog.MaxTicks.
		binary.LittleEndian.PutUint32(
			msg[5:],
			uint32(len(v.data)),
		) // #nosec G115 -- bounded by MaxEncodedSize.
		if err := conn.SendView(id, msg[:]); err != nil {
			return
		}
		v.begun = true
	}
	var chunk [netlink.MaxMessage]byte
	chunk[0] = viewChunk
	for range viewSendBudget {
		part := v.data[v.offset:]
		n := copy(chunk[1:], part)
		if err := conn.SendView(id, chunk[:1+n]); err != nil {
			return
		}
		v.offset += n
		if v.offset == len(v.data) {
			v.through = v.end
			v.data, v.offset, v.begun = nil, 0, false
			return
		}
	}
}

type viewLog struct {
	log  matchlog.Log
	size int
}

// viewInbox bounds segment assembly, queued logs, and total simulation work.
// It retains at most two MiB even if the sender supplies many small segments.
type viewInbox struct {
	data          []byte
	queue         [viewQueueLimit]viewLog
	next, count   int
	header        matchlog.Header
	end, retained int
	partialEnd    bool
}

func (v *viewInbox) receive(msg []byte) error {
	if len(msg) == 0 || v.partialEnd {
		return errors.New("unexpected live view message")
	}
	switch msg[0] {
	case viewBegin:
		if len(msg) != 9 || v.data != nil || v.count == len(v.queue) {
			return errors.New("invalid live view segment header")
		}
		first, size := binary.LittleEndian.Uint32(msg[1:]), binary.LittleEndian.Uint32(msg[5:])
		if first != uint32(v.end) || size == 0 ||
			size > uint32(matchlog.MaxEncodedSize-v.retained) { // #nosec G115 -- both counts are bounded.
			return errors.New("live view segment is out of order or exceeds memory limit")
		}
		v.data = make([]byte, 0, int(size)) // #nosec G115 -- validated above.
	case viewChunk:
		if len(msg) < 2 || v.data == nil || len(msg)-1 > cap(v.data)-len(v.data) {
			return errors.New("invalid live view segment chunk")
		}
		v.data = append(v.data, msg[1:]...)
		if len(v.data) < cap(v.data) {
			return nil
		}
		log, err := matchlog.Decode(v.data)
		if err != nil {
			return err
		}
		if log.Ticks() == 0 || log.Ticks() > matchlog.MaxTicks-v.end ||
			(v.end != 0 && (log.Header() != v.header || log.Ticks() > matchlog.Interval)) {
			return errors.New("invalid live view segment history")
		}
		v.header = log.Header()
		v.end += log.Ticks()
		v.partialEnd = v.end%matchlog.Interval != 0
		v.queue[(v.next+v.count)%len(v.queue)] = viewLog{log: log, size: len(v.data)}
		v.count++
		v.retained += len(v.data)
		v.data = nil
	default:
		return errors.New("unknown live view message")
	}
	return nil
}

func (v *viewInbox) pop() viewLog {
	log := v.queue[v.next]
	v.queue[v.next] = viewLog{}
	v.next = (v.next + 1) % len(v.queue)
	v.count--
	return log
}

type liveWatch struct {
	inbox       viewInbox
	play        *matchWatch
	applied     int
	playingSize int
	recvErr     error
}

func (a *outpostApp) updateLiveWatch(in engine.Input) {
	if in.Pressed.Has(engine.KeyEscape) || a.start || a.menuButton.Update(in) {
		a.toTitle()
		return
	}
	if a.fogButton.Update(in) {
		in.Pressed |= engine.KeyTab
	}
	if err := a.stepLiveWatch(in); err != nil {
		a.endOnline(err)
	}
}

func (a *outpostApp) stepLiveWatch(in engine.Input) error {
	v := a.live
	for i := 0; i < viewReadBudget && v.recvErr == nil; i++ {
		msg, ok, err := a.conn.Recv()
		if err != nil {
			v.recvErr = err
			break
		}
		if !ok {
			break
		}
		if err := v.inbox.receive(msg); err != nil {
			return err
		}
	}
	if a.match == nil && v.inbox.count != 0 {
		h := v.inbox.header
		setup := matchSetup{mode: h.Mode, tactics: h.Tactics}
		if h != a.world.matchHeader(setup) {
			return replay.ErrContentMismatch
		}
		v.play = &matchWatch{cmds: newMatchCommands()}
		var err error
		a.match, err = engine.Start(func() engine.Scene {
			s := a.world.onlineSession(setup, 0)
			s.sync, s.watching, s.foreign = v.play, true, nil
			a.session = s
			return s
		})
		if err != nil {
			return err
		}
	}
	budget := 1
	if v.applied == 0 || v.inbox.end-v.applied > 2*matchlog.Interval {
		budget = viewStepBudget
	}
	for i := range budget {
		if v.play == nil {
			break
		}
		if v.play.done() && v.inbox.count != 0 {
			part := v.inbox.pop()
			v.play.reader, v.play.ticks, v.play.base = part.log.Reader(), part.log.Ticks(), v.applied
			v.playingSize = part.size
		}
		wasDone := v.play.done()
		if wasDone && i != 0 {
			break
		}
		if budget == 1 {
			a.match.SetSoundOutput(a.sound)
		} else {
			a.match.SetSoundOutput(nil)
		}
		fog := a.session.fogTeam
		if err := a.match.Step(in); err != nil {
			return err
		}
		in = engine.Input{}
		if a.session.fogTeam != fog {
			slog.Info("Live view fog", slog.Int("team", int(a.session.fogTeam)))
		}
		if !wasDone && v.play.done() {
			v.applied += v.play.ticks
			v.inbox.retained -= v.playingSize
			v.play.reader, v.play.ticks = matchlog.Reader{}, 0
			slog.Info(
				"Live view verified",
				slog.Int("tick", v.applied),
				slog.String("hash", strconv.FormatUint(a.session.sim.Trace(), 16)),
			)
		}
	}
	if v.recvErr != nil && v.inbox.count == 0 && (v.play == nil || v.play.done()) {
		if v.inbox.data != nil {
			return errors.New("live view ended during segment transfer")
		}
		if a.match == nil {
			return v.recvErr
		}
	}
	return nil
}

func (a *outpostApp) drawLiveWatch(canvas engine.Canvas, face font.Face) {
	text := "LIVE WATCH"
	switch {
	case a.match == nil:
		text = "Waiting for match history..."
	case a.live.applied < a.live.inbox.end && a.live.applied == 0:
		text = "Catching up..."
	case a.live.recvErr != nil && a.live.applied == a.live.inbox.end:
		text = "LIVE VIEW ENDED"
	}
	face.Draw(canvas, text, 8, 14, 1, selectionColor)
	a.menuButton.Text = "Leave"
	a.menuButton.Draw(canvas, face)
	if a.match != nil {
		a.fogButton.Draw(canvas, face)
	}
}
