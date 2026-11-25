// Package vision computes line of sight on a grid of cells and keeps track of
// which cells a set of viewers can see. It uses symmetric shadowcasting with
// slopes held as integer fractions, so the result is the same on every desktop
// and browser target, and it is symmetric: between two cells that do not block
// sight, a sees b exactly when b sees a.
//
// The package knows only cells and a test for whether a cell blocks sight.
// What blocks sight, who views from where, and what a game does with the
// result belong to the game. The skirmish simulation in examples/skirmish uses
// it for fog of war. See specs/vision-m15.md for the contract and RFC 0016 for
// the design.
package vision

import (
	"github.com/krapht-foundation/goje/engine/grid"
)

// MaxRadius is the largest sight radius in cells. A radius of 181 reaches
// every cell of the largest board package grid supports, 128 × 128, from any
// corner. Cast treats a larger radius as MaxRadius.
const MaxRadius = 181

// The four quadrants, in the order Cast scans them. North and south together
// cover all four diagonals, so east and west skip the diagonal cells that the
// first two already visited.
const (
	north = iota
	south
	east
	west
)

// fraction is a slope num/den with den > 0.
type fraction struct{ num, den int }

// caster is the state of one Cast. It lives on the stack.
type caster struct {
	ox, oy, radius int
	quadrant       int
	blocked        func(x, y int) bool
	visit          func(x, y int)
	// diagonal records the diagonal cells visited by the north and south
	// scans: one bit per depth for each of the four diagonals.
	diagonal [4][2]uint64
}

// Cast calls visit once for each cell within radius of origin, by the test
// dx² + dy² ≤ radius², that is in line of sight of origin. The origin is
// always visible, even with a radius of zero or less. A cell for which blocked
// reports true stops sight but is itself visible, as a wall is seen from in
// front of it.
//
// Cast does not know the grid's size. blocked must report cells outside the
// grid as blocked, and visit may then receive such cells, because a blocked
// cell is visible; a caller with a bounded grid ignores them, as Field does.
//
// The order of the calls is fixed: the origin first, then the north quadrant
// (decreasing y), south, east, and west, each row by row outward from the
// origin. A cell is in line of sight when the line from the center of the
// origin to its center passes through no blocked cell, with each row of a
// quadrant treated as a line of cells through their centers; a blocked cell is
// visible when such a line reaches any part of it. specs/vision-m15.md states
// the rule exactly. Cast allocates nothing.
func Cast(origin grid.Cell, radius int, blocked func(x, y int) bool, visit func(x, y int)) {
	visit(origin.X, origin.Y)
	radius = min(radius, MaxRadius)
	if radius < 1 {
		return
	}
	c := caster{ox: origin.X, oy: origin.Y, radius: radius, blocked: blocked, visit: visit}
	for q := range 4 {
		c.quadrant = q
		c.scan(1, fraction{-1, 1}, fraction{1, 1})
	}
}

// scan visits one row of the current quadrant between the start and end
// slopes and recurses into the next row for each run of open cells. This is
// Albert Ford's symmetric shadowcasting: an open cell is visible only when its
// center lies within the slopes, which is what makes the result symmetric.
func (c *caster) scan(depth int, start, end fraction) {
	if depth > c.radius {
		return
	}
	// The first and last columns whose cells overlap the slopes, rounding a
	// tie toward the inside of the row.
	first := floorDiv(2*depth*start.num+start.den, 2*start.den)
	last := -floorDiv(-(2*depth*end.num - end.den), 2*end.den)
	const none, open, wall = 0, 1, 2
	prev := none
	for col := first; col <= last; col++ {
		x, y := c.cell(depth, col)
		blocked := c.blocked(x, y)
		symmetric := col*start.den >= depth*start.num && col*end.den <= depth*end.num
		if (blocked || symmetric) && depth*depth+col*col <= c.radius*c.radius {
			c.reveal(depth, col, x, y)
		}
		switch {
		case prev == wall && !blocked:
			start = fraction{2*col - 1, 2 * depth}
		case prev == open && blocked:
			c.scan(depth+1, start, fraction{2*col - 1, 2 * depth})
		}
		prev = open
		if blocked {
			prev = wall
		}
	}
	if prev == open {
		c.scan(depth+1, start, end)
	}
}

// cell turns a quadrant's depth and column into grid coordinates.
func (c *caster) cell(depth, col int) (x, y int) {
	switch c.quadrant {
	case north:
		return c.ox + col, c.oy - depth
	case south:
		return c.ox + col, c.oy + depth
	case east:
		return c.ox + depth, c.oy + col
	default:
		return c.ox - depth, c.oy + col
	}
}

// reveal visits a cell, once even if it lies on a diagonal that two quadrants share.
func (c *caster) reveal(depth, col, x, y int) {
	if col == depth || col == -depth {
		d := 0
		if x > c.ox {
			d |= 1
		}
		if y > c.oy {
			d |= 2
		}
		word, bit := (depth-1)/64, uint64(1)<<((depth-1)%64)
		if c.quadrant == north || c.quadrant == south {
			c.diagonal[d][word] |= bit
		} else if c.diagonal[d][word]&bit != 0 {
			return
		}
	}
	c.visit(x, y)
}

// floorDiv divides rounding toward negative infinity; b must be positive.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}
