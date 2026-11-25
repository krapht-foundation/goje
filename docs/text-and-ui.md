# Put text and a HUD in your game

Goje draws text as bitmap glyphs: each character is one region of your launch image, multiplied by a color. The `font` package reads font sheets and ships a built-in 5 × 7 font, so every game can show text without an asset. The `ui` package builds a few plain widgets on top of it: panels, labels, buttons, a vertical menu, and meters. Both produce the same pixels on desktop and in the browser. See the [font sheet specification](../specs/font-v1.md) for the full format and drawing rules.

## Pack a face into your atlas

`engine.Canvas` draws images only from the one launch image you pass in `platform.Config.Image`. Copy the font sheet into that image once at startup, and keep the face that `InAtlas` returns for drawing:

```go
face := font.Builtin()
sheet := face.Image()

// Put the font below the art you already have, with a transparent row between them.
offset := image.Pt(0, art.Bounds().Dy()+1)
fontRegion := image.Rectangle{Min: offset, Max: offset.Add(sheet.Bounds().Size())}
atlas := image.NewNRGBA(art.Bounds().Union(fontRegion))
draw.Draw(atlas, art.Bounds(), art, art.Bounds().Min, draw.Src)
draw.Draw(atlas, fontRegion, sheet, sheet.Bounds().Min, draw.Src)
face = face.InAtlas(offset)

config := platform.Config{Title: "My game", Width: 320, Height: 180, Image: atlas}
```

A `Face` is a small read-only value. Pass it to your scenes; copies share the same sheet.

## Draw text

Call `Draw` from your scene's `Draw` method with the upper-left corner of the text, an integer scale, and a color:

```go
func (s *scene) Draw(canvas engine.Canvas) {
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	s.face.Draw(canvas, "Scouts: 6\nGuards: 4", 8, 8, 2, white)

	// Right-align a message by measuring it first.
	width, _ := s.face.Measure("Paused", 2)
	s.face.Draw(canvas, "Paused", float32(320-8-width), 8, 2, color.NRGBA{R: 255, G: 210, B: 64, A: 255})
}
```

- Each glyph is one `ImageTinted` call. The built-in font is white, so the color you pass is the color you see. Lower alpha makes text translucent.
- The scale is a whole number, so pixels stay crisp. The built-in font is 6 × 8 logical pixels per character at scale 1 and 12 × 16 at scale 2.
- `\n` starts a new line. There is no wrapping; break long text yourself.
- Text is UTF-8, one glyph per rune. Characters the font lacks, and invalid bytes, draw `?`.
- `Draw` and `Measure` allocate nothing, so calling them every frame is free of garbage. Formatting numbers with `fmt` does allocate; keep your strings and rebuild them only when the value changes, or use `strconv.AppendInt` into a reused buffer.

## Make your own font

A font sheet is a PNG of equal cells plus a short JSON description. The easiest way to make one is the Font Editor, below. You can also paint the glyphs yourself in white on transparent, for example in the Pixel Art Maker, in code-point order, left to right and then top to bottom:

```json
{ "version": 1, "cellWidth": 8, "cellHeight": 10, "baseline": 8, "first": 32, "count": 95 }
```

Add an `advances` list with one width per glyph for proportional text. Check the pair before you ship it, from the repository root:

```sh
go run ./cmd/fontcheck my-font.png my-font.json
```

Load it with `font.Decode(pngReader, jsonReader)` and pack it into your atlas as above.

### Edit a font in the browser

The Font Editor edits monospaced fonts: every glyph has the same cell. Start it from the repository root and open the address it prints:

```sh
make serve-font-editor
```

- **Built-in font** opens Goje's own 5 × 7 font, so you can restyle it. **Open PNG and JSON** opens an existing font; choose both files together. **New font** starts a blank one from a cell size, a baseline, the first code point, and a glyph count.
- Choose a glyph by typing its character in **Character**, with **Previous** and **Next**, or by clicking it in the sheet below. Paint with **Pencil** and clear with **Eraser**, by dragging or with the arrow keys and **Enter**. The yellow line is the baseline. **Undo** and **Redo** step through whole strokes.
- **Download PNG** and **Download JSON** save the pair. It passes `fontcheck` and loads with `font.Decode` exactly as the editor showed it.

