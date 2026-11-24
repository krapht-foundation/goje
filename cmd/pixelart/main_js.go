// Command pixelart hosts the editing document for the browser Pixel Art Maker.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"syscall/js"

	"github.com/krapht-foundation/goje/internal/pixelart"
	"github.com/krapht-foundation/goje/internal/spriteformat"
)

func main() {
	var doc, candidate *pixelart.Document
	uint8Array := js.Global().Get("Uint8Array")
	clampedArray := js.Global().Get("Uint8ClampedArray")
	api := map[string]any{
		"maxAxis": spriteformat.MaxPNGAxis, "maxPNGBytes": spriteformat.MaxPNGBytes,
		"stageNew": callback(func(args []js.Value) any {
			var err error
			candidate, err = pixelart.New(point(args))
			return diagnostic(err)
		}),
		"stageOpen": callback(func(args []js.Value) any {
			candidate = nil
			input := args[0]
			if !input.InstanceOf(uint8Array) {
				return "Expected PNG bytes."
			}
			size := input.Length()
			if size > spriteformat.MaxPNGBytes {
				return fmt.Sprintf("PNG exceeds %d bytes", spriteformat.MaxPNGBytes)
			}
			data := make([]byte, size)
			js.CopyBytesToGo(data, input)
			var err error
			candidate, err = pixelart.Open(bytes.NewReader(data))
			return diagnostic(err)
		}),
		"finishOpen": callback(func(args []js.Value) any {
			if args[0].Bool() && candidate != nil {
				doc = candidate
			}
			candidate = nil
			return nil
		}),
		"state": callback(func(_ []js.Value) any {
			if doc == nil {
				return map[string]any{
					"width":   0,
					"height":  0,
					"dirty":   false,
					"canUndo": false,
					"canRedo": false,
				}
			}
			size := doc.Size()
			return map[string]any{
				"width": size.X, "height": size.Y, "dirty": doc.Dirty(),
				"canUndo": doc.CanUndo(), "canRedo": doc.CanRedo(),
			}
		}),
		"pixels": callback(func(args []js.Value) any {
			out := args[0]
			if doc == nil || !out.InstanceOf(clampedArray) || out.Length() != len(doc.Pixels()) {
				return "Pixel buffer does not match the current sheet."
			}
			js.CopyBytesToJS(out, doc.Pixels())
			return ""
		}),
		"begin": callback(func(args []js.Value) any {
			ink := color.NRGBA{}
			if packed := args[2].Int(); packed >= 0 {
				ink = color.NRGBA{
					R: uint8(packed >> 16 & 255),
					G: uint8(packed >> 8 & 255),
					B: uint8(packed & 255),
					A: 255,
				}
			}
			return doc != nil && doc.Begin(point(args), ink)
		}),
		"extend": callback(func(args []js.Value) any {
			if doc != nil {
				doc.Extend(point(args))
			}
			return nil
		}),
		"end": callback(func(args []js.Value) any {
			if doc != nil {
				if args[0].Bool() {
					doc.Commit()
				} else {
					doc.Cancel()
				}
			}
			return nil
		}),
		"undo": callback(func(_ []js.Value) any { return doc != nil && doc.Undo() }),
		"redo": callback(func(_ []js.Value) any { return doc != nil && doc.Redo() }),
		"exportPNG": callback(func(_ []js.Value) any {
			if doc == nil {
				return "Open or create a sheet before downloading."
			}
			data, err := doc.ExportPNG()
			if err != nil {
				return err.Error()
			}
			result := uint8Array.New(len(data))
			js.CopyBytesToJS(result, data)
			return result
		}),
		"markExported": callback(func(_ []js.Value) any {
			if doc != nil {
				doc.MarkExported()
			}
			return nil
		}),
	}
	js.Global().Set("gojePixelArt", api)
	// The page owns these callbacks for the lifetime of the Wasm instance.
	select {}
}

func callback(fn func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
}

func point(args []js.Value) image.Point { return image.Pt(args[0].Int(), args[1].Int()) }

func diagnostic(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
