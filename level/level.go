// Package level reads and writes Goje level documents without accessing the file system
// or depending on a graphics host. Games resolve placement kinds themselves.
package level

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"strconv"
	"unicode/utf8"
)

const (
	// MaxBytes is the maximum v1 document size, including whitespace.
	MaxBytes = 256 << 10

	maxSide       = 128
	maxPlacements = 1024
)

// Placement describes a game-defined object at a zero-based ground cell.
type Placement struct {
	Kind string
	ID   uint32
	X, Y int
}

// Board is a read-only v1 level. A zero Board is empty.
type Board struct {
	rows       []string
	placements []Placement
}

// Size returns the ground grid width and height in cells.
func (b Board) Size() (width, height int) {
	if len(b.rows) == 0 {
		return 0, 0
	}
	return len(b.rows[0]), len(b.rows)
}

// Walkable reports whether the cell is inside the board and not a wall.
// Placement occupancy is game state and does not affect this query.
func (b Board) Walkable(x, y int) bool {
	return y >= 0 && y < len(b.rows) && x >= 0 && x < len(b.rows[y]) && b.rows[y][x] == '.'
}

// Placements yields placement values in document order without exposing
// the backing slice. The iterator is valid for the lifetime of the board.
func (b Board) Placements() iter.Seq[Placement] { return slices.Values(b.placements) }

// Decode reads exactly one v1 JSON document, including any trailing whitespace.
// It does not close r. On error the returned board is empty.
func Decode(r io.Reader) (Board, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Board{}, fmt.Errorf("read level: %w", err)
	}
	if len(data) > MaxBytes {
		return Board{}, fmt.Errorf("level exceeds %d bytes", MaxBytes)
	}
	if !utf8.Valid(data) {
		return Board{}, errors.New("level must be UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var b Board
	if err := readObject(d, []string{"version", "rows", "placements"}, func(field string) error {
		var err error
		switch field {
		case "version":
			v, versionErr := readUint(d, 32)
			if versionErr != nil {
				return versionErr
			}
			if v != 1 {
				return fmt.Errorf("unsupported level version %d (want 1)", v)
			}
		case "rows":
			b.rows, err = readRows(d)
		case "placements":
			b.placements, err = readPlacements(d)
		}
		return err
	}); err != nil {
		return Board{}, fmt.Errorf("level: %w", err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return Board{}, fmt.Errorf("level trailing data: %w", err)
		}
		return Board{}, errors.New("level has a second JSON value")
	}
	if err := b.checkPlacements(); err != nil {
		return Board{}, err
	}
	return b, nil
}

// Read one schema-defined object. Token parsing catches duplicate fields
// before they can overwrite data and never descends into unknown structures.
func readObject(d *json.Decoder, fields []string, readField func(string) error) error {
	if err := delimiter(d, '{'); err != nil {
		return err
	}
	var seen uint8 // Both v1 objects have at most four fields.
	for d.More() {
		field, err := readString(d)
		if err != nil {
			return err
		}
		index := slices.Index(fields, field)
		if index < 0 {
			return fmt.Errorf("unknown field %.64q", field)
		}
		mask := uint8(1) << index
		if seen&mask != 0 {
			return fmt.Errorf("duplicate field %q", field)
		}
		seen |= mask
		if err := readField(field); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
	}
	if err := delimiter(d, '}'); err != nil {
		return err
	}
	for i, field := range fields {
		if seen&(uint8(1)<<i) == 0 {
			return fmt.Errorf("missing field %q", field)
		}
	}
	return nil
}

func delimiter(d *json.Decoder, want json.Delim) error {
	token, err := d.Token()
	if err != nil {
		return fmt.Errorf("read delimiter: %w", err)
	}
	if token != want {
		return fmt.Errorf("expected %q", want)
	}
	return nil
}

func readString(d *json.Decoder) (string, error) {
	token, err := d.Token()
	if err != nil {
		return "", fmt.Errorf("read string: %w", err)
	}
	value, ok := token.(string)
	if !ok {
		return "", errors.New("expected a string")
	}
	return value, nil
}

func readUint(d *json.Decoder, bits int) (uint64, error) {
	token, err := d.Token()
	if err != nil {
		return 0, fmt.Errorf("read integer: %w", err)
	}
	if number, ok := token.(json.Number); ok {
		if n, err := strconv.ParseUint(string(number), 10, bits); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("expected an unsigned %d-bit integer", bits)
}
