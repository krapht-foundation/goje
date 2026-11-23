// Package tilemaker owns the pixels and undo history of one Tile Maker document.
package tilemaker

import (
	"image"
	"image/color"
	"image/draw"
	"io"

	"github.com/krapht-foundation/goje/tile"
)

const historyLimit = 64

// Pixels is a row-major snapshot in straight-alpha RGBA byte order.
type Pixels [tile.Width * tile.Height * 4]byte

var bounds = image.Rect(0, 0, tile.Width, tile.Height)

// Document holds one tile. Its zero value is a clean transparent tile.
// A document belongs to one editor and must not be copied after use.
type Document struct {
	pixels, saved Pixels
	// The ring keeps the state before the oldest retained transaction as
	// well as each committed result. The live stroke is stored separately.
	history             [historyLimit + 1]Pixels
	first, count, index int
	active, connected   bool
	last                image.Point
	ink                 color.NRGBA
}

// Open validates and normalizes a candidate without touching the active
// document. The caller may ask before replacing the active document with it.
func Open(source io.Reader) (*Document, error) {
	decoded, err := tile.Decode(source)
	if err != nil {
		return nil, err
	}
	doc := new(Document)
	draw.Draw(doc.image(), bounds, decoded, decoded.Bounds().Min, draw.Src)
	doc.saved = doc.pixels
	return doc, nil
}

// Snapshot returns the current pixels, including an active stroke.
func (d *Document) Snapshot() Pixels { return d.pixels }

// Dirty reports whether the visible pixels differ from the last saved image.
func (d *Document) Dirty() bool { return d.pixels != d.saved }

// ExportPNG encodes the last committed state. It does not mark a download as
// saved; the caller does that only after successfully handing off the bytes.
func (d *Document) ExportPNG() ([]byte, error) {
	if d.active {
		pixels := d.history[d.slot(d.index)]
		return tile.EncodePNG(pixelImage(&pixels))
	}
	return tile.EncodePNG(d.image())
}

// MarkExported records the committed pixels after a synchronous download
// handoff. Call it before accepting another edit.
func (d *Document) MarkExported() {
	if d.active {
		d.saved = d.history[d.slot(d.index)]
		return
	}
	d.saved = d.pixels
}

// Begin starts a stroke at an in-bounds pixel and captures the tool for the
// entire gesture. An eraser uses alpha zero; a pencil paints opaquely.
func (d *Document) Begin(at image.Point, ink color.NRGBA) bool {
	if d.active || !at.In(bounds) {
		return false
	}
	if d.count == 0 {
		d.history[0], d.count = d.pixels, 1
	}
	if ink.A == 0 {
		ink = color.NRGBA{}
	} else {
		ink.A = 255
	}
	d.active, d.connected, d.last, d.ink = true, true, at, ink
	d.paint(at)
	return true
}

// Extend paints through the sample. An out-of-bounds sample breaks the line
// so a later reentry cannot paint across the gap.
func (d *Document) Extend(at image.Point) {
	if !d.active {
		return
	}
	if !at.In(bounds) {
		d.connected = false
		return
	}
	if d.connected {
		d.line(d.last, at)
	} else {
		d.paint(at)
	}
	d.last, d.connected = at, true
}

// Commit finishes the stroke and returns whether it changed the image.
func (d *Document) Commit() bool {
	if !d.active {
		return false
	}
	d.active = false
	if d.pixels == d.history[d.slot(d.index)] {
		return false
	}
	if d.index == historyLimit {
		d.first = d.slot(1)
	} else {
		d.index++
	}
	d.count = d.index + 1
	d.history[d.slot(d.index)] = d.pixels
	return true
}

// Cancel rolls back an active stroke without changing undo or redo history.
func (d *Document) Cancel() {
	if d.active {
		d.pixels = d.history[d.slot(d.index)]
		d.active = false
	}
}

// CanUndo reports whether a committed edit can be undone now.
func (d *Document) CanUndo() bool { return !d.active && d.index > 0 }

// CanRedo reports whether an undone edit can be restored now.
func (d *Document) CanRedo() bool { return !d.active && d.index+1 < d.count }

// Undo restores one committed edit's previous pixels.
func (d *Document) Undo() bool {
	if !d.CanUndo() {
		return false
	}
	d.index--
	d.pixels = d.history[d.slot(d.index)]
	return true
}

// Redo restores one undone edit.
func (d *Document) Redo() bool {
	if !d.CanRedo() {
		return false
	}
	d.index++
	d.pixels = d.history[d.slot(d.index)]
	return true
}

func (d *Document) slot(index int) int { return (d.first + index) % len(d.history) }

func (d *Document) image() *image.NRGBA { return pixelImage(&d.pixels) }

func pixelImage(pixels *Pixels) *image.NRGBA {
	return &image.NRGBA{Pix: pixels[:], Stride: 4 * tile.Width, Rect: bounds}
}

func (d *Document) paint(at image.Point) {
	i := 4 * (at.Y*tile.Width + at.X)
	d.pixels[i], d.pixels[i+1], d.pixels[i+2], d.pixels[i+3] = d.ink.R, d.ink.G, d.ink.B, d.ink.A
}

// Rasterize in integer coordinates so fast pointer motion cannot skip pixels.
func (d *Document) line(from, to image.Point) {
	dx, sx := to.X-from.X, 1
	if dx < 0 {
		dx, sx = -dx, -1
	}
	dy, sy := to.Y-from.Y, 1
	if dy < 0 {
		dy, sy = -dy, -1
	}
	balance := dx - dy
	for from != to {
		twice := 2 * balance
		if twice > -dy {
			balance -= dy
			from.X += sx
		}
		if twice < dx {
			balance += dx
			from.Y += sy
		}
		d.paint(from)
	}
}
