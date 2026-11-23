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
	err := readObject(d, []string{"id", "kind", "x", "y"}, func(field string) error {
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
