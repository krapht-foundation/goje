package sprite

import "github.com/krapht-foundation/goje/internal/spriteformat"

// Player tracks one actor's clip on the simulation loop. Its zero value is idle
// with no frame selected. Create a fresh Player for each actor and scene entry.
type Player struct {
	playback spriteformat.Playback
}

// Step selects the clip for one tick. A new clip starts on frame zero without
// consuming any of its duration; a repeated clip consumes one tick and loops
// after its last frame. Passing an empty clip clears playback.
//
// Initialize the player by calling Step once with the initial clip at scene
// entry. After that, call Step once per simulation update, never from Draw.
func (p *Player) Step(clip Clip) { p.playback.Step(clip.frames) }

// Frame returns the current frame without advancing playback. It returns the
// zero frame and false when no clip is selected.
func (p *Player) Frame() (Frame, bool) {
	frame, _, ok := p.playback.Frame()
	return Frame(frame), ok
}
