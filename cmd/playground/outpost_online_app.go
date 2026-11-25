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
	}
	if ready {
		a.beginOnline()
	}
}

// beginOnline starts the match when both players are connected. The match takes
// over the lobby's connection.
func (a *outpostApp) beginOnline() {
	conn := a.lobby.conn
	setup := a.lobby.setup
	header := a.world.matchHeader(setup)
	lock, err := lockstep.New(lockstep.Config{
		Game: onlineGame, Content: header.Content, Players: header.Players, Local: conn.Slot(),
		Delay: onlineDelay, HashInterval: onlineHashInterval, StallLimit: onlineStallLimit,
	}, conn)
	if err != nil {
		a.endOnline(err)
		return
	}
	sync := newOnlineSync(lock, conn.Slot())
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
	a.lobby.cancel()
	a.lobby, a.conn, a.osync = nil, conn, sync
	a.match, a.state, a.onlineMenu = match, stateOnline, false
	slog.Info(
		"Online match started",
		slog.Int("player", conn.Slot()),
		slog.Int("mode", int(setup.mode)),
	)
}

// updateOnline runs one tick of an online match. The match never pauses: the menu
// opens over it and the match keeps its own clock, which waits only for the other
// player.
func (a *outpostApp) updateOnline(in engine.Input) error {
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
			a.endOnline(end.err)
			return nil
		}
		return err
	}
	return nil
}

// confirmOnlineResult keeps the last state frozen until both peers have
// stepped the winning tick. An eager Bye could otherwise reach the slower
// peer before it has a chance to read the result.
func (a *outpostApp) confirmOnlineResult() {
	o := a.osync
	err := o.lock.Poll()
	switch {
	case o.lock.Confirmed() == o.lock.Tick() &&
		(err == nil || errors.Is(err, lockstep.ErrPeerLeft) || errors.Is(err, netlink.ErrPeerLeft)):
		line := strings.ToLower(a.session.hud.result.Text)
		slog.Info(
			"Online match completed",
			slog.String("result", line),
			slog.Uint64("tick", o.lock.Tick()),
		)
		a.stopMatch()
		a.endLines = []string{"Match complete: " + line}
		a.state = stateOnlineEnded
	case err != nil:
		a.endOnline(err)
	default:
		o.waited++
		if o.waited >= onlineStallLimit {
			a.endOnline(lockstep.ErrStalled)
		}
	}
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
		face.Draw(canvas, l.conn.Code(), 112, 76, 2, selectionColor)
		face.Draw(canvas, "Waiting for the other player...", 70, 104, 1, white)
	default:
		face.Draw(canvas, "Joining the room...", 100, 90, 1, white)
	}
	face.Draw(canvas, "Esc cancels", 124, 160, 1, skirmishAlarmQuiet)
}

// drawOnline draws the menu over the match and says when the match is waiting.
func (a *outpostApp) drawOnline(canvas engine.Canvas, face font.Face) {
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
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
