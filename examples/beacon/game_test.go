package main

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/krapht-foundation/goje/engine"
)

func newRound(t *testing.T) *scene {
	t.Helper()
	root, err := fs.Sub(files, "assets")
	if err != nil {
		t.Fatal(err)
	}
	config, factory, err := launch(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Music) != 1 || len(config.Music[musicTrack].PCM()) == 0 {
		t.Fatal("the decoded music track is missing")
	}
	s := factory()
	if err := s.Enter(); err != nil {
		t.Fatal(err)
	}
	return s
}

func step(t *testing.T, s *scene, input engine.Input) engine.Transition {
	t.Helper()
	transition, err := s.Update(engine.Tick{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	return transition
}

func tap(b int, s *scene) engine.Input {
	p := s.beacons[b]
	return engine.Input{PrimaryPress: engine.MousePress{
		X: float64(left + p.X*cell + cell/2), Y: float64(top + p.Y*cell + cell/2), Valid: true,
	}}
}

func restarts(transition engine.Transition) bool {
	return reflect.DeepEqual(transition, engine.Restart())
}

func TestKeyboardWin(t *testing.T) {
	s := newRound(t)
	for ticks := 0; s.phase != won; ticks++ {
		if ticks == timeLimit {
			t.Fatalf("keyboard play did not win in %d ticks; %d unlit", timeLimit, s.unlit)
		}
		input := engine.Input{Held: engine.KeyArrowRight}
		for i, b := range s.beacons {
			if !s.lit[i] && b.X == (s.x-left)/cell {
				input = engine.Input{Pressed: engine.KeySpace}
			}
		}
		step(t, s, input)
	}
	// Movement and Space do nothing after the round; Space restarts it.
	x := s.x
	step(t, s, engine.Input{Held: engine.KeyArrowLeft})
	if s.x != x {
		t.Fatal("the tomato moved after the win")
	}
	if !restarts(step(t, s, engine.Input{Pressed: engine.KeySpace})) {
		t.Fatal("Space did not restart a won round")
	}
}

func TestTouchWinAndRestart(t *testing.T) {
	s := newRound(t)
	for b := range s.beacons {
		if restarts(step(t, s, tap(b, s))) {
			t.Fatalf("tap %d restarted the round", b)
		}
		if b < len(s.beacons)-1 && s.phase != playing {
			t.Fatalf("phase %d after tap %d", s.phase, b)
		}
	}
	if s.phase != won || s.clock != len(s.beacons) {
		t.Fatalf("phase %d after %d ticks, want a win in %d", s.phase, s.clock, len(s.beacons))
	}
	// A tap anywhere restarts, so touch play never needs a keyboard.
	if !restarts(step(t, s, engine.Input{PrimaryPress: engine.MousePress{Valid: true}})) {
		t.Fatal("a tap did not restart a won round")
	}
}

func TestTimeout(t *testing.T) {
	s := newRound(t)
	// The clock waits for the first action.
	for range 2 * timeLimit {
		step(t, s, engine.Input{})
	}
	if s.phase != ready || s.clock != 0 {
		t.Fatalf("an idle round reached phase %d at clock %d", s.phase, s.clock)
	}
	step(t, s, tap(0, s))
	for s.clock < timeLimit-1 {
		step(t, s, engine.Input{})
	}
	if s.phase != playing {
		t.Fatalf("the round ended early at clock %d", s.clock)
	}
	step(t, s, engine.Input{})
	if s.phase != timedOut {
		t.Fatalf("phase %d at clock %d, want a timeout", s.phase, s.clock)
	}
	// A late tap restarts instead of lighting a beacon.
	if !restarts(step(t, s, tap(1, s))) || s.lit[1] {
		t.Fatal("a tap after the timeout did not restart the round")
	}
}

func TestRestartWhilePlaying(t *testing.T) {
	s := newRound(t)
	step(t, s, tap(0, s))
	if !restarts(step(t, s, engine.Input{Pressed: engine.KeyR})) {
		t.Fatal("R did not restart a running round")
	}
}
