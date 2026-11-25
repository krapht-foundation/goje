package main

import (
	"errors"
	"fmt"
	"image/color"
	"io/fs"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/examples/skirmish"
	"github.com/krapht-foundation/goje/level"
)

// The skirmish is the reference scene for RFC 0014: scouts that the player
// commands, guards that the simulation drives, units that block each other,
// bolts that stop at walls, and a level whose deck, alarm line, and units come
// from levels/skirmish.json. The simulation lives in examples/skirmish and
// draws nothing; this file selects, commands, and draws it. Controls are the
// courtyard's (specs/unit-commands-m3.md): left click selects a scout, right
// click orders it, the arrow keys pan, W and S zoom, and R restarts.
const (
	skirmishDeckHeight = 28 // Projected pixels the deck is drawn above the ground.
	skirmishDeckLow    = 100 << 32
	skirmishWallLow    = 1 << 32
	skirmishUnitHeight = 14 // Projected pixels from a unit's feet to its head.
)

var (
	skirmishScoutColor  = color.NRGBA{R: 80, G: 160, B: 255, A: 255}
	skirmishGuardColor  = color.NRGBA{R: 232, G: 84, B: 84, A: 255}
	skirmishBoltColor   = color.NRGBA{R: 255, G: 255, B: 190, A: 255}
	skirmishHealthColor = color.NRGBA{R: 96, G: 220, B: 96, A: 255}
	skirmishHealthBack  = color.NRGBA{R: 70, G: 24, B: 24, A: 255}
	skirmishAlarmQuiet  = color.NRGBA{R: 96, G: 74, B: 120, A: 255}
	skirmishAlarmLoud   = color.NRGBA{R: 255, G: 96, B: 200, A: 255}
	skirmishDeckColor   = yardDeckColor
)

// loadSkirmish reads and decodes the skirmish level.
func loadSkirmish(root fs.FS) (level.Board, error) {
	file, err := root.Open("levels/skirmish.json")
	if err != nil {
		return level.Board{}, fmt.Errorf("open skirmish level: %w", err)
	}
	board, decodeErr := level.Decode(file)
	return board, errors.Join(decodeErr, file.Close())
}

// skirmishFactory runs the skirmish on a decoded level with the project's art.
func (p authoredProject) skirmishFactory(board level.Board) engine.SceneFactory {
	return func() engine.Scene { return &skirmishScene{board: board} }
}

type skirmishScene struct {
	board        level.Board
	sim          *skirmish.Sim
	camera       engine.Camera
	zoomLevel    int
	homeX, homeY float64
	walls        []engine.WorldItem // Static, in sorted order.
	items        []engine.WorldItem // Walls, living units, and deck tiles, sorted every tick.
	ground       []point
	deck, alarm  []grid.Cell
	selected     uint64 // The selected scout's ID, or zero.
	// marked reports further units that draw the selection marker, for scenes that
	// select several. It is nil in the skirmish.
	marked func(id uint64) bool
}

func (s *skirmishScene) Enter() error {
	sim, err := skirmish.New(s.board)
	if err != nil {
		return err
	}
	s.sim = sim
	w, h := s.board.Size()
	s.zoomLevel = 1
	s.homeX, s.homeY = courtyardProjection.Project(float64(w)*0.5, float64(h)*0.5, 0)
	s.camera = engine.Camera{
		CenterX: s.homeX,
		CenterY: s.homeY,
		Zoom:    1,
		Width:   width,
		Height:  height,
	}
	wallID := uint64(skirmishWallLow)
	for y := range h {
		for x := range w {
			if s.board.Walkable(x, y) {
				px, py := courtyardProjection.Project(float64(x)+0.5, float64(y)+0.5, 0)
				s.ground = append(s.ground, point{px, py})
				continue
			}
			s.walls = append(s.walls, engine.WorldItem{
				ID: wallID, GroundX: float64(x) + 0.5, GroundY: float64(y) + 0.5,
				Hit: engine.HitBlock, HitBounds: engine.HitRect{Left: -5, Top: -20, Right: 5, Bottom: 2},
			})
			wallID++
		}
	}
	for z := range s.board.Zones() {
		for y := z.Y; y < z.Y+z.H; y++ {
			for x := z.X; x < z.X+z.W; x++ {
				switch {
				case z.Kind == "deck" && z.Role == level.RoleSolid:
					s.deck = append(s.deck, grid.Cell{X: x, Y: y})
				case z.Kind == "alarm" && z.Role == level.RoleTrigger:
					s.alarm = append(s.alarm, grid.Cell{X: x, Y: y})
				}
			}
		}
	}
	s.items = make([]engine.WorldItem, 0, len(s.walls)+skirmish.MaxUnits+len(s.deck))
	s.refresh()
	return nil
}

