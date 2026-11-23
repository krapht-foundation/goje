//go:build goje_bench && !goje_smoke

package main

import "github.com/krapht-foundation/goje/engine"

// benchmarkCourtyard drives the real patrol. The dense workload places
// additional selectable sprites around it in the same paint and pick list.
// Keep the extra items and their setup out of the normal playground.
type benchmarkCourtyard struct {
	*courtyard
	dense []engine.WorldItem
	slots [3]int
}

func newBenchmarkCourtyard(total int) *benchmarkCourtyard {
	s := &benchmarkCourtyard{courtyard: makeCourtyard()}
	if total == len(s.items) {
		return s
	}
	s.dense = make([]engine.WorldItem, total)
	for i := range s.items {
		s.slots[s.items[i].ID-1] = i
	}
	for i := range s.dense[len(s.items):] {
		// Choose visible projected positions, then invert onto the ground plane.
		// The outer ring and top strip stay clear for deterministic missed clicks.
		x, y := courtyardProjection.Unproject(float64(i%64)*4.5-142, float64(i/64)*4.5-70, 0)
		s.dense[i+len(s.items)] = engine.WorldItem{
			ID: courtyardWall + 1 + uint64(i), GroundX: x, GroundY: y, Hit: engine.HitSelect,
			HitBounds: engine.HitRect{Left: -3, Top: -3, Right: 3, Bottom: 3},
		}
	}
	s.refreshDense()
	return s
}

func (s *benchmarkCourtyard) refreshDense() {
	for i := range s.items {
		item := &s.items[i]
		s.dense[s.slots[item.ID-1]] = *item
	}
	engine.SortWorldItems(s.dense)
	// Find the three moving-scene slots again after they change depth relative
	// to the static sprites. No separate list is built for drawing or picking.
	remaining := len(s.slots)
	for i := range s.dense {
		if id := s.dense[i].ID; id <= courtyardWall {
			s.slots[id-1] = i
			remaining--
			if remaining == 0 {
				break
			}
		}
	}
}

func (s *benchmarkCourtyard) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = engine.Input{}
	if tick.Number == 1 {
		tick.Input.Pressed = engine.KeySpace
	}
	if tick.Number%8 == 0 {
		press := engine.MousePress{X: width / 2, Y: 88, Valid: true}
		if tick.Number%16 == 0 {
			press.Y = 4
		}
		if s.dense == nil {
			tick.Input.PrimaryPress = press
		} else {
			s.pick(press, s.dense)
		}
	}
	transition, err := s.courtyard.Update(tick)
	if s.dense != nil {
		s.refreshDense()
	}
	return transition, err
}

func (s *benchmarkCourtyard) Draw(canvas engine.Canvas) {
	if s.dense == nil {
		s.courtyard.Draw(canvas)
		return
	}
	s.drawCourtyard(canvas, s.dense)
}
