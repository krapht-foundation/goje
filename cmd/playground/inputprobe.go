//go:build !goje_smoke && !goje_bench

package main

import (
	"image/color"

	"github.com/krapht-foundation/goje/engine"
)

// The input probe draws the live engine.Input with rectangles only, so that
// scripts/smoke-input.mjs can check touch and gamepad input in canvas pixels.
// Its layout and colors are part of that smoke's contract.
var (
	probeBackground = color.NRGBA{R: 20, G: 28, B: 41, A: 255}
	probePointer    = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	// probeTouches colors the touches by their order in Input.Touches.
	probeTouches = [engine.MaxTouches]color.NRGBA{
		{R: 255, G: 64, B: 64, A: 255},
		{R: 64, G: 255, B: 64, A: 255},
		{R: 64, G: 128, B: 255, A: 255},
		{R: 255, G: 255, B: 64, A: 255},
		{R: 255, G: 64, B: 255, A: 255},
	}
	probePad      = color.NRGBA{R: 60, G: 60, B: 90, A: 255}
	probeStick    = color.NRGBA{R: 255, G: 160, B: 0, A: 255}
	probeButtonUp = color.NRGBA{R: 40, G: 40, B: 60, A: 255}
	probeButtonDn = color.NRGBA{R: 0, G: 255, B: 255, A: 255}
)

const (
	// Each pad slot has a panel at x = 4 + 80 × slot: a 40 × 40 stick box at
	// y 112 whose center moves the 6 × 6 stick dot up to 17 pixels, and 16
	// button squares of 6 × 6 in two rows of eight at y 160 and 168, 8 pixels apart.
	probePadX, probePadStride  = 4, 80
	probeStickY, probeStickBox = 112, 40
	probeButtonsY              = 160
)

type inputProbe struct{ in engine.Input }

func newInputProbe() engine.Scene { return &inputProbe{} }

func (*inputProbe) Enter() error { return nil }

func (*inputProbe) Exit() {}

func (p *inputProbe) Update(tick engine.Tick) (engine.Transition, error) {
	p.in = tick.Input
	return engine.Transition{}, nil
}

func (p *inputProbe) Draw(canvas engine.Canvas) {
	canvas.Clear(probeBackground)
	if pointer := p.in.Pointer; pointer.Inside {
		// A 12 × 12 square around the pointer, which a touch marker covers
		// only in the middle.
		canvas.Rect(float32(pointer.X)-6, float32(pointer.Y)-6, 12, 12, probePointer)
	}
	for i, touch := range p.in.Touches {
		if touch.Active {
			canvas.Rect(float32(touch.X)-3, float32(touch.Y)-3, 6, 6, probeTouches[i])
		}
	}
	for slot, pad := range p.in.Gamepads {
		if !pad.Connected {
			continue
		}
		x := float32(probePadX + probePadStride*slot)
		canvas.Rect(x, probeStickY, probeStickBox, probeStickBox, probePad)
		center := float32(probeStickBox / 2)
		canvas.Rect(
			x+center-3+float32(pad.LeftX)*17/127,
			probeStickY+center-3+float32(pad.LeftY)*17/127,
			6,
			6,
			probeStick,
		)
		for bit := range 16 {
			fill := probeButtonUp
			if pad.Buttons.Has(1 << bit) {
				fill = probeButtonDn
			}
			canvas.Rect(x+float32(bit%8*8), float32(probeButtonsY+bit/8*8), 6, 6, fill)
		}
	}
}
