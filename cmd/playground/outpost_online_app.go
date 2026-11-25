package main

import (
	"errors"
	"image"
	"image/color"
	"log/slog"
	"strings"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/lockstep"
	"github.com/krapht-foundation/goje/engine/matchlog"
	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/netlink"
	"github.com/krapht-foundation/goje/ui"
)

// updateLobby waits for the connection and for the other player. Escape, or the
// gamepad's Start button, cancels.
func (a *outpostApp) updateLobby(in engine.Input) {
	if in.Pressed.Has(engine.KeyEscape) || a.start {
		a.toTitle()
		return
	}
	ready, err := a.lobby.poll()
	if err != nil {
		a.endOnline(err)
		return
	}
	if l := a.lobby; l.host && l.conn != nil && !l.announced {
		l.announced = true
		slog.Info("Online room opened", slog.String("code", l.conn.Code()))
		slog.Info("Online spectator invitation", slog.String("code", l.conn.ViewCode()))
	}
	if ready {
		if a.lobby.watch {
			a.lobby.cancel()
			a.conn, a.lobby, a.state = a.lobby.conn, nil, stateLiveWatch
			a.live = &liveWatch{}
			return
		}
		a.beginOnline()
	}
}

// beginOnline starts the match when both players are connected. The match takes
// over the lobby's connection.
func (a *outpostApp) beginOnline() {
	conn := a.lobby.conn
	setup := a.lobby.setup
	header := a.world.matchHeader(setup)
	link := &outpostLink{conn: conn, epoch: conn.Epoch()}
	lock, err := onlineLock(header, conn.Slot(), link)
	if err != nil {
		a.endOnline(err)
		return
	}
	sync := newOnlineSync(lock, conn.Slot())
	sync.link, sync.setup = link, setup
	sync.recorder, err = matchlog.New(header)
	if err != nil {
		a.endOnline(err)
		return
	}
	match, err := engine.Start(func() engine.Scene {
		s := a.world.onlineSession(setup, sync.local)
		s.sync, s.issuer = sync, sync.issue
		a.session = s
		return s
	})
	if err != nil {
		a.endOnline(err)
		return
	}
	sync.initial = a.session.sim.Trace()
	if conn.Slot() == 0 {
		a.viewLabel = "Watch code: " + conn.ViewCode()
	}
	a.lobby.cancel()
	a.lobby, a.conn, a.osync = nil, conn, sync
	a.match, a.state, a.onlineMenu = match, stateOnline, false
	slog.Info(
		"Online match started",
		slog.Int("player", conn.Slot()),
		slog.Int("mode", int(setup.mode)),
	)
}

// updateOnline runs one tick of an online match. The menu opens over a
// running match; the simulation freezes only if the connection is recovering.
func (a *outpostApp) updateOnline(in engine.Input) error {
	if a.recovery != nil {
		a.updateRecovery(in)
		return nil
	}
	if o := a.osync; o.link != nil && a.session.outcome == outcomePlaying {
		err := a.conn.Err()
		if err != nil {
			a.failOnline(err)
			return nil
		}
		if a.conn.Epoch() != o.link.epoch {
			a.beginRecovery()
			return nil
		}
	}
	if a.session.outcome != outcomePlaying {
		a.confirmOnlineResult()
		return nil
	}
	touchMenu := a.session.controls.touchUI && a.menuButton.Update(in)
	if a.start || touchMenu || (in.Pressed.Has(engine.KeyEscape) && !a.session.cancelable()) {
		a.click()
		a.onlineMenu = !a.onlineMenu
		if a.onlineMenu {
			a.menu = ui.Menu{
				Bounds: image.Rect(100, 70, 220, 112),
				Items:  []string{"Resume", a.soundLabel(), "Leave the match"},
				Style:  ui.DefaultStyle(),
			}
		}
	} else if a.onlineMenu {
		choice, cancelled := a.menu.Update(in)
		if cancelled {
			choice = onlineResume
		}
		if choice >= 0 {
			a.click()
		}
		switch choice {
		case onlineResume:
			a.onlineMenu = false
		case onlineSound:
			a.cycleVolume()
			a.menu.Items[onlineSound] = a.soundLabel()
		case onlineLeave:
			a.toTitle()
			return nil
		}
	}
	if a.onlineMenu {
		in = engine.Input{}
	}
	if err := a.match.Step(in); err != nil {
		if end, ok := errors.AsType[*onlineEnd](err); ok {
			a.failOnline(end.err)
			return nil
		}
		return err
	}
	if a.osync.interrupted {
		a.beginRecovery()
		return nil
	}
	if err := a.osync.keepCheckpoint(a.session); err != nil {
		a.endOnline(err)
		return nil
	}
	if err := a.osync.prove(a.session.outcome != outcomePlaying); err != nil {
		a.failOnline(err)
	} else if a.conn != nil && a.osync.local == 0 {
		a.view.send(a.conn, a.osync.recorder, a.osync.shared)
	}
	return nil
}

func (a *outpostApp) failOnline(err error) {
	if a.osync.link != nil && isTransportChange(err) {
		a.beginRecovery()
	} else {
		a.endOnline(err)
	}
}

