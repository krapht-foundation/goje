package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// ErrRuntimeStopped indicates that Step or Draw was called after shutdown.
var ErrRuntimeStopped = errors.New("runtime is stopped")

// ErrRuntimeBusy indicates a call to Step, Draw, or Stop from a scene callback.
var ErrRuntimeBusy = errors.New("runtime is in a lifecycle callback")

// Scene owns the mutable state for one run. Its callbacks run on the game loop.
// If Enter fails, it must clean up its partial setup; Exit is called only after
// a successful Enter. The host keeps shared resources alive until after Exit.
type Scene interface {
	Enter() error
	Update(Tick) (Transition, error)
	// Draw records the current state without changing it.
	Draw(Canvas)
	Exit()
}

// SceneFactory creates a fresh scene for each entry. It must return a non-nil scene.
type SceneFactory func() Scene

// Tick contains the number and input snapshot for one simulation update.
// Tick numbers begin at one and do not reset on scene transitions.
type Tick struct {
	Number uint64
	Input  Input
	sound  SoundOutput
}

// PlaySound requests a prepared, zero-based cue for this update. Missing
// devices and dropped cues never affect gameplay. Do not retain the Tick to
// request sound outside Update.
func (t Tick) PlaySound(cue int) {
	if t.sound != nil {
		t.sound.Play(cue)
	}
}

// SoundOutput is the host's nonblocking bridge from simulation to playback.
// Clear discards pending requests and invalidates the current cue on scene exit.
type SoundOutput interface {
	Play(cue int)
	Clear()
}

type transitionKind uint8

const (
	keepScene transitionKind = iota
	restartScene
	replaceScene
)

// Transition is a scene's request to change the active scene after an update.
// Its zero value leaves the current scene active.
type Transition struct {
	factory SceneFactory
	kind    transitionKind
}

// Restart requests a new scene from the active scene's factory.
func Restart() Transition { return Transition{kind: restartScene} }

// Replace requests a new scene from factory, which must not be nil.
func Replace(factory SceneFactory) Transition {
	return Transition{factory: factory, kind: replaceScene}
}

// Runtime owns one active scene. It does not schedule ticks or read a clock.
// Use it from one game loop, and do not copy it or call it from its callbacks.
type Runtime struct {
	factory    SceneFactory
	scene      Scene
	logContext context.Context
	logger     *slog.Logger
	sound      SoundOutput
	tick       uint64
	busy       bool
}

// Start creates and enters the first scene. On failure it returns no runtime.
func Start(factory SceneFactory) (*Runtime, error) {
	return StartWithLogger(context.Background(), factory, nil)
}

// StartWithLogger is Start with opt-in logging of successful scene transitions.
// The logger is local to this runtime and is never used for routine ticks.
func StartWithLogger(
	ctx context.Context,
	factory SceneFactory,
	logger *slog.Logger,
) (*Runtime, error) {
	r := &Runtime{logContext: ctx, logger: logger}
	if err := r.enter(factory); err != nil {
		return nil, err
	}
	return r, nil
}

// SetSoundOutput installs the host's sound bridge before the first Step.
// Headless callers can leave the bridge unset.
func (r *Runtime) SetSoundOutput(output SoundOutput) { r.sound = output }

// Step executes one update. A transition happens only after a successful
// update, and the resulting scene will not update until the next call. An
// update or transition error stops the runtime; it cannot be stepped again.
func (r *Runtime) Step(input Input) error {
	if r.busy {
		return ErrRuntimeBusy
	}
	if r.scene == nil {
		return ErrRuntimeStopped
	}
	r.busy = true
	defer func() { r.busy = false }()

	r.tick++
	transition, err := r.scene.Update(Tick{Number: r.tick, Input: input, sound: r.sound})
	if err != nil {
		r.exit()
		return fmt.Errorf("update scene at tick %d: %w", r.tick, err)
	}
	if transition.kind == keepScene {
		return nil
	}
	factory := transition.factory
	if transition.kind == restartScene {
		factory = r.factory
	}

	r.exit()
	if err := r.enter(factory); err != nil {
		return err
	}
	if r.logger != nil {
		action := "replace"
		if transition.kind == restartScene {
			action = "restart"
		}
		r.logger.LogAttrs(r.logContext, slog.LevelInfo, "Goje scene transitioned",
			slog.String("action", action), slog.Uint64("tick", r.tick))
	}
	return nil
}

// Draw records the active scene without advancing simulation. Following a
// successful transition, it draws the replacement even before its first step.
func (r *Runtime) Draw(canvas Canvas) error {
	if r.busy {
		return ErrRuntimeBusy
	}
	if r.scene == nil {
		return ErrRuntimeStopped
	}
	r.busy = true
	defer func() { r.busy = false }()
	r.scene.Draw(canvas)
	return nil
}

// Stop exits the active scene once. A host calls it before releasing shared
// resources even after an error; it is safe to call more than once.
func (r *Runtime) Stop() error {
	if r.busy {
		return ErrRuntimeBusy
	}
	r.busy = true
	defer func() { r.busy = false }()
	r.exit()
	return nil
}

func (r *Runtime) enter(factory SceneFactory) error {
	if factory == nil {
		return errors.New("enter scene: nil factory")
	}
	scene := factory()
	if scene == nil {
		return errors.New("enter scene: factory returned nil")
	}
	if err := scene.Enter(); err != nil {
		return fmt.Errorf("enter scene: %w", err)
	}
	r.factory, r.scene = factory, scene
	return nil
}

func (r *Runtime) exit() {
	if scene := r.scene; scene != nil {
		// Drop ownership before the callback so a failed transition cannot restore this scene.
		r.factory, r.scene = nil, nil
		if r.sound != nil {
			r.sound.Clear()
		}
		scene.Exit()
	}
}
