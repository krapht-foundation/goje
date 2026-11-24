// Package collide moves rectangular actors through a static world of solids and
// reports which shapes overlap a region. It works in ground coordinates, never
// in screen pixels or draw order, so collision does not change when the camera,
// the projection, or the paint order does.
//
// The package uses the exact arithmetic of package fixed, so one sequence of
// moves produces identical positions and contacts on every desktop and browser
// target. It decides only whether a move is possible. Routing, steering, and
// the meaning of a hit belong to the game. See RFC 0013 for the design.
package collide

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/krapht-foundation/goje/engine/fixed"
)

const (
	// MaxSide is the largest supported board side in tiles. It matches
	// package grid.
	MaxSide = 128
	// MaxShapes is the largest number of solids, and of triggers, in a world.
	MaxShapes = 1 << 16
)

var (
	// ErrInvalid reports a malformed board or shape.
	ErrInvalid = errors.New("collide: invalid world or shape")
	// ErrOutside reports a body that is not fully inside the board.
	ErrOutside = errors.New("collide: body outside the board")
	// ErrEmbedded reports a body that overlaps a solid before it moves. Move
	// does not push it out, because the direction would be arbitrary.
	ErrEmbedded = errors.New("collide: body overlaps a solid")
)

// Interval is the half-open vertical extent [Bottom, Top) in whole projected
// pixels, the unit of engine.WorldItem.Elevation. An empty interval overlaps
// nothing.
type Interval struct{ Bottom, Top int32 }

// Empty reports whether the interval contains no heights.
func (i Interval) Empty() bool { return i.Top <= i.Bottom }

// Overlaps reports whether the intervals share a height. Intervals that only
// touch do not overlap, so a body standing at the top of a solid is not
// blocked by it.
func (i Interval) Overlaps(o Interval) bool {
	return !i.Empty() && !o.Empty() && i.Bottom < o.Top && o.Bottom < i.Top
}

// Shape is a footprint on the ground plane with a vertical extent. Solids,
// triggers, and moving bodies all use it. The ID is chosen by the game and must
// be unique among the solids of a world and among its triggers.
type Shape struct {
	ID        uint64
	Footprint fixed.Rect
	Vertical  Interval
}

// Hit identifies a shape returned by World.Overlaps.
type Hit struct {
	ID      uint64
	Trigger bool
}

// Contact describes what shortened one axis of a move. A zero Contact means
// the axis moved the full distance.
type Contact struct {
	// Blocked is true when the move was shortened.
	Blocked bool
	// Edge is true when the board edge stopped the move. Solid and Body are then zero.
	Edge bool
	// Solid is the ID of the nearest solid that stopped the move, with the
	// lowest ID winning a tie.
	Solid uint64
	// Body is the nonzero ID of the nearest body that stopped the move, with the
	// lowest ID winning a tie. A solid or the board edge at the same distance
	// is reported instead, and Body is then zero.
	Body uint64
}

// Result is the outcome of World.Move.
type Result struct {
	// Body is the moved body. It touches, and never overlaps, what stopped it.
	Body Shape
	// Moved is the distance actually travelled on each axis.
	Moved fixed.Vec2
	// X and Y describe what shortened the move on each axis.
	X, Y Contact
}

// World is an immutable set of solids and triggers on a rectangular board. It
// is safe for concurrent use. Create it with New.
type World struct {
	width, height int
	maxX, maxY    fixed.Num
	solids        index
	triggers      index
}

// New builds a world for a board of 1–MaxSide tiles on each side, in tiles from
// the origin. Every shape must be non-empty on the ground and vertically, lie
// inside the board, and have an ID unique within its list. New copies the
// shapes; the caller keeps its slices.
func New(width, height int, solids, triggers []Shape) (*World, error) {
	if width < 1 || width > MaxSide || height < 1 || height > MaxSide {
		return nil, fmt.Errorf("%w: board must be 1–%d tiles on each side", ErrInvalid, MaxSide)
	}
	w := &World{
		width: width, height: height,
		maxX: fixed.FromInt(width), maxY: fixed.FromInt(height),
	}
	var err error
	if w.solids, err = w.newIndex("solid", solids); err != nil {
		return nil, err
	}
	if w.triggers, err = w.newIndex("trigger", triggers); err != nil {
		return nil, err
	}
	return w, nil
}

// Overlaps appends the shapes that overlap rect at some height in v, solids
// first and then triggers, each in ascending ID order. The caller owns dst and
// can reuse its capacity.
func (w *World) Overlaps(dst []Hit, rect fixed.Rect, v Interval) []Hit {
	start := len(dst)
	w.solids.each(w, rect, v, func(s *Shape) bool {
		dst = append(dst, Hit{ID: s.ID})
		return true
	})
	slices.SortFunc(dst[start:], compareHits)
	start = len(dst)
	w.triggers.each(w, rect, v, func(s *Shape) bool {
		dst = append(dst, Hit{ID: s.ID, Trigger: true})
		return true
	})
	slices.SortFunc(dst[start:], compareHits)
	return dst
}

