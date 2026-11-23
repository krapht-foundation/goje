package engine

import (
	"cmp"
	"slices"
)

// HitMode determines whether a world item responds to or intercepts a press.
type HitMode uint8

const (
	// HitPassThrough lets the query continue to items behind this one.
	HitPassThrough HitMode = iota
	// HitSelect returns the item's ID when the press is inside its bounds.
	HitSelect
	// HitBlock stops the query without selecting anything behind this item.
	HitBlock
)

// HitRect describes a hit area in unzoomed pixels relative to the sprite's
// projected anchor. Left and Top are inclusive; Right and Bottom are exclusive.
// An empty rectangle never hits.
type HitRect struct {
	Left, Top, Right, Bottom float64
}

// WorldItem describes one presentation item, separate from simulation storage.
// GroundX and GroundY are in tiles. Elevation is in unzoomed projected pixels;
// it moves the sprite and hit area without changing the item's paint depth.
// Coordinates and their depth sum must be finite. ID is unique in the world list.
type WorldItem struct {
	ID               uint64
	GroundX, GroundY float64
	Elevation        float64
	HitBounds        HitRect
	Hit              HitMode
}

// ProjectedAnchor places the sprite and its hit bounds at the same unzoomed
// projected position. A renderer can pass this point to Camera.ToLogical.
func (item WorldItem) ProjectedAnchor(projection Isometric) (x, y float64) {
	return projection.Project(item.GroundX, item.GroundY, item.Elevation)
}

// SortWorldItems orders presentation items in place, back to front. A lower
// ground-depth sum draws first; lower IDs draw first when depth ties. Keep this
// slice separate from simulation storage and reuse its capacity between updates.
func SortWorldItems(items []WorldItem) {
	slices.SortFunc(items, func(a, b WorldItem) int {
		if depth := cmp.Compare(a.GroundX+a.GroundY, b.GroundX+b.GroundY); depth != 0 {
			return depth
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// PickWorld returns the frontmost selectable item at an unzoomed projected
// point. Use the same back-to-front list as drawing, and transform the press
// with Camera.ToProjected before calling. A blocker or a miss returns false.
func PickWorld(items []WorldItem, projection Isometric, x, y float64) (id uint64, ok bool) {
	for i := len(items) - 1; i >= 0; i-- {
		item := &items[i]
		if item.Hit == HitPassThrough {
			continue
		}
		ax, ay := item.ProjectedAnchor(projection)
		localX, localY := x-ax, y-ay
		bounds := item.HitBounds
		if localX >= bounds.Left && localX < bounds.Right && localY >= bounds.Top &&
			localY < bounds.Bottom {
			if item.Hit == HitSelect {
				return item.ID, true
			}
			return 0, false
		}
	}
	return 0, false
}
