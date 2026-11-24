// Command spriteeditor hosts the edit model and preview cursor for the browser
// Sprite Clip Editor.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
	"syscall/js"

	"github.com/krapht-foundation/goje/internal/spriteeditor"
	"github.com/krapht-foundation/goje/internal/spriteformat"
	"github.com/krapht-foundation/goje/sprite"
)

const (
	// Bridge integers stay far below int overflow so x+width cannot wrap.
	maxBridgeInt = 1 << 24
	// Keep the host's catch-up bounded even if a caller supplies a delayed tick batch.
	maxPreviewTicks = 3600
)

type previewPose struct {
	frame                   spriteformat.Frame
	index                   int
	anchorX, anchorY, scale float32
}

func main() {
	doc := new(spriteeditor.Document)
	var staged *spriteeditor.Document
	var playback spriteformat.Playback
	uint8Array := js.Global().Get("Uint8Array")
	uint8ClampedArray := js.Global().Get("Uint8ClampedArray")
	pose := js.Global().Get("Object").New() // reused so ticks do not build a JS object per call
	var previousPose previewPose
	api := map[string]any{
		"maxPNGBytes": spriteformat.MaxPNGBytes, "maxJSONBytes": spriteformat.MaxJSONBytes,
		"stageOpen": callback(func(args []js.Value) any {
			staged = nil
			png, err := readBytes(arg(args, 0), uint8Array, spriteformat.MaxPNGBytes, "PNG")
			if err != nil {
				return err.Error()
			}
			var sidecar io.Reader
			if v := arg(args, 1); !v.IsNull() && !v.IsUndefined() {
				data, sidecarErr := readBytes(v, uint8Array, spriteformat.MaxJSONBytes, "JSON")
				if sidecarErr != nil {
					return sidecarErr.Error()
				}
				sidecar = bytes.NewReader(data)
			}
			// A fresh document is never dirty; the page confirms replacement before finishOpen.
			candidate := new(spriteeditor.Document)
			if _, openErr := candidate.Open(bytes.NewReader(png), sidecar, nil); openErr != nil {
				return openErr.Error()
			}
			staged = candidate
			return ""
		}),
		"finishOpen": callback(func(args []js.Value) any {
			if arg(args, 0).Truthy() && staged != nil {
				doc = staged
				playback = spriteformat.Playback{}
			}
			staged = nil
			return nil
		}),
		"snapshot": callback(func(_ []js.Value) any {
			var size image.Point
			if img := doc.Image(); img != nil {
				size = img.Bounds().Size()
			}
			clips := doc.Clips()
			list := make([]any, len(clips))
			for i, clip := range clips {
				frames := make([]any, len(clip.Frames))
				for j, f := range clip.Frames {
					frames[j] = map[string]any{
						"x": f.Source.Min.X, "y": f.Source.Min.Y,
						"width": f.Source.Dx(), "height": f.Source.Dy(),
						"pivotX": f.Pivot.X, "pivotY": f.Pivot.Y, "ticks": f.Ticks,
					}
				}
				list[i] = map[string]any{"name": clip.Name, "frames": frames}
			}
			return map[string]any{
				"width": size.X, "height": size.Y,
				"dirty": doc.Dirty(), "canUndo": doc.CanUndo(), "canRedo": doc.CanRedo(),
				"clips": list,
			}
		}),
		"pixels": callback(func(args []js.Value) any {
			target := arg(args, 0)
			img := doc.Image()
			if img == nil {
				return "open a sprite sheet first"
			}
			size := img.Bounds().Size()
			length := 4 * size.X * size.Y
			if !target.InstanceOf(uint8ClampedArray) || target.Length() != length {
				return fmt.Sprintf("pixels need a Uint8ClampedArray of %d bytes", length)
			}
			js.CopyBytesToJS(target, straightRGBA(img))
			return ""
		}),
		"edit": callback(func(args []js.Value) any {
			if err := edit(doc, args); err != nil {
				return err.Error()
			}
			return ""
		}),
		"undo": callback(func(_ []js.Value) any { return doc.Undo() }),
		"redo": callback(func(_ []js.Value) any { return doc.Redo() }),
		"exportJSON": callback(func(_ []js.Value) any {
			data, err := doc.Export()
			if err != nil {
				return err.Error()
			}
			result := uint8Array.New(len(data))
			js.CopyBytesToJS(result, data)
			return result
		}),
		"markSaved": callback(func(_ []js.Value) any { doc.MarkSaved(); return nil }),
		"preview": callback(func(args []js.Value) any {
			clip, clipOK := integer(arg(args, 0))
			ticks, ticksOK := integer(arg(args, 1))
			anchorX, xOK := finite(arg(args, 2))
			anchorY, yOK := finite(arg(args, 3))
			scale, scaleOK := finite(arg(args, 4))
			if !clipOK || !ticksOK || !xOK || !yOK || !scaleOK ||
				ticks < 0 || ticks > maxPreviewTicks || scale <= 0 {
				return nil
			}
			clips := doc.Clips()
			if clip < 0 || clip >= len(clips) {
				playback = spriteformat.Playback{}
				return nil
			}
			frames := clips[clip].Frames
			// A new selection or committed frame edit restarts on frame zero before ticking.
			if !playback.Playing(frames) {
				playback.Step(frames)
			}
			for range ticks {
				playback.Step(frames)
			}
			frame, index, _ := playback.Frame()
			next := previewPose{
				frame:   frame,
				index:   index,
				anchorX: anchorX,
				anchorY: anchorY,
				scale:   scale,
			}
			if next == previousPose {
				return pose
			}
			previousPose = next
			dx, dy, dw, dh := sprite.Frame(frame).Destination(anchorX, anchorY, scale)
			pose.Set("index", index)
			pose.Set("x", frame.Source.Min.X)
			pose.Set("y", frame.Source.Min.Y)
			pose.Set("width", frame.Source.Dx())
			pose.Set("height", frame.Source.Dy())
			pose.Set("dx", dx)
			pose.Set("dy", dy)
			pose.Set("dw", dw)
			pose.Set("dh", dh)
			return pose
		}),
	}
	js.Global().Set("gojeSpriteEditor", api)
	// The page owns these callbacks for the lifetime of the Wasm instance.
	select {}
}

