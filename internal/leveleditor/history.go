package leveleditor

import (
	"slices"

	"github.com/krapht-foundation/goje/level"
)

const (
	maxTransactions = 64
	maxChanges      = 65_536
)

// A stroke stores each changed cell once, since the old terrain must be the
// other brush value. A placement move needs only its source and destination,
// and a zone edit its index and the zone before and after.
type transaction struct {
	cells         []int
	from, to      int
	before, after uint64
	tile          byte
	zone          *zoneEdit
}

// zoneEdit is nil-free for a replacement. Before is nil for an addition and
// After is nil for a deletion.
type zoneEdit struct {
	index         int
	before, after *level.Zone
}

func (tx transaction) changes() int { return max(1, len(tx.cells)) }

// CanUndo and CanRedo report available actions outside an active stroke.
func (d *Document) CanUndo() bool { return d.brush == 0 && d.cursor != 0 }

// CanRedo reports whether an undone action can be reapplied.
func (d *Document) CanRedo() bool { return d.brush == 0 && d.cursor < len(d.history) }

// Undo restores the prior state and reports whether it applied a transaction.
func (d *Document) Undo() bool {
	if !d.CanUndo() {
		return false
	}
	d.cursor--
	tx := d.history[d.cursor]
	d.apply(tx, false)
	d.revision = tx.before
	return true
}

// Redo reapplies an undone transaction and reports whether it succeeded.
func (d *Document) Redo() bool {
	if !d.CanRedo() {
		return false
	}
	tx := d.history[d.cursor]
	d.apply(tx, true)
	d.cursor++
	d.revision = tx.after
	return true
}

func (d *Document) record(tx transaction) {
	for _, redo := range d.history[d.cursor:] {
		d.entries -= redo.changes()
	}
	d.history = slices.Delete(d.history, d.cursor, len(d.history))

	drop := 0
	for len(d.history)-drop >= maxTransactions || d.entries+tx.changes() > maxChanges {
		d.entries -= d.history[drop].changes()
		drop++
	}
	d.history = slices.Delete(d.history, 0, drop)

	// Revision IDs never get reused after branching or history eviction, so
	// undo can recognize an exported revision without a content comparison.
	d.serial++
	tx.before, tx.after = d.revision, d.serial
	d.history = append(d.history, tx)
	d.entries += tx.changes()
	d.cursor = len(d.history)
	d.revision = tx.after
}

func (d *Document) apply(tx transaction, forward bool) {
	if tx.zone != nil {
		from, to := tx.zone.before, tx.zone.after
		if !forward {
			from, to = to, from
		}
		switch {
		case from == nil:
			d.zones = slices.Insert(d.zones, tx.zone.index, *to)
		case to == nil:
			d.zones = slices.Delete(d.zones, tx.zone.index, tx.zone.index+1)
		default:
			d.zones[tx.zone.index] = *to
		}
		return
	}
	if tx.tile != 0 {
		tile := tx.tile
		if !forward {
			if tile == Floor {
				tile = Wall
			} else {
				tile = Floor
			}
		}
		for _, i := range tx.cells {
			d.terrain[i] = tile
		}
		return
	}

	from, to := tx.from, tx.to
	if !forward {
		from, to = to, from
	}
	p := d.occupancy[from]
	d.occupancy[from], d.occupancy[to] = 0, p
	d.placements[p-1].X, d.placements[p-1].Y = to%d.width, to/d.width
}
