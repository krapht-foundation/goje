package ui_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/ui"
)

// recorder is an engine.Canvas that keeps Rect and ImageTinted calls.
type recorder struct {
	engine.Canvas
	rects  []rect
	glyphs []rect
}

type rect struct {
	x, y, w, h float32
	tint       color.NRGBA
}

func (r *recorder) Rect(x, y, w, h float32, tint color.NRGBA) {
	r.rects = append(r.rects, rect{x, y, w, h, tint})
}

func (r *recorder) ImageTinted(_ image.Rectangle, x, y, w, h float32, tint color.NRGBA) {
	r.glyphs = append(r.glyphs, rect{x, y, w, h, tint})
}

func (r *recorder) reset() { r.rects, r.glyphs = r.rects[:0], r.glyphs[:0] }

func press(x, y float64) engine.MousePress { return engine.MousePress{X: x, Y: y, Valid: true} }

func pointer(x, y float64) engine.Pointer { return engine.Pointer{X: x, Y: y, Inside: true} }

func TestContainsIsHalfOpen(t *testing.T) {
	t.Parallel()
	bounds := image.Rect(10, 20, 30, 40)
	for _, contains := range []func(x, y float64) bool{
		(&ui.Panel{Bounds: bounds}).Contains, (&ui.Label{Bounds: bounds}).Contains,
		(&ui.Button{Bounds: bounds}).Contains, (&ui.Menu{Bounds: bounds}).Contains,
		(&ui.Meter{Bounds: bounds}).Contains,
	} {
		for _, p := range [][2]float64{{10, 20}, {29.9, 39.9}, {20, 30}} {
			if !contains(p[0], p[1]) {
				t.Errorf("point %v is outside", p)
			}
		}
		for _, p := range [][2]float64{{9.9, 25}, {30, 25}, {15, 19.9}, {15, 40}} {
			if contains(p[0], p[1]) {
				t.Errorf("point %v is inside", p)
			}
		}
	}
}

func TestButton(t *testing.T) {
	t.Parallel()
	style := ui.DefaultStyle()
	b := ui.Button{Bounds: image.Rect(0, 0, 40, 20), Text: "Go", Style: style}
	steps := []struct {
		name            string
		in              engine.Input
		clicked, hot, d bool
	}{
		{"hover", engine.Input{Pointer: pointer(5, 5)}, false, true, false},
		{"pointer outside", engine.Input{Pointer: pointer(50, 5)}, false, false, false},
		{
			"press outside",
			engine.Input{PrimaryPress: press(50, 5), PrimaryHeld: true},
			false,
			false,
			false,
		},
		{
			"held after outside press",
			engine.Input{Pointer: pointer(5, 5), PrimaryHeld: true},
			false,
			true,
			false,
		},
		{"release", engine.Input{Pointer: pointer(5, 5)}, false, true, false},
		{
			"press inside",
			engine.Input{Pointer: pointer(5, 5), PrimaryPress: press(5, 5), PrimaryHeld: true},
			true,
			true,
			true,
		},
		{"drag away", engine.Input{Pointer: pointer(90, 5), PrimaryHeld: true}, false, false, true},
		{
			"release away",
			engine.Input{Pointer: pointer(90, 5), PrimaryRelease: press(90, 5)},
			false,
			false,
			false,
		},
		{
			"tap within a tick",
			engine.Input{PrimaryPress: press(39, 19), PrimaryRelease: press(39, 19)},
			true,
			false,
			false,
		},
	}
	for _, step := range steps {
		if clicked := b.Update(
			step.in,
		); clicked != step.clicked || b.Hot != step.hot ||
			b.Down != step.d {
			t.Fatalf("%s: clicked %t, hot %t, down %t", step.name, clicked, b.Hot, b.Down)
		}
	}

	var canvas recorder
	for _, state := range []struct {
		hot, down bool
		fill      color.NRGBA
	}{{false, false, style.Fill}, {true, false, style.Hot}, {true, true, style.Down}, {false, true, style.Down}} {
		canvas.reset()
		b.Hot, b.Down = state.hot, state.down
		b.Draw(&canvas, font.Builtin())
		if canvas.rects[0] != (rect{0, 0, 40, 20, state.fill}) || len(canvas.rects) != 5 {
			t.Fatalf("hot %t, down %t: rects %v", state.hot, state.down, canvas.rects)
		}
	}
	// "Go" is 12 × 8 at scale 1, centered in 40 × 20.
	if canvas.glyphs[0] != (rect{14, 6, 6, 8, style.Text}) || len(canvas.glyphs) != 2 {
		t.Fatalf("glyphs %v", canvas.glyphs)
	}
}

