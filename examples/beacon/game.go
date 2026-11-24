// Command beacon is a game-owned, top-down 2D Goje playground.
package main

import (
	"embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"log"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/level"
	"github.com/krapht-foundation/goje/platform"
	"github.com/krapht-foundation/goje/project"
)

//go:embed assets
var files embed.FS

const (
	width  = 160
	height = 96
	cell   = 32
	left   = 16
	top    = 16
)

var (
	background = color.NRGBA{R: 20, G: 28, B: 41, A: 255}
	floor      = color.NRGBA{R: 52, G: 69, B: 87, A: 255}
	wall       = color.NRGBA{R: 99, G: 112, B: 129, A: 255}
	dormant    = color.NRGBA{R: 91, G: 141, B: 209, A: 255}
	lit        = color.NRGBA{R: 248, G: 190, B: 66, A: 255}
)

func main() {
	root, err := fs.Sub(files, "assets")
	if err != nil {
		log.Fatal(err)
	}
	config, factory, err := launch(root)
	if err != nil {
		log.Fatal(err)
	}
	if err := run(config, factory); err != nil {
		log.Fatal(err)
	}
}

func launch(root fs.FS) (platform.Config, func() *scene, error) {
	loaded, err := project.Load(root)
	if err != nil {
		return platform.Config{}, nil, err
	}
	w, h := loaded.Level.Size()
	if w > (width-2*left)/cell || h > (height-2*top)/cell {
		return platform.Config{}, nil, fmt.Errorf(
			"board %d × %d does not fit the beacon viewport",
			w,
			h,
		)
	}
	var beacon level.Placement
	for placement := range loaded.Level.Placements() {
		if placement.Kind != "beacon" || beacon.ID != 0 {
			return platform.Config{}, nil, errors.New("expected exactly one beacon placement")
		}
		beacon = placement
	}
	if beacon.ID == 0 {
		return platform.Config{}, nil, errors.New("the beacon game requires a beacon placement")
	}
	board := loaded.Level
	source := image.Rectangle{Max: loaded.Ground.Bounds().Size()}
	// Capture startup data, not the filesystem. Every restart gets an unlit beacon.
	factory := func() *scene { return &scene{board: board, source: source, beacon: beacon} }
	config := platform.Config{
		Title: "Goje Beacon · Click the beacon or press Space · R to reset",
		Width: width, Height: height, Image: loaded.Ground,
	}
	return config, factory, nil
}

type scene struct {
	board  level.Board
	source image.Rectangle
	beacon level.Placement
	on     bool
}

func (*scene) Enter() error { return nil }
func (*scene) Exit()        {}

func (s *scene) Update(tick engine.Tick) (engine.Transition, error) {
	if tick.Input.Pressed.Has(engine.KeyR) {
		return engine.Restart(), nil
	}
	press := tick.Input.PrimaryPress
	x, y := float64(left+s.beacon.X*cell), float64(top+s.beacon.Y*cell)
	clicked := press.Valid && press.X >= x && press.X < x+cell && press.Y >= y && press.Y < y+cell
	if clicked || tick.Input.Pressed.Has(engine.KeySpace) {
		s.on = !s.on
	}
	return engine.Transition{}, nil
}

func (s *scene) Draw(canvas engine.Canvas) {
	canvas.Clear(background)
	w, h := s.board.Size()
	for row := range h {
		for column := range w {
			x, y := float32(left+column*cell), float32(top+row*cell)
			if s.board.Walkable(column, row) {
				canvas.Rect(x, y, cell-1, cell-1, floor)
				canvas.Image(s.source, x, y, cell, cell)
			} else {
				canvas.Rect(x, y, cell-1, cell-1, wall)
			}
		}
	}
	tint := dormant
	if s.on {
		tint = lit
	}
	x, y := float32(left+s.beacon.X*cell), float32(top+s.beacon.Y*cell)
	canvas.Rect(x+10, y+10, 12, 12, tint)
}
