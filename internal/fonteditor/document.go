// Package fonteditor owns one editable monospaced font sheet v1 and its pixel
// history for the browser Font Editor. See specs/font-editor-m17.md.
package fonteditor

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"

	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/internal/pixelart"
)

// preferredColumns is the canonical glyphs per sheet row, as in the built-in font.
const preferredColumns = 16

// Document is a single-owner font created by Builtin, Open, or New. The
// embedded pixel document holds the canonical sheet, strokes, history, and the
// saved state; Begin, Extend, and ExportPNG replace its sheet-wide forms so
// edits stay inside one glyph cell and exports stay within the font limits.
// Do not copy it.
type Document struct {
	*pixelart.Document

	cell           image.Point
	baseline       int
	first          rune
	count, columns int
	stroke         image.Point // Sheet origin of the glyph cell being edited.
}

// Builtin stages the built-in font with its exact pixels.
func Builtin() (*Document, error) { return fromFace(font.Builtin()) }

// Open stages a font sheet v1 pair through font.Decode. It rejects a font
// whose advances differ from its cell width instead of dropping them.
func Open(sheetPNG, fontJSON io.Reader) (*Document, error) {
	face, err := font.Decode(sheetPNG, fontJSON)
	if err != nil {
		return nil, err
	}
	return fromFace(face)
}

// New stages a blank font. font.Decode checks the metrics against the largest
// sheet, so New accepts exactly the monospaced fonts the decoder can hold.
func New(cell image.Point, baseline int, first rune, count int) (*Document, error) {
	var blank bytes.Buffer
	if err := png.Encode(
		&blank,
		image.NewNRGBA(image.Rect(0, 0, font.MaxPNGAxis, font.MaxPNGAxis)),
	); err != nil {
		return nil, fmt.Errorf("encode blank sheet: %w", err)
	}
	return Open(&blank, bytes.NewReader(metadata(cell, baseline, first, count)))
}

// fromFace copies every glyph cell into the canonical layout: 16 glyphs per
// row, or more when that many rows would exceed the axis limit. Every face the
// decoder accepts fits, because its own sheet held the same cells.
func fromFace(face font.Face) (*Document, error) {
	if !face.Monospaced() {
		return nil, errors.New(
			"the Font Editor edits monospaced fonts only; this font has advances narrower than its cells",
		)
	}
	cell, count := face.Cell(), face.Count()
	columns := max(min(count, preferredColumns), ceilDiv(count, font.MaxPNGAxis/cell.Y))
	sheet := image.NewNRGBA(image.Rect(0, 0, columns*cell.X, ceilDiv(count, columns)*cell.Y))
	source := face.Image()
	sourceColumns := source.Bounds().Dx() / cell.X
	for i := range count {
		from := source.Bounds().Min.Add(origin(i, sourceColumns, cell))
		to := origin(i, columns, cell)
		draw.Draw(sheet, image.Rectangle{Min: to, Max: to.Add(cell)}, source, from, draw.Src)
	}
	return &Document{
		Document: pixelart.TakePixels(
			sheet,
		),
		cell:     cell,
		baseline: face.Baseline(),
		first:    face.First(),
		count:    count,
		columns:  columns,
	}, nil
}

// Cell returns the glyph cell size in pixels.
func (d *Document) Cell() image.Point { return d.cell }

// Baseline returns the distance from the top of a cell to the baseline.
func (d *Document) Baseline() int { return d.baseline }

// First returns the code point of glyph 0.
func (d *Document) First() rune { return d.first }

// Count returns the number of glyphs.
func (d *Document) Count() int { return d.count }

// Columns returns the glyphs per sheet row; glyph n is in cell n.
func (d *Document) Columns() int { return d.columns }

// Begin starts a stroke at a pixel of one glyph's cell, painting opaque white
// or, when erase is set, transparent. It reports false for a glyph or pixel
// outside the font or while a stroke is active.
func (d *Document) Begin(glyph int, at image.Point, erase bool) bool {
	if glyph < 0 || glyph >= d.count || !at.In(image.Rectangle{Max: d.cell}) {
		return false
	}
	ink := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if erase {
		ink = color.NRGBA{}
	}
	start := origin(glyph, d.columns, d.cell)
	if !d.Document.Begin(start.Add(at), ink) {
		return false
	}
	d.stroke = start
	return true
}

// Extend paints toward a pixel of the stroke's glyph cell. A pixel outside the
// cell breaks the line, so a stroke never reaches a neighboring glyph.
func (d *Document) Extend(at image.Point) {
	if !at.In(image.Rectangle{Max: d.cell}) {
		d.Document.Extend(image.Pt(-1, -1))
		return
	}
	d.Document.Extend(d.stroke.Add(at))
}

// ExportPNG encodes the committed sheet, or fails if it exceeds the font PNG
// limit. Like the JSON, it does not mark the font saved; call MarkExported.
func (d *Document) ExportPNG() ([]byte, error) {
	data, err := d.Document.ExportPNG()
	if err != nil {
		return nil, err
	}
	if len(data) > font.MaxPNGBytes {
		return nil, fmt.Errorf(
			"font PNG would be %d bytes, more than the %d-byte limit",
			len(data),
			font.MaxPNGBytes,
		)
	}
	return data, nil
}

// ExportJSON returns the canonical description of the exported sheet.
func (d *Document) ExportJSON() []byte {
	return metadata(d.cell, d.baseline, d.first, d.count)
}

func metadata(cell image.Point, baseline int, first rune, count int) []byte {
	return fmt.Appendf(nil,
		`{"version":1,"cellWidth":%d,"cellHeight":%d,"baseline":%d,"first":%d,"count":%d}`+"\n",
		cell.X, cell.Y, baseline, first, count)
}

func origin(glyph, columns int, cell image.Point) image.Point {
	return image.Pt(glyph%columns*cell.X, glyph/columns*cell.Y)
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }
