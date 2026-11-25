package leveleditor

import (
	"errors"
	"fmt"

	"github.com/krapht-foundation/goje/engine/vision"
)

// Sight returns the level's sight radius in cells, or 0 when it sets none.
func (d *Document) Sight() int { return d.sight }

// SetSight sets the level's sight radius as one undoable action. A radius of 0
// removes it, so the level exports as version 1 or 2 again; any other radius,
// from 1 to vision.MaxRadius, makes it export as version 3. Setting the radius
// it already has does nothing and enters no history.
func (d *Document) SetSight(radius int) error {
	if d.width == 0 {
		return errors.New("sight edit requires an open level")
	}
	if d.brush != 0 {
		return errors.New("finish the current terrain stroke before changing the sight radius")
	}
	if radius < 0 || radius > vision.MaxRadius {
		return fmt.Errorf("sight radius %d must be 0 for none or 1–%d", radius, vision.MaxRadius)
	}
	if radius == d.sight {
		return nil
	}
	tx := transaction{sight: &[2]int{d.sight, radius}}
	d.apply(tx, true)
	d.record(tx)
	return nil
}
