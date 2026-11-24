package level

import (
	"encoding/json"
	"errors"
	"fmt"
)

func readRows(d *json.Decoder) ([]string, error) {
	if err := delimiter(d, '['); err != nil {
		return nil, err
	}
	var rows []string
	for d.More() {
		if len(rows) == maxSide {
			return nil, fmt.Errorf("at most %d rows are allowed", maxSide)
		}
		row, err := readString(d)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", len(rows), err)
		}
		width := len(row)
		if len(rows) != 0 {
			width = len(rows[0])
		}
		if err := checkRow(row, len(rows), width); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := delimiter(d, ']'); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("board must have at least one row")
	}
	return rows, nil
}

func checkRow(row string, y, width int) error {
	if row == "" || len(row) > maxSide || len(row) != width {
		return fmt.Errorf("row %d must have the common width of 1–%d cells", y, maxSide)
	}
	for x := range len(row) {
		if row[x] != '.' && row[x] != '#' {
			return fmt.Errorf("cell (%d, %d): expected '.' or '#'", x, y)
		}
	}
	return nil
}

func readPlacements(d *json.Decoder) ([]Placement, error) {
	if err := delimiter(d, '['); err != nil {
		return nil, err
	}
	var placements []Placement
	for d.More() {
		if len(placements) == maxPlacements {
			return nil, fmt.Errorf("at most %d placements are allowed", maxPlacements)
		}
		p, err := readPlacement(d)
		if err != nil {
			return nil, fmt.Errorf("placement %d: %w", len(placements), err)
		}
		placements = append(placements, p)
	}
	return placements, delimiter(d, ']')
}

func readPlacement(d *json.Decoder) (Placement, error) {
	var p Placement
	err := readObject(d, []string{"id", "kind", "x", "y"}, 4, func(field string) error {
		if field == "kind" {
			kind, err := readString(d)
			if err != nil {
				return err
			}
			p.Kind = kind
			return nil
		}
		bits := 32
		if field != "id" {
			bits = 7 // An index cannot exceed the largest supported board.
		}
		n, err := readUint(d, bits)
		if err != nil {
			return err
		}
		switch field {
		case "id":
			p.ID = uint32(n) // #nosec G115 -- readUint enforced 32 bits for ID.
		case "x":
			p.X = int(n) // #nosec G115 -- readUint enforced seven bits for X.
		case "y":
			p.Y = int(n) // #nosec G115 -- readUint enforced seven bits for Y.
		}
		return nil
	})
	return p, err
}

func validKind(kind string) bool {
	if kind == "" || len(kind) > 32 || kind[0] < 'a' || kind[0] > 'z' {
		return false
	}
	for i := 1; i < len(kind); i++ {
		c := kind[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func (b Board) checkPlacements() error {
	width, height := b.Size()
	ids := make(map[uint32]struct{}, len(b.placements))
	occupied := make(map[int]struct{}, len(b.placements))
	for _, p := range b.placements {
		if p.ID == 0 {
			return errors.New("placement ID must be positive")
		}
		if !validKind(p.Kind) {
			return fmt.Errorf("placement ID %d: invalid kind identifier", p.ID)
		}
		if p.X < 0 || p.Y < 0 || p.X >= width || p.Y >= height {
			return fmt.Errorf("placement ID %d: cell (%d, %d) is outside the board", p.ID, p.X, p.Y)
		}
		if !b.Walkable(p.X, p.Y) {
			return fmt.Errorf("placement ID %d: cell (%d, %d) is blocked", p.ID, p.X, p.Y)
		}
		if _, ok := ids[p.ID]; ok {
			return fmt.Errorf("duplicate placement ID %d", p.ID)
		}
		cell := p.Y*width + p.X
		if _, ok := occupied[cell]; ok {
			return fmt.Errorf("placement ID %d: cell (%d, %d) is occupied", p.ID, p.X, p.Y)
		}
		ids[p.ID] = struct{}{}
		occupied[cell] = struct{}{}
	}
	return nil
}

func readZones(d *json.Decoder) ([]Zone, error) {
	if err := delimiter(d, '['); err != nil {
		return nil, err
	}
	var zones []Zone
	for d.More() {
		if len(zones) == maxZones {
			return nil, fmt.Errorf("at most %d zones are allowed", maxZones)
		}
		z, err := readZone(d)
		if err != nil {
			return nil, fmt.Errorf("zone %d: %w", len(zones), err)
		}
		zones = append(zones, z)
	}
	return zones, delimiter(d, ']')
}

func readZone(d *json.Decoder) (Zone, error) {
	var z Zone
	fields := []string{"id", "kind", "role", "x", "y", "w", "h", "bottom", "top"}
	err := readObject(d, fields, len(fields), func(field string) error {
		if field == "kind" || field == "role" {
			text, err := readString(d)
			if err != nil {
				return err
			}
			if field == "kind" {
				z.Kind = text
			} else {
				z.Role = Role(text)
			}
			return nil
		}
		// Each width is just enough for the largest valid value of its field.
		bits := 11
		switch field {
		case "id":
			bits = 32
		case "x", "y":
			bits = 7
		case "w", "h":
			bits = 8
		}
		n, err := readUint(d, bits)
		if err != nil {
			return err
		}
		// #nosec G115 -- readUint enforced the width above, and every width fits an int.
		v := int(n)
		switch field {
		case "id":
			z.ID = uint32(n) // #nosec G115 -- readUint enforced 32 bits for ID.
		case "x":
			z.X = v
		case "y":
			z.Y = v
		case "w":
			z.W = v
		case "h":
			z.H = v
		case "bottom":
			z.Bottom = v
		case "top":
			z.Top = v
		}
		return nil
	})
	return z, err
}

// Check reports whether the zone is valid on a board of width × height cells:
// a positive ID, a valid kind and role, a rectangle inside the board, and a
// vertical extent of 0 <= Bottom < Top <= 1,024. It does not check that the ID
// is unique among a level's zones.
func (z Zone) Check(width, height int) error {
	if z.ID == 0 {
		return errors.New("zone ID must be positive")
	}
	if !validKind(z.Kind) {
		return fmt.Errorf("zone ID %d: invalid kind identifier", z.ID)
	}
	if z.Role != RoleSolid && z.Role != RoleTrigger {
		return fmt.Errorf("zone ID %d: role must be %q or %q", z.ID, RoleSolid, RoleTrigger)
	}
	if z.W < 1 || z.H < 1 || z.X < 0 || z.Y < 0 || z.X+z.W > width || z.Y+z.H > height {
		return fmt.Errorf("zone ID %d: cells (%d, %d) to (%d, %d) are not inside the %d × %d board",
			z.ID, z.X, z.Y, z.X+z.W-1, z.Y+z.H-1, width, height)
	}
	if z.Bottom < 0 || z.Top <= z.Bottom || z.Top > maxHeight {
		return fmt.Errorf("zone ID %d: vertical extent [%d, %d) must satisfy 0 <= bottom < top <= %d",
			z.ID, z.Bottom, z.Top, maxHeight)
	}
	return nil
}

func (b Board) checkZones() error {
	if len(b.zones) > maxZones {
		return fmt.Errorf("at most %d zones are allowed", maxZones)
	}
	width, height := b.Size()
	ids := make(map[uint32]struct{}, len(b.zones))
	for _, z := range b.zones {
		if err := z.Check(width, height); err != nil {
			return err
		}
		if _, ok := ids[z.ID]; ok {
			return fmt.Errorf("duplicate zone ID %d", z.ID)
		}
		ids[z.ID] = struct{}{}
	}
	return nil
}
