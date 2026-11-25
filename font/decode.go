package font

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"slices"
	"strconv"
	"unicode/utf8"
)

// Limits of font sheet v1; see specs/font-v1.md.
const (
	MaxPNGBytes  = 256 << 10
	MaxPNGAxis   = 512
	MaxJSONBytes = 16 << 10
	MaxCell      = 32
)

// Decode reads one font sheet v1 asset: a PNG of equal cells and its JSON
// description. It checks the PNG dimensions and the description before it
// decodes any pixels, returns the zero Face on error, and does not close the
// readers.
func Decode(sheetPNG, fontJSON io.Reader) (Face, error) {
	data, err := readBounded(sheetPNG, MaxPNGBytes)
	if err != nil {
		return Face{}, fmt.Errorf("read font PNG: %w", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Face{}, fmt.Errorf("read font PNG dimensions: %w", err)
	}
	if config.Width < 1 || config.Width > MaxPNGAxis || config.Height < 1 ||
		config.Height > MaxPNGAxis {
		return Face{}, fmt.Errorf("font PNG axes must be 1–%d pixels", MaxPNGAxis)
	}
	metadata, err := readBounded(fontJSON, MaxJSONBytes)
	if err != nil {
		return Face{}, fmt.Errorf("read font metadata: %w", err)
	}
	m, err := decodeMetadata(metadata, image.Pt(config.Width, config.Height))
	if err != nil {
		return Face{}, fmt.Errorf("font metadata: %w", err)
	}
	pixels, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return Face{}, fmt.Errorf("decode font PNG: %w", err)
	}
	return newFace(pixels, m.cell, m.baseline, m.first, m.count, m.advances), nil
}

type metadata struct {
	cell     image.Point
	baseline int
	first    rune
	count    int
	advances []uint8
}

// Fields in file order of the specification. All but advances are required.
var fields = [...]string{
	"version",
	"cellWidth",
	"cellHeight",
	"baseline",
	"first",
	"count",
	"advances",
}

const advancesField = len(fields) - 1

func decodeMetadata(data []byte, size image.Point) (metadata, error) {
	if !utf8.Valid(data) {
		return metadata{}, errors.New("must be UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := delimiter(d, '{'); err != nil {
		return metadata{}, err
	}
	var values [advancesField]int
	var advances []uint8
	var seen uint
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return metadata{}, fmt.Errorf("read field name: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return metadata{}, errors.New("expected field name")
		}
		field := slices.Index(fields[:], name)
		if field < 0 {
			return metadata{}, fmt.Errorf("unknown field %.64q", name)
		}
		if seen&(1<<field) != 0 {
			return metadata{}, fmt.Errorf("duplicate field %q", name)
		}
		seen |= 1 << field
		if field == advancesField {
			advances, err = readAdvances(d)
		} else {
			values[field], err = readInt(d)
		}
		if err != nil {
			return metadata{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := delimiter(d, '}'); err != nil {
		return metadata{}, err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return metadata{}, errors.New("unexpected data after font metadata")
	}
	for i, name := range fields[:advancesField] {
		if seen&(1<<i) == 0 {
			return metadata{}, fmt.Errorf("missing field %q", name)
		}
	}
	return validate(values, advances, size)
}

func validate(values [advancesField]int, advances []uint8, size image.Point) (metadata, error) {
	version, width, height, baseline, first, count := values[0], values[1], values[2], values[3], values[4], values[5]
	switch {
	case version != 1:
		return metadata{}, fmt.Errorf("unsupported version %d", version)
	case width < 1 || width > MaxCell || height < 1 || height > MaxCell:
		return metadata{}, fmt.Errorf("cell axes must be 1–%d pixels", MaxCell)
	case baseline < 0 || baseline > height:
		return metadata{}, errors.New("baseline must be 0 to the cell height")
	case first < 0 || first > utf8.MaxRune:
		return metadata{}, errors.New("first must be a Unicode code point")
	// Subtract first only after checking its range, so the bound cannot overflow.
	case count < 1 || count > utf8.MaxRune+1-first:
		return metadata{}, errors.New("count must be positive and end at or before U+10FFFF")
	case count > (size.X/width)*(size.Y/height):
		return metadata{}, fmt.Errorf("%d glyphs do not fit in the sheet's whole cells", count)
	case advances != nil && len(advances) != count:
		return metadata{}, errors.New("advances must have one entry per glyph")
	}
	for i, advance := range advances {
		if int(advance) > width {
			return metadata{}, fmt.Errorf("advance %d exceeds the cell width", i+1)
		}
	}
	return metadata{
		cell:     image.Pt(width, height),
		baseline: baseline,
		first:    rune(first), // #nosec G115 -- Checked against utf8.MaxRune above.
		count:    count,
		advances: advances,
	}, nil
}

// readAdvances reads integers of 0–MaxCell. The bounded input limits the
// slice; the count check happens once all fields are known.
func readAdvances(d *json.Decoder) ([]uint8, error) {
	if err := delimiter(d, '['); err != nil {
		return nil, err
	}
	advances := []uint8{}
	for d.More() {
		advance, err := readInt(d)
		if err != nil {
			return nil, err
		}
		if advance < 0 || advance > MaxCell {
			return nil, fmt.Errorf("entry %d must be 0–%d", len(advances)+1, MaxCell)
		}
		advances = append(advances, uint8(advance)) // #nosec G115 -- Checked above.
	}
	return advances, delimiter(d, ']')
}

func delimiter(d *json.Decoder, want json.Delim) error {
	token, err := d.Token()
	if err != nil {
		return fmt.Errorf("read %q: %w", want, err)
	}
	if token != want {
		return fmt.Errorf("expected %q", want)
	}
	return nil
}

func readInt(d *json.Decoder) (int, error) {
	token, err := d.Token()
	if err != nil {
		return 0, fmt.Errorf("read integer: %w", err)
	}
	number, ok := token.(json.Number)
	if !ok {
		return 0, errors.New("expected integer")
	}
	value, err := strconv.Atoi(string(number))
	if err != nil {
		return 0, errors.New("expected integer in range")
	}
	return value, nil
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
