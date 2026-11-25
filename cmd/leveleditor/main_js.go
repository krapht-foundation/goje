// Command leveleditor supplies the Go editing core to the browser Level Builder.
package main

import (
	"bytes"
	"fmt"
	"math"
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
		"addZone": callback(func(args []js.Value) any {
			_, err := doc.AddZone(zoneFromArgs(0, args))
			return diagnostic(err)
		}),
		"editZone": callback(func(args []js.Value) any {
			id, ok := objectID(args[0])
			if !ok {
				return "zone ID is out of range"
			}
			z := zoneFromArgs(1, args)
			z.ID = id
			return diagnostic(doc.EditZone(z))
		}),
		"deleteZone": callback(func(args []js.Value) any {
			id, ok := objectID(args[0])
			if !ok {
				return "zone ID is out of range"
			}
			return diagnostic(doc.DeleteZone(id))
		}),
		// addPlacement takes kind, x, y, and an ID, where 0 asks the document for the next ID.
		"addPlacement": callback(func(args []js.Value) any {
			p := level.Placement{Kind: args[0].String(), X: args[1].Int(), Y: args[2].Int()}
			if args[3].Int() != 0 {
				id, ok := objectID(args[3])
				if !ok {
					return "placement ID must be 1 to 4294967295"
				}
				p.ID = id
			}
			_, err := doc.AddPlacement(p)
			return diagnostic(err)
		}),
		"setPlacementKind": callback(func(args []js.Value) any {
			id, ok := objectID(args[0])
			if !ok {
				return "placement ID is out of range"
			}
			return diagnostic(doc.SetPlacementKind(id, args[1].String()))
		}),
		"deletePlacement": callback(func(args []js.Value) any {
			id, ok := objectID(args[0])
			if !ok {
				return "placement ID is out of range"
			}
			return diagnostic(doc.DeletePlacement(id))
		}),
		// setSight takes a radius, where 0 removes the level's sight object.
		"setSight": callback(
			func(args []js.Value) any { return diagnostic(doc.SetSight(args[0].Int())) },
		),
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

// zoneFromArgs reads kind, role, x, y, w, h, bottom, and top from args[first:].
// The document validates the values, so a mistyped field is reported, not trusted.
func zoneFromArgs(first int, args []js.Value) level.Zone {
	return level.Zone{
		Kind: args[first].String(), Role: level.Role(args[first+1].String()),
		X: args[first+2].Int(), Y: args[first+3].Int(), W: args[first+4].Int(), H: args[first+5].Int(),
		Bottom: args[first+6].Int(), Top: args[first+7].Int(),
	}
}

func objectID(v js.Value) (uint32, bool) {
	id := v.Int()
	if id < 1 || id > math.MaxUint32 {
		return 0, false
	}
	return uint32(id), true // #nosec G115 -- the range was checked above.
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
	list := doc.Zones()
	zones := make([]any, 0, len(list))
	for _, z := range list {
		zones = append(zones, map[string]any{
			"id": z.ID, "kind": z.Kind, "role": string(z.Role),
			"x": z.X, "y": z.Y, "w": z.W, "h": z.H, "bottom": z.Bottom, "top": z.Top,
		})
	}
	return map[string]any{
		"width":      width,
		"height":     height,
		"terrain":    terrain.String(),
		"placements": placements,
		"zones":      zones,
		"sight":      doc.Sight(),
		"dirty":      doc.Dirty(),
		"canUndo":    doc.CanUndo(),
		"canRedo":    doc.CanRedo(),
	}
}
