package driver

import (
	"encoding/binary"
	"image"
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
