// Package level reads and writes Goje level documents without accessing the file system
// or depending on a graphics host. Games resolve placement and zone kinds themselves.
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
	// MaxBytes is the maximum document size of any version, including whitespace.
	MaxBytes = 256 << 10

	maxSide = 128
	// MaxPlacements is the largest number of placements in a level.
	MaxPlacements = maxPlacements
	maxPlacements = 1024
	// MaxZones is the largest number of zones in a level.
	MaxZones  = maxZones
	maxZones  = 1024
	maxHeight = 1024
)

// Placement describes a game-defined object at a zero-based ground cell.
type Placement struct {
	Kind string
	ID   uint32
	X, Y int
}

// Role says whether a zone blocks movement or only reports overlaps.
type Role string

const (
	// RoleSolid zones block bodies whose vertical extent overlaps theirs.
	RoleSolid Role = "solid"
	// RoleTrigger zones never block; a game reacts when a body overlaps them.
	RoleTrigger Role = "trigger"
)

// Zone is a game-defined rectangle of whole cells, [X, X+W) × [Y, Y+H), with a
// vertical extent [Bottom, Top) in whole projected pixels. It may overlap walls,
// placements, and other zones.
type Zone struct {
	Kind        string
	ID          uint32
	Role        Role
	X, Y, W, H  int
	Bottom, Top int
}

// SightBlockKind is the zone kind that blocks line of sight in a version 3
// level. Such a zone must be a trigger, so it blocks sight but not movement.
const SightBlockKind = "sight_block"

// Board is a read-only level of version 1, 2, or 3. A zero Board is empty.
type Board struct {
	rows       []string
	placements []Placement
	zones      []Zone
	sight      int // Sight radius in cells; zero means the level sets none.
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

// Zones yields zone values in document order without exposing the backing
// slice. A version 1 level has none. The iterator is valid for the lifetime of
// the board.
func (b Board) Zones() iter.Seq[Zone] { return slices.Values(b.zones) }

// Sight returns the level's sight radius in cells, from 1 to vision.MaxRadius,
// and whether the level sets one. Only a version 3 level does.
func (b Board) Sight() (radius int, ok bool) { return b.sight, b.sight != 0 }

// SightBlocked reports whether a sight_block zone covers the cell. It is false
// on a level without a sight object, where the kind has no meaning. It visits
// every zone, so a game should build its mask once.
func (b Board) SightBlocked(x, y int) bool {
	if b.sight == 0 {
		return false
	}
	for _, z := range b.zones {
		if z.Kind == SightBlockKind && x >= z.X && y >= z.Y && x < z.X+z.W && y < z.Y+z.H {
			return true
		}
	}
	return false
}

// Decode reads exactly one version 1, 2, or 3 JSON document, including any
// trailing whitespace. It does not close r. On error the returned board is empty.
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
	var version uint64
	zonesSeen, sightSeen := false, false
	if err := readObject(
		d,
		[]string{"version", "rows", "placements", "zones", "sight"},
		3,
		func(field string) error {
			var err error
			switch field {
			case "version":
				version, err = readUint(d, 32)
				if err == nil && (version < 1 || version > 3) {
					err = fmt.Errorf("unsupported level version %d (want 1, 2, or 3)", version)
				}
			case "rows":
				b.rows, err = readRows(d)
			case "placements":
				b.placements, err = readPlacements(d)
			case "zones":
				zonesSeen = true
				b.zones, err = readZones(d)
			case "sight":
				sightSeen = true
				b.sight, err = readSight(d)
			}
			return err
		},
	); err != nil {
		return Board{}, fmt.Errorf("level: %w", err)
	}
	if version == 1 && zonesSeen {
		return Board{}, errors.New("level: zones need version 2")
	}
	if version >= 2 && !zonesSeen {
		return Board{}, errors.New(`level: missing field "zones"`)
	}
	if version < 3 && sightSeen {
		return Board{}, errors.New("level: sight needs version 3")
	}
	if version == 3 && !sightSeen {
		return Board{}, errors.New(`level: missing field "sight"`)
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
	if err := b.checkZones(); err != nil {
		return Board{}, err
	}
	return b, nil
}

// Read one schema-defined object. Token parsing catches duplicate fields
// before they can overwrite data and never descends into unknown structures.
// The first required fields of fields must appear; the rest are optional.
func readObject(
	d *json.Decoder,
	fields []string,
	required int,
	readField func(string) error,
) error {
	if err := delimiter(d, '{'); err != nil {
		return err
	}
	var seen uint16 // The widest object, a zone, has nine fields.
	for d.More() {
		field, err := readString(d)
		if err != nil {
			return err
		}
		index := slices.Index(fields, field)
		if index < 0 {
			return fmt.Errorf("unknown field %.64q", field)
		}
		mask := uint16(1) << index
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
	for i, field := range fields[:required] {
		if seen&(uint16(1)<<i) == 0 {
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
