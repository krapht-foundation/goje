package main

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/matchlog"
	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/platform"
	"github.com/krapht-foundation/goje/ui"
)

const outpostMatchKey = "last-online"

// finishMatchLog saves the completed ticks even after a disconnect or desync.
// The unwritten frames in the lockstep ring are not part of the log.
func (a *outpostApp) finishMatchLog() {
	o := a.osync
	if o.recorder == nil {
		return
	}
	a.lastMatch = o.recorder.Encode()
	o.recorder = nil
	if o.logErr != nil {
		a.say("Only the start of the match was saved: " + o.logErr.Error())
	}
	if a.world.store != nil {
		if err := a.world.store.Put(outpostMatchKey, a.lastMatch); err != nil {
			a.say("Match log kept for this run only: " + err.Error())
		}
	}
}

// matchWatch feeds the same command path as network play, without the network
// clock or a local command issuer. The camera and view team are presentation.
type matchWatch struct {
	reader matchlog.Reader
	cmds   matchCommands
	ticks  int
}

func (m *matchWatch) done() bool { return m.reader.Tick() == m.ticks }

func (m *matchWatch) before(s *outpostSession) (bool, error) {
	payloads := m.reader.Next()
	if payloads == nil {
		return false, nil
	}
	if err := m.cmds.apply(s, payloads); err != nil {
		return false, fmt.Errorf("tick %d: %w", m.reader.Tick(), err)
	}
	return true, nil
}

func (m *matchWatch) after(hash uint64) error {
	if want, ok := m.reader.Checkpoint(); ok && hash != want {
		return &replay.DivergenceError{
			Tick:   m.reader.Tick(),
			Detail: fmt.Sprintf("match log has %016x, playback has %016x", want, hash),
		}
	}
	return nil
}

func (a *outpostApp) watchMatch() {
	data := a.lastMatch
	if data == nil && a.world.store != nil {
		var err error
		data, err = a.world.store.Get(outpostMatchKey)
		if err != nil && !errors.Is(err, platform.ErrNotFound) {
			a.say("Could not read the match log: " + err.Error())
			return
		}
	}
	if data == nil {
		a.say("There is no online match log yet.")
		return
	}
	if err := a.startMatchWatch(data); err != nil {
		a.say("Could not watch the match: " + err.Error())
	}
}

func (a *outpostApp) startMatchWatch(data []byte) error {
	log, err := matchlog.Decode(data)
	if err != nil {
		return err
	}
	h := log.Header()
	setup := matchSetup{mode: h.Mode, tactics: h.Tactics}
	if h != a.world.matchHeader(setup) {
		return replay.ErrContentMismatch
	}
	watch := &matchWatch{reader: log.Reader(), cmds: newMatchCommands(), ticks: log.Ticks()}
	a.stopMatch()
	match, err := engine.Start(func() engine.Scene {
		s := a.world.onlineSession(setup, 0)
		s.sync, s.watching, s.foreign = watch, true, nil
		a.session = s
		return s
	})
	if err != nil {
		return err
	}
	a.match, a.matchPlayer, a.state = match, watch, stateMatchWatch
	if watch.done() {
		slog.Info("Online replay verified", slog.Int("ticks", 0))
	}
	return nil
}

func (a *outpostApp) updateMatchWatch(in engine.Input) {
	if in.Pressed.Has(engine.KeyEscape) || a.start || a.menuButton.Update(in) {
		a.toTitle()
		return
	}
	if a.fogButton.Update(in) {
		in.Pressed |= engine.KeyTab
	}
	watch := a.matchPlayer
	wasDone := watch.done()
	if err := a.match.Step(in); err != nil {
		a.toTitle()
		if divergence, ok := errors.AsType[*replay.DivergenceError](err); ok {
			err = divergence
		}
		a.say(clipLine(err.Error()))
		return
	}
	if !wasDone && watch.done() {
		slog.Info("Online replay verified", slog.Int("ticks", watch.ticks))
	}
}

func (a *outpostApp) drawMatchWatch(canvas engine.Canvas) {
	face := a.world.face
	label := ui.Label{Bounds: a.recLabel.Bounds, Style: a.recLabel.Style, Text: "ONLINE REPLAY"}
	if a.matchPlayer.done() {
		label.Text = "REPLAY ENDED"
	}
	label.Draw(canvas, face)
	a.menuButton.Text = "Leave"
	a.menuButton.Draw(canvas, face)
	a.fogButton.Draw(canvas, face)
	face.Draw(canvas, "Tab switches fog; Esc leaves", 76, 169, 1, skirmishAlarmQuiet)
}
