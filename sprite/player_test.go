package sprite_test

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/sprite"
)

func TestPlaybackAndPivotThroughRestart(t *testing.T) {
	t.Parallel()
	sheet, err := sprite.Decode(bytes.NewReader(tomatoPNG), strings.NewReader(tomatoJSON))
	if err != nil {
		t.Fatal(err)
	}
	idle, idleOK := sheet.Clip("idle")
	move, moveOK := sheet.Clip("move")
	if !idleOK || !moveOK {
		t.Fatal("the fixture must have both clips")
	}
	offset := image.Pt(11, 23)
	idleAtlas, moveAtlas := idle.InAtlas(offset), move.InAtlas(offset)
	if got := move.Frame(1).Source; got != image.Rect(74, 0, 98, 24) {
		t.Fatalf("preparing an atlas clip mutated the sheet: %v", got)
	}
	runtime, err := engine.Start(func() engine.Scene {
		return &spriteScene{idle: idleAtlas, move: moveAtlas}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})

	// At 1.5× zoom, both poses must use the same fractional ground anchor
	// despite the hop being narrower, taller, and higher above its pivot.
	standing := imageDraw{image.Rect(11, 23, 35, 43), 82.5, 70.75, 36, 30}
	squash := imageDraw{image.Rect(59, 25, 85, 43), 81, 73.75, 39, 27}
	hop := imageDraw{image.Rect(85, 23, 109, 47), 82.5, 64.75, 36, 36}
	for tick := range 21 {
		if tick != 0 {
			if err := runtime.Step(engine.Input{}); err != nil {
				t.Fatal(err)
			}
		}
		want := standing
		switch {
		case tick >= 1 && tick <= 4, tick == 17, tick == 19:
			want = squash
		case tick >= 5 && tick <= 12:
			want = hop
		}
		// Multiple draws between ticks must not consume frame duration.
		for range 2 {
			var canvas captureCanvas
			if err := runtime.Draw(&canvas); err != nil {
				t.Fatal(err)
			}
			if canvas.draw != want {
				t.Fatalf("tick %d drew %+v, want %+v", tick, canvas.draw, want)
			}
		}
	}
}

type spriteScene struct {
	idle, move sprite.Clip
	player     sprite.Player
}

func (s *spriteScene) Enter() error {
	s.player.Step(s.idle)
	return nil
}

func (s *spriteScene) Update(tick engine.Tick) (engine.Transition, error) {
	switch tick.Number {
	case 20:
		return engine.Restart(), nil
	case 18:
		s.player.Step(s.idle)
	default:
		s.player.Step(s.move)
	}
	return engine.Transition{}, nil
}

func (s *spriteScene) Draw(canvas engine.Canvas) {
	if frame, ok := s.player.Frame(); ok {
		x, y, w, h := frame.Destination(100.5, 100.75, 1.5)
		canvas.Image(frame.Source, x, y, w, h)
	}
}

func (*spriteScene) Exit() {}

type imageDraw struct {
	source     image.Rectangle
	x, y, w, h float32
}

type captureCanvas struct {
	engine.Canvas
	draw imageDraw
}

func (c *captureCanvas) Image(source image.Rectangle, x, y, w, h float32) {
	c.draw = imageDraw{source, x, y, w, h}
}