func TestLabelAndPanel(t *testing.T) {
	t.Parallel()
	style := ui.DefaultStyle()
	style.TextScale = 2
	var canvas recorder
	label := ui.Label{Bounds: image.Rect(10, 20, 110, 40), Text: "Hi", Style: style}
	label.Draw(&canvas, font.Builtin())
	if len(canvas.rects) != 0 || len(canvas.glyphs) != 2 ||
		canvas.glyphs[1] != (rect{10 + 4 + 12, 22, 12, 16, style.Text}) {
		t.Fatalf("label drew rects %v and glyphs %v", canvas.rects, canvas.glyphs)
	}

	canvas.reset()
	style.BorderWidth = 2
	panel := ui.Panel{Bounds: image.Rect(0, 0, 10, 8), Style: style}
	panel.Draw(&canvas)
	want := []rect{
		{0, 0, 10, 8, style.Fill},
		{0, 0, 10, 2, style.Border},
		{0, 6, 10, 2, style.Border},
		{0, 2, 2, 4, style.Border},
		{8, 2, 2, 4, style.Border},
	}
	if len(canvas.rects) != len(want) {
		t.Fatalf("panel drew %v", canvas.rects)
	}
	for i := range want {
		if canvas.rects[i] != want[i] {
			t.Fatalf("panel rect %d = %v, want %v", i, canvas.rects[i], want[i])
		}
	}
}

func TestMenu(t *testing.T) {
	t.Parallel()
	// Three 10-pixel rows and two leftover pixels.
	m := ui.Menu{Bounds: image.Rect(0, 0, 50, 32), Items: []string{"Start", "Load", "Quit"}}
	key := func(k engine.Keys) engine.Input { return engine.Input{Pressed: k} }
	steps := []struct {
		name      string
		in        engine.Input
		activated int
		cancelled bool
		selected  int
	}{
		{"up wraps", key(engine.KeyArrowUp), -1, false, 2},
		{"down wraps", key(engine.KeyArrowDown), -1, false, 0},
		{"down", key(engine.KeyArrowDown), -1, false, 1},
		{"enter", key(engine.KeyEnter), 1, false, 1},
		{"escape", key(engine.KeyEscape), -1, true, 1},
		{"hover", engine.Input{Pointer: pointer(5, 25)}, -1, false, 2},
		{
			"resting pointer keeps keys",
			engine.Input{Pointer: pointer(5, 25), Pressed: engine.KeyArrowUp},
			-1,
			false,
			1,
		},
		{"pointer outside", engine.Input{Pointer: pointer(60, 5)}, -1, false, 1},
		{
			"leftover row",
			engine.Input{Pointer: pointer(5, 31), PrimaryPress: press(5, 31)},
			-1,
			false,
			1,
		},
		{"click", engine.Input{Pointer: pointer(5, 5), PrimaryPress: press(5, 5)}, 0, false, 0},
		{
			"click wins over enter",
			engine.Input{PrimaryPress: press(5, 15), Pressed: engine.KeyEnter},
			1,
			false,
			1,
		},
	}
	for _, step := range steps {
		activated, cancelled := m.Update(step.in)
		if activated != step.activated || cancelled != step.cancelled || m.Selected != step.selected {
			t.Fatalf(
				"%s: activated %d, cancelled %t, selected %d",
				step.name,
				activated,
				cancelled,
				m.Selected,
			)
		}
	}

	m.Selected = 9
	if activated, _ := m.Update(key(engine.KeyEnter)); activated != 2 {
		t.Fatalf("an out-of-range selection activated %d, want the clamped last item", activated)
	}
	var empty ui.Menu
	if activated, cancelled := empty.Update(engine.Input{
		Pressed: engine.KeyEnter | engine.KeyEscape, PrimaryPress: press(0, 0),
	}); activated != -1 || !cancelled {
		t.Fatalf("empty menu activated %d, cancelled %t", activated, cancelled)
	}

	style := ui.DefaultStyle()
	m.Style, m.Selected = style, 1
	var canvas recorder
	m.Draw(&canvas, font.Builtin())
	if highlight := canvas.rects[5]; highlight != (rect{1, 10, 48, 10, style.Hot}) ||
		len(canvas.rects) != 6 {
		t.Fatalf("menu rects %v", canvas.rects)
	}
	// "Load" is 24 × 8, centered in the second row.
	if canvas.glyphs[5] != (rect{13, 11, 6, 8, style.Text}) {
		t.Fatalf("first glyph of Load = %v", canvas.glyphs[5])
	}
}

