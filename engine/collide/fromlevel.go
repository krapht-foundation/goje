package collide

import (
	"fmt"

	"github.com/krapht-foundation/goje/engine/fixed"
	"github.com/krapht-foundation/goje/level"
)

// WallIDBase is the smallest ID FromLevel gives a wall cell. Zone IDs are 32-bit,
// so they never reach it. A game that adds its own solids to a level's world
// should choose IDs from the same range as zones or below WallIDBase.
const WallIDBase uint64 = 1 << 32

// WallID returns the ID of the solid that FromLevel builds for the wall cell
// (x, y) on a board that is width cells wide.
func WallID(width, x, y int) uint64 {
	return WallIDBase + uint64(y*width+x) // #nosec G115 -- a cell index is never negative.
}

// FromLevel builds a world from a decoded level with LevelShapes.
func FromLevel(board level.Board, wall Interval) (*World, error) {
	solids, triggers, err := LevelShapes(board, wall)
	if err != nil {
		return nil, err
	}
	width, height := board.Size()
	return New(width, height, solids, triggers)
}

// LevelShapes returns the solids and triggers of a decoded level, for a game
// that wants to add shapes of its own before calling New. Each wall cell becomes
// a solid that fills the cell and has the vertical extent wall, which the game
// chooses because a level does not say how tall its walls are. Each zone becomes
// a solid or a trigger, by its role, with its own ID and vertical extent. The
// game gives each ID a meaning: read the level's zones for kinds, and use WallID
// to recognize a wall.
func LevelShapes(board level.Board, wall Interval) (solids, triggers []Shape, err error) {
	width, height := board.Size()
	if width == 0 {
		return nil, nil, fmt.Errorf("%w: the level is empty", ErrInvalid)
	}
	for y := range height {
		for x := range width {
			if !board.Walkable(x, y) {
				solids = append(solids, Shape{
					ID:        WallID(width, x, y),
					Footprint: cellRect(x, y, 1, 1),
					Vertical:  wall,
				})
			}
		}
	}
	for z := range board.Zones() {
		shape := Shape{
			ID:        uint64(z.ID),
			Footprint: cellRect(z.X, z.Y, z.W, z.H),
			Vertical:  interval(z.Bottom, z.Top),
		}
		if z.Role == level.RoleTrigger {
			triggers = append(triggers, shape)
		} else {
			solids = append(solids, shape)
		}
	}
	return solids, triggers, nil
}

func interval(bottom, top int) Interval {
	return Interval{
		Bottom: int32(bottom),
		Top:    int32(top),
	} // #nosec G115 -- the decoder limits heights to 1,024.
}

func cellRect(x, y, w, h int) fixed.Rect {
	return fixed.Rect{
		Min: fixed.Vec2{X: fixed.FromInt(x), Y: fixed.FromInt(y)},
		Max: fixed.Vec2{X: fixed.FromInt(x + w), Y: fixed.FromInt(y + h)},
	}
}
