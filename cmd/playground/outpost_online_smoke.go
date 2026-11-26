//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/examples/skirmish"
)

// The online smoke hosts a match from a native window and plays it with a guest
// that scripts/smoke-online.mjs runs in Chromium. The two print a line for every
// state hash they compare, and the script checks that the lines agree. With
// GOJE_ONLINE_FAULT set to a tick, this player changes its hash from that tick on,
// and both players must end the match with the divergence. With
// GOJE_ONLINE_VERSUS=1 it hosts versus, leaves through the menu at
// onlineSmokeLeave unless a team has already won, and then watches the match
// log from the title.

const (
	onlineSmokeTicks = 900
	onlineSmokeLeave = 720
)

// injectFault reads GOJE_ONLINE_FAULT and, when it names a tick, makes this player's
// hashes differ from that tick on.
func injectFault() {
	if v := os.Getenv("GOJE_ONLINE_FAULT"); v != "" {
		from, err := strconv.ParseUint(v, 10, 64)
		if err == nil {
			onlineHashFault = func(tick, hash uint64) uint64 {
				if tick >= from {
					return hash ^ 1
				}
				return hash
			}
		}
	}
}

// onlineHostSmoke feeds the app the inputs of a host: it picks Host co-op or Host
// versus in the title menu, waits for the guest, selects its units, orders them
// to the middle, and then focuses fire once a second.
type onlineHostSmoke struct {
	app    *outpostApp
	versus bool
	n      int // Updates since starting from checkpoint zero.
	epoch  uint64
	menu   []engine.Input
	cancel context.CancelFunc
	hosted bool // Host was chosen; the title after that is the way to the log.
	leave  bool // The leave keys were queued.
	watch  bool // The watch keys were queued.
	// start is where each unit began, and moved says which player's units left it.
	start    map[uint64][2]float64
	moved    [2]bool
	fault    bool
	finished bool
	err      error
}

type onlineHostScene struct {
	engine.Scene
	smoke *onlineHostSmoke
}

func (s *onlineHostScene) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = s.smoke.input()
	return s.Scene.Update(tick)
}

// menuChoice moves down to a menu item from the first one and activates it.
func menuChoice(item int) []engine.Input {
	inputs := make([]engine.Input, item+1)
	for i := range item {
		inputs[i].Pressed = engine.KeyArrowDown
	}
	inputs[item].Pressed = engine.KeyEnter
	return inputs
}

func (k *onlineHostSmoke) fail(err error) {
	k.err = err
	k.cancel()
}

func (k *onlineHostSmoke) input() engine.Input {
	if len(k.menu) > 0 {
		in := k.menu[0]
		k.menu = k.menu[1:]
		return in
	}
	app := k.app
	switch app.state {
	case stateTitle:
		switch {
		case !k.hosted:
			k.hosted = true
			k.menu = menuChoice(titleHostCoop)
			if k.versus {
				k.menu = menuChoice(titleHostVersus)
			}
		case !k.versus:
			k.fail(errors.New("the co-op match returned to the title"))
		case !k.watch:
			k.watch = true
			k.menu = menuChoice(titleWatchMatch)
		default:
			k.fail(errors.New("the last online match did not play: " + app.notice))
		}
	case stateOnline:
		if app.recovery != nil {
			break
		}
		if epoch := app.osync.link.epoch; epoch != k.epoch {
			k.epoch = epoch
			if app.osync.base == 0 {
				k.n = 0 // The first move must be sent again after a rewind to zero.
			}
		}
		k.track(app.session)
		n := k.n
		k.n++
		tick := uint64(
			app.osync.base,
		) + app.osync.lock.Tick() // #nosec G115 -- checkpoint is nonnegative.
		switch {
		case n == 0:
			return engine.Input{
				PrimaryPress: press(0, 0), PrimaryHeld: true,
				Pointer: engine.Pointer{X: 0, Y: 0, Inside: true},
			}
		case n == 1:
			return engine.Input{
				PrimaryRelease: press(width-1, height-1),
				Pointer:        engine.Pointer{X: width - 1, Y: height - 1, Inside: true},
			}
		case n == 2:
			gx, gy := 3.5, 4.5
			if k.versus {
				gx = 5.5 // The middle of the versus board.
			}
			x, y := app.session.camera.ToLogical(courtyardProjection.Project(gx, gy, 0))
			return engine.Input{SecondaryPress: press(x, y)}
		case n == 3 && len(app.session.selection) == 0:
			k.fail(errors.New("the drag selected none of the host's units"))
		case k.versus && !k.leave && tick >= onlineSmokeLeave:
			// Escape opens the menu over the running match; Leave is its third entry.
			k.leave = true
			k.menu = menuChoice(onlineLeave)
			return engine.Input{Pressed: engine.KeyEscape}
		case !k.versus && tick >= onlineSmokeTicks:
			k.finished = true
			k.cancel()
		case n > 60 && n%60 == 0 && !k.leave:
			return engine.Input{Pressed: engine.KeyF}
		}
	case stateOnlineEnded:
		switch {
		case len(app.endLines) == 0 || !strings.HasPrefix(app.endLines[0], "Match complete"):
			if onlineHashFault == nil {
				k.fail(errors.New("the online match ended: " + strings.Join(app.endLines, " ")))
			}
			k.fault = true
			k.cancel()
		case !k.versus:
			k.finished = true // A team won before onlineSmokeTicks; the script reports the tick.
			k.cancel()
		default:
			return engine.Input{Pressed: engine.KeyEnter}
		}
	case stateMatchWatch:
		if app.matchPlayer.done() {
			k.finished = true
			k.cancel()
		}
	case statePlaying, statePaused, stateWatching, stateLobby, stateLiveWatch:
	}
	return engine.Input{}
}

