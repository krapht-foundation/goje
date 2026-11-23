package main

import (
	_ "embed"
	"fmt"
	"image/color"
	"strings"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/grid"
	courtyardlevel "github.com/krapht-foundation/goje/examples/courtyard"
	"github.com/krapht-foundation/goje/platform"
	"github.com/krapht-foundation/goje/tile"
)

//go:embed levels/courtyard.json
var authoredLevel string

//go:embed art/ground.png
var authoredTile string

const scoutEdgeTicks = 16

var destinationColor = color.NRGBA{R: 116, G: 246, B: 144, A: 255}

// authoredCourtyard keeps the integer route and substep separate from the
// presentation list. Camera and canvas changes cannot affect that movement.
type authoredCourtyard struct {
	layout    courtyardlevel.Layout
	router    *grid.Router
	camera    engine.Camera
	items     []engine.WorldItem
	ground    []point // Cell centers in projected pixels, independent of the camera.
	route     []grid.Cell
	cell      grid.Cell // Last center reached; route[next] is the active edge's endpoint.
	next      int
	substep   int
	zoomLevel int
	selected  bool

	cameraHomeX, cameraHomeY float64
}

// Load once per launch. Restart builds new scene state from the same validated layout.
func authoredFactory() (engine.SceneFactory, error) {
	layout, err := courtyardlevel.Decode(strings.NewReader(authoredLevel))
	if err != nil {
		return nil, err
	}
	return func() engine.Scene { return &authoredCourtyard{layout: layout} }, nil
}

func authoredConfig() (platform.Config, error) {
	ground, err := tile.Decode(strings.NewReader(authoredTile))
	if err != nil {
		return platform.Config{}, fmt.Errorf("load art/ground.png: %w", err)
	}
	config := playgroundConfig(ground)
	config.Title = "Goje Courtyard · Left click: select · Right click: move · Arrows: pan · W/S: zoom · R: reset"
	return config, nil
}

func (s *authoredCourtyard) Enter() error {
	w, h := s.layout.Board.Size()
	router, err := grid.NewRouter(w, h)
	if err != nil {
		return err
	}
	s.router = router
	// Keep space for the edge already under way, plus a full new route.
	s.route = make([]grid.Cell, 0, w*h)
	s.zoomLevel = 1
	s.cameraHomeX, s.cameraHomeY = courtyardProjection.Project(float64(w)*0.5, float64(h)*0.5, 0)
	s.camera = engine.Camera{
		CenterX: s.cameraHomeX, CenterY: s.cameraHomeY, Zoom: 1, Width: width, Height: height,
	}
	scout, lookout := s.layout.Scout, s.layout.Lookout
	s.cell = grid.Cell{X: scout.X, Y: scout.Y}
	s.items = append(s.items,
		engine.WorldItem{
			ID: uint64(scout.ID), GroundX: float64(scout.X) + 0.5, GroundY: float64(scout.Y) + 0.5,
			Elevation: 12, Hit: engine.HitSelect,
			HitBounds: engine.HitRect{Left: -6, Top: -7, Right: 6, Bottom: 5},
		},
		engine.WorldItem{
			ID: uint64(lookout.ID), GroundX: float64(lookout.X) + 0.5, GroundY: float64(lookout.Y) + 0.5,
			Elevation: 18, Hit: engine.HitBlock,
			HitBounds: engine.HitRect{Left: -8, Top: -9, Right: 8, Bottom: 5},
		},
	)
	s.ground = make([]point, 0, w*h)
	// Wall IDs live above the entire file ID range, so depth ties stay stable.
	wallID := uint64(1 << 32)
	for y := range h {
		for x := range w {
			if s.layout.Board.Walkable(x, y) {
				px, py := courtyardProjection.Project(float64(x)+0.5, float64(y)+0.5, 0)
				s.ground = append(s.ground, point{px, py})
			} else {
				s.items = append(s.items, engine.WorldItem{
					ID: wallID, GroundX: float64(x) + 0.5, GroundY: float64(y) + 0.5,
					Hit: engine.HitBlock, HitBounds: engine.HitRect{Left: -5, Top: -20, Right: 5, Bottom: 2},
				})
				wallID++
			}
		}
	}
	engine.SortWorldItems(s.items)
	return nil
}

func (*authoredCourtyard) Exit() {}

func (s *authoredCourtyard) Update(tick engine.Tick) (engine.Transition, error) {
	input := tick.Input
	if input.Pressed.Has(engine.KeyR) {
		return engine.Restart(), nil
	}
	// Both edges use the tick-start camera and paint order. The primary edge
	// makes the selection that the secondary edge then observes.
	if press := input.PrimaryPress; press.Valid {
		x, y := s.camera.ToProjected(press.X, press.Y)
		id, hit := engine.PickWorld(s.items, courtyardProjection, x, y)
		s.selected = hit && id == uint64(s.layout.Scout.ID)
	}
	if press := input.SecondaryPress; press.Valid && s.selected {
		x, y := s.camera.ToProjected(press.X, press.Y)
		if goal, ok := s.router.CellAt(courtyardProjection.Unproject(x, y, 0)); ok {
			s.command(goal)
		}
	}
	moveCourtyardCamera(&s.camera, &s.zoomLevel, input, s.cameraHomeX, s.cameraHomeY)
	s.advance()
	return engine.Transition{}, nil
}

