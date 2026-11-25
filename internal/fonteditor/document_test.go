package fonteditor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/krapht-foundation/goje/font"
)

// decode reads an exported pair back through the runtime decoder.
func decode(t *testing.T, d *Document) font.Face {
	t.Helper()
	sheet, err := d.ExportPNG()
	if err != nil {
		t.Fatal(err)
	}
	face, err := font.Decode(bytes.NewReader(sheet), bytes.NewReader(d.ExportJSON()))
	if err != nil {
		t.Fatalf("export fails font.Decode: %v", err)
	}
	return face
}

func pixels(face font.Face) []byte {
	sheet := face.Image().(*image.NRGBA)
	return sheet.Pix
}

func encodePNG(t *testing.T, pixels image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, pixels); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestBuiltinExportsExactPixels(t *testing.T) {
	t.Parallel()
	d, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if d.Dirty() || d.Cell() != image.Pt(6, 8) || d.Baseline() != 7 || d.First() != ' ' ||
		d.Count() != 95 || d.Columns() != 16 || d.Size() != image.Pt(96, 48) {
		t.Fatalf(
			"built-in metrics: %v %d %d %d %d %v",
			d.Cell(),
			d.Baseline(),
			d.First(),
			d.Count(),
			d.Columns(),
			d.Size(),
		)
	}
	const want = `{"version":1,"cellWidth":6,"cellHeight":8,"baseline":7,"first":32,"count":95}` + "\n"
	if got := string(d.ExportJSON()); got != want {
		t.Fatalf("JSON %q", got)
	}
	face := decode(t, d)
	if !bytes.Equal(pixels(face), pixels(font.Builtin())) {
		t.Fatal("exported sheet differs from the built-in pixels")
	}
}

func TestEditUndoRedoStaysInGlyph(t *testing.T) {
	t.Parallel()
	d, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	glyph := int('A' - ' ') // Cell (1, 2): sheet origin (6, 16).
	before := bytes.Clone(d.Pixels())
	at := func(pix []byte, x, y int) [4]byte { return [4]byte(pix[((16+y)*96+6+x)*4:]) }
	on, off := [4]byte{255, 255, 255, 255}, [4]byte{}
	for _, bad := range []struct {
		glyph int
		at    image.Point
	}{{-1, image.Pt(0, 0)}, {95, image.Pt(0, 0)}, {glyph, image.Pt(6, 0)}, {glyph, image.Pt(0, -1)}} {
		if d.Begin(bad.glyph, bad.at, false) {
			t.Fatalf("began outside the font: %+v", bad)
		}
	}
	if !d.Begin(glyph, image.Pt(5, 0), false) {
		t.Fatal("Begin refused the last column")
	}
	d.Extend(image.Pt(6, 0)) // Leaves the cell: the line breaks.
	d.Extend(image.Pt(5, 3)) // Re-entering paints only this pixel.
	d.Extend(image.Pt(5, 5)) // Then interpolates.
	if !d.Commit() || !d.Dirty() {
		t.Fatal("stroke not committed")
	}
	for _, y := range []int{0, 3, 4, 5} {
		if at(d.Pixels(), 5, y) != on {
			t.Fatalf("pixel (5, %d) not painted", y)
		}
	}
	// Glyph-relative (6, 0) is the first texel of 'B'.
	if at(d.Pixels(), 5, 1) != off || at(d.Pixels(), 6, 0) != at(before, 6, 0) {
		t.Fatal("stroke crossed a gap or a neighboring glyph")
	}
	if !d.Begin(glyph, image.Pt(0, 1), true) || !d.Commit() || at(d.Pixels(), 0, 1) != off {
		t.Fatal("eraser did not clear")
	}
	if !d.Undo() || at(d.Pixels(), 0, 1) != on || !d.Undo() || !bytes.Equal(d.Pixels(), before) ||
		d.Dirty() {
		t.Fatal("undo did not restore the built-in pixels")
	}
	if !d.Redo() || at(d.Pixels(), 5, 4) != on {
		t.Fatal("redo did not repaint")
	}
	d.MarkExported()
	if d.Dirty() {
		t.Fatal("MarkExported left the font dirty")
	}
	face := decode(t, d)
	if !bytes.Equal(pixels(face), d.Pixels()) {
		t.Fatal("export does not decode to the edited pixels")
	}
}

func TestOpenRelaysOutAndKeepsPixels(t *testing.T) {
	t.Parallel()
	// Three 4 × 5 glyphs in a 2 × 2 grid, with a translucent texel per glyph
	// and a stray texel in the unused fourth cell.
	sheet := image.NewNRGBA(image.Rect(0, 0, 9, 11))
	for i := range 4 {
		sheet.SetNRGBA(i%2*4+3, i/2*5+4, color.NRGBA{R: 200, G: 100, B: 50, A: uint8(40 * (i + 1))})
	}
	for _, metadata := range []string{
		`{"version":1,"cellWidth":4,"cellHeight":5,"baseline":3,"first":97,"count":3}`,
		`{"version":1,"cellWidth":4,"cellHeight":5,"baseline":3,"first":97,"count":3,"advances":[4,4,4]}`,
	} {
		d, err := Open(bytes.NewReader(encodePNG(t, sheet)), strings.NewReader(metadata))
		if err != nil {
			t.Fatal(err)
		}
		if d.Size() != image.Pt(12, 5) || d.Columns() != 3 || d.Dirty() {
			t.Fatalf("layout %v, %d columns", d.Size(), d.Columns())
		}
		const want = `{"version":1,"cellWidth":4,"cellHeight":5,"baseline":3,"first":97,"count":3}` + "\n"
		if string(d.ExportJSON()) != want {
			t.Fatalf("JSON %q", d.ExportJSON())
		}
		face := decode(t, d)
		for i := range 4 {
			got := face.Image().At(i*4+3, 4)
			want := color.NRGBA{R: 200, G: 100, B: 50, A: uint8(40 * (i + 1))}
			if i == 3 {
				want = color.NRGBA{} // Not a glyph: the sheet has no fourth cell.
			}
			if got != want {
				t.Fatalf("glyph %d texel %v, want %v", i, got, want)
			}
		}
	}
}

