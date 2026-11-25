package font_test

import (
	"image"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/font"
)

// render draws text with the built-in font at scale 1 and returns the covered
// pixels as rows of '#' and '.'.
func render(t *testing.T, text string) string {
	t.Helper()
	face := font.Builtin()
	width, height := face.Measure(text, 1)
	var canvas recorder
	face.Draw(&canvas, text, 0, 0, 1, red)
	dump := make([][]byte, height)
	for y := range dump {
		dump[y] = []byte(strings.Repeat(".", width))
	}
	for _, c := range canvas.calls {
		if c.w != float32(c.source.Dx()) || c.h != float32(c.source.Dy()) || c.tint != red {
			t.Fatalf("unexpected call %v", c)
		}
		for y := range c.source.Dy() {
			for x := range c.source.Dx() {
				if _, _, _, a := face.Image().At(c.source.Min.X+x, c.source.Min.Y+y).RGBA(); a != 0 {
					dump[int(c.y)+y][int(c.x)+x] = '#'
				}
			}
		}
	}
	rows := make([]string, len(dump))
	for i, row := range dump {
		rows[i] = string(row)
	}
	return strings.Join(rows, "\n")
}

func TestBuiltinGlyphs(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"A!": `
.###....#...
#...#...#...
#...#...#...
#####...#...
#...#...#...
#...#.......
#...#...#...
............`,
		"g0": `
.......###..
......#...#.
.####.#..##.
#...#.#.#.#.
.####.##..#.
....#.#...#.
.###...###..
............`,
		"Hi\nyo": `
#...#...#...
#...#.......
#...#..##...
#####...#...
#...#...#...
#...#...#...
#...#..###..
............
............
............
#...#..###..
#...#.#...#.
.####.#...#.
....#.#...#.
.###...###..
............`,
	} {
		if got := render(t, text); got != strings.TrimPrefix(want, "\n") {
			t.Errorf("%q renders as\n%s\nwant\n%s", text, got, want)
		}
	}
}

func TestBuiltinSheet(t *testing.T) {
	t.Parallel()
	face := font.Builtin()
	sheet := face.Image()
	if sheet.Bounds() != image.Rect(0, 0, 96, 48) || face.LineHeight() != 8 || face.Baseline() != 7 {
		t.Fatalf(
			"sheet %v, line height %d, baseline %d",
			sheet.Bounds(),
			face.LineHeight(),
			face.Baseline(),
		)
	}
	for code := ' '; code <= '~'; code++ {
		var canvas recorder
		face.Draw(&canvas, string(code), 0, 0, 1, red)
		if len(canvas.calls) != 1 || canvas.calls[0].source.Size() != image.Pt(6, 8) {
			t.Fatalf("%q draws %v, want one 6 × 8 cell", code, canvas.calls)
		}
		cell := canvas.calls[0].source
		inked := 0
		for y := cell.Min.Y; y < cell.Max.Y; y++ {
			for x := cell.Min.X; x < cell.Max.X; x++ {
				r, g, b, a := sheet.At(x, y).RGBA()
				switch {
				case a == 0:
				case r != 0xffff || g != 0xffff || b != 0xffff || a != 0xffff:
					t.Fatalf("%q has a pixel that is not opaque white", code)
				case x == cell.Max.X-1 || y == cell.Max.Y-1:
					// The empty last column and row keep neighboring glyphs apart.
					t.Fatalf("%q touches the last column or row of its cell", code)
				default:
					inked++
				}
			}
		}
		if (inked == 0) != (code == ' ') {
			t.Fatalf("%q has %d inked pixels", code, inked)
		}
	}
	if w, h := face.Measure("Goje\nM14", 3); w != 4*6*3 || h != 2*8*3 {
		t.Fatalf("Measure = %d × %d", w, h)
	}
}