// track notes which players' units have left their start, which only their
// commands can do: the scouts have no AI, and a versus match turns off the guards'.
func (k *onlineHostSmoke) track(s *outpostSession) {
	units := s.sim.Units()
	at := func(u *skirmish.Unit) [2]float64 {
		c := u.Center()
		return [2]float64{c.X.Float64(), c.Y.Float64()}
	}
	if k.start == nil {
		k.start = make(map[uint64][2]float64, len(units))
		for i := range units {
			k.start[units[i].ID] = at(&units[i])
		}
		return
	}
	for i := range units {
		u := &units[i]
		if at(u) == k.start[u.ID] {
			continue
		}
		for p := range k.moved {
			if s.versus && u.Team == playerTeam(p) ||
				!s.versus && u.Team == skirmish.Scouts && onlineOwner(u.ID) == p {
				k.moved[p] = true
			}
		}
	}
}

// runOnlineHostSmoke runs until the match has played onlineSmokeTicks ticks, or,
// when a fault is injected, until it ended with the divergence. In versus it runs
// until this player has watched the match's log to its end. It steps the app
// at 60 ticks per second from a timer and opens no window: a native window that
// is covered or in the background stops its frames on some systems, which would
// stall the guest, and the window itself is covered by the other desktop smokes.
func runOnlineHostSmoke() error {
	injectFault()
	loaded, err := loadAuthored(authoredFiles)
	if err != nil {
		return err
	}
	world, _, err := newOutpost(authoredFiles, loaded, smokeStore{})
	if err != nil {
		return err
	}
	if world.online.relay == "" {
		return errors.New("set GOJE_RELAY to the relay's ws:// address")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	smoke := &onlineHostSmoke{cancel: cancel, versus: os.Getenv("GOJE_ONLINE_VERSUS") == "1"}
	runtime, err := engine.Start(func() engine.Scene {
		smoke.app = newOutpostApp(world)
		return &onlineHostScene{Scene: smoke.app, smoke: smoke}
	})
	if err != nil {
		return fmt.Errorf("start online smoke: %w", err)
	}
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-ticker.C:
			if err := runtime.Step(engine.Input{}); err != nil {
				return fmt.Errorf("online smoke: %w", err)
			}
		}
	}
	if err := runtime.Stop(); err != nil {
		return fmt.Errorf("stop online smoke: %w", err)
	}
	faulty := onlineHashFault != nil
	switch {
	case smoke.err != nil:
		return smoke.err
	case faulty && smoke.fault:
		return nil
	case faulty:
		return errors.New("the injected divergence was not reported")
	case !smoke.finished:
		return errors.New("the online match did not play to its end within the time limit")
	case smoke.moved != [2]bool{true, true}:
		return fmt.Errorf("the players' commands should move their units; moved %v", smoke.moved)
	}
	return nil
}
