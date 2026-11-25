package collide

import (
	"cmp"
	"fmt"
	"slices"
)

// Overlap is one body overlapping one trigger, as a Tracker remembers it
// between updates. Games save and restore these pairs so that a loaded scene
// does not report entries again for bodies that were already inside.
type Overlap struct{ Trigger, Body uint64 }

// AppendOverlaps appends the pairs that overlapped at the previous Update, or
// that the last Restore set, ordered by trigger ID and then body ID. The caller
// owns dst.
func (t *Tracker) AppendOverlaps(dst []Overlap) []Overlap {
	for _, p := range t.prev {
		dst = append(dst, Overlap{Trigger: p.trigger, Body: p.body})
	}
	return dst
}

// Restore replaces the remembered pairs with overlaps, as if the previous
// Update had found exactly them, so the next Update reports only what differs.
// The pairs must be in strictly ascending order of trigger ID and then body ID,
// as AppendOverlaps writes them, and every trigger must be in the tracker's
// world. Restore does not check that the bodies still overlap their triggers:
// a pair that no longer holds exits on the next Update. On error, which wraps
// ErrInvalid, the tracker is unchanged.
func (t *Tracker) Restore(overlaps []Overlap) error {
	for i, o := range overlaps {
		if i > 0 && compareOverlaps(overlaps[i-1], o) >= 0 {
			return fmt.Errorf("%w: tracker overlaps are not in strictly ascending order", ErrInvalid)
		}
		if _, ok := slices.BinarySearchFunc(
			t.world.triggers.shapes,
			o.Trigger,
			func(s Shape, id uint64) int {
				return cmp.Compare(s.ID, id)
			},
		); !ok {
			return fmt.Errorf("%w: tracker overlap names unknown trigger %d", ErrInvalid, o.Trigger)
		}
	}
	t.prev = t.prev[:0]
	for _, o := range overlaps {
		t.prev = append(t.prev, pair{trigger: o.Trigger, body: o.Body})
	}
	return nil
}

func compareOverlaps(a, b Overlap) int {
	return comparePairs(
		pair{trigger: a.Trigger, body: a.Body},
		pair{trigger: b.Trigger, body: b.Body},
	)
}