func callback(fn func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
}

// arg tolerates short calls so a page bug returns an error instead of a Go panic.
func arg(args []js.Value, i int) js.Value {
	if i < len(args) {
		return args[i]
	}
	return js.Undefined()
}

// readBytes checks the encoded size before copying any bytes into Wasm.
func readBytes(v, uint8Array js.Value, limit int, kind string) ([]byte, error) {
	if !v.InstanceOf(uint8Array) {
		return nil, fmt.Errorf("%s input must be bytes", kind)
	}
	if v.Get("byteLength").Float() > float64(limit) {
		return nil, fmt.Errorf("%s exceeds %d bytes", kind, limit)
	}
	data := make([]byte, v.Length())
	js.CopyBytesToGo(data, v)
	return data, nil
}

func edit(doc *spriteeditor.Document, args []js.Value) error {
	action := arg(args, 0)
	if action.Type() != js.TypeString {
		return errors.New("edit action must be a string")
	}
	// Non-integer indexes become -1 so the edit model reports them as out of range.
	clip, ok := integer(arg(args, 1))
	if !ok {
		clip = -1
	}
	index, ok := integer(arg(args, 2))
	if !ok {
		index = -1
	}
	value := arg(args, 3)
	switch name := action.String(); name {
	case "addClip":
		clipName, err := text(field(value, "name"))
		if err != nil {
			return err
		}
		f, err := frame(field(value, "frame"))
		if err != nil {
			return err
		}
		return doc.AddClip(clipName, f)
	case "renameClip":
		clipName, err := text(value)
		if err != nil {
			return err
		}
		return doc.RenameClip(clip, clipName)
	case "deleteClip":
		return doc.DeleteClip(clip)
	case "addFrame":
		f, err := frame(value)
		if err != nil {
			return err
		}
		return doc.AddFrame(clip, f)
	case "setFrame":
		f, err := frame(value)
		if err != nil {
			return err
		}
		return doc.SetFrame(clip, index, f)
	case "duplicateFrame":
		return doc.DuplicateFrame(clip, index)
	case "deleteFrame":
		return doc.DeleteFrame(clip, index)
	case "moveFrame":
		to, ok := integer(value)
		if !ok {
			return errors.New("frame destination must be an integer")
		}
		return doc.MoveFrame(clip, index, to)
	default:
		return fmt.Errorf("unknown edit %q", name)
	}
}

// frame builds the rectangle without image.Rect, whose canonicalization would
// turn a negative width into a valid region instead of letting validation reject it.
func frame(v js.Value) (spriteformat.Frame, error) {
	keys := [...]string{"x", "y", "width", "height", "pivotX", "pivotY", "ticks"}
	var n [len(keys)]int
	for i, key := range keys {
		var ok bool
		if n[i], ok = integer(field(v, key)); !ok {
			return spriteformat.Frame{}, fmt.Errorf("frame %s must be an integer", key)
		}
	}
	return spriteformat.Frame{
		Source: image.Rectangle{Min: image.Pt(n[0], n[1]), Max: image.Pt(n[0]+n[2], n[1]+n[3])},
		Pivot:  image.Pt(n[4], n[5]),
		Ticks:  n[6],
	}, nil
}

func field(v js.Value, key string) js.Value {
	if v.Type() != js.TypeObject {
		return js.Undefined()
	}
	return v.Get(key)
}

// text checks the JS string length before copying it to Go. The document
// applies the exact clip name rules after conversion.
func text(v js.Value) (string, error) {
	if v.Type() != js.TypeString || js.Global().Get("Object").Invoke(v).Length() > 64 {
		return "", errors.New("invalid clip name")
	}
	return v.String(), nil
}

// integer accepts only an exact JS integer within ±maxBridgeInt.
func integer(v js.Value) (int, bool) {
	if v.Type() != js.TypeNumber {
		return 0, false
	}
	f := v.Float()
	if f != math.Trunc(f) || math.Abs(f) > maxBridgeInt {
		return 0, false // also rejects NaN and infinities
	}
	return int(f), true
}

func finite(v js.Value) (float32, bool) {
	if v.Type() != js.TypeNumber {
		return 0, false
	}
	f := float32(v.Float())
	return f, !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
}

// straightRGBA lends a packed read-only NRGBA buffer when possible. Other
// image types need one conversion before copying their pixels to the canvas.
func straightRGBA(img image.Image) []byte {
	bounds := img.Bounds()
	if src, ok := img.(*image.NRGBA); ok && src.Stride == 4*bounds.Dx() {
		return src.Pix[:4*bounds.Dx()*bounds.Dy()]
	}
	rgba := image.NewNRGBA(image.Rectangle{Max: bounds.Size()})
	draw.Draw(rgba, rgba.Bounds(), img, bounds.Min, draw.Src)
	return rgba.Pix
}
