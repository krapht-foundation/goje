package spriteformat

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
)

// MaxPNGBytes is the v1 limit on encoded sheet bytes.
const MaxPNGBytes = 1 << 20

// MaxPNGAxis is the largest supported width or height in sheet pixels.
const MaxPNGAxis = 512

// PNG retains the checked bytes so callers can validate metadata before
// allocating decoded pixels. Its zero value is not an opened image.
type PNG struct {
	data []byte
	size image.Point
}

// ReadPNG reads bounded bytes and checks the encoded dimensions.
func ReadPNG(r io.Reader) (PNG, error) {
	data, err := readBounded(r, MaxPNGBytes)
	if err != nil {
		return PNG{}, fmt.Errorf("read sprite PNG: %w", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return PNG{}, fmt.Errorf("read sprite PNG dimensions: %w", err)
	}
	if config.Width < 1 || config.Width > MaxPNGAxis || config.Height < 1 ||
		config.Height > MaxPNGAxis {
		return PNG{}, fmt.Errorf("sprite PNG axes must be 1–%d pixels", MaxPNGAxis)
	}
	return PNG{data: data, size: image.Pt(config.Width, config.Height)}, nil
}

// Size returns the checked width and height in sheet pixels.
func (p PNG) Size() image.Point { return p.size }

// Decode loads pixels from the same immutable bytes that passed inspection.
func (p PNG) Decode() (image.Image, error) {
	pixels, err := png.Decode(bytes.NewReader(p.data))
	if err != nil {
		return nil, fmt.Errorf("decode sprite PNG: %w", err)
	}
	return pixels, nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return data, nil
}
