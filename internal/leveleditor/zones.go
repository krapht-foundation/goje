package leveleditor

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/krapht-foundation/goje/level"
)

// Zones returns a copy of the level's zones in document order.
func (d *Document) Zones() []level.Zone { return slices.Clone(d.zones) }

// AddZone appends a zone as one undoable action and returns the ID it was
// given. The ID in z is ignored: the new zone gets one more than the largest
// existing ID, so IDs of deleted zones are not reused while a later zone exists.
func (d *Document) AddZone(z level.Zone) (uint32, error) {
	if err := d.zoneEditable(); err != nil {
		return 0, err
	}
	if len(d.zones) >= level.MaxZones {
		return 0, fmt.Errorf("add zone: a level holds at most %d zones", level.MaxZones)
	}
	var largest uint32
	for _, existing := range d.zones {
		largest = max(largest, existing.ID)
	}
	if largest == math.MaxUint32 {
		return 0, errors.New("add zone: no zone ID is available")
	}
	z.ID = largest + 1
	if err := z.Check(d.width, d.height); err != nil {
		return 0, fmt.Errorf("add zone: %w", err)
	}
	d.commitZone(&zoneEdit{index: len(d.zones), after: &z})
	return z.ID, nil
}

// EditZone replaces the zone that has z's ID, keeping its place in the document
// order, as one undoable action. Replacing a zone with an identical one does
// nothing and enters no history.
func (d *Document) EditZone(z level.Zone) error {
	if err := d.zoneEditable(); err != nil {
		return err
	}
	i := d.zoneIndex(z.ID)
	if i < 0 {
		return fmt.Errorf("edit zone %d: no such zone", z.ID)
	}
	if err := z.Check(d.width, d.height); err != nil {
		return fmt.Errorf("edit zone: %w", err)
	}
	if d.zones[i] == z {
		return nil
	}
	before := d.zones[i]
	d.commitZone(&zoneEdit{index: i, before: &before, after: &z})
	return nil
}

// DeleteZone removes the zone with the given ID as one undoable action.
func (d *Document) DeleteZone(id uint32) error {
	if err := d.zoneEditable(); err != nil {
		return err
	}
	i := d.zoneIndex(id)
	if i < 0 {
		return fmt.Errorf("delete zone %d: no such zone", id)
	}
	before := d.zones[i]
	d.commitZone(&zoneEdit{index: i, before: &before})
	return nil
}

func (d *Document) zoneEditable() error {
	if d.width == 0 {
		return errors.New("zone edit requires an open level")
	}
	if d.brush != 0 {
		return errors.New("finish the current terrain stroke before editing a zone")
	}
	return nil
}

func (d *Document) zoneIndex(id uint32) int {
	return slices.IndexFunc(d.zones, func(z level.Zone) bool { return z.ID == id })
}

func (d *Document) commitZone(edit *zoneEdit) {
	tx := transaction{zone: edit}
	d.apply(tx, true)
	d.record(tx)
}
