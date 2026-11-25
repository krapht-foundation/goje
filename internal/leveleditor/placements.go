package leveleditor

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/krapht-foundation/goje/level"
)

// AddPlacement appends a placement as one undoable action and returns its ID.
// A zero ID in p asks for one more than the largest existing ID; any other ID
// must be unused. The cell must be a free floor cell, and a level holds at most
// level.MaxPlacements placements.
func (d *Document) AddPlacement(p level.Placement) (uint32, error) {
	if err := d.placementEditable(); err != nil {
		return 0, err
	}
	if len(d.placements) >= level.MaxPlacements {
		return 0, fmt.Errorf("add placement: a level holds at most %d placements", level.MaxPlacements)
	}
	if p.ID == 0 {
		var largest uint32
		for _, existing := range d.placements {
			largest = max(largest, existing.ID)
		}
		if largest == math.MaxUint32 {
			return 0, errors.New("add placement: no placement ID is available")
		}
		p.ID = largest + 1
	} else if d.placementIndex(p.ID) >= 0 {
		return 0, fmt.Errorf("add placement: ID %d is already in use", p.ID)
	}
	if err := p.Check(d.width, d.height); err != nil {
		return 0, fmt.Errorf("add placement: %w", err)
	}
	i, _ := d.index(p.X, p.Y)
	if d.terrain[i] == Wall {
		return 0, fmt.Errorf("add placement at (%d, %d): the cell is a wall", p.X, p.Y)
	}
	if d.occupancy[i] != 0 {
		return 0, fmt.Errorf("add placement at (%d, %d): the cell already has a placement", p.X, p.Y)
	}
	d.commitPlacement(&placementEdit{index: len(d.placements), after: &p})
	return p.ID, nil
}

// SetPlacementKind changes the kind of the placement with the given ID, keeping
// its ID, cell, and place in the document order, as one undoable action. Setting
// the kind it already has does nothing and enters no history.
func (d *Document) SetPlacementKind(id uint32, kind string) error {
	if err := d.placementEditable(); err != nil {
		return err
	}
	i := d.placementIndex(id)
	if i < 0 {
		return fmt.Errorf("change placement %d: no such placement", id)
	}
	before := d.placements[i]
	after := before
	after.Kind = kind
	if err := after.Check(d.width, d.height); err != nil {
		return fmt.Errorf("change placement: %w", err)
	}
	if after == before {
		return nil
	}
	d.commitPlacement(&placementEdit{index: i, before: &before, after: &after})
	return nil
}

// DeletePlacement removes the placement with the given ID as one undoable action.
func (d *Document) DeletePlacement(id uint32) error {
	if err := d.placementEditable(); err != nil {
		return err
	}
	i := d.placementIndex(id)
	if i < 0 {
		return fmt.Errorf("delete placement %d: no such placement", id)
	}
	before := d.placements[i]
	d.commitPlacement(&placementEdit{index: i, before: &before})
	return nil
}

func (d *Document) placementEditable() error {
	if d.width == 0 {
		return errors.New("placement edit requires an open level")
	}
	if d.brush != 0 {
		return errors.New("finish the current terrain stroke before editing a placement")
	}
	return nil
}

func (d *Document) placementIndex(id uint32) int {
	return slices.IndexFunc(d.placements, func(p level.Placement) bool { return p.ID == id })
}

func (d *Document) commitPlacement(edit *placementEdit) {
	tx := transaction{placement: edit}
	d.apply(tx, true)
	d.record(tx)
}

// applyPlacement inserts, removes, or replaces one placement and renumbers the
// occupancy of every later placement, whose index in the slice has shifted.
func (d *Document) applyPlacement(edit *placementEdit, forward bool) {
	from, to := edit.before, edit.after
	if !forward {
		from, to = to, from
	}
	switch {
	case from == nil:
		d.placements = slices.Insert(d.placements, edit.index, *to)
	case to == nil:
		d.occupancy[from.Y*d.width+from.X] = 0
		d.placements = slices.Delete(d.placements, edit.index, edit.index+1)
	default:
		// A kind change keeps the cell, so occupancy is unchanged.
		d.placements[edit.index] = *to
		return
	}
	for i := edit.index; i < len(d.placements); i++ {
		p := d.placements[i]
		d.occupancy[p.Y*d.width+p.X] = i + 1
	}
}
