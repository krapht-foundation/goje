package replay

import (
	"fmt"

	"github.com/krapht-foundation/goje/engine"
)

// Recorder collects the ticks of every scene created by Record. One recorder
// spans restarts and replacements, so a session is one recording. It stops
// recording, and Full reports true, after the tick that leaves too little room
// for another tick under MaxTicks or MaxEncodedSize, so Finish always returns a
// recording that Encode accepts. Use it from the game loop only.
type Recorder struct {
	rec   Recording
	ticks int
	// size is the encoded size of rec so far, used to stop before the limit.
	size int
	full bool
	done bool
	// The last recorded tick's hash is taken lazily, because most ticks are
	// not checkpoints: pending is the scene that ran it while it is active, and
	// exitHash holds its hash once it exits.
	pending  *recordScene
	exitHash uint64
	exited   bool
}

// NewRecorder returns a recorder for the game and content in meta. Start is
// the snapshot the first recorded scene begins from, such as a loaded save, or
// nil; the recorder retains it, so do not modify it. Capacity preallocates
// room for that many ticks, about 150 bytes each, so recording allocates
// nothing until the session grows past it.
func NewRecorder(meta Meta, start []byte, capacity int) (*Recorder, error) {
	if err := meta.Validate(); err != nil {
		return nil, fmt.Errorf("new recorder: %w", err)
	}
	if len(start) > MaxStartSize {
		return nil, fmt.Errorf(
			"new recorder: start state of %d bytes exceeds %d",
			len(start),
			MaxStartSize,
		)
	}
	if capacity < 0 || capacity > MaxTicks {
		return nil, fmt.Errorf("new recorder: capacity %d is outside 0-%d", capacity, MaxTicks)
	}
	if len(start) == 0 {
		start = nil
	}
	return &Recorder{
		rec: Recording{
			Meta: meta, Interval: CheckpointInterval, Start: start,
			Runs:        make([]Run, 0, capacity),
			Checkpoints: make([]Checkpoint, 0, capacity/CheckpointInterval+1),
		},
		size: headerSize + len(meta.Game) + len(start),
	}, nil
}

// Full reports whether the recorder stopped at its size or tick limit. The
// game keeps running; later ticks are not recorded.
func (r *Recorder) Full() bool { return r.full }

// Ticks returns the number of ticks recorded so far.
func (r *Recorder) Ticks() int { return r.ticks }

// Finish stops recording, takes the state hash after the last tick if the
// scene that ran it implements engine.Hasher, and returns the recording. Later
// calls return the same recording. Call it from the game loop, outside scene
// callbacks or after the runtime stops; the recording must not be modified
// while the recorder is still in use by scenes.
func (r *Recorder) Finish() *Recording {
	if !r.done {
		r.done = true
		switch {
		case r.pending != nil:
			r.checkpoint(r.pending.hasher.StateHash())
		case r.exited:
			r.checkpoint(r.exitHash)
		}
		r.pending, r.exited = nil, false
	}
	return &r.rec
}

// record appends one tick's input and reports whether it was recorded.
func (r *Recorder) record(in engine.Input) bool {
	if r.full || r.done {
		return false
	}
	if n := len(r.rec.Runs); n > 0 && sameInput(r.rec.Runs[n-1].Input, in) {
		r.rec.Runs[n-1].Ticks++
	} else {
		r.rec.Runs = append(r.rec.Runs, Run{Input: in, Ticks: 1})
		r.size += runSize(in)
	}
	r.ticks++
	r.pending, r.exited = nil, false
	return true
}

// updated runs after the scene s finished a recorded tick.
func (r *Recorder) updated(s *recordScene) {
	// Reserve room for one more run and two checkpoints, a periodic one and
	// the final one, so that the recording stays encodable.
	if r.ticks == MaxTicks || r.size+maxRunSize+2*checkpointSize > MaxEncodedSize {
		r.full = true
		if s.hasher != nil {
			r.checkpoint(s.hasher.StateHash())
		}
		return
	}
	if s.hasher == nil {
		return
	}
	if r.ticks%CheckpointInterval == 0 {
		r.checkpoint(s.hasher.StateHash())
		return
	}
	r.pending = s
}

func (r *Recorder) checkpoint(hash uint64) {
	r.rec.Checkpoints = append(r.rec.Checkpoints, Checkpoint{Tick: r.ticks, Hash: hash})
	r.size += checkpointSize
}

// Record returns a factory whose scenes record each tick's input into rec
// before the wrapped scene updates, and a state hash every
// CheckpointInterval ticks when the wrapped scene implements engine.Hasher.
// Restarts and replacements are wrapped too, so they keep recording. The
// scenes implement engine.Hasher and engine.Snapshotter exactly when the
// wrapped scene does.
func Record(factory engine.SceneFactory, rec *Recorder) engine.SceneFactory {
	if factory == nil {
		return nil
	}
	return func() engine.Scene {
		inner := factory()
		if inner == nil {
			return nil
		}
		s := &recordScene{inner: inner, rec: rec}
		if hasher, ok := inner.(engine.Hasher); ok {
			s.hasher = hasher
		}
		return expose(s, inner)
	}
}

type recordScene struct {
	inner  engine.Scene
	hasher engine.Hasher
	rec    *Recorder
}

func (s *recordScene) Enter() error { return s.inner.Enter() }

func (s *recordScene) Update(tick engine.Tick) (engine.Transition, error) {
	recorded := s.rec.record(tick.Input)
	transition, err := s.inner.Update(tick)
	if err != nil {
		return transition, err
	}
	if recorded {
		s.rec.updated(s)
	}
	if factory, ok := transition.Replacement(); ok {
		return engine.Replace(Record(factory, s.rec)), nil
	}
	return transition, nil
}

func (s *recordScene) Draw(canvas engine.Canvas) { s.inner.Draw(canvas) }

func (s *recordScene) Exit() {
	// Hash before Exit releases anything, in case this scene ran the last tick.
	if s.rec.pending == s {
		s.rec.exitHash, s.rec.exited, s.rec.pending = s.hasher.StateHash(), true, nil
	}
	s.inner.Exit()
}

// expose returns wrapper with the optional scene interfaces of inner, so type
// assertions on the wrapper succeed exactly when they would on inner. The
// wrapper itself must not implement them.
func expose(wrapper, inner engine.Scene) engine.Scene {
	hasher, hashes := inner.(engine.Hasher)
	snapshotter, snapshots := inner.(engine.Snapshotter)
	switch {
	case hashes && snapshots:
		return hashSnapScene{wrapper, hasher, snapshotter}
	case hashes:
		return hashScene{wrapper, hasher}
	case snapshots:
		return snapScene{wrapper, snapshotter}
	}
	return wrapper
}

type hashScene struct {
	engine.Scene
	engine.Hasher
}

type snapScene struct {
	engine.Scene
	engine.Snapshotter
}

type hashSnapScene struct {
	engine.Scene
	engine.Hasher
	engine.Snapshotter
}
