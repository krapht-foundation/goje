// Package font draws bitmap text: one tinted launch-image region per glyph.
// A face comes from Builtin or from a font sheet v1 asset read by Decode; see
// specs/font-v1.md for the format and the drawing rules.
package font

import (
	"image"
	"image/color"
	"unicode/utf8"

	"github.com/krapht-foundation/goje/engine"
)

// Face is a read-only bitmap font placed in a launch image. Copies share the
// sheet and may be used from any goroutine. The zero value has no glyphs and
// draws nothing.
type Face struct {
	pixels   image.Image
	origin   image.Point // Sheet position in the launch image.
	cell     image.Point
	columns  int
	baseline int
	first    rune
	count    int
	advances []uint8 // Nil for a monospaced face.
}

func newFace(
	pixels image.Image,
	cell image.Point,
	baseline int,
	first rune,
	count int,
	advances []uint8,
) Face {
	return Face{
		pixels: pixels, cell: cell, columns: pixels.Bounds().Dx() / cell.X, baseline: baseline,
		first: first, count: count, advances: advances,
	}
}

// Image returns the font sheet. Callers must treat it as read-only.
func (f Face) Image() image.Image { return f.pixels }

// InAtlas returns the face with its sheet at offset in the launch image, as
// sprite.Clip.InAtlas does for clips. The original face is unchanged.
func (f Face) InAtlas(offset image.Point) Face {
	f.origin = f.origin.Add(offset)
	return f
}

// LineHeight is the distance between lines at scale 1: the cell height.
func (f Face) LineHeight() int { return f.cell.Y }

// Baseline is the distance from the top of a cell to the text baseline at
// scale 1, for aligning text with other art.
func (f Face) Baseline() int { return f.baseline }

// Draw paints text with the upper-left corner of its first cell at (x, y),
// using one ImageTinted call per glyph, each cell scale times its sheet size.
// A scale below 1 draws at scale 1. Each UTF-8 rune is one glyph, and \n
// starts a new line at x. Invalid bytes and runes the face lacks draw the '?'
// glyph if the face has one and otherwise advance by one cell width without
// drawing. Draw allocates nothing.
func (f Face) Draw(canvas engine.Canvas, text string, x, y float32, scale int, tint color.NRGBA) {
	scale = max(scale, 1)
	width, height := float32(f.cell.X*scale), float32(f.cell.Y*scale)
	penX := x
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		if r == '\n' {
			penX, y = x, y+height
			continue
		}
		glyph := f.glyph(r, size)
		if glyph >= 0 {
			corner := f.origin.Add(image.Pt(glyph%f.columns*f.cell.X, glyph/f.columns*f.cell.Y))
			canvas.ImageTinted(
				image.Rectangle{Min: corner, Max: corner.Add(f.cell)},
				penX,
				y,
				width,
				height,
				tint,
			)
		}
		penX += float32(f.advance(glyph) * scale)
	}
}

// Measure returns the size Draw covers: the widest line's total advance and
// the line count times the line height, both multiplied by scale. A scale
// below 1 measures at scale 1. Empty text is one empty line.
func (f Face) Measure(text string, scale int) (width, height int) {
	line, lines := 0, 1
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		if r == '\n' {
			width, line = max(width, line), 0
			lines++
			continue
		}
		line += f.advance(f.glyph(r, size))
	}
	scale = max(scale, 1)
	return max(width, line) * scale, lines * f.cell.Y * scale
}

// glyph returns the glyph index for a decoded rune, or -1 to skip it.
func (f Face) glyph(r rune, size int) int {
	if r == utf8.RuneError && size == 1 {
		return f.index('?')
	}
	if i := f.index(r); i >= 0 {
		return i
	}
	return f.index('?')
}

func (f Face) index(r rune) int {
	if i := int(r) - int(f.first); i >= 0 && i < f.count {
		return i
	}
	return -1
}

func (f Face) advance(glyph int) int {
	if glyph < 0 || f.advances == nil {
		return f.cell.X
	}
	return int(f.advances[glyph])
}
