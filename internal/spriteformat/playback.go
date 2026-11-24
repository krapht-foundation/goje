package spriteformat

// Playback is the fixed-tick frame cursor shared by the game runtime and the
// editor preview. Its zero value is idle with no frame selected.
type Playback struct {
	frames  []Frame
	index   int
	elapsed int
}

// Playing reports whether frames is the current non-empty sequence. Sequences
// share immutable storage, so identity rather than contents decides.
func (p *Playback) Playing(frames []Frame) bool {
	return len(frames) != 0 && len(p.frames) == len(frames) && &p.frames[0] == &frames[0]
}

// Step selects frames for one tick. A new sequence starts on frame zero
// without consuming any of its duration; the current one consumes one tick and
// loops after its last frame. An empty sequence clears playback.
func (p *Playback) Step(frames []Frame) {
	if !p.Playing(frames) {
		*p = Playback{frames: frames}
		return
	}
	p.elapsed++
	if p.elapsed == frames[p.index].Ticks {
		p.elapsed = 0
		p.index++
		if p.index == len(frames) {
			p.index = 0
		}
	}
}

// Frame returns the current frame and its index without advancing playback.
// It returns false when no sequence is selected.
func (p *Playback) Frame() (Frame, int, bool) {
	if len(p.frames) == 0 {
		return Frame{}, 0, false
	}
	return p.frames[p.index], p.index, true
}
