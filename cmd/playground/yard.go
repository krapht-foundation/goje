package main

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
	courtyardlevel "github.com/krapht-foundation/goje/examples/courtyard"
)

// The yard is the reference scene for RFC 0013. It reuses the authored
// courtyard's level, art, and camera, but moves the scout freely with the
// keyboard through the collision package instead of routing it between tiles.
// It is a separate fixture, because the courtyard assigns the arrow keys and
// W/S to camera pan and zoom (see specs/unit-commands-m3.md).
const (
	yardBodySize      fixed.Num = 96  // 3/8 tile, in 1/256 tile.
	yardSpeed         fixed.Num = 20  // Units per tick on one ground axis.
	yardDiagonalScale fixed.Num = 181 // 1/√2 in 1/256, so diagonals are not faster.
	yardBodyTop                 = 20  // Projected pixels; the scout stands from zero up to here.
	yardWallTop                 = 32  // Walls and the lookout are tall enough to block the scout.
	yardDeckBottom              = 24  // The deck clears the scout's head by four pixels.
	yardDeckHeight              = 28  // Projected pixels the deck is drawn above the ground.

	yardDeckID  uint64 = 1 << 32 // Above every cell-derived solid ID.
	yardDoorID  uint64 = 1
	yardDeckLow        = 100 << 32 // Presentation IDs for deck tiles, above wall IDs.
)

var (
	yardDeckColor    = color.NRGBA{R: 186, G: 142, B: 92, A: 255}
	yardDoorDark     = color.NRGBA{R: 74, G: 64, B: 86, A: 255}
	yardDoorLit      = color.NRGBA{R: 255, G: 224, B: 102, A: 255}
	yardFootprintInk = color.NRGBA{R: 255, G: 0, B: 255, A: 255}
)

// yardSim is the simulation: positions are integers in 1/256 tile, so one input
// history gives one result on every target. It knows nothing about drawing.
type yardSim struct {
	world   *collide.World
	tracker *collide.Tracker
	bodies  [1]collide.Shape
	events  []collide.Event
	spawn   fixed.Vec2
	// deck and door are in ground units; the scene derives its art from them.
	deck, door fixed.Rect
	// doorLit is true while the scout overlaps the doorway trigger.
	doorLit bool
	// moved is true if the last step changed the scout's position.
	moved bool
}

func newYardSim(layout courtyardlevel.Layout) (*yardSim, error) {
	w, h := layout.Board.Size()
	if w < 4 || h < 4 {
		return nil, errors.New("the yard needs a board at least four tiles on each side")
	}
	solid := func(id uint64, r fixed.Rect, bottom, top int32) collide.Shape {
		return collide.Shape{ID: id, Footprint: r, Vertical: collide.Interval{Bottom: bottom, Top: top}}
	}
	var solids []collide.Shape
	cell := uint64(0) // Solid IDs follow the cell order, from one.
	for y := range h {
		for x := range w {
			cell++
			if !layout.Board.Walkable(x, y) || (x == layout.Lookout.X && y == layout.Lookout.Y) {
				solids = append(solids, solid(cell, yardTiles(x, y, x+1, y+1), 0, yardWallTop))
			}
		}
	}
	s := &yardSim{
		// The deck bridges the south of the board; the doorway is the north gap.
		deck: yardTiles(w/2-1, h-2, w/2+2, h-1),
		door: yardTiles(w/2, 0, w/2+1, 1),
		spawn: fixed.Vec2{
			X: fixed.FromInt(layout.Scout.X) + (fixed.One-yardBodySize)/2,
			Y: fixed.FromInt(layout.Scout.Y) + (fixed.One-yardBodySize)/2,
		},
	}
	solids = append(solids, solid(yardDeckID, s.deck, yardDeckBottom, yardWallTop))
	var err error
	s.world, err = collide.New(
		w,
		h,
		solids,
		[]collide.Shape{solid(yardDoorID, s.door, 0, yardWallTop)},
	)
	if err != nil {
		return nil, fmt.Errorf("build yard collision world: %w", err)
	}
	s.tracker = collide.NewTracker(s.world)
	s.bodies[0] = collide.Shape{
		ID: uint64(layout.Scout.ID),
		Footprint: fixed.Rect{
			Min: s.spawn,
			Max: fixed.Vec2{X: s.spawn.X + yardBodySize, Y: s.spawn.Y + yardBodySize},
		},
		Vertical: collide.Interval{Bottom: 0, Top: yardBodyTop},
	}
	s.events = make([]collide.Event, 0, 4)
	return s, nil
}

