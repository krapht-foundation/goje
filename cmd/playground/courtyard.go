package main

import (
	"image/color"
	"math"

	"github.com/krapht-foundation/goje/engine"
)

const (
	courtyardAmber uint64 = iota + 1
	courtyardTeal
	courtyardWall
)

const patrolLegTicks = 120

var (
	courtyardProjection = engine.Isometric{HalfTileWidth: 16, HalfTileHeight: 8}
	courtyardZoom       = [...]float64{0.8, 1, 1.25, 1.5, 2}
	courtyardWallColor  = color.NRGBA{R: 148, G: 122, B: 105, A: 255}
	selectionColor      = color.NRGBA{R: 255, G: 235, B: 87, A: 255}
)

// courtyard keeps patrol progress separate from the reusable paint and hit
// list. Sorting presentation items never changes the patrol routes or IDs.
type courtyard struct {
	camera     engine.Camera
	items      [3]engine.WorldItem
	selected   uint64
	phase      int
	zoomLevel  int
	patrolling bool
}

func newCourtyard() engine.Scene {
	return makeCourtyard()
}

func makeCourtyard() *courtyard {
	s := &courtyard{camera: engine.Camera{Width: width, Height: height, Zoom: 1}, zoomLevel: 1}
	s.prepareWorld()
	return s
}

func (*courtyard) Enter() error { return nil }
func (*courtyard) Exit()        {}

func (s *courtyard) Update(tick engine.Tick) (engine.Transition, error) {
	input := tick.Input
	if input.Pressed.Has(engine.KeyR) {
		return engine.Restart(), nil
	}
	// Presentations can skip over simulation ticks. A click uses the world and
	// camera at tick start, not the next patrol position or a historical frame.
	if press := input.PrimaryPress; press.Valid {
		s.pick(press, s.items[:])
	}
	s.moveCamera(input)
	if input.Pressed.Has(engine.KeySpace) {
		s.patrolling = !s.patrolling
	}
	if s.patrolling {
		s.phase = (s.phase + 1) % (2 * patrolLegTicks)
		s.prepareWorld()
	}
	return engine.Transition{}, nil
}

func (s *courtyard) pick(press engine.MousePress, items []engine.WorldItem) {
	x, y := s.camera.ToProjected(press.X, press.Y)
	s.selected = 0
	if id, ok := engine.PickWorld(items, courtyardProjection, x, y); ok {
		s.selected = id
	}
}

func (s *courtyard) moveCamera(input engine.Input) {
	s.zoomLevel = min(
		len(courtyardZoom)-1,
		max(0, s.zoomLevel+axis(input.Pressed, engine.KeyS, engine.KeyW)),
	)
	s.camera.Zoom = courtyardZoom[s.zoomLevel]
	x := axis(input.Held, engine.KeyArrowLeft, engine.KeyArrowRight)
	y := axis(input.Held, engine.KeyArrowUp, engine.KeyArrowDown)
	// Pan at the same screen speed at each zoom. Zoom itself leaves the
	// projected center alone, so the world expands around the view center.
	distance := 2 / s.camera.Zoom
	if x != 0 && y != 0 {
		distance /= math.Sqrt2
	}
	s.camera.CenterX = min(128, max(-128, s.camera.CenterX+float64(x)*distance))
	s.camera.CenterY = min(64, max(-64, s.camera.CenterY+float64(y)*distance))
}