func (*skirmishScene) Exit() {}

func (s *skirmishScene) Update(tick engine.Tick) (engine.Transition, error) {
	input := tick.Input
	if input.Pressed.Has(engine.KeyR) {
		return engine.Restart(), nil
	}
	// Both press edges use the camera and paint order from the start of the
	// tick. The primary edge selects, and the secondary edge then orders.
	if press := input.PrimaryPress; press.Valid {
		x, y := s.camera.ToProjected(press.X, press.Y)
		s.selected = 0
		if id, hit := engine.PickWorld(s.items, courtyardProjection, x, y); hit {
			if u, ok := s.sim.Unit(id); ok && u.Alive && u.Team == skirmish.Scouts {
				s.selected = id
			}
		}
	}
	if press := input.SecondaryPress; press.Valid && s.selected != 0 {
		x, y := s.camera.ToProjected(press.X, press.Y)
		if goal, ok := s.sim.CellAt(courtyardProjection.Unproject(x, y, 0)); ok {
			s.sim.Command(s.selected, goal)
		}
	}
	moveCourtyardCamera(&s.camera, &s.zoomLevel, input, s.homeX, s.homeY)
	if err := s.sim.Step(); err != nil {
		return engine.Transition{}, err
	}
	s.refresh()
	return engine.Transition{}, nil
}

// refresh rebuilds the presentation list from the simulation. A unit that died
// leaves the list, and the selection with it.
func (s *skirmishScene) refresh() {
	s.items = append(s.items[:0], s.walls...)
	for i := range s.sim.Units() {
		u := &s.sim.Units()[i]
		if !u.Alive {
			if u.ID == s.selected {
				s.selected = 0
			}
			continue
		}
		c := u.Center()
		hit := engine.HitBlock
		if u.Team == skirmish.Scouts {
			hit = engine.HitSelect
		}
		s.items = append(s.items, engine.WorldItem{
			ID: u.ID, GroundX: c.X.Float64(), GroundY: c.Y.Float64(), Hit: hit,
			HitBounds: engine.HitRect{Left: -5, Top: -skirmishUnitHeight, Right: 5, Bottom: 0},
		})
	}
	// Deck tiles sort by their far corner, so the deck draws over a unit beneath it.
	for i, cell := range s.deck {
		s.items = append(s.items, engine.WorldItem{
			ID: skirmishDeckLow + uint64(i), GroundX: float64(cell.X + 1), GroundY: float64(cell.Y + 1),
		})
	}
	engine.SortWorldItems(s.items)
}

func (s *skirmishScene) Draw(canvas engine.Canvas) { s.drawWorld(canvas) }