The editor refuses a font with an `advances` list that differs from the cell width rather than drop those widths; edit such a font by hand. It saves the sheet with 16 glyphs a row, or more when a font has too many glyphs for 512 pixels of rows, so an opened font's PNG can change layout while its glyphs stay the same. The [Font Editor specification](../specs/font-editor-m17.md) lists every rule.

## Build a HUD and menus

The `ui` widgets are plain structs. Place each with an `image.Rectangle` in logical pixels, call `Update` once per tick from your scene's `Update`, and call `Draw` once per frame. Colors, the border width, the text scale, and padding come from a `ui.Style`; start from `ui.DefaultStyle()` and change the fields you want.

```go
style := ui.DefaultStyle()
style.TextScale = 2

s.menu = ui.Menu{Bounds: image.Rect(100, 50, 220, 130), Items: []string{"Resume", "Save", "Quit"}, Style: style}
s.health = ui.Meter{Bounds: image.Rect(8, 164, 108, 172), Fraction: 1, Style: style}

// In Update:
if item, cancelled := s.menu.Update(tick.Input); cancelled {
	s.paused = false
} else if item >= 0 {
	s.choose(s.menu.Items[item])
}

// In Draw:
s.health.Draw(canvas)
s.menu.Draw(canvas, s.face)
```

- `Panel` draws a filled, bordered box. `Label` draws text, vertically centered and `Padding` from the left edge of its bounds, with no background. `Meter` draws a bar filled from the left by `Fraction`, clamped to 0–1. These three have no `Update`, and `Panel` and `Meter` draw no text, so their `Draw` takes only the canvas.
- `Button.Update` returns true in the tick when a primary press lands inside the button, or when the south face button of a gamepad goes down while the pointer is inside. `Hot` is true while the pointer is inside, and `Down` is true while the primary button, or the south face button after a gamepad click, stays held after a press inside, even if the pointer leaves. `Draw` fills the button with the `Down`, `Hot`, or `Fill` color, in that priority, and centers its text.
- `Menu` divides its height into one equal row per item; pixels left over at the bottom belong to no item. **Arrow Up** and **Arrow Down** move `Selected` and wrap around, and so do the up and down directions of a gamepad's D-pad and of its left stick leaned at least halfway; a held direction moves one step. Moving the pointer over an item selects it, while a pointer that rests does not override the arrow keys. **Enter**, a gamepad's south face button, or a primary press on an item activates it, and `Update` returns its index, or -1 when nothing was activated. A press wins over **Enter** in the same tick. **Escape** or a gamepad's east face button sets the second result, so the scene can close the menu. The selected row is drawn with the `Hot` color.
- Widgets find gamepad presses by comparing each `Update` with the previous one, because `Input` carries pad state, not edges. Every connected pad counts. The first `Update` of a new widget value only records the pads, so a button still held from the press that opened a menu does not act on it. A scene that stops updating a widget while it is hidden should assign a new value when it shows the widget again; otherwise a button that was down when the widget was hidden needs to be released before it acts.
- Every widget's `Contains(x, y)` uses the same rule: a point is inside when it is at or right of the left edge and left of the right edge, and likewise vertically. Adjacent widgets therefore never both claim a point.
- Before you treat a press as a click on the game world, ask the widgets under it. A press on the HUD should not also order a unit:

```go
if p := tick.Input.PrimaryPress; p.Valid && !s.hud.Contains(p.X, p.Y) && !s.menu.Contains(p.X, p.Y) {
	s.selectAt(p.X, p.Y)
}
```

- Widget updates and drawing allocate nothing. There is no layout engine, scrolling, text entry, theme, or keyboard focus beyond the menu; lay widgets out with rectangles in your scene.

## Cost

`go test -bench Glyphs ./internal/platform/driver` builds the vertices of a screen with 1,000 tinted glyphs in about 16 microseconds natively and about 95 microseconds as WebAssembly under Node, on an Apple M4 Pro, with no allocations once the frame's buffer has its size. `go test -bench OutpostDrawFullHUD ./cmd/playground` draws the outpost's whole HUD, a title, and every widget in about 2 microseconds natively and 16 microseconds under Node, also without allocating. These measure the work the scene and the frame do on the CPU, not the graphics driver or the browser's compositor, and they were not measured in Chromium itself.
