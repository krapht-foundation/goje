package collide

import (
	"fmt"
	"slices"

	"github.com/krapht-foundation/goje/engine/fixed"
)

// MaxBodies is the largest number of bodies in a Bodies set.
const MaxBodies = 1 << 12

// tileReserve is how many bodies each tile can list before it allocates. Bodies
// are usually smaller than a tile, so a crowd rarely exceeds it; a tile that does
// grows once and keeps the larger list.
const tileReserve = 8

// Bodies is a set of moving shapes on one board, for World.MoveAmong and
// overlap queries. Unlike World it changes during play, so it is not safe for
// concurrent use. Create it with NewBodies.
//
// Every body has a nonzero ID chosen by the game and unique within the set. A
// body is listed under each tile its footprint touches, so a query visits only
// the tiles it covers.
type Bodies struct {
	width, height int
	maxX, maxY    fixed.Num
	slots         []Shape
	free          []int
	byID          map[uint64]int
	tiles         [][]int // Slot numbers per tile, in no particular order.
}

// NewBodies returns an empty set for a board of 1–MaxSide tiles on each side.
func NewBodies(width, height int) (*Bodies, error) {
	if width < 1 || width > MaxSide || height < 1 || height > MaxSide {
		return nil, fmt.Errorf("%w: board must be 1–%d tiles on each side", ErrInvalid, MaxSide)
	}
	// One backing array holds every tile's reserve, so play does not allocate
	// the first time a body visits a tile.
	tiles, slab := make([][]int, width*height), make([]int, width*height*tileReserve)
	for i := range tiles {
		tiles[i] = slab[i*tileReserve : i*tileReserve : (i+1)*tileReserve]
	}
	return &Bodies{
		width: width, height: height,
		maxX: fixed.FromInt(width), maxY: fixed.FromInt(height),
		free:  make([]int, 0, MaxBodies), // Removing bodies never allocates.
		byID:  make(map[uint64]int),
		tiles: tiles,
	}, nil
}

// Len returns the number of bodies in the set.
func (b *Bodies) Len() int { return len(b.byID) }

// Get returns the body with the given ID.
func (b *Bodies) Get(id uint64) (Shape, bool) {
	slot, ok := b.byID[id]
	if !ok {
		return Shape{}, false
	}
	return b.slots[slot], true
}

// Set inserts the body, or replaces the body that has the same ID. The ID must
// be nonzero, and the footprint and vertical extent must be non-empty with the
// footprint inside the board. A failed call leaves the set unchanged. Set does
// not check for overlaps with other bodies; MoveAmong reports them when the
// body next moves.
func (b *Bodies) Set(s Shape) error {
	if s.ID == 0 || s.Footprint.Empty() || s.Vertical.Empty() {
		return fmt.Errorf("%w: body %d must have a nonzero ID and a non-empty shape", ErrInvalid, s.ID)
	}
	if fp := s.Footprint; fp.Min.X < 0 || fp.Min.Y < 0 || fp.Max.X > b.maxX || fp.Max.Y > b.maxY {
		return fmt.Errorf("%w: body %d", ErrOutside, s.ID)
	}
	slot, ok := b.byID[s.ID]
	switch {
	case ok:
		oldX0, oldY0, oldX1, oldY1 := cells(b.slots[slot].Footprint)
		newX0, newY0, newX1, newY1 := cells(s.Footprint)
		if oldX0 != newX0 || oldY0 != newY0 || oldX1 != newX1 || oldY1 != newY1 {
			b.unlist(slot)
			b.slots[slot] = s
			b.list(slot)
		} else {
			b.slots[slot] = s
		}
	case len(b.byID) >= MaxBodies:
		return fmt.Errorf("%w: a set holds at most %d bodies", ErrInvalid, MaxBodies)
	default:
		if n := len(b.free); n > 0 {
			slot, b.free = b.free[n-1], b.free[:n-1]
			b.slots[slot] = s
		} else {
			slot = len(b.slots)
			b.slots = append(b.slots, s)
		}
		b.byID[s.ID] = slot
		b.list(slot)
	}
	return nil
}

// Remove deletes the body with the given ID and reports whether it existed.
func (b *Bodies) Remove(id uint64) bool {
	slot, ok := b.byID[id]
	if !ok {
		return false
	}
	b.unlist(slot)
	delete(b.byID, id)
	b.free = append(b.free, slot)
	return true
}

// Overlaps appends the IDs of the bodies that overlap rect at some height in v,
// in ascending order. The caller owns dst and can reuse its capacity. A query
// may extend past the board.
func (b *Bodies) Overlaps(dst []uint64, rect fixed.Rect, v Interval) []uint64 {
	start := len(dst)
	b.each(rect, v, 0, func(s *Shape) bool {
		dst = append(dst, s.ID)
		return true
	})
	slices.Sort(dst[start:])
	return dst
}

func (b *Bodies) list(slot int) {
	x0, y0, x1, y1 := cells(b.slots[slot].Footprint)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c := y*b.width + x
			b.tiles[c] = append(b.tiles[c], slot)
		}
	}
}

func (b *Bodies) unlist(slot int) {
	x0, y0, x1, y1 := cells(b.slots[slot].Footprint)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c := y*b.width + x
			i := slices.Index(b.tiles[c], slot)
			last := len(b.tiles[c]) - 1
			b.tiles[c][i] = b.tiles[c][last]
			b.tiles[c] = b.tiles[c][:last]
		}
	}
}

// each calls fn once for every body except the one with ID skip whose footprint
// overlaps q and whose vertical extent overlaps v, in no particular order,
// until fn returns false. A nil set has no bodies.
func (b *Bodies) each(q fixed.Rect, v Interval, skip uint64, fn func(*Shape) bool) {
	if b == nil {
		return
	}
	q.Min.X, q.Min.Y = max(q.Min.X, 0), max(q.Min.Y, 0)
	q.Max.X, q.Max.Y = min(q.Max.X, b.maxX), min(q.Max.Y, b.maxY)
	if q.Empty() || v.Empty() {
		return
	}
	qx0, qy0, qx1, qy1 := cells(q)
	for cy := qy0; cy <= qy1; cy++ {
		for cx := qx0; cx <= qx1; cx++ {
			for _, slot := range b.tiles[cy*b.width+cx] {
				s := &b.slots[slot]
				// Report a body once, from the first tile it shares with the query.
				sx0, sy0, _, _ := cells(s.Footprint)
				if cx != max(qx0, sx0) || cy != max(qy0, sy0) {
					continue
				}
				if s.ID != skip && s.Footprint.Overlaps(q) && s.Vertical.Overlaps(v) && !fn(s) {
					return
				}
			}
		}
	}
}
