// Package leveleditor owns the Level Builder's working copy and edit history.
// It does not open files, draw the editor, or depend on the game runtime.
package leveleditor

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/krapht-foundation/goje/level"
)

const (
	// Floor and Wall are the v1 terrain brush values.
	Floor byte = '.'
	Wall  byte = '#'
)

// Document is a single-owner editing session. Its zero value has no level.
// Open replaces it after validation; the caller owns replacement confirmation.
type Document struct {
	width, height int
	terrain       []byte
	occupancy     []int // Placement index plus one; zero means empty.
	placements    []level.Placement
	zones         []level.Zone
	brush         byte
	stroke        []int
	history       []transaction
	cursor        int // Entries before cursor can be undone; those after it can be redone.
	entries       int
	revision      uint64
	serial        uint64
	exported      uint64
}

// Open leaves the active board, pending edits, and history intact on failure.
func (d *Document) Open(r io.Reader) error {
	board, err := level.Decode(r)
	if err != nil {
		return fmt.Errorf("open level: %w", err)
	}
	w, h := board.Size()
	next := Document{
		width: w, height: h,
		terrain: make([]byte, w*h), occupancy: make([]int, w*h),
		placements: slices.Collect(board.Placements()),
		zones:      slices.Collect(board.Zones()),
	}
	for y := range h {
		for x := range w {
			tile := Wall
			if board.Walkable(x, y) {
				tile = Floor
			}
			next.terrain[y*w+x] = tile
		}
	}
	for i, p := range next.placements {
		next.occupancy[p.Y*w+p.X] = i + 1
	}
	*d = next
	return nil
}

// Size returns the board dimensions, or zero for an unopened document.
func (d *Document) Size() (width, height int) { return d.width, d.height }

// Cell returns the visible terrain and placement at a cell. A zero tile means
// out of bounds; a zero placement ID means the cell is unoccupied.
func (d *Document) Cell(x, y int) (byte, level.Placement) {
	i, ok := d.index(x, y)
	if !ok {
		return 0, level.Placement{}
	}
	if p := d.occupancy[i]; p != 0 {
		return d.terrain[i], d.placements[p-1]
	}
	return d.terrain[i], level.Placement{}
}

// Dirty reports edits relative to the last opened or exported revision.
// An in-progress stroke with changes is also dirty.
func (d *Document) Dirty() bool { return d.revision != d.exported || len(d.stroke) != 0 }

// Export encodes the current committed state without marking it as exported.
func (d *Document) Export() ([]byte, error) {
	if d.width == 0 || d.brush != 0 {
		return nil, errors.New("export requires an open level with no active stroke")
	}
	rows := make([]string, d.height)
	// One immutable copy backs all the row strings for the synchronous encoder.
	terrain := string(d.terrain)
	for y := range rows {
		rows[y] = terrain[y*d.width : (y+1)*d.width]
	}
	data, err := level.Encode(rows, d.placements, d.zones)
	if err != nil {
		return nil, fmt.Errorf("export level: %w", err)
	}
	return data, nil
}

// MarkExported marks the current revision clean after its bytes are handed off.
// The caller must not edit or replace the document between Export and handoff.
func (d *Document) MarkExported() { d.exported = d.revision }

// BeginStroke starts a gesture using a fixed terrain brush.
func (d *Document) BeginStroke(tile byte) error {
	if d.width == 0 || d.brush != 0 {
		return errors.New("start stroke requires an open level with no active stroke")
	}
	if tile != Floor && tile != Wall {
		return errors.New("unknown terrain brush")
	}
	d.brush = tile
	return nil
}

// Paint applies one cell of the current stroke. Rejected cells do not cancel
// the gesture or discard its earlier valid changes.
func (d *Document) Paint(x, y int) error {
	if d.brush == 0 {
		return errors.New("no terrain stroke is active")
	}
	i, ok := d.index(x, y)
	if !ok {
		return fmt.Errorf("paint cell (%d, %d): outside the board", x, y)
	}
	if d.brush == Wall && d.occupancy[i] != 0 {
		return fmt.Errorf("paint cell (%d, %d): a wall cannot cover a placement", x, y)
	}
	if d.terrain[i] != d.brush {
		// A fixed brush changes a cell at most once per stroke.
		d.terrain[i] = d.brush
		d.stroke = append(d.stroke, i)
	}
	return nil
}

// EndStroke commits all changed cells as one action, or rolls them back.
func (d *Document) EndStroke(commit bool) {
	if len(d.stroke) != 0 {
		tx := transaction{cells: d.stroke, tile: d.brush}
		if commit {
			d.record(tx)
			d.stroke = nil // The history now owns these cells.
		} else {
			d.apply(tx, false)
			d.stroke = d.stroke[:0]
		}
	}
	d.brush = 0
}

// Move relocates a placement from one cell to another as one undoable action.
func (d *Document) Move(fromX, fromY, toX, toY int) error {
	if d.brush != 0 {
		return errors.New("finish the current terrain stroke before moving a placement")
	}
	from, ok := d.index(fromX, fromY)
	if !ok || d.occupancy[from] == 0 {
		return fmt.Errorf("move from (%d, %d): no placement", fromX, fromY)
	}
	to, ok := d.index(toX, toY)
	if !ok || d.terrain[to] == Wall {
		return fmt.Errorf("move to (%d, %d): target is not a floor cell", toX, toY)
	}
	if from == to {
		return nil
	}
	if d.occupancy[to] != 0 {
		return fmt.Errorf("move to (%d, %d): target already has a placement", toX, toY)
	}
	tx := transaction{from: from, to: to}
	d.apply(tx, true)
	d.record(tx)
	return nil
}

func (d *Document) index(x, y int) (int, bool) {
	return y*d.width + x, x >= 0 && x < d.width && y >= 0 && y < d.height
}
