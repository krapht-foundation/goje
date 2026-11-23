package engine_test

import (
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine"
)

func TestCameraRoundTripOnChosenPlane(t *testing.T) {
	projection := engine.Isometric{HalfTileWidth: 16, HalfTileHeight: 8}
	camera := engine.Camera{CenterX: -4.5, CenterY: 2.5, Width: 321, Height: 201, Zoom: 1.25}
	const tileX, tileY, elevation = -1.25, 0.5, 6.5
	projectedX, projectedY := projection.Project(tileX, tileY, elevation)
	x, y := camera.ToLogical(projectedX, projectedY)
	if x != 131.125 || y != 81.75 {
		t.Fatalf("logical point = (%v, %v); want (131.125, 81.75)", x, y)
	}
	px, py := camera.ToProjected(x, y)
	if px != projectedX || py != projectedY {
		t.Fatalf("camera round trip = (%v, %v); want (%v, %v)", px, py, projectedX, projectedY)
	}
	if x, y := projection.Unproject(px, py, elevation); x != tileX || y != tileY {
		t.Fatalf("sprite plane = (%v, %v); want (%v, %v)", x, y, tileX, tileY)
	}
	if x, y := projection.Unproject(px, py, 0); x != -1.65625 || y != 0.09375 {
		t.Fatalf("ground plane = (%v, %v); want (-1.65625, 0.09375)", x, y)
	}
}

func TestWorldOrderUsesGroundDepthThenID(t *testing.T) {
	items := []engine.WorldItem{
		{ID: 9, GroundX: -3, GroundY: 4, Elevation: 50},
		{ID: 3, GroundX: -1, Elevation: -10},
		{ID: 2, GroundX: 1},
		{ID: 1, GroundX: 2},
	}
	// Three different inputs must give the same tie order. Projected sprite
	// heights differ enough to make sorting by sprite Y fail this fixture.
	for _, shift := range []int{0, 1, 2} {
		list := append(slices.Clone(items[shift:]), items[:shift]...)
		engine.SortWorldItems(list)
		for i, want := range []uint64{3, 2, 9, 1} {
			if list[i].ID != want {
				t.Fatalf("input shift %d: paint slot %d = %d; want %d", shift, i, list[i].ID, want)
			}
		}
	}
}

func TestPickWorldUsesPaintOrderAndSpriteBounds(t *testing.T) {
	projection := engine.Isometric{HalfTileWidth: 16, HalfTileHeight: 8}
	camera := engine.Camera{CenterX: 8, CenterY: -4, Width: 320, Height: 200, Zoom: 1.5}
	unit := engine.HitRect{Left: -8, Top: -8, Right: 8, Bottom: 8}
	items := []engine.WorldItem{
		{ID: 18, GroundX: 2, GroundY: 2, Elevation: 32, HitBounds: unit},
		{ID: 0, HitBounds: unit, Hit: engine.HitSelect},
		{
			ID: 10, GroundX: 1, GroundY: 1, Elevation: 16, Hit: engine.HitBlock,
			HitBounds: engine.HitRect{Left: -2, Top: -8, Right: 2, Bottom: 8},
		},
		{
			ID: 7, GroundX: 0.5, GroundY: 0.5, Elevation: 8, Hit: engine.HitSelect,
			HitBounds: engine.HitRect{Left: -4, Top: -6, Right: 4, Bottom: 6},
		},
		{ID: 19, Hit: engine.HitBlock}, // An empty area cannot intercept a press.
	}
	engine.SortWorldItems(items)
	for _, tc := range []struct {
		name string
		x, y float64
		id   uint64
		ok   bool
	}{
		{name: "wall blocks both units"},
		{name: "elevated front unit through decoration", x: 3, id: 7, ok: true},
		{name: "front unit left top inclusive", x: -4, y: -6, id: 7, ok: true},
		{name: "front unit right exclusive", x: 4, id: 0, ok: true},
		{name: "front unit bottom exclusive", x: -3, y: 6, id: 0, ok: true},
		{name: "wall right exclusive", x: 2, id: 7, ok: true},
		{name: "front unit ground position is not its hit area", x: 3, y: 8},
		{name: "empty world", x: 25, y: 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y := camera.ToLogical(tc.x, tc.y)
			px, py := camera.ToProjected(x, y)
			id, ok := engine.PickWorld(items, projection, px, py)
			if id != tc.id || ok != tc.ok {
				t.Fatalf("PickWorld() = (%d, %t); want (%d, %t)", id, ok, tc.id, tc.ok)
			}
		})
	}
}

func BenchmarkWorldView(b *testing.B) {
	for _, fixture := range []struct {
		name  string
		count int
	}{
		{name: "3-items", count: 3},
		{name: "2048-items", count: 2048},
	} {
		b.Run(fixture.name, func(b *testing.B) { benchmarkWorldView(b, fixture.count) })
	}
}

func benchmarkWorldView(b *testing.B, count int) {
	b.Helper()
	projection := engine.Isometric{HalfTileWidth: 16, HalfTileHeight: 8}
	items := make([]engine.WorldItem, count)
	for i := range items {
		items[i] = engine.WorldItem{
			ID: uint64(i), GroundX: float64(i % 64), GroundY: float64(i / 64), Hit: engine.HitSelect,
			HitBounds: engine.HitRect{Left: -4, Top: -8, Right: 4, Bottom: 0},
		}
	}
	engine.SortWorldItems(items)
	b.Run("rebuild-after-movement", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			last := len(items) - 1
			items[0].GroundX, items[last].GroundX = items[last].GroundX, items[0].GroundX
			items[0].GroundY, items[last].GroundY = items[last].GroundY, items[0].GroundY
			engine.SortWorldItems(items)
		}
	})
	b.Run("pick-miss", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			engine.PickWorld(items, projection, 0, -16)
		}
	})
}