func TestOpenRejects(t *testing.T) {
	t.Parallel()
	sheet := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 8, 10)))
	for metadata, reason := range map[string]string{
		`{"version":1,"cellWidth":4,"cellHeight":5,"baseline":3,"first":97,"count":3,"advances":[2,3,4]}`: "monospaced",
		`{"version":2,"cellWidth":4,"cellHeight":5,"baseline":3,"first":97,"count":3}`:                    "version",
		`{"version":1,"cellWidth":4,"cellHeight":5,"baseline":3,"first":97,"count":5}`:                    "fit",
	} {
		if d, err := Open(
			bytes.NewReader(sheet),
			strings.NewReader(metadata),
		); err == nil ||
			d != nil ||
			!strings.Contains(err.Error(), reason) {
			t.Errorf("%s: %v, want %q", metadata, err, reason)
		}
	}
}

func TestNewLimits(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cell            image.Point
		baseline, count int
		first           rune
		size            image.Point
	}{
		{image.Pt(8, 10), 8, 3, 'A', image.Pt(24, 10)},
		{image.Pt(1, 1), 0, 512 * 512, 0, image.Pt(512, 512)},
		{image.Pt(32, 32), 32, 256, 0, image.Pt(512, 512)},
		{image.Pt(32, 1), 1, 16 * 512, 0, image.Pt(512, 512)},
		{image.Pt(1, 32), 0, 512 * 16, 0, image.Pt(512, 512)},
		{image.Pt(3, 7), 7, 1, utf8.MaxRune, image.Pt(3, 7)},
	} {
		d, err := New(c.cell, c.baseline, c.first, c.count)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if d.Size() != c.size || d.Dirty() {
			t.Fatalf("%+v: size %v", c, d.Size())
		}
		face := decode(t, d)
		if face.Count() != c.count || face.First() != c.first || face.Cell() != c.cell {
			t.Fatalf("%+v: decoded %v %d %d", c, face.Cell(), face.First(), face.Count())
		}
	}
	for _, c := range []struct {
		cell            image.Point
		baseline, count int
		first           rune
	}{
		{image.Pt(0, 8), 0, 1, 0},
		{image.Pt(33, 8), 0, 1, 0},
		{image.Pt(6, 33), 0, 1, 0},
		{image.Pt(6, 8), 9, 1, 0},
		{image.Pt(6, 8), -1, 1, 0},
		{image.Pt(6, 8), 7, 0, 0},
		{image.Pt(6, 8), 7, 2, utf8.MaxRune},
		{image.Pt(6, 8), 7, 1, -1},
		{image.Pt(32, 32), 7, 257, 0},
		{image.Pt(1, 1), 0, 512*512 + 1, 0},
	} {
		if d, err := New(c.cell, c.baseline, c.first, c.count); err == nil || d != nil {
			t.Errorf("accepted %+v", c)
		}
	}
}

func TestExportPNGLimit(t *testing.T) {
	t.Parallel()
	// 16 random colors per texel compress well as a palette and poorly as RGBA,
	// so the sheet opens but its canonical export exceeds the font PNG limit.
	palette := make(color.Palette, 16)
	random := rand.New(rand.NewPCG(1, 2))
	for i := range palette {
		palette[i] = color.NRGBA{
			R: uint8(random.Uint32()),
			G: uint8(random.Uint32()),
			B: uint8(random.Uint32()),
			A: 255,
		}
	}
	sheet := image.NewPaletted(image.Rect(0, 0, 512, 512), palette)
	for i := range sheet.Pix {
		sheet.Pix[i] = uint8(random.IntN(len(palette)))
	}
	source := encodePNG(t, sheet)
	if len(source) > font.MaxPNGBytes {
		t.Fatalf("fixture is %d bytes", len(source))
	}
	d, err := Open(bytes.NewReader(source), strings.NewReader(
		`{"version":1,"cellWidth":32,"cellHeight":32,"baseline":32,"first":0,"count":256}`))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := d.ExportPNG(); err == nil || data != nil ||
		!strings.Contains(err.Error(), "limit") {
		t.Fatalf("exported %d bytes: %v", len(data), err)
	}
}

func TestActiveStrokeBlocksExport(t *testing.T) {
	t.Parallel()
	d, err := New(image.Pt(2, 2), 2, 'a', 2)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Begin(1, image.Pt(1, 1), false) || d.Begin(0, image.Pt(0, 0), false) {
		t.Fatal("second Begin during a stroke")
	}
	if _, err := d.ExportPNG(); err == nil {
		t.Fatal("exported during a stroke")
	}
	d.Cancel()
	if d.Dirty() || d.CanUndo() {
		t.Fatal("Cancel kept the stroke")
	}
}
