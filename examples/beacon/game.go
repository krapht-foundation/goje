// Command beacon is a game-owned, top-down 2D Goje playground.
package main

import (
	"embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/fs"
	"log"

	"github.com/krapht-foundation/goje/audio"
	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/level"
	"github.com/krapht-foundation/goje/platform"
	"github.com/krapht-foundation/goje/project"
	"github.com/krapht-foundation/goje/sprite"
)

//go:embed assets
var files embed.FS

const (
	width   = 160
	height  = 128
	cell    = 32
	left    = 16
	top     = 16
	columns = 4
	rows    = 2
	// The tomato walks in a clear lane below the board, so it needs no collision rules.
	// Its anchor is the bottom center of the sprite; bounds keep every frame inside the lane.
	laneLeft, laneRight = left + 16, width - left - 16
	laneTop, laneBottom = top + rows*cell + 24, height - 8
	startX, startY      = laneLeft, 112
	toggleCue           = 0
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
	if w > columns || h > rows {
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
	sheet, err := decodeTomato(root)
	if err != nil {
		return platform.Config{}, nil, fmt.Errorf("tomato sprite: %w", err)
	}
	idle, idleOK := sheet.Clip("idle")
	move, moveOK := sheet.Clip("move")
	if !idleOK || !moveOK {
		return platform.Config{}, nil, errors.New("tomato sprite needs idle and move clips")
	}
	// Pack the ground tile and the sheet below it into the single launch image.
	ground := loaded.Ground.Bounds()
	pixels := sheet.Image().Bounds()
	offset := image.Pt(0, ground.Dy())
	atlas := image.NewNRGBA(image.Rect(0, 0, max(ground.Dx(), pixels.Dx()), offset.Y+pixels.Dy()))
	source := image.Rectangle{Max: ground.Size()}
	draw.Draw(atlas, source, loaded.Ground, ground.Min, draw.Src)
	draw.Draw(atlas, image.Rectangle{Min: offset, Max: offset.Add(pixels.Size())}, sheet.Image(),
		pixels.Min, draw.Src)
	idle, move = idle.InAtlas(offset), move.InAtlas(offset)
	cue, err := loadToggleCue(root)
	if err != nil {
		return platform.Config{}, nil, err
	}
	board := loaded.Level
	// Capture startup data, not the filesystem. Every restart gets an unlit
	// beacon and an idle tomato at the lane start.
	factory := func() *scene {
		return &scene{
			board: board, source: source, beacon: beacon,
			idle: idle, move: move, x: startX, y: startY,
		}
	}
	config := platform.Config{
		Title: "Goje Beacon · Click the beacon or press Space · Arrows / WASD: move · R: reset",
		Width: width, Height: height, Image: atlas,
		Sounds: []audio.Clip{cue},
	}
	return config, factory, nil
}

func loadToggleCue(root fs.FS) (_ audio.Clip, err error) {
	file, err := root.Open("toggle.wav")
	if err != nil {
		return audio.Clip{}, fmt.Errorf("open beacon cue: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	return audio.DecodeWAV(file)
}

func decodeTomato(root fs.FS) (_ *sprite.Sheet, err error) {
	pixels, err := root.Open("tomato.png")
	if err != nil {
		return nil, fmt.Errorf("open tomato PNG: %w", err)
	}
	defer func() { err = errors.Join(err, pixels.Close()) }()
	clips, err := root.Open("tomato.json")
	if err != nil {
		return nil, fmt.Errorf("open tomato clips: %w", err)
	}
	defer func() { err = errors.Join(err, clips.Close()) }()
	return sprite.Decode(pixels, clips)
}

type scene struct {
	board      level.Board
	source     image.Rectangle
	beacon     level.Placement
	on         bool
	idle, move sprite.Clip
	player     sprite.Player
	x, y       int
}

func (s *scene) Enter() error {
	s.player.Step(s.idle)
	return nil
}

func (*scene) Exit() {}

func (s *scene) Update(tick engine.Tick) (engine.Transition, error) {
	input := tick.Input
	if input.Pressed.Has(engine.KeyR) {
		return engine.Restart(), nil
	}
	press := input.PrimaryPress
	x, y := float64(left+s.beacon.X*cell), float64(top+s.beacon.Y*cell)
	clicked := press.Valid && press.X >= x && press.X < x+cell && press.Y >= y && press.Y < y+cell
	if clicked || input.Pressed.Has(engine.KeySpace) {
		s.on = !s.on
		tick.PlaySound(toggleCue)
	}
	tomatoX := min(laneRight, max(laneLeft,
		s.x+axis(input.Held, engine.KeyArrowLeft|engine.KeyA, engine.KeyArrowRight|engine.KeyD)))
	tomatoY := min(laneBottom, max(laneTop,
		s.y+axis(input.Held, engine.KeyArrowUp|engine.KeyW, engine.KeyArrowDown|engine.KeyS)))
	// Hop only while actually moving; pushing against the lane edge is idle.
	if tomatoX != s.x || tomatoY != s.y {
		s.x, s.y = tomatoX, tomatoY
		s.player.Step(s.move)
	} else {
		s.player.Step(s.idle)
	}
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
	frame, _ := s.player.Frame()
	tomatoX, tomatoY, tomatoW, tomatoH := frame.Destination(float32(s.x), float32(s.y), 1)
	canvas.Image(frame.Source, tomatoX, tomatoY, tomatoW, tomatoH)
}