func (s *courtyard) prepareWorld() {
	// Both units reverse at the end of their route. The lateral bend keeps both
	// visible as their ground depths pass each other and the wall's anchor.
	t := float64(min(s.phase, 2*patrolLegTicks-s.phase)) / patrolLegTicks
	bend := 0.375 * (1 - math.Abs(2*t-1))
	s.items = [3]engine.WorldItem{
		{
			ID: courtyardAmber, GroundX: 2*t + bend, GroundY: 2*t - bend, Elevation: 12,
			Hit: engine.HitSelect, HitBounds: engine.HitRect{Left: -6, Top: -7, Right: 6, Bottom: 5},
		},
		{
			ID:        courtyardTeal,
			GroundX:   0.75 - 1.75*t - bend,
			GroundY:   0.75 - 1.75*t + bend,
			Elevation: 18,
			Hit:       engine.HitSelect,
			HitBounds: engine.HitRect{Left: -8, Top: -9, Right: 8, Bottom: 5},
		},
		{
			ID: courtyardWall, GroundX: 0.5, GroundY: 0.5, Hit: engine.HitBlock,
			HitBounds: engine.HitRect{Left: -4, Top: -27, Right: 4, Bottom: 2},
		},
	}
	engine.SortWorldItems(s.items[:])
}

func (s *courtyard) Draw(canvas engine.Canvas) {
	s.drawCourtyard(canvas, s.items[:])
}

func (s *courtyard) drawCourtyard(canvas engine.Canvas, items []engine.WorldItem) {
	canvas.Clear(color.NRGBA{R: 20, G: 28, B: 41, A: 255})
	s.drawGround(canvas)
	s.drawWorld(canvas, items)
}

// drawWorld paints the order prepared by the simulation. The benchmark uses
// this same path with a larger ordered list rather than appending screen quads.
func (s *courtyard) drawWorld(canvas engine.Canvas, items []engine.WorldItem) {
	var markerX, markerY float32
	zoom := float32(s.camera.Zoom)
	for i := range items {
		item := &items[i]
		x, y := s.camera.ToLogical(item.ProjectedAnchor(courtyardProjection))
		bounds := item.HitBounds
		left := float32(x + bounds.Left*s.camera.Zoom)
		top := float32(y + bounds.Top*s.camera.Zoom)
		w := float32(bounds.Right-bounds.Left) * zoom
		h := float32(bounds.Bottom-bounds.Top) * zoom
		switch item.ID {
		case courtyardAmber:
			canvas.Image(left, top, w, h)
		case courtyardTeal:
			canvas.Rect(left, top, w, h, tokenColor)
		case courtyardWall:
			canvas.Rect(left, top, w, h, courtyardWallColor)
		default:
			canvas.Image(left, top, w, h)
		}
		if item.ID == s.selected {
			markerX, markerY = float32(x), top
		}
	}
	if s.selected != 0 {
		// The marker is feedback rather than a world item; it cannot intercept a click.
		canvas.Rect(markerX-4*zoom, markerY-5*zoom, 8*zoom, 3*zoom, selectionColor)
	}
}

func (s *courtyard) drawGround(canvas engine.Canvas) {
	const edge = 5
	grid := color.NRGBA{R: 63, G: 84, B: 108, A: 255}
	for i := float64(-edge); i <= edge; i++ {
		x1, y1 := s.camera.ToLogical(courtyardProjection.Project(i, -edge, 0))
		x2, y2 := s.camera.ToLogical(courtyardProjection.Project(i, edge, 0))
		canvas.Line(float32(x1), float32(y1), float32(x2), float32(y2), 1, grid)
		x1, y1 = s.camera.ToLogical(courtyardProjection.Project(-edge, i, 0))
		x2, y2 = s.camera.ToLogical(courtyardProjection.Project(edge, i, 0))
		canvas.Line(float32(x1), float32(y1), float32(x2), float32(y2), 1, grid)
	}
	for i := range s.items {
		item := &s.items[i]
		if item.Hit == engine.HitSelect {
			x, y := s.camera.ToLogical(courtyardProjection.Project(item.GroundX, item.GroundY, 0))
			zoom := float32(s.camera.Zoom)
			canvas.Rect(float32(x)-7*zoom, float32(y)-2*zoom, 14*zoom, 4*zoom,
				color.NRGBA{R: 7, G: 13, B: 22, A: 255})
		}
	}
}
