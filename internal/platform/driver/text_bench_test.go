package driver

import (
	"image/color"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/font"
)

// glyphs is the RFC 0015 budget: a screen with a thousand glyphs in it.
const glyphs = 1000

func drawGlyphs(frame *Frame, face font.Face, text string) {
	frame.reset()
	face.Draw(frame, text, 0, 0, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
}

func newGlyphFrame() (*Frame, font.Face, string) {
	face := font.Builtin()
	frame := &Frame{imageSize: face.Image().Bounds().Size()}
	return frame, face, strings.Repeat("A", glyphs)
}

// BenchmarkDrawThousandGlyphs measures building the vertices of a thousand
// tinted glyphs, the CPU side of a text-heavy frame on every host.
func BenchmarkDrawThousandGlyphs(b *testing.B) {
	frame, face, text := newGlyphFrame()
	b.ReportAllocs()
	for b.Loop() {
		drawGlyphs(frame, face, text)
	}
	if got := len(frame.Vertices); got != glyphs*6*VertexStride {
		b.Fatalf("frame holds %d vertex bytes, want %d", got, glyphs*6*VertexStride)
	}
}

func TestDrawingGlyphsDoesNotAllocateOnceTheFrameIsSized(t *testing.T) {
	frame, face, text := newGlyphFrame()
	drawGlyphs(frame, face, text) // Size the vertex buffer.
	if allocs := testing.AllocsPerRun(20, func() { drawGlyphs(frame, face, text) }); allocs != 0 {
		t.Errorf("drawing %d glyphs allocated %v times per frame", glyphs, allocs)
	}
}
