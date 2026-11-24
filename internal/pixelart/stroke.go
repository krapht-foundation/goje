package pixelart

import (
	"image"
	"image/color"

	"github.com/krapht-foundation/goje/internal/pixelstroke"
)

const (
	maxEdits        = 64
	maxHistoryBytes = 8 << 20
	changeBytes     = 8 // One 32-bit offset and four straight-alpha channels.
)

type pixel = [4]byte

type change struct {
	offset uint32
	before pixel
}

// A gesture has one ink, so redo needs only one new color for the whole stroke.
type stroke struct {
	changes []change
	ink     pixel
}

func (s stroke) bytes() int { return cap(s.changes) * changeBytes }

// Begin captures the ink at an in-bounds sample. Any nonzero alpha selects an
// opaque pencil; zero alpha selects a transparent eraser.
func (d *Document) Begin(at image.Point, ink color.NRGBA) bool {
	if d.active || !at.In(d.pixels.Rect) {
		return false
	}
	if ink.A == 0 {
		d.pending.ink = pixel{}
	} else {
		d.pending.ink = pixel{ink.R, ink.G, ink.B, 255}
	}
	d.active, d.connected, d.last = true, true, at
	d.paint(at)
	return true
}

// Extend paints toward the next point. An out-of-bounds point breaks the line
// so a later in-bounds sample cannot paint across the gap.
func (d *Document) Extend(at image.Point) {
	if !d.active {
		return
	}
	if !at.In(d.pixels.Rect) {
		d.connected = false
		return
	}
	if d.connected {
		pixelstroke.Line(d.last, at, d.paint)
	} else {
		d.paint(at)
	}
	d.last, d.connected = at, true
}

// Commit retains a changed stroke as one transaction and discards any redo.
func (d *Document) Commit() bool {
	if !d.active {
		return false
	}
	d.active = false
	if len(d.pending.changes) == 0 {
		return false
	}
	for _, undone := range d.history[d.cursor:] {
		d.historyBytes -= undone.bytes()
	}
	clear(d.history[d.cursor:])
	d.history = d.history[:d.cursor]
	// Budget backing capacity, not just the entries in use, so evicted slices
	// release the actual retained storage. Even a full sheet fits in one edit.
	d.historyBytes += d.pending.bytes()
	oldest := 0
	for d.historyBytes > maxHistoryBytes || len(d.history)-oldest == maxEdits {
		d.historyBytes -= d.history[oldest].bytes()
		oldest++
	}
	if oldest != 0 {
		remaining := copy(d.history, d.history[oldest:])
		clear(d.history[remaining:])
		d.history = d.history[:remaining]
	}
	d.history = append(d.history, d.pending)
	d.cursor = len(d.history)
	d.pending = stroke{}
	return true
}

// Cancel undoes the live stroke without recording an edit or discarding redo.
func (d *Document) Cancel() {
	if !d.active {
		return
	}
	for _, c := range d.pending.changes {
		d.set(int(c.offset), c.before)
	}
	d.pending = stroke{}
	d.active = false
}

// CanUndo reports whether a committed stroke can be undone now.
func (d *Document) CanUndo() bool { return !d.active && d.cursor > 0 }

// CanRedo reports whether a committed stroke can be reapplied now.
func (d *Document) CanRedo() bool { return !d.active && d.cursor < len(d.history) }

// Undo restores the pixels before the last committed stroke.
func (d *Document) Undo() bool {
	if !d.CanUndo() {
		return false
	}
	d.cursor--
	for _, c := range d.history[d.cursor].changes {
		d.set(int(c.offset), c.before)
	}
	return true
}

// Redo restores the pixels from the next retained stroke.
func (d *Document) Redo() bool {
	if !d.CanRedo() {
		return false
	}
	s := d.history[d.cursor]
	for _, c := range s.changes {
		d.set(int(c.offset), s.ink)
	}
	d.cursor++
	return true
}

func (d *Document) paint(at image.Point) {
	offset := d.pixels.PixOffset(at.X, at.Y)
	before := pixel(d.pixels.Pix[offset:])
	if before == d.pending.ink {
		return // Includes repeated visits during the same stroke.
	}
	//nolint:gosec // G115: Validated canvas coordinates keep the offset below 1 MiB.
	d.pending.changes = append(d.pending.changes, change{offset: uint32(offset), before: before})
	d.set(offset, d.pending.ink)
}

func (d *Document) set(offset int, next pixel) {
	current := (*pixel)(d.pixels.Pix[offset:])
	saved := pixel(d.saved[offset:])
	// Update just this pixel so a pointer sample never scans the whole sheet.
	if *current != saved {
		d.dirty--
	}
	if next != saved {
		d.dirty++
	}
	*current = next
}