// drawWorld paints the board, the units, and the bolts, without any interface.
func (s *skirmishScene) drawWorld(canvas engine.Canvas) {
	zoom := float32(s.camera.Zoom)
	canvas.Clear(color.NRGBA{R: 20, G: 28, B: 41, A: 255})
	for _, tile := range s.ground {
		x, y := s.camera.ToLogical(tile.x, tile.y)
		canvas.Image(groundRegion, float32(x)-16*zoom, float32(y)-8*zoom, 32*zoom, 16*zoom)
	}
	s.drawGrid(canvas)
	alarm := skirmishAlarmQuiet
	if s.sim.Alarm() {
		alarm = skirmishAlarmLoud
	}
	for _, cell := range s.alarm {
		x, y := s.camera.ToLogical(
			courtyardProjection.Project(float64(cell.X)+0.5, float64(cell.Y)+0.5, 0),
		)
		canvas.Rect(float32(x)-4*zoom, float32(y)-2*zoom, 8*zoom, 4*zoom, alarm)
	}
	if u, ok := s.sim.Unit(s.selected); ok {
		if goal, moving := s.sim.Goal(u); moving {
			x, y := s.camera.ToLogical(
				courtyardProjection.Project(float64(goal.X)+0.5, float64(goal.Y)+0.5, 0),
			)
			canvas.Rect(float32(x)-4*zoom, float32(y)-2*zoom, 8*zoom, 4*zoom, destinationColor)
		}
	}
	for _, item := range s.items {
		switch {
		case item.ID >= skirmishDeckLow:
			// The deck is flat, so draw it from its tile, one tile before its sort anchor.
			x, y := s.camera.ToLogical(courtyardProjection.Project(
				item.GroundX-0.5, item.GroundY-0.5, skirmishDeckHeight))
			canvas.Rect(float32(x)-16*zoom, float32(y)-8*zoom, 32*zoom, 16*zoom, skirmishDeckColor)
		case item.ID >= skirmishWallLow:
			x, y := s.camera.ToLogical(item.ProjectedAnchor(courtyardProjection))
			b := item.HitBounds
			canvas.Rect(float32(x)+float32(b.Left)*zoom, float32(y)+float32(b.Top)*zoom,
				float32(b.Right-b.Left)*zoom, float32(b.Bottom-b.Top)*zoom, courtyardWallColor)
		default:
			s.drawUnit(canvas, item, zoom)
		}
	}
	s.drawBolts(canvas, zoom)
}

func (s *skirmishScene) drawUnit(canvas engine.Canvas, item engine.WorldItem, zoom float32) {
	u, ok := s.sim.Unit(item.ID)
	if !ok {
		return
	}
	x, y := s.camera.ToLogical(courtyardProjection.Project(item.GroundX, item.GroundY, 0))
	fx, fy := float32(x), float32(y)
	fill := skirmishGuardColor
	if u.Team == skirmish.Scouts {
		fill = skirmishScoutColor
	}
	canvas.Rect(fx-5*zoom, fy-skirmishUnitHeight*zoom, 10*zoom, skirmishUnitHeight*zoom, fill)
	// The bar is 12 pixels wide at full health and always shows a red backing.
	barTop := fy - (skirmishUnitHeight+4)*zoom
	canvas.Rect(fx-6*zoom, barTop, 12*zoom, 2*zoom, skirmishHealthBack)
	canvas.Rect(
		fx-6*zoom,
		barTop,
		12*zoom*float32(max(u.HP, 0))/float32(u.MaxHP),
		2*zoom,
		skirmishHealthColor,
	)
	if u.ID == s.selected || (s.marked != nil && s.marked(u.ID)) {
		canvas.Rect(fx-4*zoom, barTop-4*zoom, 8*zoom, 3*zoom, selectionColor)
	}
}

func (s *skirmishScene) drawBolts(canvas engine.Canvas, zoom float32) {
	for _, b := range s.sim.Bolts() {
		fp := b.Body.Footprint
		cx := fp.Min.X.Add(fp.Max.X.Sub(fp.Min.X) / 2).Float64()
		cy := fp.Min.Y.Add(fp.Max.Y.Sub(fp.Min.Y) / 2).Float64()
		// Bolts fly at chest height, so lift them to match.
		x, y := s.camera.ToLogical(courtyardProjection.Project(cx, cy, float64(b.Body.Vertical.Bottom)))
		canvas.Rect(float32(x)-2*zoom, float32(y)-2*zoom, 4*zoom, 4*zoom, skirmishBoltColor)
	}
}

func (s *skirmishScene) drawGrid(canvas engine.Canvas) {
	w, h := s.board.Size()
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
