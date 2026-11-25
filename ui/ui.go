// Package ui provides plain HUD and menu widgets drawn on an engine.Canvas
// with a font.Face. Scenes own the widgets, place them with rectangles in
// logical pixels, and call Update once per tick and Draw once per frame.
// See docs/text-and-ui.md for the behavior contract.
package ui

import (
	"image"
	"image/color"
	"math"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/font"
)

// Style holds the colors and metrics a widget draws with. The zero value
// draws nothing visible; start from DefaultStyle and change fields.
type Style struct {
	Fill   color.NRGBA // Background of panels, buttons, menu items, and meters.
	Hot    color.NRGBA // Background of a hovered button and the selected menu item.
	Down   color.NRGBA // Background of a pressed button.
	Border color.NRGBA
	Text   color.NRGBA
	Bar    color.NRGBA // The filled part of a meter.
	// BorderWidth is drawn inside the bounds; 0 draws no border.
	BorderWidth int
	// TextScale is the integer font scale; values below 1 draw at scale 1.
	TextScale int
	// Padding is the gap between the left edge and left-aligned label text.
	Padding int
}

// DefaultStyle returns light text and borders on translucent dark slate.
func DefaultStyle() Style {
	return Style{
		Fill:        color.NRGBA{R: 24, G: 28, B: 40, A: 220},
		Hot:         color.NRGBA{R: 56, G: 72, B: 108, A: 235},
		Down:        color.NRGBA{R: 92, G: 116, B: 168, A: 255},
		Border:      color.NRGBA{R: 196, G: 206, B: 228, A: 255},
		Text:        color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		Bar:         color.NRGBA{R: 96, G: 200, B: 112, A: 255},
		BorderWidth: 1,
		TextScale:   1,
		Padding:     4,
	}
}

// contains is the hit test shared by every widget: a point is inside when
// Min <= p < Max on both axes, so adjacent widgets never both claim a point.
func contains(r image.Rectangle, x, y float64) bool {
	return x >= float64(r.Min.X) && x < float64(r.Max.X) && y >= float64(r.Min.Y) &&
		y < float64(r.Max.Y)
}

func pressInside(r image.Rectangle, press engine.MousePress) bool {
	return press.Valid && contains(r, press.X, press.Y)
}

func pointerInside(r image.Rectangle, pointer engine.Pointer) bool {
	return pointer.Inside && contains(r, pointer.X, pointer.Y)
}

func drawBox(canvas engine.Canvas, r image.Rectangle, fill color.NRGBA, style *Style) {
	x, y, w, h := float32(r.Min.X), float32(r.Min.Y), float32(r.Dx()), float32(r.Dy())
	canvas.Rect(x, y, w, h, fill)
	if b := float32(style.BorderWidth); b > 0 {
		canvas.Rect(x, y, w, b, style.Border)
		canvas.Rect(x, y+h-b, w, b, style.Border)
		canvas.Rect(x, y+b, b, h-2*b, style.Border)
		canvas.Rect(x+w-b, y+b, b, h-2*b, style.Border)
	}
}

// drawText draws text vertically centered in r, horizontally centered or
// starting Padding from the left edge.
func drawText(
	canvas engine.Canvas,
	face font.Face,
	r image.Rectangle,
	text string,
	centered bool,
	style *Style,
) {
	width, height := face.Measure(text, style.TextScale)
	x := r.Min.X + style.Padding
	if centered {
		x = r.Min.X + (r.Dx()-width)/2
	}
	face.Draw(
		canvas,
		text,
		float32(x),
		float32(r.Min.Y+(r.Dy()-height)/2),
		style.TextScale,
		style.Text,
	)
}

// Panel is a filled, bordered rectangle behind other widgets.
type Panel struct {
	Bounds image.Rectangle
	Style  Style
}

// Contains reports whether a logical point is inside the panel.
func (p *Panel) Contains(x, y float64) bool { return contains(p.Bounds, x, y) }

// Draw paints the panel.
func (p *Panel) Draw(canvas engine.Canvas) { drawBox(canvas, p.Bounds, p.Style.Fill, &p.Style) }

// Label is one or more lines of text, vertically centered and Padding from the
// left edge of its bounds. It draws no background.
type Label struct {
	Bounds image.Rectangle
	Text   string
	Style  Style
}

// Contains reports whether a logical point is inside the label.
func (l *Label) Contains(x, y float64) bool { return contains(l.Bounds, x, y) }

// Draw paints the text.
func (l *Label) Draw(canvas engine.Canvas, face font.Face) {
	drawText(canvas, face, l.Bounds, l.Text, false, &l.Style)
}

// Button is a box with centered text that is clicked by a primary press.
type Button struct {
	Bounds image.Rectangle
	Text   string
	Style  Style
	// Hot is true while the pointer is inside. Down is true while the primary
	// button stays held after a press inside, wherever the pointer moves.
	// Update sets both; they select the fill that Draw uses.
	Hot, Down bool
}

// Contains reports whether a logical point is inside the button.
func (b *Button) Contains(x, y float64) bool { return contains(b.Bounds, x, y) }

