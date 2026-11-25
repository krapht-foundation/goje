package driver

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestFrameImageRegion(t *testing.T) {
	source := image.Rect(4, 2, 12, 6)
	var frame Frame
	frame.Image(source, 7, 9, 16, 12)
	if len(frame.Vertices) != 0 {
		t.Fatal("recorded textured vertices without a launch image")
	}

	frame.imageSize = image.Pt(16, 8)
	for _, invalid := range []image.Rectangle{
		image.Rect(-1, 0, 1, 1), image.Rect(0, 0, 17, 8), image.Rect(0, 0, 16, 9),
		image.Rect(0, 2, 2, 2),
	} {
		frame.Image(invalid, 7, 9, 16, 12)
	}
	if len(frame.Vertices) != 0 {
		t.Fatal("recorded textured vertices for an invalid source region")
	}

	frame.Image(source, 7, 9, 16, 12)
	want := [][4]float32{
		{7, 9, 0.25, 0.25},
		{23, 9, 0.75, 0.25},
		{23, 21, 0.75, 0.75},
		{7, 9, 0.25, 0.25},
		{23, 21, 0.75, 0.75},
		{7, 21, 0.25, 0.75},
	}
	if len(frame.Vertices) != len(want)*VertexStride {
		t.Fatalf("got %d vertex bytes, want %d", len(frame.Vertices), len(want)*VertexStride)
	}
	for i, expected := range want {
		var got [4]float32
		for j := range got {
			got[j] = math.Float32frombits(binary.LittleEndian.Uint32(frame.Vertices[i*VertexStride+j*4:]))
		}
		if got != expected {
			t.Errorf("vertex %d = %v, want %v", i, got, expected)
		}
	}
}

func TestFrameImageTinted(t *testing.T) {
	frame := Frame{imageSize: image.Pt(16, 8)}
	source := image.Rect(4, 2, 12, 6)
	tint := color.NRGBA{R: 200, G: 100, B: 50, A: 128}
	frame.ImageTinted(source, 7, 9, 16, 12, tint)
	tinted := bytes.Clone(frame.Vertices)
	frame.Vertices = frame.Vertices[:0]
	frame.Image(source, 7, 9, 16, 12)
	if len(tinted) != 6*VertexStride || len(frame.Vertices) != len(tinted) {
		t.Fatalf(
			"got %d tinted and %d plain vertex bytes, want %d",
			len(tinted),
			len(frame.Vertices),
			6*VertexStride,
		)
	}
	for i := range 6 {
		vertex := i * VertexStride
		// Positions and UVs match Image; only the color bytes carry the tint.
		if !bytes.Equal(tinted[vertex:vertex+16], frame.Vertices[vertex:vertex+16]) {
			t.Errorf("vertex %d geometry differs from Image", i)
		}
		if got := [4]byte(tinted[vertex+16 : vertex+20]); got != [4]byte{200, 100, 50, 128} {
			t.Errorf("vertex %d color = %v, want the straight-alpha tint", i, got)
		}
		if got := [4]byte(frame.Vertices[vertex+16 : vertex+20]); got != [4]byte{255, 255, 255, 255} {
			t.Errorf("vertex %d color = %v, want opaque white for Image", i, got)
		}
	}
}