// confirmOnlineResult keeps the last state frozen until both peers have
// stepped the winning tick. An eager Bye could otherwise reach the slower
// peer before it has a chance to read the result.
func (a *outpostApp) confirmOnlineResult() {
	o := a.osync
	if o.link != nil && o.logErr == nil {
		a.confirmLiveResult()
		return
	}
	err := o.lock.Poll()
	switch {
	case o.lock.Confirmed() == o.lock.Tick() &&
		(err == nil || errors.Is(err, lockstep.ErrPeerLeft) || errors.Is(err, netlink.ErrPeerLeft)):
		a.finishOnlineResult()
	case err != nil:
		a.endOnline(err)
	default:
		o.waited++
		if o.waited >= onlineStallLimit {
			a.endOnline(lockstep.ErrStalled)
		}
	}
}

func (a *outpostApp) confirmLiveResult() {
	o := a.osync
	link := o.link
	err := o.lock.Poll()
	final := o.finalSent && link.final.tick == o.recorder.Ticks()
	if final {
		if mismatch := o.checkProof(link.final); mismatch != nil {
			a.endOnline(mismatch)
			return
		}
	}
	if err != nil && !errors.Is(err, netlink.ErrPeerLeft) {
		if final && isTransportChange(err) {
			a.finishOnlineResult()
		} else {
			a.failOnline(err)
		}
		return
	}
	if err != nil && !final {
		a.endOnline(err)
		return
	}
	if o.lock.Running() {
		if proofErr := o.prove(true); proofErr != nil {
			a.failOnline(proofErr)
			return
		}
	}
	if o.finalSent && link.final.tick == o.recorder.Ticks() {
		if o.local == 0 {
			a.view.send(a.conn, o.recorder, link.final.tick)
			if a.conn.Watcher() == 0 || a.view.through == link.final.tick || o.waited >= 900 {
				a.finishOnlineResult()
				return
			}
		} else if err != nil || o.waited >= 900 {
			// The host drains the spectator stream before it ends the room.
			a.finishOnlineResult()
			return
		}
	}
	o.waited++
	if o.waited >= onlineLiveStall {
		a.endOnline(lockstep.ErrStalled)
	}
}

func (a *outpostApp) finishOnlineResult() {
	result := strings.ToLower(a.session.hud.result.Text)
	tick := uint64(
		a.osync.base,
	) + a.osync.lock.Tick() // #nosec G115 -- base is a validated checkpoint.
	slog.Info("Online match completed", slog.String("result", result), slog.Uint64("tick", tick))
	a.stopMatch()
	a.endLines = []string{"Match complete: " + result}
	a.state = stateOnlineEnded
}

// endOnline stops the lobby or the match and shows why it ended.
func (a *outpostApp) endOnline(err error) {
	slog.Error("Online match ended", slog.String("reason", err.Error()))
	a.stopMatch()
	a.endLines = describeOnlineEnd(err)
	a.state = stateOnlineEnded
}

func (a *outpostApp) updateOnlineEnded(in engine.Input) {
	if in.Pressed.Has(engine.KeyEscape) || in.Pressed.Has(engine.KeyEnter) || a.start ||
		in.PrimaryPress.Valid {
		a.toTitle()
	}
}

func (a *outpostApp) drawLobby(canvas engine.Canvas, face font.Face) {
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	l := a.lobby
	switch {
	case l.conn == nil:
		face.Draw(canvas, "Connecting to the relay...", 82, 90, 1, white)
	case l.host:
		face.Draw(canvas, "Room code", 124, 62, 1, white)
		face.Draw(canvas, "Watch: "+l.conn.ViewCode(), 85, 122, 1, selectionColor)
		face.Draw(canvas, l.conn.Code(), 112, 76, 2, selectionColor)
		face.Draw(canvas, "Waiting for the other player...", 70, 104, 1, white)
	default:
		face.Draw(canvas, "Joining the room...", 100, 90, 1, white)
	}
	face.Draw(canvas, "Esc cancels", 124, 160, 1, skirmishAlarmQuiet)
}

// drawOnline draws the menu over the match and says when the match is waiting.
// During recovery the frozen game accepts only Leave.
func (a *outpostApp) drawOnline(canvas engine.Canvas, face font.Face) {
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if a.recovery != nil {
		a.panel.Draw(canvas)
		face.Draw(canvas, "Rejoining the match...", 88, 70, 1, white)
		face.Draw(canvas, "Waiting for a verified checkpoint", 58, 88, 1, selectionColor)
		face.Draw(canvas, "Esc leaves", 130, 106, 1, white)
		a.menuButton.Text = "Leave"
		a.menuButton.Draw(canvas, face)
		return
	}
	if a.viewLabel != "" {
		face.Draw(canvas, a.viewLabel, 8, 148, 1, selectionColor)
	}
	if a.session.controls.touchUI {
		a.menuButton.Text = "Menu"
		a.menuButton.Draw(canvas, face)
	}
	if a.osync.waiting() {
		face.Draw(canvas, "Waiting for the other player...", 8, 16, 1, selectionColor)
	}
	if a.onlineMenu {
		a.panel.Draw(canvas)
		face.Draw(canvas, "MENU", 140, 46, 2, white)
		a.menu.Draw(canvas, face)
	}
}

func (a *outpostApp) drawOnlineEnded(canvas engine.Canvas, face font.Face) {
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	y := float32(70)
	for i, line := range a.endLines {
		colour := white
		if i > 0 {
			colour = skirmishAlarmQuiet
		}
		face.Draw(canvas, line, float32(max(8, (width-6*len(line))/2)), y, 1, colour)
		y += 14
	}
	face.Draw(canvas, "Press Enter", 128, 160, 1, white)
}
