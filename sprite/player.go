package sprite

// Player tracks one actor's clip on the simulation loop. Its zero value is idle
// with no frame selected. Create a fresh Player for each actor and scene entry.
type Player struct {
	clip    Clip
	index   int
	elapsed int
}

// Step selects the clip for one tick. A new clip starts on frame zero without
// consuming any of its duration; a repeated clip consumes one tick and loops
// after its last frame. Passing an empty clip clears playback.
//
// Initialize the player by calling Step once with the initial clip at scene
// entry. After that, call Step once per simulation update, never from Draw.
func (p *Player) Step(clip Clip) {
	if len(clip.frames) == 0 {
		*p = Player{}
		return
	}
	// Clips share immutable frame storage. Compare identity, not every frame.
	if len(p.clip.frames) == 0 || &p.clip.frames[0] != &clip.frames[0] {
		*p = Player{clip: clip}
		return
	}
	p.elapsed++
	if p.elapsed == clip.frames[p.index].Ticks {
		p.elapsed = 0
		p.index++
		if p.index == len(clip.frames) {
			p.index = 0
		}
	}
}

// Frame returns the current frame without advancing playback. It returns the
// zero frame and false when no clip is selected.
func (p *Player) Frame() (Frame, bool) {
	if len(p.clip.frames) == 0 {
		return Frame{}, false
	}
	return p.clip.frames[p.index], true
}
