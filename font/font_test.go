package font_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/font"
)

// recorder is an engine.Canvas that keeps only ImageTinted calls.
type recorder struct {
	engine.Canvas
	calls []call
}

type call struct {
	source     image.Rectangle
	x, y, w, h float32
	tint       color.NRGBA
}

func (r *recorder) ImageTinted(source image.Rectangle, x, y, w, h float32, tint color.NRGBA) {
	r.calls = append(r.calls, call{source, x, y, w, h, tint})
}

var red = color.NRGBA{R: 255, A: 255}

// The custom sheet has 4 × 5 cells in a 2 × 2 grid for "a", "b", and "c";
// glyph i marks its own top-left texel with alpha 10 × (i + 1).
const customJSON = `{"version":1,"cellWidth":4,"cellHeight":5,"baseline":4,"first":97,"count":3,"advances":[2,3,4]}`

func customPNG(tb testing.TB, size image.Point) []byte {
	tb.Helper()
	pixels := image.NewNRGBA(image.Rectangle{Max: size})
	for i := range 3 {
		pixels.SetNRGBA(i%2*4, i/2*5, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(10 * (i + 1))})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, pixels); err != nil {
		tb.Fatal(err)
	}
	return b.Bytes()
}

func decodeCustom(t *testing.T, metadata string) font.Face {
	t.Helper()
	face, err := font.Decode(
		bytes.NewReader(customPNG(t, image.Pt(8, 10))),
		strings.NewReader(metadata),
	)
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func TestCustomSheet(t *testing.T) {
	t.Parallel()
	face := decodeCustom(t, customJSON)
	if _, _, _, a := face.Image().At(4, 0).RGBA(); a != 20*0x101 {
		t.Fatalf("glyph b marker alpha = %d, want the decoded pixel", a)
	}
	if face.LineHeight() != 5 || face.Baseline() != 4 || face.Cell() != image.Pt(4, 5) ||
		face.First() != 'a' || face.Count() != 3 || face.Monospaced() {
		t.Fatalf("metrics %d %d %v %d %d %t", face.LineHeight(), face.Baseline(), face.Cell(),
			face.First(), face.Count(), face.Monospaced())
	}

	var canvas recorder
	face.InAtlas(image.Pt(100, 50)).Draw(&canvas, "ab\nzc", 10, 20, 2, red)
	want := []call{
		{image.Rect(100, 50, 104, 55), 10, 20, 8, 10, red},
		{image.Rect(104, 50, 108, 55), 14, 20, 8, 10, red},
		// No '?' glyph: z is skipped with a one-cell advance.
		{image.Rect(100, 55, 104, 60), 18, 30, 8, 10, red},
	}
	if fmt.Sprint(canvas.calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %v, want %v", canvas.calls, want)
	}
	if w, h := face.Measure("ab\nzc", 2); w != 16 || h != 20 {
		t.Fatalf("Measure = %d × %d, want 16 × 20", w, h)
	}
	if w, h := face.Measure("", 0); w != 0 || h != 5 {
		t.Fatalf("empty Measure = %d × %d, want 0 × 5", w, h)
	}

	monospace := decodeCustom(t, strings.Replace(customJSON, `,"advances":[2,3,4]`, "", 1))
	if w, _ := monospace.Measure("abc", 1); w != 12 {
		t.Fatalf("monospace width = %d, want 12", w)
	}
	full := decodeCustom(t, strings.Replace(customJSON, "[2,3,4]", "[4,4,4]", 1))
	if !monospace.Monospaced() || !full.Monospaced() || !font.Builtin().Monospaced() {
		t.Fatal("cell-wide advances must be monospaced")
	}
}

func TestMissingGlyph(t *testing.T) {
	t.Parallel()
	var canvas recorder
	// An invalid byte, a rune outside the font, and an encoded U+FFFD all draw '?'.
	font.Builtin().Draw(&canvas, "\xffé\uFFFD!", 0, 0, 0, red)
	question := image.Rect(15*6, 8, 16*6, 16)
	want := []call{
		{question, 0, 0, 6, 8, red},
		{question, 6, 0, 6, 8, red},
		{question, 12, 0, 6, 8, red},
		{image.Rect(6, 0, 12, 8), 18, 0, 6, 8, red},
	}
	if fmt.Sprint(canvas.calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %v, want %v", canvas.calls, want)
	}

	var zero font.Face
	zero.Draw(&canvas, "a\xff", 0, 0, 1, red)
	if len(canvas.calls) != len(want) {
		t.Fatal("the zero face drew a glyph")
	}
}

func TestDecodeInvalidMetadata(t *testing.T) {
	t.Parallel()
	doc := customJSON
	cases := map[string]string{
		"missing field":        strings.Replace(doc, `"baseline":4,`, "", 1),
		"unknown field":        strings.Replace(doc, `"first"`, `"First"`, 1),
		"duplicate field":      strings.Replace(doc, `"count":3`, `"count":3,"count":3`, 1),
		"null value":           strings.Replace(doc, `"count":3`, `"count":null`, 1),
		"null advances":        strings.Replace(doc, `[2,3,4]`, `null`, 1),
		"decimal":              strings.Replace(doc, `"cellWidth":4`, `"cellWidth":4.0`, 1),
		"exponent":             strings.Replace(doc, `"cellWidth":4`, `"cellWidth":4e0`, 1),
		"string number":        strings.Replace(doc, `"cellWidth":4`, `"cellWidth":"4"`, 1),
		"huge integer":         strings.Replace(doc, `"first":97`, `"first":99999999999999999999`, 1),
		"unsupported version":  strings.Replace(doc, `"version":1`, `"version":2`, 1),
		"zero cell":            strings.Replace(doc, `"cellWidth":4`, `"cellWidth":0`, 1),
		"large cell":           strings.Replace(doc, `"cellHeight":5`, `"cellHeight":33`, 1),
		"negative baseline":    strings.Replace(doc, `"baseline":4`, `"baseline":-1`, 1),
		"low baseline":         strings.Replace(doc, `"baseline":4`, `"baseline":6`, 1),
		"negative first":       strings.Replace(doc, `"first":97`, `"first":-1`, 1),
		"first beyond Unicode": strings.Replace(doc, `"first":97`, `"first":1114112`, 1),
		"range beyond Unicode": strings.Replace(doc, `"first":97`, `"first":1114110`, 1),
		"zero count":           strings.Replace(doc, `"count":3,"advances":[2,3,4]`, `"count":0`, 1),
		"count beyond cells":   strings.Replace(doc, `"count":3,"advances":[2,3,4]`, `"count":5`, 1),
		"short advances":       strings.Replace(doc, `[2,3,4]`, `[2,3]`, 1),
		"empty advances":       strings.Replace(doc, `[2,3,4]`, `[]`, 1),
		"wide advance":         strings.Replace(doc, `[2,3,4]`, `[2,3,5]`, 1),
		"negative advance":     strings.Replace(doc, `[2,3,4]`, `[2,-3,4]`, 1),
		"huge advance":         strings.Replace(doc, `[2,3,4]`, `[2,300,4]`, 1),
		"array root":           `[` + doc + `]`,
		"trailing JSON":        doc + `{}`,
		"truncated":            doc[:len(doc)-1],
		"invalid UTF-8":        doc + "\xff",
		"byte budget":          doc + strings.Repeat(" ", 16<<10),
	}
	for name, metadata := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			face, err := font.Decode(
				bytes.NewReader(customPNG(t, image.Pt(8, 10))),
				strings.NewReader(metadata),
			)
			if err == nil || face.Image() != nil {
				t.Fatalf("invalid metadata returned face %v, error %v", face, err)
			}
		})
	}
	// Trailing whitespace is allowed.
	decodeCustom(t, doc+" \n")
}

