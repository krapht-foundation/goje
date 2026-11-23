// Package grid maps ground positions to cells and finds shortest cardinal routes
// on rectangular, uniform-cost boards. Games supply their own walkability rules.
package grid

import (
	"fmt"
	"slices"
)

const maxSide = 128

// Cell identifies a ground tile by its zero-based column X and row Y.
type Cell struct{ X, Y int }

// Router holds reusable search storage for one board size. Construct it with
// NewRouter. A Router must not be copied or used concurrently or recursively.
type Router struct {
	parent []int
	queue  []int
	width  int
	height int
}

// NewRouter allocates search storage for a board of 1–128 cells on each side.
func NewRouter(width, height int) (*Router, error) {
	if width < 1 || height < 1 || width > maxSide || height > maxSide {
		return nil, fmt.Errorf("grid size %d×%d: dimensions must be 1–%d", width, height, maxSide)
	}
	cells := width * height
	return &Router{
		width: width, height: height,
		parent: make([]int, cells), queue: make([]int, cells),
	}, nil
}

// CellAt returns the cell containing (x, y) in ground tile units. Cell edges
// belong to the cell on their positive side. Positions outside the board and
// nonfinite coordinates return false.
func (r *Router) CellAt(x, y float64) (Cell, bool) {
	// Check before converting: truncation equals floor only on nonnegative
	// values, and comparing with a half-open interval also rejects NaN.
	if x >= 0 && x < float64(r.width) && y >= 0 && y < float64(r.height) {
		return Cell{X: int(x), Y: int(y)}, true
	}
	return Cell{}, false
}

// AppendRoute appends the first shortest route in neighbor order +X, +Y, −X,
// −Y. It excludes start and includes goal. A walkable start equal to goal
// succeeds without appending. Invalid, blocked, and unreachable endpoints
// return dst unchanged and false. Walkable must be non-nil and stable during
// the search; it is called only for cells in bounds. The caller owns dst;
// reserve up to width*height-1 free entries to avoid allocations on commands.
func (r *Router) AppendRoute(
	dst []Cell,
	start, goal Cell,
	walkable func(x, y int) bool,
) ([]Cell, bool) {
	if !r.contains(start) || !r.contains(goal) || !walkable(start.X, start.Y) {
		return dst, false
	}
	if start == goal {
		return dst, true
	}
	if !walkable(goal.X, goal.Y) {
		return dst, false
	}

	clear(r.parent)
	from, to := start.Y*r.width+start.X, goal.Y*r.width+goal.X
	// Store parent index + 1 so zero means unseen, including for cell 0.
	r.parent[from] = from + 1
	r.queue[0] = from
	steps := [...]Cell{{X: 1}, {Y: 1}, {X: -1}, {Y: -1}}
	tail := 1
	for head, distance := 0, 1; head < tail; distance++ {
		// Every cell currently queued is at the same depth. Its neighbors
		// are distance steps from start, so no extra length pass is needed.
		end := tail
		for ; head < end; head++ {
			index := r.queue[head]
			x, y := index%r.width, index/r.width
			for _, step := range &steps {
				next := Cell{X: x + step.X, Y: y + step.Y}
				if !r.contains(next) {
					continue
				}
				neighbor := next.Y*r.width + next.X
				if r.parent[neighbor] != 0 || !walkable(next.X, next.Y) {
					continue
				}
				r.parent[neighbor] = index + 1
				if neighbor == to {
					return r.appendPath(dst, to, distance), true
				}
				r.queue[tail] = neighbor
				tail++
			}
		}
	}
	return dst, false
}

func (r *Router) contains(c Cell) bool {
	return c.X >= 0 && c.Y >= 0 && c.X < r.width && c.Y < r.height
}

func (r *Router) appendPath(dst []Cell, goal, distance int) []Cell {
	end := len(dst) + distance
	dst = slices.Grow(dst, distance)[:end]
	for i := end - 1; i >= end-distance; i-- {
		dst[i] = Cell{X: goal % r.width, Y: goal / r.width}
		goal = r.parent[goal] - 1
	}
	return dst
}
