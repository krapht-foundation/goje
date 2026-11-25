package vision

import (
	"fmt"

	"github.com/krapht-foundation/goje/engine/grid"
)

// maxSide matches package grid, so every cell index fits in a uint16.
const maxSide = 128

// Field counts, for each cell of a width × height grid, how many viewers see
// it. Each viewer stands in one cell and sees what Cast reports from there.
// The Field remembers the cells each viewer saw, so moving a viewer takes its
// old cells away and adds its new ones, and it does that work only when the
// viewer's cell changes: a still army costs nothing.
//
// Viewers are numbered from 0 to the count given to NewField. A Field
// allocates nothing after NewField, because each viewer's list of cells is
// sized for the most cells its radius can reach. A Field is for one goroutine.
type Field struct {
	width, height, radius int
	span                  int // Room for one viewer's cells.

	count []int32  // Viewers that see each cell, row-major.
	at    []int32  // Each viewer's cell, or -1 when it has none.
	n     []int32  // How many cells each viewer sees.
	cells []uint16 // Viewer v's cells are cells[v*span : v*span+n[v]].

	isBlocked func(x, y int) bool // The caller's test, which sees only cells on the grid.
	viewer    int                 // The viewer being cast.
	// Method values made once, so that Move passes them to Cast without allocating.
	block func(x, y int) bool
	add   func(x, y int)
}

// NewField makes a Field for a grid of 1 to 128 cells on each side, viewers
// viewers, and a sight radius of 0 to MaxRadius cells. blocked reports whether
// a cell on the grid blocks sight; the Field treats cells outside the grid as
// blocked and never calls it for them. blocked must give the same answer for a
// cell for as long as the Field is used.
func NewField(width, height, viewers, radius int, blocked func(x, y int) bool) (*Field, error) {
	switch {
	case width < 1 || height < 1 || width > maxSide || height > maxSide:
		return nil, fmt.Errorf(
			"vision: grid size %d×%d: dimensions must be 1–%d",
			width,
			height,
			maxSide,
		)
	case viewers < 1:
		return nil, fmt.Errorf("vision: %d viewers: there must be at least one", viewers)
	case radius < 0 || radius > MaxRadius:
		return nil, fmt.Errorf("vision: radius %d: must be 0–%d", radius, MaxRadius)
	}
	// Cast visits each cell once, so a viewer sees at most the cells of the
	// (2r+1)² square around it, and never more than the whole grid.
	span := min((2*radius+1)*(2*radius+1), width*height)
	f := &Field{
		width: width, height: height, radius: radius, span: span,
		count:     make([]int32, width*height),
		at:        make([]int32, viewers),
		n:         make([]int32, viewers),
		cells:     make([]uint16, viewers*span),
		isBlocked: blocked,
	}
	for i := range f.at {
		f.at[i] = -1
	}
	f.block = f.blocks
	f.add = f.see
	return f, nil
}

// Move places viewer at cell and reports whether what it sees changed. A
// viewer already at cell is left alone. A cell off the grid removes the
// viewer, as Remove does. viewer must be at least 0 and less than the count
// given to NewField.
func (f *Field) Move(viewer int, cell grid.Cell) bool {
	if !f.contains(cell.X, cell.Y) {
		return f.Remove(viewer)
	}
	at := int32(cell.Y*f.width + cell.X) // #nosec G115 -- at most 16,384 cells.
	if f.at[viewer] == at {
		return false
	}
	f.Remove(viewer)
	f.at[viewer], f.viewer = at, viewer
	Cast(cell, f.radius, f.block, f.add)
	return true
}

// Remove takes viewer away and reports whether it was placed.
func (f *Field) Remove(viewer int) bool {
	if f.at[viewer] < 0 {
		return false
	}
	for _, c := range f.Cells(viewer) {
		f.count[c]--
	}
	f.at[viewer], f.n[viewer] = -1, 0
	return true
}

// Reset removes every viewer.
func (f *Field) Reset() {
	clear(f.count)
	clear(f.n)
	for i := range f.at {
		f.at[i] = -1
	}
}

// Visible reports whether any viewer sees the cell (x, y). Cells off the grid
// are never visible.
func (f *Field) Visible(x, y int) bool {
	return f.contains(x, y) && f.count[y*f.width+x] > 0
}

// Cells returns the row-major indices, y × width + x, of the cells viewer
// sees, in the order Cast visited them. The slice is valid until the next
// change to the Field and must not be modified.
func (f *Field) Cells(viewer int) []uint16 {
	base := viewer * f.span
	return f.cells[base : base+int(f.n[viewer])]
}

func (f *Field) contains(x, y int) bool {
	return x >= 0 && y >= 0 && x < f.width && y < f.height
}

func (f *Field) blocks(x, y int) bool { return !f.contains(x, y) || f.isBlocked(x, y) }

// see records that the viewer being cast sees (x, y).
func (f *Field) see(x, y int) {
	if !f.contains(x, y) {
		return
	}
	c := y*f.width + x
	f.cells[f.viewer*f.span+int(f.n[f.viewer])] = uint16(c) // #nosec G115 -- at most 16,384 cells.
	f.n[f.viewer]++
	f.count[c]++
}
