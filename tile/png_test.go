package tile_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/tile"
)

func TestDecodePNG(t *testing.T) {
	const limit = 64 << 10
	want := color.NRGBA{R: 123, G: 77, B: 45, A: 199}
	pixels := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	pixels.SetNRGBA(10, 4, want)
	valid := encodePNG(t, pixels)
	t.Run("preserves asymmetric color and alpha", func(t *testing.T) {
		got, err := tile.Decode(bytes.NewReader(valid))
		if err != nil {
			t.Fatal(err)
		}
		if pixel := color.NRGBAModel.Convert(got.At(10, 4)); pixel != want {
			t.Fatalf("accent pixel = %v, want %v", pixel, want)
		}
		if _, _, _, alpha := got.At(10, 11).RGBA(); alpha != 0 {
			t.Fatalf("mirrored pixel alpha = %d, want transparent", alpha)
		}
	})

	for _, test := range []struct {
		name string
		data []byte
		want string
	}{
		{"malformed", []byte("not a PNG"), "PNG header"},
		{"wrong dimensions", encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 16, 32))), "32 × 16"},
		{"truncated", valid[:len(valid)-1], "decode tile PNG"},
		{"trailing data", append(bytes.Clone(valid), 0), "unexpected data"},
		{"over byte limit", append(bytes.Clone(valid), make([]byte, limit-len(valid)+1)...), "exceeds 64 KiB"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := tile.Decode(bytes.NewReader(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func encodePNG(t *testing.T, pixels image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, pixels); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
