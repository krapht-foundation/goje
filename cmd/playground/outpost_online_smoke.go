//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/krapht-foundation/goje/engine"
)

// The online smoke hosts a match from a native window and plays it with a guest
// that scripts/smoke-online.mjs runs in Chromium. The two print a line for every
// state hash they compare, and the script checks that the lines agree. With
// GOJE_ONLINE_FAULT set to a tick, this player changes its hash from that tick on,
// and both players must end the match with the divergence.

const onlineSmokeTicks = 900

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

// onlineHostSmoke feeds the app the inputs of a host: it picks Host in the title
// menu, waits for the guest, selects its scouts, orders them to the middle, and
// then focuses fire once a second.
type onlineHostSmoke struct {
	app      *outpostApp
	n        int // Ticks since the match started.
	menu     []engine.Input
	cancel   context.CancelFunc
	fault    bool
	finished bool
}

type onlineHostScene struct {
	engine.Scene
	smoke *onlineHostSmoke
}

func (s *onlineHostScene) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = s.smoke.input()
	return s.Scene.Update(tick)
}

func (k *onlineHostSmoke) input() engine.Input {
	app := k.app
	switch app.state {
	case stateTitle:
		if k.menu == nil {
			k.menu = keys(engine.KeyArrowDown, engine.KeyArrowDown, engine.KeyArrowDown, engine.KeyEnter)
		}
		if len(k.menu) > 0 {
			in := k.menu[0]
			k.menu = k.menu[1:]
			return in
		}
	case stateOnline:
		n := k.n
		k.n++
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
			x, y := app.session.camera.ToLogical(courtyardProjection.Project(3.5, 4.5, 0))
			return engine.Input{SecondaryPress: press(x, y)}
		case n > 60 && n%60 == 0:
			return engine.Input{Pressed: engine.KeyF}
		case app.osync != nil && app.osync.lock.Tick() >= onlineSmokeTicks:
			k.finished = true
			k.cancel()
		}
	case stateOnlineEnded:
		k.fault = true
		k.cancel()
	case statePlaying, statePaused, stateWatching, stateLobby:
	}
	return engine.Input{}
}

// runOnlineHostSmoke runs until the match has played onlineSmokeTicks ticks, or,
// when a fault is injected, until it ended with the divergence. It steps the app
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	smoke := &onlineHostSmoke{cancel: cancel}
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
	case faulty && smoke.fault, !faulty && smoke.finished:
		return nil
	case faulty:
		return errors.New("the injected divergence was not reported")
	}
	return errors.New("the online match did not play to its end within the time limit")
}
