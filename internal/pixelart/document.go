// Package pixelart owns one editable sprite sheet and its pixel history. It
// stages assets independently of the browser's file picker and discard prompt.
package pixelart

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"

	"github.com/krapht-foundation/goje/internal/spriteformat"
)

// Document is a single-owner sheet created by New or Open. Do not copy it.
type Document struct {
	pixels *image.NRGBA
	saved  []byte
	dirty  int // Count of pixels different from saved, including the live stroke.

	pending           stroke
	active, connected bool
	last              image.Point

	history              []stroke
	cursor, historyBytes int
}

// New stages a clean, transparent sheet without affecting the current document.
func New(size image.Point) (*Document, error) {
	if size.X < 1 || size.X > spriteformat.MaxPNGAxis || size.Y < 1 ||
		size.Y > spriteformat.MaxPNGAxis {
		return nil, fmt.Errorf("artwork axes must be 1–%d pixels", spriteformat.MaxPNGAxis)
	}
	pixels := image.NewNRGBA(image.Rectangle{Max: size})
	return &Document{pixels: pixels, saved: make([]byte, len(pixels.Pix))}, nil
}

// Open stages a clean candidate. A failed decode cannot change the live sheet.
// The normalized pixels use straight 8-bit alpha, not the source's bit depth.
func Open(r io.Reader) (*Document, error) {
	checked, err := spriteformat.ReadPNG(r)
	if err != nil {
		return nil, err
	}
	decoded, err := checked.Decode()
	if err != nil {
		return nil, err
	}
	pixels, ok := decoded.(*image.NRGBA)
	if !ok {
		pixels = image.NewNRGBA(image.Rectangle{Max: checked.Size()})
		draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	}
	return &Document{pixels: pixels, saved: bytes.Clone(pixels.Pix)}, nil
}

// Size returns the width and height of the sheet in pixels.
func (d *Document) Size() image.Point { return d.pixels.Rect.Size() }

// Pixels lends packed, read-only RGBA bytes, including a live stroke. Copy the
// view to retain a snapshot across edits; callers must not modify the buffer.
func (d *Document) Pixels() []byte { return d.pixels.Pix }

// Dirty reports whether any visible pixel differs from the saved artwork.
func (d *Document) Dirty() bool { return d.dirty != 0 }

// ExportPNG encodes a committed sheet without marking it saved. Finish or cancel
// a stroke before exporting so the handoff always describes a stable revision.
func (d *Document) ExportPNG() ([]byte, error) {
	if d.active {
		return nil, errors.New("finish the active stroke before exporting")
	}
	var output pngOutput
	if err := png.Encode(&output, d.pixels); err != nil {
		return nil, fmt.Errorf("encode artwork PNG: %w", err)
	}
	return output, nil
}

// MarkExported records a successful handoff. Call it immediately after the
// browser accepts the bytes from ExportPNG, before applying any further edits.
func (d *Document) MarkExported() {
	if !d.active && d.Dirty() {
		copy(d.saved, d.pixels.Pix)
		d.dirty = 0
	}
}

// pngOutput stops buffering as soon as the result cannot be opened as v1 art.
type pngOutput []byte

func (out *pngOutput) Write(p []byte) (int, error) {
	if len(p) > spriteformat.MaxPNGBytes-len(*out) {
		return 0, fmt.Errorf("PNG exceeds %d bytes", spriteformat.MaxPNGBytes)
	}
	*out = append(*out, p...)
	return len(p), nil
}
