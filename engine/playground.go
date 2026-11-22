package engine

// Playground runs one game-owned scenario at a time. The game owns ticking,
// input, rendering, and resources that outlive a scenario. Use it on the game
// loop only, and do not copy it after initialization.
type Playground[T any] struct {
	load  func() T
	scene T
}

// NewPlayground loads the initial scene. On every call, load must return a fresh
// mutable scene configured for the same starting encounter. It may reuse assets
// that the game owns separately from scene state. load must not be nil.
func NewPlayground[T any](load func() T) *Playground[T] {
	return &Playground[T]{load: load, scene: load()}
}

// Scene returns the mutable scene for the current run. Do not retain the pointer
// or references to mutable scene state across Reset.
func (p *Playground[T]) Scene() *T { return &p.scene }

// Reset replaces the current scene using the same loader as initialization.
func (p *Playground[T]) Reset() { p.scene = p.load() }
