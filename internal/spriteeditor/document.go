// Package spriteeditor owns the Sprite Clip Editor's headless document and
// metadata history. It does not choose files or own preview state.
package spriteeditor

import (
	"errors"
	"fmt"
	"image"
	"io"
	"slices"

	"github.com/krapht-foundation/goje/internal/spriteformat"
)

const historyLimit = 64

// Document owns one open sheet. Its zero value has no open image.
// Committed snapshots share untouched frame slices and never copy the PNG.
type Document struct {
	pixels image.Image
	states [][]spriteformat.Clip
	cursor int
	saved  []spriteformat.Clip
}

// Open stages a PNG and an optional sidecar before asking to replace dirty work.
// A nil sidecar starts an empty draft. If confirmation is absent or declines,
// Open returns false without changing the current sheet or history.
func (d *Document) Open(png, sidecar io.Reader, confirmDiscard func() bool) (bool, error) {
	sheet, err := spriteformat.ReadPNG(png)
	if err != nil {
		return false, err
	}
	var clips []spriteformat.Clip
	if sidecar != nil {
		if decodeErr := spriteformat.DecodeClips(sidecar, sheet.Size(), func(clip spriteformat.Clip) {
			clips = append(clips, clip)
		}); decodeErr != nil {
			return false, fmt.Errorf("sprite clips: %w", decodeErr)
		}
	}
	pixels, err := sheet.Decode()
	if err != nil {
		return false, err
	}
	if d.Dirty() && (confirmDiscard == nil || !confirmDiscard()) {
		return false, nil
	}
	states := make([][]spriteformat.Clip, 1, historyLimit+1)
	states[0] = clips
	*d = Document{pixels: pixels, states: states, saved: clips}
	return true, nil
}

// Image returns read-only pixels, or nil before the first successful open.
func (d *Document) Image() image.Image { return d.pixels }

// Clips returns the current snapshot in authored order. Neither the returned
// slice nor its frame slices may be modified, even after later edits.
func (d *Document) Clips() []spriteformat.Clip {
	if d.pixels == nil {
		return nil
	}
	return d.states[d.cursor]
}

// Export returns the sidecar without marking it saved or rewriting the image.
func (d *Document) Export() ([]byte, error) {
	if d.pixels == nil {
		return nil, errors.New("open a sprite sheet first")
	}
	return spriteformat.Encode(d.pixels.Bounds().Size(), d.Clips())
}

// MarkSaved records a successful download handoff. Call it after handing off
// the bytes from Export, with no intervening edit.
func (d *Document) MarkSaved() { d.saved = d.Clips() }

// Dirty reports whether the ordered metadata differs from the saved revision.
func (d *Document) Dirty() bool { return !sameClips(d.Clips(), d.saved) }

// CanUndo reports whether Undo has a previous commit to restore.
func (d *Document) CanUndo() bool { return d.cursor > 0 }

// CanRedo reports whether Redo has an undone commit to restore.
func (d *Document) CanRedo() bool { return d.cursor+1 < len(d.states) }

// Undo moves to the previous commit, if any.
func (d *Document) Undo() bool {
	if !d.CanUndo() {
		return false
	}
	d.cursor--
	return true
}

// Redo advances to the next commit, if no new edit replaced it.
func (d *Document) Redo() bool {
	if !d.CanRedo() {
		return false
	}
	d.cursor++
	return true
}

func (d *Document) apply(clips []spriteformat.Clip) error {
	if d.pixels == nil {
		return errors.New("open a sprite sheet first")
	}
	if err := spriteformat.Validate(d.pixels.Bounds().Size(), clips); err != nil {
		return err
	}
	if sameClips(d.Clips(), clips) {
		return nil
	}
	cut := d.cursor + 1
	clear(d.states[cut:])
	d.states = d.states[:cut]
	if cut == cap(d.states) {
		copy(d.states, d.states[1:])
		d.states = d.states[:cut-1]
	}
	d.states = append(d.states, clips)
	d.cursor = len(d.states) - 1
	return nil
}

func sameClips(a, b []spriteformat.Clip) bool {
	return slices.EqualFunc(a, b, func(a, b spriteformat.Clip) bool {
		return a.Name == b.Name && slices.Equal(a.Frames, b.Frames)
	})
}
