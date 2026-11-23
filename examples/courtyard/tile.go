package courtyard

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
)

const (
	tileWidth    = 32
	tileHeight   = 16
	maxTileBytes = 64 << 10
)

// DecodeTile reads a single courtyard ground tile: a 32 × 16 PNG of at most
// 64 KiB. It preserves the source alpha and does not infer a diamond mask.
func DecodeTile(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxTileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read tile PNG: %w", err)
	}
	if len(data) > maxTileBytes {
		return nil, errors.New("tile PNG exceeds 64 KiB")
	}
	// Check the header before allocating pixels, and decode the exact same
	// bounded bytes so the dimensions cannot change between the two passes.
	source := bytes.NewReader(data)
	cfg, err := png.DecodeConfig(source)
	if err != nil {
		return nil, fmt.Errorf("read tile PNG header: %w", err)
	}
	if cfg.Width != tileWidth || cfg.Height != tileHeight {
		return nil, fmt.Errorf("tile PNG must be %d × %d pixels, got %d × %d",
			tileWidth, tileHeight, cfg.Width, cfg.Height)
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