func yardTiles(x0, y0, x1, y1 int) fixed.Rect {
	return fixed.Rect{
		Min: fixed.Vec2{X: fixed.FromInt(x0), Y: fixed.FromInt(y0)},
		Max: fixed.Vec2{X: fixed.FromInt(x1), Y: fixed.FromInt(y1)},
	}
}

// step applies one tick of held keys. Keys are relative to the screen, as in the
// pickup scene: each screen direction combines both ground axes.
func (s *yardSim) step(held engine.Keys) error {
	horizontal := axis(held, engine.KeyArrowLeft|engine.KeyA, engine.KeyArrowRight|engine.KeyD)
	vertical := axis(held, engine.KeyArrowUp|engine.KeyW, engine.KeyArrowDown|engine.KeyS)
	gx, gy := min(1, max(-1, horizontal+vertical)), min(1, max(-1, vertical-horizontal))
	speed := yardSpeed
	if gx != 0 && gy != 0 {
		speed = speed.Mul(yardDiagonalScale)
	}
	result, err := s.world.Move(s.bodies[0], signed(gx, speed), signed(gy, speed))
	if err != nil {
		return fmt.Errorf("move scout: %w", err)
	}
	s.moved = result.Moved != fixed.Vec2{}
	s.bodies[0] = result.Body
	s.events = s.tracker.Update(s.events[:0], s.bodies[:])
	for _, event := range s.events {
		if event.Trigger == yardDoorID {
			s.doorLit = event.Enter
		}
	}
	return nil
}

// signed returns v with the sign of direction, which is −1, 0, or 1.
func signed(direction int, v fixed.Num) fixed.Num {
	switch {
	case direction < 0:
		return v.Neg()
	case direction > 0:
		return v
	}
	return 0
}

// center is the scout's ground position in tiles, for the presentation only.
func (s *yardSim) center() (x, y float64) {
	fp := s.bodies[0].Footprint
	return fp.Min.X.Add(yardBodySize / 2).Float64(), fp.Min.Y.Add(yardBodySize / 2).Float64()
}

// yardCourtyard embeds the authored scene for its level, art, camera, and
// ground drawing, and replaces input, movement, and drawing.
type yardCourtyard struct {
	authoredCourtyard
	sim *yardSim
}

func (p authoredProject) yardFactory() engine.SceneFactory {
	layout, idle, move := p.layout, p.idle, p.move
	return func() engine.Scene {
		return &yardCourtyard{layout: layout, idle: idle, move: move}
	}
}

func (s *yardCourtyard) Enter() error {
	if err := s.authoredCourtyard.Enter(); err != nil {
		return err
	}
	sim, err := newYardSim(s.layout)
	if err != nil {
		return err
	}
	s.sim = sim
	// Deck tiles sort by their far corner, so the deck draws over a scout
	// anywhere beneath it. Presentation order never affects collision.
	for i, tileX := 0, sim.deck.Min.X.Floor(); tileX < sim.deck.Max.X.Floor(); i, tileX = i+1, tileX+1 {
		s.items = append(s.items, engine.WorldItem{
			ID: yardDeckLow + uint64(i), GroundX: float64(tileX + 1), GroundY: sim.deck.Max.Y.Float64(),
		})
	}
	s.placeScout()
	return nil
}

func (s *yardCourtyard) Update(tick engine.Tick) (engine.Transition, error) {
	if tick.Input.Pressed.Has(engine.KeyR) {
		return engine.Restart(), nil
	}
	if err := s.sim.step(tick.Input.Held); err != nil {
		return engine.Transition{}, err
	}
	clip := s.idle
	if s.sim.moved {
		clip = s.move
	}
	s.player.Step(clip)
	s.placeScout()
	return engine.Transition{}, nil
}

