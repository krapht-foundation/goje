package main

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/platform"
)

const (
	width, height = 320, 200
	originX       = width / 2
	originY       = 108
	boardEdge     = 4
	step          = 0.125
	bodyLift      = 16
)

var (
	markerColor = color.NRGBA{R: 248, G: 164, B: 76, A: 255}
	tokenColor  = color.NRGBA{R: 92, G: 220, B: 210, A: 255}
)

type point struct{ x, y float64 }

type scene struct {
	player, token point
	collected     bool
}

func newScene() engine.Scene { return &scene{token: point{1, -1}} }

func (*scene) Enter() error { return nil }

func (*scene) Exit() {}

func (s *scene) Update(tick engine.Tick) (engine.Transition, error) {
	// Restart takes priority over movement. The runtime re-enters the factory
	// without closing the window or reuploading the shared marker texture.
	if tick.Input.Pressed.Has(engine.KeyR) {
		return engine.Restart(), nil
	}
	held := tick.Input.Held
	s.move(axis(held, engine.KeyArrowLeft|engine.KeyA, engine.KeyArrowRight|engine.KeyD),
		axis(held, engine.KeyArrowUp|engine.KeyW, engine.KeyArrowDown|engine.KeyS))
	return engine.Transition{}, nil
}

func axis(held, negative, positive engine.Keys) int {
	value := 0
	if held&negative != 0 {
		value--
	}
	if held&positive != 0 {
		value++
	}
	return value
}

func (s *scene) move(horizontal, vertical int) {
	if horizontal == 0 && vertical == 0 {
		return
	}
	distance := step
	if horizontal != 0 && vertical != 0 {
		distance /= math.Sqrt2
	}
	// Each screen axis combines both ground axes, so input stays screen-relative.
	s.player.x = min(boardEdge, max(-boardEdge, s.player.x+float64(horizontal+vertical)*distance))
	s.player.y = min(boardEdge, max(-boardEdge, s.player.y+float64(vertical-horizontal)*distance))
	if math.Abs(s.player.x-s.token.x) <= step && math.Abs(s.player.y-s.token.y) <= step {
		s.collected = true
	}
}

func playgroundConfig() platform.Config {
	// Build the marker once per host run. Scene restarts only reset world state.
	marker := image.NewNRGBA(image.Rect(0, 0, 12, 12))
	draw.Draw(
		marker,
		image.Rect(0, 2, 12, 12),
		image.NewUniform(markerColor),
		image.Point{},
		draw.Src,
	)
	highlight := image.NewUniform(color.NRGBA{R: 255, G: 214, B: 147, A: 255})
	draw.Draw(marker, image.Rect(2, 0, 10, 2), highlight, image.Point{}, draw.Src)
	return platform.Config{
		Title: "Goje Playground · Arrows / WASD: move · R: reset",
		Width: width, Height: height, Image: marker,
	}
}

func project(x, y, elevation float64) (screenX, screenY float32) {
	projection := engine.Isometric{HalfTileWidth: 16, HalfTileHeight: 8}
	sx, sy := projection.Project(x, y, elevation)
	return originX + float32(sx), originY + float32(sy)
}

func (s *scene) Draw(canvas engine.Canvas) {
	canvas.Clear(color.NRGBA{R: 20, G: 28, B: 41, A: 255})
	grid := color.NRGBA{R: 63, G: 84, B: 108, A: 255}
	for i := float64(-boardEdge); i <= boardEdge; i++ {
		x, y := project(i, -boardEdge, 0)
		x2, y2 := project(i, boardEdge, 0)
		canvas.Line(x, y, x2, y2, 1, grid)
		x, y = project(-boardEdge, i, 0)
		x2, y2 = project(boardEdge, i, 0)
		canvas.Line(x, y, x2, y2, 1, grid)
	}
	if !s.collected {
		x, y := project(s.token.x, s.token.y, 0)
		canvas.Rect(x-4, y-4, 8, 8, tokenColor)
	}
	x, spriteY := project(s.player.x, s.player.y, bodyLift)
	groundY := spriteY + bodyLift
	canvas.Rect(x-8, groundY-2, 16, 4, color.NRGBA{R: 7, G: 13, B: 22, A: 255})
	canvas.Image(x-6, spriteY-7, 12, 12)
}
