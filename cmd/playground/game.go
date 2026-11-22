package main

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/krapht-foundation/goje"
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
	markerColor = color.RGBA{R: 248, G: 164, B: 76, A: 255}
	tokenColor  = color.RGBA{R: 92, G: 220, B: 210, A: 255}
)

type point struct{ x, y float64 }

type scene struct {
	player, token point
	collected     bool
}

func (s *scene) move(horizontal, vertical int) {
	if horizontal == 0 && vertical == 0 {
		return
	}
	distance := step
	if horizontal != 0 && vertical != 0 {
		distance /= math.Sqrt2
	}
	s.player.x = min(boardEdge, max(-boardEdge, s.player.x+float64(horizontal+vertical)*distance))
	s.player.y = min(boardEdge, max(-boardEdge, s.player.y+float64(vertical-horizontal)*distance))
	if math.Abs(s.player.x-s.token.x) <= step && math.Abs(s.player.y-s.token.y) <= step {
		s.collected = true
	}
}

type game struct {
	projection goje.Isometric
	grid       *ebiten.Image
	playground *goje.Playground[scene]
}

func run(smoke bool) error {
	ebiten.SetWindowSize(width*2, height*2)
	ebiten.SetWindowTitle("Goje Playground")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	g := &game{
		projection: goje.Isometric{HalfTileWidth: 16, HalfTileHeight: 8},
		playground: goje.NewPlayground(func() scene {
			return scene{player: point{0, 0}, token: point{1, -1}}
		}),
	}
	if smoke {
		return runSmoke(g)
	}
	if err := ebiten.RunGame(g); err != nil {
		return fmt.Errorf("run playground: %w", err)
	}
	return nil
}

func (g *game) Layout(_, _ int) (logicalWidth, logicalHeight int) { return width, height }

func axis(negative, negativeAlt, positive, positiveAlt ebiten.Key) int {
	value := 0
	if ebiten.IsKeyPressed(negative) || ebiten.IsKeyPressed(negativeAlt) {
		value--
	}
	if ebiten.IsKeyPressed(positive) || ebiten.IsKeyPressed(positiveAlt) {
		value++
	}
	return value
}

func (g *game) Update() error {
	g.update(axis(ebiten.KeyArrowLeft, ebiten.KeyA, ebiten.KeyArrowRight, ebiten.KeyD),
		axis(ebiten.KeyArrowUp, ebiten.KeyW, ebiten.KeyArrowDown, ebiten.KeyS),
		inpututil.IsKeyJustPressed(ebiten.KeyR))
	return nil
}

func (g *game) update(horizontal, vertical int, reset bool) {
	if reset {
		g.playground.Reset()
		return
	}
	g.playground.Scene().move(horizontal, vertical)
}

func (g *game) project(x, y, elevation float64) (screenX, screenY float32) {
	sx, sy := g.projection.Project(x, y, elevation)
	return originX + float32(sx), originY + float32(sy)
}

func (g *game) Draw(screen *ebiten.Image) {
	if g.grid == nil {
		g.grid = ebiten.NewImage(width, height)
		g.grid.Fill(color.RGBA{R: 20, G: 28, B: 41, A: 255})
		line := color.RGBA{R: 63, G: 84, B: 108, A: 255}
		for i := float64(-boardEdge); i <= boardEdge; i++ {
			x, y := g.project(i, -boardEdge, 0)
			x2, y2 := g.project(i, boardEdge, 0)
			vector.StrokeLine(g.grid, x, y, x2, y2, 1, line, true)
			x, y = g.project(-boardEdge, i, 0)
			x2, y2 = g.project(boardEdge, i, 0)
			vector.StrokeLine(g.grid, x, y, x2, y2, 1, line, true)
		}
		ebitenutil.DebugPrintAt(g.grid, "Arrows / WASD: move  |  R: reset", 8, 8)
	}
	screen.DrawImage(g.grid, nil)
	s := g.playground.Scene()
	if !s.collected {
		x, y := g.project(s.token.x, s.token.y, 0)
		vector.FillRect(screen, x-4, y-4, 8, 8, tokenColor, false)
	}
	x, spriteY := g.project(s.player.x, s.player.y, bodyLift)
	groundY := spriteY + bodyLift
	vector.FillRect(screen, x-8, groundY-2, 16, 4, color.RGBA{R: 7, G: 13, B: 22, A: 255}, false)
	vector.FillRect(screen, x-6, spriteY-5, 12, 10, markerColor, false)
	vector.FillRect(screen, x-4, spriteY-7, 8, 2, color.RGBA{R: 255, G: 214, B: 147, A: 255}, false)
}
