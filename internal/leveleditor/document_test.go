package leveleditor_test

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/examples/courtyard"
	"github.com/krapht-foundation/goje/internal/leveleditor"
	"github.com/krapht-foundation/goje/level"
)

func TestCourtyardRoundTrip(t *testing.T) {
	source, readErr := os.ReadFile("../../cmd/playground/levels/courtyard.json")
	if readErr != nil {
		t.Fatal(readErr)
	}
	var doc leveleditor.Document
	if err := doc.Open(bytes.NewReader(source)); err != nil {
		t.Fatal(err)
	}
	stroke(t, &doc, leveleditor.Floor, [][2]int{{4, 2}}, true)
	if err := doc.Move(7, 4, 7, 5); err != nil {
		t.Fatal(err)
	}
	data := export(t, &doc)
	if !doc.Dirty() {
		t.Fatal("encoding should not mark changes as exported before download handoff")
	}
	doc.MarkExported()
	if doc.Dirty() || !doc.Undo() || !doc.Dirty() {
		t.Fatal("undo did not leave the exported revision")
	}
	if !doc.Undo() {
		t.Fatal("terrain edit should also be undoable")
	}
	tile, _ := doc.Cell(4, 2)
	_, lookout := doc.Cell(7, 4)
	if tile != leveleditor.Wall || lookout.ID != 2 {
		t.Fatal("undo did not restore the wall and lookout")
	}
	if !doc.Redo() || !doc.Dirty() {
		t.Fatal("redoing only the terrain edit should leave the document dirty")
	}
	if !doc.Redo() || doc.Dirty() || !bytes.Equal(data, export(t, &doc)) {
		t.Fatal("redo did not reproduce the exported bytes and clean state")
	}
	// Reopen using both the editor and the runtime's reference validator.
	var reopened leveleditor.Document
	if err := reopened.Open(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	layout, err := courtyard.Decode(bytes.NewReader(export(t, &reopened)))
	if err != nil {
		t.Fatal(err)
	}
	want := []level.Placement{
		{ID: 1, Kind: "scout", X: 2, Y: 2},
		{ID: 2, Kind: "lookout", X: 7, Y: 5},
	}
	if !layout.Board.Walkable(4, 2) ||
		!slices.Equal(slices.Collect(layout.Board.Placements()), want) {
		t.Fatal("export lost terrain edits, placement identity, or document order")
	}
}

func TestInvalidAndCanceledEditsPreserveHistory(t *testing.T) {
	var doc leveleditor.Document
	open := `{"version":1,"rows":["...","..#"],"placements":[{"id":7,"kind":"scout","x":0,"y":0},{"id":8,"kind":"lookout","x":2,"y":0}]}`
	if err := doc.Open(strings.NewReader(open)); err != nil {
		t.Fatal(err)
	}
	original := export(t, &doc)
	for _, to := range [][2]int{{2, 0}, {2, 1}, {-1, 0}} {
		if err := doc.Move(0, 0, to[0], to[1]); err == nil {
			t.Errorf("accepted blocked destination %v", to)
		}
	}
	if err := doc.Move(0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	stroke(t, &doc, leveleditor.Floor, [][2]int{{1, 0}}, true)
	if doc.CanUndo() || doc.Dirty() {
		t.Fatal("invalid and no-op edits must not create transactions")
	}
	if err := doc.BeginStroke(leveleditor.Wall); err != nil {
		t.Fatal(err)
	}
	if err := doc.Paint(0, 0); err == nil {
		t.Fatal("wall covered the scout")
	}
	for _, cell := range [][2]int{{1, 0}, {1, 0}, {1, 1}} {
		if err := doc.Paint(cell[0], cell[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := doc.Export(); err == nil {
		t.Fatal("export accepted an unfinished gesture")
	}
	doc.EndStroke(true)
	if !doc.Undo() || doc.CanUndo() || !bytes.Equal(original, export(t, &doc)) {
		t.Fatal("a single undo should restore both cells in the brush gesture")
	}
	for _, invalid := range []string{`{"version":2}`, open + strings.Repeat(" ", 256<<10)} {
		if err := doc.Open(strings.NewReader(invalid)); err == nil || !doc.CanRedo() {
			t.Fatal("an invalid open replaced the current history")
		}
	}
	stroke(t, &doc, leveleditor.Wall, [][2]int{{0, 1}}, false)
	if doc.Dirty() || !doc.CanRedo() || !bytes.Equal(original, export(t, &doc)) {
		t.Fatal("cancel changed the document or discarded redo")
	}
	if err := doc.Move(0, 0, 0, 1); err != nil {
		t.Fatal(err)
	}
	if doc.CanRedo() || !doc.Undo() || !bytes.Equal(original, export(t, &doc)) {
		t.Fatal("a new edit after undo did not replace the redo branch")
	}
}

func TestHistoryEvictsCompleteOldestActions(t *testing.T) {
	for _, tc := range []struct {
		name          string
		side          int
		strokes, keep int
	}{
		{"transaction limit", 1, 65, 64},
		{"change entry limit", 128, 5, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]string, tc.side)
			cells := make([][2]int, 0, tc.side*tc.side)
			for y := range tc.side {
				rows[y] = strings.Repeat(".", tc.side)
				for x := range tc.side {
					cells = append(cells, [2]int{x, y})
				}
			}
			data, err := level.Encode(rows, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var doc leveleditor.Document
			if err := doc.Open(bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			for i := range tc.strokes {
				tile := leveleditor.Wall
				if i%2 != 0 {
					tile = leveleditor.Floor
				}
				stroke(t, &doc, tile, cells, true)
			}
			for range tc.keep {
				if !doc.Undo() {
					t.Fatal("history dropped too many actions")
				}
			}
			if doc.Undo() {
				t.Fatal("history retained an action over its limit")
			}
			for _, cell := range cells {
				if tile, _ := doc.Cell(cell[0], cell[1]); tile != leveleditor.Wall {
					t.Fatal("eviction must keep the oldest stroke applied in full")
				}
			}
			for range tc.keep {
				if !doc.Redo() {
					t.Fatal("undo lost an action that should be redoable")
				}
			}
		})
	}
}

func stroke(t *testing.T, doc *leveleditor.Document, tile byte, cells [][2]int, commit bool) {
	t.Helper()
	if err := doc.BeginStroke(tile); err != nil {
		t.Fatal(err)
	}
	for _, cell := range cells {
		if err := doc.Paint(cell[0], cell[1]); err != nil {
			t.Fatal(err)
		}
	}
	doc.EndStroke(commit)
}

func export(t *testing.T, doc *leveleditor.Document) []byte {
	t.Helper()
	data, err := doc.Export()
	if err != nil {
		t.Fatal(err)
	}
	return data
}
