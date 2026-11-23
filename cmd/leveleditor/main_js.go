// Command leveleditor supplies the Go editing core to the browser Level Builder.
package main

import (
	"bytes"
	"fmt"
	"strings"
	"syscall/js"

	"github.com/krapht-foundation/goje/internal/leveleditor"
	"github.com/krapht-foundation/goje/level"
)

func main() {
	var doc leveleditor.Document
	uint8Array := js.Global().Get("Uint8Array")
	api := map[string]any{
		"maxBytes": level.MaxBytes,
		"open": callback(func(args []js.Value) any {
			input := args[0]
			size := input.Length()
			if size > level.MaxBytes {
				return fmt.Sprintf("level exceeds %d bytes", level.MaxBytes)
			}
			data := make([]byte, size)
			js.CopyBytesToGo(data, input)
			return diagnostic(doc.Open(bytes.NewReader(data)))
		}),
		"begin": callback(func(args []js.Value) any {
			switch args[0].String() {
			case ".":
				return diagnostic(doc.BeginStroke(leveleditor.Floor))
			case "#":
				return diagnostic(doc.BeginStroke(leveleditor.Wall))
			default:
				return "unknown terrain brush"
			}
		}),
		"paint": callback(func(args []js.Value) any {
			return diagnostic(doc.Paint(args[0].Int(), args[1].Int()))
		}),
		"end": callback(func(args []js.Value) any { doc.EndStroke(args[0].Bool()); return "" }),
		"move": callback(func(args []js.Value) any {
			return diagnostic(doc.Move(args[0].Int(), args[1].Int(), args[2].Int(), args[3].Int()))
		}),
		"undo":         callback(func(_ []js.Value) any { doc.Undo(); return "" }),
		"redo":         callback(func(_ []js.Value) any { doc.Redo(); return "" }),
		"markExported": callback(func(_ []js.Value) any { doc.MarkExported(); return "" }),
		"state":        callback(func(_ []js.Value) any { return snapshot(&doc) }),
		"exportLevel": callback(func(_ []js.Value) any {
			data, err := doc.Export()
			if err != nil {
				return err.Error()
			}
			result := uint8Array.New(len(data))
			js.CopyBytesToJS(result, data)
			return result
		}),
	}
	js.Global().Set("gojeLevelEditor", api)
	// The page owns the callbacks for the lifetime of this Wasm instance.
	select {}
}

func callback(fn func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
}

func diagnostic(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

// Send one terrain string and only occupied cells across the JS boundary.
// Painting a single cell doesn't need a snapshot; the UI already has its coordinates.
func snapshot(doc *leveleditor.Document) map[string]any {
	width, height := doc.Size()
	var terrain strings.Builder
	terrain.Grow(width * height)
	placements := make([]any, 0)
	for y := range height {
		for x := range width {
			tile, p := doc.Cell(x, y)
			terrain.WriteByte(tile)
			if p.ID != 0 {
				placements = append(placements, map[string]any{"id": p.ID, "kind": p.Kind, "x": x, "y": y})
			}
		}
	}
	return map[string]any{
		"width": width, "height": height, "terrain": terrain.String(), "placements": placements,
		"dirty": doc.Dirty(), "canUndo": doc.CanUndo(), "canRedo": doc.CanRedo(),
	}
}
