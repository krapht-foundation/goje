// Command tilemaker hosts the editing document for the browser Tile Maker.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"syscall/js"

	"github.com/krapht-foundation/goje/internal/tilemaker"
	"github.com/krapht-foundation/goje/tile"
)

func main() {
	doc := new(tilemaker.Document)
	var candidate *tilemaker.Document
	uint8Array := js.Global().Get("Uint8Array")
	api := map[string]any{
		"width": tile.Width, "height": tile.Height, "maxBytes": tile.MaxPNGBytes,
		"newTile": callback(func(_ []js.Value) any { doc = new(tilemaker.Document); return nil }),
		"stageOpen": callback(func(args []js.Value) any {
			input := args[0]
			size := input.Length()
			if size > tile.MaxPNGBytes {
				return fmt.Sprintf("PNG exceeds %d bytes", tile.MaxPNGBytes)
			}
			data := make([]byte, size)
			js.CopyBytesToGo(data, input)
			var err error
			candidate, err = tilemaker.Open(bytes.NewReader(data))
			if err != nil {
				return err.Error()
			}
			return ""
		}),
		"finishOpen": callback(func(args []js.Value) any {
			if args[0].Bool() && candidate != nil {
				doc = candidate
			}
			candidate = nil
			return nil
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
			return doc.Begin(point(args), ink)
		}),
		"extend": callback(func(args []js.Value) any { doc.Extend(point(args)); return nil }),
		"end": callback(func(args []js.Value) any {
			if args[0].Bool() {
				doc.Commit()
			} else {
				doc.Cancel()
			}
			return nil
		}),
		"undo":         callback(func(_ []js.Value) any { return doc.Undo() }),
		"redo":         callback(func(_ []js.Value) any { return doc.Redo() }),
		"markExported": callback(func(_ []js.Value) any { doc.MarkExported(); return nil }),
		"snapshot": callback(func(args []js.Value) any {
			pixels := doc.Snapshot()
			js.CopyBytesToJS(args[0], pixels[:])
			return map[string]any{
				"dirty":   doc.Dirty(),
				"canUndo": doc.CanUndo(),
				"canRedo": doc.CanRedo(),
			}
		}),
		"exportPNG": callback(func(_ []js.Value) any {
			data, err := doc.ExportPNG()
			if err != nil {
				return err.Error()
			}
			result := uint8Array.New(len(data))
			js.CopyBytesToJS(result, data)
			return result
		}),
	}
	js.Global().Set("gojeTileMaker", api)
	// The page owns these callbacks for the lifetime of the Wasm instance.
	select {}
}

func callback(fn func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
}

func point(args []js.Value) image.Point { return image.Pt(args[0].Int(), args[1].Int()) }