func compareHits(a, b Hit) int { return cmp.Compare(a.ID, b.ID) }

// Move moves body by (dx, dy), first along X and then along Y, and stops it
// flush against the nearest solid or the board edge on each axis. The body
// slides: being stopped on one axis does not cancel the other. The sweep covers
// the whole step, so a large step cannot skip a thin solid. A solid blocks only
// a body whose vertical extent overlaps its own. Triggers never block.
//
// Move returns ErrOutside if the body is not inside the board and ErrEmbedded,
// wrapped with the solid's ID, if it already overlaps a solid.
func (w *World) Move(body Shape, dx, dy fixed.Num) (Result, error) {
	return w.MoveAmong(nil, body, dx, dy)
}

// MoveAmong is Move for a body that must also stop flush against other moving
// bodies. A body in bodies blocks the mover only if its vertical extent
// overlaps the mover's. An entry with the mover's own ID is ignored, so a
// stored body can move without being removed first. MoveAmong does not store the
// result; the caller does that with Bodies.Set. A nil set behaves like Move.
//
// A body that already overlaps another body returns ErrEmbedded, as it does
// for a solid. Games must not place bodies on top of each other.
func (w *World) MoveAmong(bodies *Bodies, body Shape, dx, dy fixed.Num) (Result, error) {
	fp := body.Footprint
	if fp.Empty() || body.Vertical.Empty() {
		return Result{}, fmt.Errorf("%w: body %d is empty", ErrInvalid, body.ID)
	}
	if !w.contains(fp) {
		return Result{}, fmt.Errorf("%w: body %d", ErrOutside, body.ID)
	}
	embedded, blocker := false, uint64(0)
	w.solids.each(w, fp, body.Vertical, func(s *Shape) bool {
		if !embedded || s.ID < blocker {
			embedded, blocker = true, s.ID
		}
		return true
	})
	if embedded {
		return Result{}, fmt.Errorf("%w: body %d and solid %d", ErrEmbedded, body.ID, blocker)
	}
	embedded = false
	bodies.each(fp, body.Vertical, body.ID, func(s *Shape) bool {
		if !embedded || s.ID < blocker {
			embedded, blocker = true, s.ID
		}
		return true
	})
	if embedded {
		return Result{}, fmt.Errorf("%w: body %d and body %d", ErrEmbedded, body.ID, blocker)
	}

	var result Result
	result.Moved.X, result.X = w.sweep(bodies, body, false, dx)
	body.Footprint = body.Footprint.Translate(fixed.Vec2{X: result.Moved.X})
	result.Moved.Y, result.Y = w.sweep(bodies, body, true, dy)
	body.Footprint = body.Footprint.Translate(fixed.Vec2{Y: result.Moved.Y})
	result.Body = body
	return result, nil
}

// sweep moves body along one axis. It works in the X axis and transposes the
// rectangles when moving along Y, so both axes share one implementation.
func (w *World) sweep(bodies *Bodies, body Shape, vertical bool, d fixed.Num) (fixed.Num, Contact) {
	if d == 0 {
		return 0, Contact{}
	}
	fp, limit := body.Footprint, w.maxX
	if vertical {
		fp, limit = transpose(fp), w.maxY
	}
	// The region the body sweeps through, beyond its leading edge.
	region := fixed.Rect{
		Min: fixed.Vec2{X: fp.Max.X, Y: fp.Min.Y},
		Max: fixed.Vec2{X: fp.Max.X.Add(d), Y: fp.Max.Y},
	}
	if d < 0 {
		region = fixed.Rect{
			Min: fixed.Vec2{X: fp.Min.X.Add(d), Y: fp.Min.Y},
			Max: fixed.Vec2{X: fp.Min.X, Y: fp.Max.Y},
		}
	}
	if vertical {
		region = transpose(region)
	}

	// A solid in the region cannot overlap the body: Move rejected that case
	// and each axis stops flush, so every distance here is at least zero.
	best, contact := d.Abs(), Contact{}
	w.solids.each(w, region, body.Vertical, func(s *Shape) bool {
		sf := s.Footprint
		if vertical {
			sf = transpose(sf)
		}
		distance := sf.Min.X.Sub(fp.Max.X)
		if d < 0 {
			distance = fp.Min.X.Sub(sf.Max.X)
		}
		if distance < best || (contact.Blocked && distance == best && s.ID < contact.Solid) {
			best, contact = distance, Contact{Blocked: true, Solid: s.ID}
		}
		return true
	})
	edge := limit.Sub(fp.Max.X)
	if d < 0 {
		edge = fp.Min.X
	}
	if edge < best {
		best, contact = edge, Contact{Blocked: true, Edge: true}
	}
	// Solids and the edge win ties, so a body only shortens the move if it is
	// strictly nearer, or ties with another body that has a higher ID.
	bodies.each(region, body.Vertical, body.ID, func(s *Shape) bool {
		sf := s.Footprint
		if vertical {
			sf = transpose(sf)
		}
		distance := sf.Min.X.Sub(fp.Max.X)
		if d < 0 {
			distance = fp.Min.X.Sub(sf.Max.X)
		}
		if distance < best || (contact.Body != 0 && distance == best && s.ID < contact.Body) {
			best, contact = distance, Contact{Blocked: true, Body: s.ID}
		}
		return true
	})
	if d < 0 {
		best = best.Neg()
	}
	return best, contact
}