func (s *authoredCourtyard) command(goal grid.Cell) {
	start, prefix := s.cell, 0
	if s.substep != 0 {
		start, prefix = s.route[s.next], 1
	} else if start == goal {
		return
	}
	// The router only writes after it finds a path. Preserve the old order
	// on failure and put the committed edge in the reserved slot on success.
	path, ok := s.router.AppendRoute(s.route[:prefix], start, goal, s.walkable)
	if !ok {
		return
	}
	if prefix != 0 {
		path[0] = start
	}
	s.route, s.next = path, 0
}

func (s *authoredCourtyard) walkable(x, y int) bool {
	lookout := s.layout.Lookout
	return (x != lookout.X || y != lookout.Y) && s.layout.Board.Walkable(x, y)
}

func (s *authoredCourtyard) advance() {
	if s.next == len(s.route) {
		return
	}
	end := s.route[s.next]
	s.substep++
	if s.substep == scoutEdgeTicks {
		s.cell, s.next, s.substep = end, s.next+1, 0
	}
	// Derive position from an integer tick count to land exactly on centers,
	// with no accumulated floating-point error or mid-edge reversal.
	fraction := float64(s.substep) / scoutEdgeTicks
	for i := range s.items {
		item := &s.items[i]
		if item.ID == uint64(s.layout.Scout.ID) {
			item.GroundX = float64(s.cell.X) + 0.5 + fraction*float64(end.X-s.cell.X)
			item.GroundY = float64(s.cell.Y) + 0.5 + fraction*float64(end.Y-s.cell.Y)
			break
		}
	}
	engine.SortWorldItems(s.items)
}

func (s *authoredCourtyard) Draw(canvas engine.Canvas) {
	canvas.Clear(color.NRGBA{R: 20, G: 28, B: 41, A: 255})
	zoom := float32(s.camera.Zoom)
	for _, tile := range s.ground {
		x, y := s.camera.ToLogical(tile.x, tile.y)
		canvas.Image(groundRegion, float32(x)-16*zoom, float32(y)-8*zoom, 32*zoom, 16*zoom)
	}
	s.drawGrid(canvas)
	if s.next < len(s.route) {
		goal := s.route[len(s.route)-1]
		px, py := courtyardProjection.Project(float64(goal.X)+0.5, float64(goal.Y)+0.5, 0)
		x, y := s.camera.ToLogical(px, py)
		canvas.Rect(float32(x)-4*zoom, float32(y)-2*zoom, 8*zoom, 4*zoom, destinationColor)
	}
	var markerX, markerY float32
	for _, item := range s.items {
		x, y := s.camera.ToLogical(item.ProjectedAnchor(courtyardProjection))
		bounds := item.HitBounds
		left, top := float32(x)+float32(bounds.Left)*zoom, float32(y)+float32(bounds.Top)*zoom
		w, h := float32(bounds.Right-bounds.Left)*zoom, float32(bounds.Bottom-bounds.Top)*zoom
		switch item.ID {
		case uint64(s.layout.Scout.ID):
			canvas.Image(markerRegion, left, top, w, h)
			markerX, markerY = float32(x), top
		case uint64(s.layout.Lookout.ID):
			canvas.Rect(left, top, w, h, tokenColor)
		default:
			canvas.Rect(left, top, w, h, courtyardWallColor)
		}
	}
	if s.selected {
		canvas.Rect(markerX-4*zoom, markerY-5*zoom, 8*zoom, 3*zoom, selectionColor)
	}
}

func (s *authoredCourtyard) drawGrid(canvas engine.Canvas) {
	w, h := s.layout.Board.Size()
	line := color.NRGBA{R: 63, G: 84, B: 108, A: 255}
	for x := range w + 1 {
		x1, y1 := s.camera.ToLogical(courtyardProjection.Project(float64(x), 0, 0))
		x2, y2 := s.camera.ToLogical(courtyardProjection.Project(float64(x), float64(h), 0))
		canvas.Line(float32(x1), float32(y1), float32(x2), float32(y2), 1, line)
	}
	for y := range h + 1 {
		x1, y1 := s.camera.ToLogical(courtyardProjection.Project(0, float64(y), 0))
		x2, y2 := s.camera.ToLogical(courtyardProjection.Project(float64(w), float64(y), 0))
		canvas.Line(float32(x1), float32(y1), float32(x2), float32(y2), 1, line)
	}
}
