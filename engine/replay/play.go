package replay

import (
	"errors"
	"fmt"

	"github.com/krapht-foundation/goje/engine"
)

// DivergenceError reports that playback stopped matching the recording right
// after recorded tick Tick, counted from 1. Find it with errors.As.
type DivergenceError struct {
	Tick   int
	Detail string
}

func (e *DivergenceError) Error() string {
	return fmt.Sprintf("replay diverged at tick %d: %s", e.Tick, e.Detail)
}

// Player plays one recording once. Its scenes ignore live input, feed the
// recorded input to the wrapped scenes, and compare each checkpoint. The first
// difference ends playback with a *DivergenceError from Update, which the
// runtime returns from Step. After the last tick, the scene stays on its final
// state and Done reports true. Use it from the game loop only.
type Player struct {
	rec     *Recording
	ticks   int
	played  int
	run     int // index of the run that supplies the next tick
	used    int // ticks of that run already played
	check   int // index of the next checkpoint
	started bool
	factory engine.SceneFactory
}

// Play validates rec, checks that it was made for expected, and returns a
// player whose Factory wraps factory. A recording for another game or content
// is refused with an error wrapping ErrContentMismatch. The player reads rec
// during playback, so do not modify it.
func Play(factory engine.SceneFactory, rec *Recording, expected Meta) (*Player, error) {
	if factory == nil {
		return nil, errors.New("play replay: nil factory")
	}
	ticks, err := rec.validate()
	if err != nil {
		return nil, fmt.Errorf("play replay: %w", err)
	}
	if err := rec.Check(expected); err != nil {
		return nil, fmt.Errorf("play replay: %w", err)
	}
	p := &Player{rec: rec, ticks: ticks}
	p.factory = p.wrap(factory)
	return p, nil
}

// Factory returns the factory of the first playback scene. Start a runtime
// with it, or Replace the current scene with it, once.
func (p *Player) Factory() engine.SceneFactory { return p.factory }

// Done reports whether every recorded tick has been played and checked.
func (p *Player) Done() bool { return p.played == p.ticks }

// Played returns the number of recorded ticks played so far.
func (p *Player) Played() int { return p.played }

// Ticks returns the number of ticks in the recording.
func (p *Player) Ticks() int { return p.ticks }

func (p *Player) wrap(factory engine.SceneFactory) engine.SceneFactory {
	if factory == nil {
		return nil
	}
	return func() engine.Scene {
		inner := factory()
		if inner == nil {
			return nil
		}
		return expose(&playScene{inner: inner, player: p}, inner)
	}
}

// next returns the input of the next recorded tick.
func (p *Player) next() engine.Input {
	run := &p.rec.Runs[p.run]
	p.used++
	if p.used == run.Ticks {
		p.run, p.used = p.run+1, 0
	}
	p.played++
	return run.Input
}

// verify compares the state after tick p.played with the recording. The
// recorder takes a hash on every interval tick and on the last tick exactly
// when the scene implements engine.Hasher, so a hash on one side only is also
// a divergence.
func (p *Player) verify(inner engine.Scene) error {
	hasher, hashes := inner.(engine.Hasher)
	checkpoints := p.rec.Checkpoints
	if p.check < len(checkpoints) && checkpoints[p.check].Tick == p.played {
		want := checkpoints[p.check].Hash
		p.check++
		if !hashes {
			return &DivergenceError{
				Tick:   p.played,
				Detail: "the recording has a state hash but the scene has none",
			}
		}
		if got := hasher.StateHash(); got != want {
			return &DivergenceError{
				Tick: p.played, Detail: fmt.Sprintf("state hash %016x, recorded %016x", got, want),
			}
		}
		return nil
	}
	if hashes && (p.played%p.rec.Interval == 0 || p.played == p.ticks) {
		return &DivergenceError{
			Tick:   p.played,
			Detail: "the scene has a state hash but the recording has none",
		}
	}
	return nil
}

type playScene struct {
	inner  engine.Scene
	player *Player
}

func (s *playScene) Enter() error {
	if err := s.inner.Enter(); err != nil {
		return err
	}
	p := s.player
	if p.started {
		return nil
	}
	p.started = true
	if len(p.rec.Start) == 0 {
		return nil
	}
	snapshotter, ok := s.inner.(engine.Snapshotter)
	if !ok {
		s.inner.Exit()
		return errors.New("replay has a start state but the scene is not an engine.Snapshotter")
	}
	if err := snapshotter.Restore(p.rec.Start); err != nil {
		s.inner.Exit()
		return fmt.Errorf("restore replay start state: %w", err)
	}
	return nil
}

func (s *playScene) Update(tick engine.Tick) (engine.Transition, error) {
	p := s.player
	if p.Done() {
		return engine.Transition{}, nil
	}
	tick.Input = p.next()
	transition, err := s.inner.Update(tick)
	if err != nil {
		return transition, err
	}
	if err := p.verify(s.inner); err != nil {
		return engine.Transition{}, err
	}
	if factory, ok := transition.Replacement(); ok {
		return engine.Replace(p.wrap(factory)), nil
	}
	return transition, nil
}

func (s *playScene) Draw(canvas engine.Canvas) { s.inner.Draw(canvas) }

func (s *playScene) Exit() { s.inner.Exit() }

// Verify plays rec headlessly through engine.Start and Runtime.Step, one step
// per recorded tick, and stops the runtime. It returns nil when every
// checkpoint matches, a *DivergenceError for the first one that does not, an
// error wrapping ErrContentMismatch for a recording of other content, or the
// error of a scene.
func Verify(factory engine.SceneFactory, rec *Recording, expected Meta) error {
	p, err := Play(factory, rec, expected)
	if err != nil {
		return err
	}
	runtime, err := engine.Start(p.Factory())
	if err != nil {
		return err
	}
	for !p.Done() {
		if err := runtime.Step(engine.Input{}); err != nil {
			return err
		}
	}
	return runtime.Stop()
}