func TestMeter(t *testing.T) {
	t.Parallel()
	style := ui.DefaultStyle()
	for fraction, width := range map[float64]float32{0.25: 12, -1: 0, 2: 48} {
		var canvas recorder
		meter := ui.Meter{Bounds: image.Rect(10, 10, 60, 18), Fraction: fraction, Style: style}
		meter.Draw(&canvas)
		if bar := canvas.rects[len(canvas.rects)-1]; bar != (rect{11, 11, width, 6, style.Bar}) {
			t.Fatalf("fraction %g draws bar %v", fraction, bar)
		}
	}
}

func TestAllocations(t *testing.T) {
	style := ui.DefaultStyle()
	face := font.Builtin()
	panel := ui.Panel{Bounds: image.Rect(0, 0, 200, 100), Style: style}
	label := ui.Label{Bounds: image.Rect(0, 0, 200, 12), Text: "Scouts: 6", Style: style}
	button := ui.Button{Bounds: image.Rect(0, 20, 60, 36), Text: "Save", Style: style}
	menu := ui.Menu{
		Bounds: image.Rect(0, 40, 80, 70),
		Items:  []string{"Resume", "Load"},
		Style:  style,
	}
	meter := ui.Meter{Bounds: image.Rect(0, 80, 60, 86), Fraction: 0.5, Style: style}
	canvas := recorder{rects: make([]rect, 0, 64), glyphs: make([]rect, 0, 64)}
	in := engine.Input{
		Pointer:      pointer(5, 45),
		PrimaryPress: press(5, 25),
		PrimaryHeld:  true,
		Pressed:      engine.KeyArrowDown,
	}
	if allocs := testing.AllocsPerRun(100, func() {
		canvas.reset()
		button.Update(in)
		menu.Update(in)
		panel.Draw(&canvas)
		label.Draw(&canvas, face)
		button.Draw(&canvas, face)
		menu.Draw(&canvas, face)
		meter.Draw(&canvas)
	}); allocs != 0 {
		t.Fatalf("Update and Draw allocated %.0f times", allocs)
	}
}

type discard struct{ recorder }

func (discard) Rect(float32, float32, float32, float32, color.NRGBA) {}

func (discard) ImageTinted(image.Rectangle, float32, float32, float32, float32, color.NRGBA) {}

func BenchmarkDrawHUD(b *testing.B) {
	style := ui.DefaultStyle()
	face := font.Builtin()
	panel := ui.Panel{Bounds: image.Rect(0, 0, 320, 40), Style: style}
	labels := [4]ui.Label{}
	for i := range labels {
		labels[i] = ui.Label{Bounds: image.Rect(i*80, 0, i*80+80, 20), Text: "Scouts 6/6", Style: style}
	}
	meter := ui.Meter{Bounds: image.Rect(4, 24, 120, 32), Fraction: 0.7, Style: style}
	menu := ui.Menu{
		Bounds: image.Rect(100, 60, 220, 140),
		Items:  []string{"Resume", "Save", "Load", "Restart"},
		Style:  style,
	}
	for b.Loop() {
		panel.Draw(discard{})
		for i := range labels {
			labels[i].Draw(discard{}, face)
		}
		meter.Draw(discard{})
		menu.Draw(discard{}, face)
	}
}