func TestDecodeInvalidPNG(t *testing.T) {
	t.Parallel()
	valid := customPNG(t, image.Pt(8, 10))
	noisy := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for i := range noisy.Pix {
		noisy.Pix[i] = uint8(i * 7919 >> 3) // #nosec G115 -- Deliberate wraparound noise.
	}
	var large bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&large, noisy); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"malformed":   []byte("not a PNG"),
		"truncated":   valid[:len(valid)-12],
		"wide axis":   customPNG(t, image.Pt(513, 10)),
		"tall axis":   customPNG(t, image.Pt(8, 513)),
		"byte budget": large.Bytes(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			face, err := font.Decode(bytes.NewReader(data), strings.NewReader(customJSON))
			if err == nil || face.Image() != nil {
				t.Fatalf("invalid PNG returned face %v, error %v", face, err)
			}
		})
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(customPNG(f, image.Pt(8, 10)), []byte(customJSON))
	f.Add(
		customPNG(f, image.Pt(8, 10)),
		[]byte(`{"version":1,"cellWidth":1,"cellHeight":1,"baseline":0,"first":0,"count":80}`),
	)
	f.Fuzz(func(t *testing.T, sheet, metadata []byte) {
		face, err := font.Decode(bytes.NewReader(sheet), bytes.NewReader(metadata))
		if err != nil {
			return
		}
		var canvas recorder
		face.Draw(&canvas, "abc?\n\xff\U0010FFFF", 0, 0, 1, red)
		bounds := face.Image().Bounds()
		for _, c := range canvas.calls {
			if !c.source.In(bounds) {
				t.Fatalf("glyph source %v is outside the sheet %v", c.source, bounds)
			}
		}
	})
}

func TestDrawAllocations(t *testing.T) {
	face := font.Builtin()
	canvas := recorder{calls: make([]call, 0, 64)}
	if allocs := testing.AllocsPerRun(100, func() {
		canvas.calls = canvas.calls[:0]
		face.Draw(&canvas, "Score: 12\nété \xff", 3, 4, 2, red)
		face.Measure("Score: 12\nété \xff", 2)
	}); allocs != 0 {
		t.Fatalf("Draw and Measure allocated %.0f times", allocs)
	}
}

type discard struct{ engine.Canvas }

func (discard) ImageTinted(image.Rectangle, float32, float32, float32, float32, color.NRGBA) {}

func BenchmarkDraw1000Glyphs(b *testing.B) {
	face := font.Builtin()
	line := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 3)[:100]
	text := strings.Repeat(line+"\n", 10)
	for b.Loop() {
		face.Draw(discard{}, text, 0, 0, 1, red)
	}
}