// Update reads one tick of input and reports whether a primary press landed
// inside the button this tick.
func (b *Button) Update(in engine.Input) (clicked bool) {
	clicked = pressInside(b.Bounds, in.PrimaryPress)
	b.Hot = pointerInside(b.Bounds, in.Pointer)
	b.Down = (b.Down || clicked) && in.PrimaryHeld
	return clicked
}

// Draw paints the box with the Down, Hot, or Fill color, in that priority,
// and the centered text.
func (b *Button) Draw(canvas engine.Canvas, face font.Face) {
	fill := b.Style.Fill
	switch {
	case b.Down:
		fill = b.Style.Down
	case b.Hot:
		fill = b.Style.Hot
	}
	drawBox(canvas, b.Bounds, fill, &b.Style)
	drawText(canvas, face, b.Bounds, b.Text, true, &b.Style)
}

// Menu is a vertical list of items that divide its bounds into equal rows of
// Bounds.Dy() / len(Items) pixels; leftover pixels at the bottom belong to no
// item. Selected is the highlighted item, which the scene may set.
type Menu struct {
	Bounds   image.Rectangle
	Items    []string
	Selected int
	Style    Style
	pointer  engine.Pointer
}

// Contains reports whether a logical point is inside the menu.
func (m *Menu) Contains(x, y float64) bool { return contains(m.Bounds, x, y) }

// Update reads one tick of input. Arrow Up and Arrow Down presses move the
// selection and wrap around, the pointer selects the item under it when it
// moves, and Enter or a primary press on an item activates it. It returns the
// activated item, or -1, and whether Escape was pressed. A press wins over
// Enter in the same tick. A menu without items activates nothing.
func (m *Menu) Update(in engine.Input) (activated int, cancelled bool) {
	activated, cancelled = -1, in.Pressed.Has(engine.KeyEscape)
	count := len(m.Items)
	if count == 0 {
		return activated, cancelled
	}
	m.Selected = min(max(m.Selected, 0), count-1)
	if in.Pressed.Has(engine.KeyArrowUp) {
		m.Selected = (m.Selected + count - 1) % count
	}
	if in.Pressed.Has(engine.KeyArrowDown) {
		m.Selected = (m.Selected + 1) % count
	}
	// Hover only on movement, so a resting pointer does not undo arrow keys.
	if in.Pointer != m.pointer {
		m.pointer = in.Pointer
		if item := m.item(in.Pointer.X, in.Pointer.Y); in.Pointer.Inside && item >= 0 {
			m.Selected = item
		}
	}
	if in.Pressed.Has(engine.KeyEnter) {
		activated = m.Selected
	}
	if in.PrimaryPress.Valid {
		if item := m.item(in.PrimaryPress.X, in.PrimaryPress.Y); item >= 0 {
			m.Selected, activated = item, item
		}
	}
	return activated, cancelled
}

// item returns the item at a logical point, or -1.
func (m *Menu) item(x, y float64) int {
	if len(m.Items) == 0 || !contains(m.Bounds, x, y) {
		return -1
	}
	if item := int(y-float64(m.Bounds.Min.Y)) / m.rowHeight(); item < len(m.Items) {
		return item
	}
	return -1
}

func (m *Menu) rowHeight() int { return max(m.Bounds.Dy()/len(m.Items), 1) }

// Draw paints the menu box and each item as a row with centered text, the
// selected one with the Hot color.
func (m *Menu) Draw(canvas engine.Canvas, face font.Face) {
	drawBox(canvas, m.Bounds, m.Style.Fill, &m.Style)
	if len(m.Items) == 0 {
		return
	}
	height := m.rowHeight()
	for i, text := range m.Items {
		row := image.Rect(
			m.Bounds.Min.X,
			m.Bounds.Min.Y+i*height,
			m.Bounds.Max.X,
			m.Bounds.Min.Y+(i+1)*height,
		)
		if i == m.Selected {
			// Keep the highlight inside the menu's border.
			r := row.Intersect(m.Bounds.Inset(max(m.Style.BorderWidth, 0)))
			canvas.Rect(float32(r.Min.X), float32(r.Min.Y), float32(r.Dx()), float32(r.Dy()), m.Style.Hot)
		}
		drawText(canvas, face, row, text, true, &m.Style)
	}
}

// Meter is a horizontal bar filled from the left by Fraction, such as health.
type Meter struct {
	Bounds image.Rectangle
	// Fraction is clamped to 0–1; NaN draws an empty bar.
	Fraction float64
	Style    Style
}

// Contains reports whether a logical point is inside the meter.
func (m *Meter) Contains(x, y float64) bool { return contains(m.Bounds, x, y) }

// Draw paints the bordered background and the filled part inside the border.
func (m *Meter) Draw(canvas engine.Canvas) {
	drawBox(canvas, m.Bounds, m.Style.Fill, &m.Style)
	inner := m.Bounds.Inset(max(m.Style.BorderWidth, 0))
	fraction := m.Fraction
	if math.IsNaN(fraction) {
		fraction = 0
	}
	width := float32(inner.Dx()) * float32(min(max(fraction, 0), 1))
	canvas.Rect(float32(inner.Min.X), float32(inner.Min.Y), width, float32(inner.Dy()), m.Style.Bar)
}