func (s *yardCourtyard) placeScout() {
	x, y := s.sim.center()
	for i := range s.items {
		if s.items[i].ID == uint64(s.layout.Scout.ID) {
			s.items[i].GroundX, s.items[i].GroundY = x, y
			break
		}
	}
	engine.SortWorldItems(s.items)
}

func (s *yardCourtyard) Draw(canvas engine.Canvas) {
	zoom := float32(s.camera.Zoom)
	s.drawGround(canvas, zoom)
	for _, item := range s.items {
		switch {
		case item.ID == uint64(s.layout.Scout.ID):
			s.drawScout(canvas, item, zoom)
		case item.ID >= yardDeckLow:
			// The deck is flat, so draw it from its tile rather than its sort anchor,
			// which is the tile's far corner, one tile past its origin.
			tileX := int(item.GroundX) - 1
			x, y := s.camera.ToLogical(courtyardProjection.Project(
				float64(tileX)+0.5, s.sim.deck.Min.Y.Float64()+0.5, yardDeckHeight))
			canvas.Rect(float32(x)-16*zoom, float32(y)-8*zoom, 32*zoom, 16*zoom, yardDeckColor)
		default:
			x, y := s.camera.ToLogical(item.ProjectedAnchor(courtyardProjection))
			b := item.HitBounds
			fill := courtyardWallColor
			if item.ID == uint64(s.layout.Lookout.ID) {
				fill = tokenColor
			}
			canvas.Rect(float32(x)+float32(b.Left)*zoom, float32(y)+float32(b.Top)*zoom,
				float32(b.Right-b.Left)*zoom, float32(b.Bottom-b.Top)*zoom, fill)
		}
	}
	// The lamp above the doorway shows the trigger's state and is never covered.
	doorX, doorY := s.sim.door.Min.X.Float64()+0.5, s.sim.door.Min.Y.Float64()+0.5
	x, y := s.camera.ToLogical(courtyardProjection.Project(doorX, doorY, 32))
	lamp := yardDoorDark
	if s.sim.doorLit {
		lamp = yardDoorLit
	}
	canvas.Rect(float32(x)-4*zoom, float32(y)-4*zoom, 8*zoom, 8*zoom, lamp)
	s.drawFootprint(canvas, zoom)
}

func (s *yardCourtyard) drawScout(canvas engine.Canvas, item engine.WorldItem, zoom float32) {
	gx, gy := s.camera.ToLogical(courtyardProjection.Project(item.GroundX, item.GroundY, 0))
	if frame, ok := s.player.Frame(); ok {
		sx, sy, sw, sh := frame.Destination(float32(gx), float32(gy), 0.75*zoom)
		canvas.Image(frame.Source, sx, sy, sw, sh)
	}
}

// drawFootprint outlines the rectangle that collision actually uses, over every
// other item, so a viewer can see where the scout stops even behind a wall.
func (s *yardCourtyard) drawFootprint(canvas engine.Canvas, zoom float32) {
	fp := s.sim.bodies[0].Footprint
	corners := [4][2]float64{
		{fp.Min.X.Float64(), fp.Min.Y.Float64()},
		{fp.Max.X.Float64(), fp.Min.Y.Float64()},
		{fp.Max.X.Float64(), fp.Max.Y.Float64()},
		{fp.Min.X.Float64(), fp.Max.Y.Float64()},
	}
	for i, corner := range corners {
		next := corners[(i+1)%4]
		x1, y1 := s.camera.ToLogical(courtyardProjection.Project(corner[0], corner[1], 0))
		x2, y2 := s.camera.ToLogical(courtyardProjection.Project(next[0], next[1], 0))
		canvas.Line(float32(x1), float32(y1), float32(x2), float32(y2), 1, yardFootprintInk)
	}
	cx, cy := s.sim.center()
	x, y := s.camera.ToLogical(courtyardProjection.Project(cx, cy, 0))
	canvas.Rect(float32(x)-2*zoom, float32(y)-2*zoom, 4*zoom, 4*zoom, yardFootprintInk)
}
