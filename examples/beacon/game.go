// Command beacon is a game-owned, top-down 2D Goje playground: light every
// marked beacon before a fixed-tick timer runs out, then play again.
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
	"github.com/krapht-foundation/goje/font"
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
	// timeLimit is the challenge length in 60 Hz ticks. The clock starts with
	// the player's first action, so a page that loads unattended cannot lose.
	timeLimit = 600
	// The music loops from the first update; a finished round lowers it and a
	// restart raises it again without restarting the track.
	musicTrack                         = 0
	musicGain, finishGain, musicFadeMS = 0.5, 0.2, 500
	// The timer bar and the result banner share the strip above the board.
	barY, barHeight       = 6, 4
	bannerY, bannerHeight = 2, 12
)

var (
	background = color.NRGBA{R: 20, G: 28, B: 41, A: 255}
	floor      = color.NRGBA{R: 52, G: 69, B: 87, A: 255}
	wall       = color.NRGBA{R: 99, G: 112, B: 129, A: 255}
	dormant    = color.NRGBA{R: 91, G: 141, B: 209, A: 255}
	lit        = color.NRGBA{R: 248, G: 190, B: 66, A: 255}
	timeLeft   = color.NRGBA{R: 244, G: 241, B: 235, A: 255}
	timeUp     = color.NRGBA{R: 215, G: 84, B: 73, A: 255}
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
	var beacons []level.Placement
	for placement := range loaded.Level.Placements() {
		if placement.Kind != "beacon" {
			return platform.Config{}, nil, fmt.Errorf("unexpected %q placement", placement.Kind)
		}
		beacons = append(beacons, placement)
	}
	if len(beacons) == 0 {
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
	// Pack the ground tile, the sheet, and the built-in font into the single launch image.
	face := font.Builtin()
	ground := loaded.Ground.Bounds()
	pixels := sheet.Image().Bounds()
	glyphs := face.Image().Bounds()
	offset := image.Pt(0, ground.Dy())
	fontAt := image.Pt(0, offset.Y+pixels.Dy())
	atlas := image.NewNRGBA(image.Rect(0, 0, max(ground.Dx(), pixels.Dx(), glyphs.Dx()),
		fontAt.Y+glyphs.Dy()))
	source := image.Rectangle{Max: ground.Size()}
	draw.Draw(atlas, source, loaded.Ground, ground.Min, draw.Src)
	draw.Draw(atlas, image.Rectangle{Min: offset, Max: offset.Add(pixels.Size())}, sheet.Image(),
		pixels.Min, draw.Src)
	draw.Draw(atlas, image.Rectangle{Min: fontAt, Max: fontAt.Add(glyphs.Size())}, face.Image(),
		glyphs.Min, draw.Src)
	idle, move, face = idle.InAtlas(offset), move.InAtlas(offset), face.InAtlas(fontAt)
	cue, err := loadToggleCue(root)
	if err != nil {
		return platform.Config{}, nil, err
	}
	music, err := loadMusic(root)
	if err != nil {
		return platform.Config{}, nil, err
	}
	board := loaded.Level
	// Capture startup data, not the filesystem. Every restart gets unlit
	// beacons, a full timer, and an idle tomato at the lane start.
	factory := func() *scene {
		return &scene{
			board: board, source: source, face: face, beacons: beacons,
			lit: make([]bool, len(beacons)), unlit: len(beacons),
			idle: idle, move: move, x: startX, y: startY,
		}
	}
	config := platform.Config{
		Title: "Goje Beacon · Light every beacon in 10 s: tap it, or press Space below it · " +
			"Arrows / WASD: move · R: restart",
		Width: width, Height: height, Image: atlas,
		Sounds: []audio.Clip{cue},
		Music:  []audio.Clip{music},
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

func loadMusic(root fs.FS) (_ audio.Clip, err error) {
	file, err := root.Open("music.flac")
	if err != nil {
		return audio.Clip{}, fmt.Errorf("open beacon music: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	clip, err := audio.DecodeFLAC(file)
	if err != nil {
		return audio.Clip{}, fmt.Errorf("decode beacon music: %w", err)
	}
	return clip, nil
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

// phase is the round's progress. Only won and timedOut end it.
type phase uint8

const (
	ready phase = iota
	playing
	won
	timedOut
)

type scene struct {
	board      level.Board
	source     image.Rectangle
	face       font.Face
	beacons    []level.Placement
	lit        []bool
	unlit      int
	phase      phase
	clock      int // ticks played
	music      bool
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
	if !s.music {
		s.music = true
		tick.PlayMusic(musicTrack, musicGain, musicFadeMS)
	}
	input := tick.Input
	press := input.PrimaryPress
	space := input.Pressed.Has(engine.KeySpace)
	again := press.Valid || space || input.Pressed.Has(engine.KeyEnter)
	if input.Pressed.Has(engine.KeyR) || s.phase >= won && again {
		return engine.Restart(), nil
	}
	if s.phase >= won {
		s.player.Step(s.idle)
		return engine.Transition{}, nil
	}
	dx := axis(input.Held, engine.KeyArrowLeft|engine.KeyA, engine.KeyArrowRight|engine.KeyD)
	dy := axis(input.Held, engine.KeyArrowUp|engine.KeyW, engine.KeyArrowDown|engine.KeyS)
	if s.phase == ready && (press.Valid || space || dx != 0 || dy != 0) {
		s.phase = playing
	}
	// A tap or click lights the beacon in its cell; Space lights the
	// beacons in the column above the tomato.
	column, lighted := (s.x-left)/cell, false
	for i, b := range s.beacons {
		x, y := float64(left+b.X*cell), float64(top+b.Y*cell)
		tapped := press.Valid && press.X >= x && press.X < x+cell && press.Y >= y && press.Y < y+cell
		if !s.lit[i] && (tapped || space && b.X == column) {
			s.lit[i], lighted = true, true
			s.unlit--
		}
	}
	if lighted {
		tick.PlaySound(toggleCue)
	}
	tomatoX := min(laneRight, max(laneLeft, s.x+dx))
	tomatoY := min(laneBottom, max(laneTop, s.y+dy))
	// Hop only while actually moving; pushing against the lane edge is idle.
	if tomatoX != s.x || tomatoY != s.y {
		s.x, s.y = tomatoX, tomatoY
		s.player.Step(s.move)
	} else {
		s.player.Step(s.idle)
	}
	if s.phase == playing {
		s.clock++
		switch {
		case s.unlit == 0:
			s.phase = won
		case s.clock >= timeLimit:
			s.phase = timedOut
		}
		if s.phase != playing {
			tick.SetMusicGain(finishGain, musicFadeMS)
		}
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
	for i, b := range s.beacons {
		tint := dormant
		if s.lit[i] {
			tint = lit
		}
		x, y := float32(left+b.X*cell), float32(top+b.Y*cell)
		canvas.Rect(x+10, y+10, 12, 12, tint)
	}
	frame, _ := s.player.Frame()
	tomatoX, tomatoY, tomatoW, tomatoH := frame.Destination(float32(s.x), float32(s.y), 1)
	canvas.Image(frame.Source, tomatoX, tomatoY, tomatoW, tomatoH)

	const stripWidth = width - 2*left
	banner, message := lit, "ALL LIT! TAP OR R"
	switch s.phase {
	case ready, playing:
		canvas.Rect(left, barY, stripWidth, barHeight, floor)
		canvas.Rect(left, barY, float32(stripWidth*(timeLimit-s.clock)/timeLimit), barHeight, timeLeft)
		return
	case timedOut:
		banner, message = timeUp, "TIME UP! TAP OR R"
	case won:
	}
	canvas.Rect(left, bannerY, stripWidth, bannerHeight, banner)
	textWidth, textHeight := s.face.Measure(message, 1)
	s.face.Draw(canvas, message, float32((width-textWidth)/2),
		float32(bannerY+(bannerHeight-textHeight)/2), 1, background)
}
