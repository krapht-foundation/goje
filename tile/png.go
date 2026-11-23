// Package tile reads and writes the fixed-size ground PNG used by the Goje
// Tile Maker and reference playground. Callers own file access and rendering.
package tile

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
)

const (
	// Width and Height are the ground tile dimensions in pixels.
	Width  = 32
	Height = 16
	// MaxPNGBytes limits the encoded file, including ancillary chunks.
	MaxPNGBytes = 64 << 10
)

// Decode reads a single 32 × 16 PNG of at most 64 KiB. It preserves the
// source color model and alpha and does not infer a diamond mask.
func Decode(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxPNGBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read tile PNG: %w", err)
	}
	if len(data) > MaxPNGBytes {
		return nil, errors.New("tile PNG exceeds 64 KiB")
	}
	// Check the header before allocating pixels, and decode the exact same
	// bounded bytes so the dimensions cannot change between the two passes.
	source := bytes.NewReader(data)
	cfg, err := png.DecodeConfig(source)
	if err != nil {
		return nil, fmt.Errorf("read tile PNG header: %w", err)
	}
	if sizeErr := checkSize(cfg.Width, cfg.Height); sizeErr != nil {
		return nil, sizeErr
	}
	source.Reset(data)
	tile, err := png.Decode(source)
	if err != nil {
		return nil, fmt.Errorf("decode tile PNG: %w", err)
	}
	if source.Len() != 0 {
		return nil, errors.New("unexpected data after tile PNG")
	}
	return tile, nil
}

// EncodePNG returns a new PNG without source metadata. Equivalent pixels
// in the same encoder build produce the same bytes.
func EncodePNG(pixels *image.NRGBA) ([]byte, error) {
	if err := checkSize(pixels.Bounds().Dx(), pixels.Bounds().Dy()); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := png.Encode(&output, pixels); err != nil {
		return nil, fmt.Errorf("encode tile PNG: %w", err)
	}
	return output.Bytes(), nil
}

func checkSize(width, height int) error {
	if width != Width || height != Height {
		return fmt.Errorf("tile PNG must be %d × %d pixels, got %d × %d", Width, Height, width, height)
	}
	return nil
}
