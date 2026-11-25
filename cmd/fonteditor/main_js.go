// Command fonteditor hosts the editing document for the browser Font Editor.
package main

import (
	"bytes"
	"fmt"
	"image"
	"syscall/js"

	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/internal/fonteditor"
)

func main() {
	var doc, candidate *fonteditor.Document
	uint8Array := js.Global().Get("Uint8Array")
	clampedArray := js.Global().Get("Uint8ClampedArray")
	api := map[string]any{
		"maxCell": font.MaxCell, "maxPNGBytes": font.MaxPNGBytes, "maxJSONBytes": font.MaxJSONBytes,
		"stageBuiltin": callback(func(_ []js.Value) any {
			var err error
			candidate, err = fonteditor.Builtin()
			return diagnostic(err)
		}),
		"stageNew": callback(func(args []js.Value) any {
			var err error
			candidate, err = fonteditor.New(
				image.Pt(args[0].Int(), args[1].Int()), args[2].Int(), rune(args[3].Int()), args[4].Int())
			return diagnostic(err)
		}),
		"stageOpen": callback(func(args []js.Value) any {
			candidate = nil
			sheet, err := input(args[0], uint8Array, font.MaxPNGBytes, "PNG")
			if err != "" {
				return err
			}
			metadata, err := input(args[1], uint8Array, font.MaxJSONBytes, "JSON")
			if err != "" {
				return err
			}
			var openErr error
			candidate, openErr = fonteditor.Open(bytes.NewReader(sheet), bytes.NewReader(metadata))
			return diagnostic(openErr)
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
				return map[string]any{"count": 0, "dirty": false, "canUndo": false, "canRedo": false}
			}
			cell, size := doc.Cell(), doc.Size()
			return map[string]any{
				"cellWidth": cell.X, "cellHeight": cell.Y, "baseline": doc.Baseline(),
				"first": int(doc.First()), "count": doc.Count(), "columns": doc.Columns(),
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
			return doc != nil &&
				doc.Begin(args[0].Int(), image.Pt(args[1].Int(), args[2].Int()), args[3].Bool())
		}),
		"extend": callback(func(args []js.Value) any {
			if doc != nil {
				doc.Extend(image.Pt(args[0].Int(), args[1].Int()))
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
				return "Open or create a font before downloading."
			}
			data, err := doc.ExportPNG()
			if err != nil {
				return err.Error()
			}
			result := uint8Array.New(len(data))
			js.CopyBytesToJS(result, data)
			return result
		}),
		"exportJSON": callback(func(_ []js.Value) any {
			if doc == nil {
				return nil
			}
			return string(doc.ExportJSON())
		}),
		"markExported": callback(func(_ []js.Value) any {
			if doc != nil {
				doc.MarkExported()
			}
			return nil
		}),
	}
	js.Global().Set("gojeFontEditor", api)
	// The page owns these callbacks for the lifetime of the Wasm instance.
	select {}
}

func callback(fn func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
}

// input copies bounded file bytes from the page, checking the size first.
func input(value, uint8Array js.Value, limit int, kind string) ([]byte, string) {
	if !value.InstanceOf(uint8Array) {
		return nil, fmt.Sprintf("Expected %s bytes.", kind)
	}
	if value.Length() > limit {
		return nil, fmt.Sprintf("%s exceeds %d bytes", kind, limit)
	}
	data := make([]byte, value.Length())
	js.CopyBytesToGo(data, value)
	return data, ""
}

func diagnostic(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
