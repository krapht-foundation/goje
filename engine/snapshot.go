package engine

// Hasher is an optional Scene interface for replay checkpoints. StateHash
// returns a 64-bit digest of every piece of state that later updates can read,
// so two runs that agree on it are expected to behave the same from then on.
// It must not change the scene, and it must return the same value for the same
// state on every target. See specs/replay-v1.md.
type Hasher interface {
	StateHash() uint64
}

// Snapshotter is an optional Scene interface for saving and loading a whole
// game. Snapshot returns bytes that the caller owns, in a layout the scene
// chooses. Restore replaces the scene's state with a snapshot it produced
// earlier, rejecting bytes it cannot read without changing the scene. Restoring
// a snapshot and updating further must reach the same state as continuing the
// run that produced it. See specs/save-v1.md.
type Snapshotter interface {
	Snapshot() ([]byte, error)
	Restore([]byte) error
}

// Replacement returns the factory of a Replace transition. It reports false for
// the zero transition and for Restart. Scene wrappers such as the replay
// recorder use it to wrap the replacement scene in turn.
func (t Transition) Replacement() (SceneFactory, bool) {
	return t.factory, t.kind == replaceScene
}