func (w *World) contains(r fixed.Rect) bool {
	return r.Min.X >= 0 && r.Min.Y >= 0 && r.Max.X <= w.maxX && r.Max.Y <= w.maxY
}

func transpose(r fixed.Rect) fixed.Rect {
	return fixed.Rect{
		Min: fixed.Vec2{X: r.Min.Y, Y: r.Min.X},
		Max: fixed.Vec2{X: r.Max.Y, Y: r.Max.X},
	}
}

// index stores shapes sorted by ID and lists them under every tile their
// footprint touches, in compressed form: items[start[c]:start[c+1]] are the
// shape indices of tile c. A query visits only the tiles it covers, and each
// shape is reported once, from the first covered tile it touches.
type index struct {
	shapes []Shape
	start  []int32
	items  []int32
}

func (w *World) newIndex(kind string, shapes []Shape) (index, error) {
	if len(shapes) > MaxShapes {
		return index{}, fmt.Errorf(
			"%w: %d %ss exceed the limit of %d",
			ErrInvalid,
			len(shapes),
			kind,
			MaxShapes,
		)
	}
	ix := index{shapes: slices.Clone(shapes), start: make([]int32, w.width*w.height+1)}
	slices.SortFunc(ix.shapes, func(a, b Shape) int { return cmp.Compare(a.ID, b.ID) })
	for i := range ix.shapes {
		s := &ix.shapes[i]
		if i > 0 && s.ID == ix.shapes[i-1].ID {
			return index{}, fmt.Errorf("%w: duplicate %s ID %d", ErrInvalid, kind, s.ID)
		}
		if s.Footprint.Empty() || s.Vertical.Empty() || !w.contains(s.Footprint) {
			return index{}, fmt.Errorf(
				"%w: %s %d must be non-empty and inside the board",
				ErrInvalid,
				kind,
				s.ID,
			)
		}
		x0, y0, x1, y1 := cells(s.Footprint)
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				ix.start[y*w.width+x+1]++
			}
		}
	}
	for c := 1; c < len(ix.start); c++ {
		ix.start[c] += ix.start[c-1]
	}
	ix.items = make([]int32, ix.start[len(ix.start)-1])
	next := slices.Clone(ix.start[:len(ix.start)-1])
	for i := range ix.shapes {
		x0, y0, x1, y1 := cells(ix.shapes[i].Footprint)
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				c := y*w.width + x
				ix.items[next[c]] = int32(i)
				next[c]++
			}
		}
	}
	return ix, nil
}

// cells returns the inclusive range of tiles a non-empty rectangle touches.
// Max is exclusive, so a rectangle ending on a tile edge does not touch the
// next tile.
func cells(r fixed.Rect) (x0, y0, x1, y1 int) {
	return r.Min.X.Floor(), r.Min.Y.Floor(), r.Max.X.Sub(1).Floor(), r.Max.Y.Sub(1).Floor()
}

// each calls fn once for every shape whose footprint overlaps q and whose
// vertical extent overlaps v, in no particular order, until fn returns false.
func (ix *index) each(w *World, q fixed.Rect, v Interval, fn func(*Shape) bool) {
	q.Min.X, q.Min.Y = max(q.Min.X, 0), max(q.Min.Y, 0)
	q.Max.X, q.Max.Y = min(q.Max.X, w.maxX), min(q.Max.Y, w.maxY)
	if q.Empty() || v.Empty() {
		return
	}
	qx0, qy0, qx1, qy1 := cells(q)
	for cy := qy0; cy <= qy1; cy++ {
		for cx := qx0; cx <= qx1; cx++ {
			c := cy*w.width + cx
			for _, i := range ix.items[ix.start[c]:ix.start[c+1]] {
				s := &ix.shapes[i]
				// A shape spans several tiles; report it only from the first
				// tile of its footprint that the query covers.
				sx0, sy0, _, _ := cells(s.Footprint)
				if cx != max(qx0, sx0) || cy != max(qy0, sy0) {
					continue
				}
				if s.Footprint.Overlaps(q) && s.Vertical.Overlaps(v) && !fn(s) {
					return
				}
			}
		}
	}
}
