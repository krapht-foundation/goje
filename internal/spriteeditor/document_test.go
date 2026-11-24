package spriteeditor_test

import (
	"bytes"
	"image"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/internal/spriteeditor"
	"github.com/krapht-foundation/goje/internal/spriteformat"
	"github.com/krapht-foundation/goje/sprite"
)

func fixture(t *testing.T) (png, json []byte) {
	t.Helper()
	png, err := os.ReadFile("../../sprite/testdata/tomato.png")
	if err != nil {
		t.Fatal(err)
	}
	json, err = os.ReadFile("../../sprite/testdata/tomato.json")
	if err != nil {
		t.Fatal(err)
	}
	return png, json
}

func opened(t *testing.T, png, json []byte) *spriteeditor.Document {
	t.Helper()
	var doc spriteeditor.Document
	ok, err := doc.Open(bytes.NewReader(png), bytes.NewReader(json), nil)
	if err != nil || !ok {
		t.Fatalf("opening fixture: %v", err)
	}
	return &doc
}

func export(t *testing.T, doc *spriteeditor.Document) []byte {
	t.Helper()
	data, err := doc.Export()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestEditorExportsPlayableOrderedMetadata(t *testing.T) {
	t.Parallel()
	png, json := fixture(t)
	doc := opened(t, png, json)
	original, err := sprite.Decode(bytes.NewReader(png), bytes.NewReader(json))
	if err != nil {
		t.Fatal(err)
	}
	reopened := opened(t, png, export(t, doc))
	for i, name := range []string{"idle", "move"} {
		clip := reopened.Clips()[i]
		want, ok := original.Clip(name)
		if !ok || clip.Name != name || len(clip.Frames) != want.Len() {
			t.Fatalf("reopened clip %d differs from %q", i, name)
		}
		for j, frame := range clip.Frames {
			if sprite.Frame(frame) != want.Frame(j) {
				t.Fatalf("%s frame %d changed on export", name, j)
			}
		}
	}
	if !bytes.Equal(export(t, doc), export(t, reopened)) {
		t.Fatal("equivalent metadata must export identical bytes")
	}

	move := doc.Clips()[1]
	hop := move.Frames[1]
	hop.Ticks++
	hop.Pivot.X++
	requireEdit(t, doc.SetFrame(1, 1, hop))
	requireEdit(t, doc.MoveFrame(1, 1, 0))
	requireEdit(t, doc.DuplicateFrame(1, 0))
	requireEdit(t, doc.DeleteFrame(1, 2))
	requireEdit(t, doc.RenameClip(1, "hop"))
	if !doc.Dirty() || move.Frames[1].Ticks != 8 {
		t.Fatal("editing overwrote the imported frame snapshot or did not mark the document dirty")
	}
	data := export(t, doc)
	if !doc.Dirty() {
		t.Fatal("encoding without a download handoff cleared unsaved changes")
	}
	played, err := sprite.Decode(bytes.NewReader(png), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	clip, ok := played.Clip("hop")
	if !ok || clip.Len() != 3 || clip.Frame(0) != sprite.Frame(hop) ||
		clip.Frame(1) != sprite.Frame(hop) || clip.Frame(2) != sprite.Frame(move.Frames[2]) {
		t.Fatalf("runtime did not load edited frames in order: %v", clip)
	}
	doc.MarkSaved()
	if doc.Dirty() || !doc.Undo() || !doc.Dirty() || !doc.Redo() || doc.Dirty() {
		t.Fatal("dirty state did not follow the saved metadata through undo and redo")
	}
}

func TestRejectedEditsAndOpensPreserveUnsavedWork(t *testing.T) {
	t.Parallel()
	png, json := fixture(t)
	doc := opened(t, png, json)
	requireEdit(t, doc.RenameClip(0, "renamed"))
	requireEdit(t, doc.MoveFrame(1, 0, 2))
	if !doc.Undo() {
		t.Fatal("undo failed")
	}
	saved := export(t, doc)
	bad := doc.Clips()[0].Frames[0]
	bad.Source.Max.X = 999
	if doc.SetFrame(0, 0, bad) == nil || doc.RenameClip(0, "move") == nil {
		t.Fatal("invalid rectangle or duplicate name was accepted")
	}
	requireEdit(t, doc.RenameClip(0, "renamed")) // An equivalent edit must retain redo.
	pixels := doc.Image()
	prompts := 0
	decline := func() bool { prompts++; return false }
	for _, invalid := range []string{"{}", strings.Repeat(" ", spriteformat.MaxJSONBytes+1)} {
		ok, err := doc.Open(bytes.NewReader(png), strings.NewReader(invalid), decline)
		if ok || err == nil || prompts != 0 {
			t.Fatalf("invalid sidecar must not reach confirmation: %v, %v, %d", ok, err, prompts)
		}
	}
	ok, err := doc.Open(bytes.NewReader(png), bytes.NewReader(json), decline)
	if ok || err != nil || prompts != 1 {
		t.Fatalf("canceled replacement changed open result: %v, %v, %d", ok, err, prompts)
	}
	if !bytes.Equal(saved, export(t, doc)) || doc.Image() != pixels || !doc.Dirty() || !doc.Redo() {
		t.Fatal("invalid edit or canceled open changed artwork, metadata, dirty state, or redo")
	}
	ok, err = doc.Open(bytes.NewReader(png), nil, func() bool { return true })
	if !ok || err != nil || doc.Dirty() || doc.Undo() || doc.Redo() || len(doc.Clips()) != 0 {
		t.Fatalf("PNG-only replacement did not reset the document: %v, %v", ok, err)
	}
	if _, err := doc.Export(); err == nil {
		t.Fatal("empty draft was exported")
	}
}

func TestEditHistoryEvictionAndBranching(t *testing.T) {
	t.Parallel()
	png, _ := fixture(t)
	var doc spriteeditor.Document
	if ok, err := doc.Open(bytes.NewReader(png), nil, nil); err != nil || !ok {
		t.Fatalf("opening PNG-only draft: %v", err)
	}
	first := spriteformat.Frame{Source: image.Rect(2, 3, 6, 9), Pivot: image.Pt(4, 6), Ticks: 1}
	requireEdit(t, doc.AddClip("walk", first))
	if doc.DeleteFrame(0, 0) == nil {
		t.Fatal("deleted the only frame without deleting its clip")
	}
	for ticks := 2; ticks <= 71; ticks++ {
		frame := first
		frame.Ticks = ticks
		requireEdit(t, doc.SetFrame(0, 0, frame))
	}
	for range 64 {
		if !doc.Undo() {
			t.Fatal("lost an edit before the history limit")
		}
	}
	if doc.Undo() || doc.Clips()[0].Frames[0].Ticks != 7 {
		t.Fatal("history did not evict the oldest commits")
	}
	requireEdit(t, doc.AddFrame(0, first))
	if doc.Redo() || !slices.Equal(doc.Clips()[0].Frames[1:], []spriteformat.Frame{first}) {
		t.Fatal("new edit did not branch from the retained revision")
	}
	requireEdit(t, doc.DeleteClip(0))
	if _, err := doc.Export(); err == nil || !doc.Undo() || len(doc.Clips()[0].Frames) != 2 {
		t.Fatal("undo did not restore the deleted clip")
	}
}

func TestFrameBudgetRejectsChangesWithoutLosingHistory(t *testing.T) {
	t.Parallel()
	png, _ := fixture(t)
	const frame = `{"x":0,"y":0,"width":1,"height":1,"pivotX":0,"pivotY":1,"ticks":1}`
	full := `{"version":1,"clips":[{"name":"idle","frames":[` +
		strings.Repeat(frame+",", spriteformat.MaxFrames-1) + frame + `]}]}`
	doc := opened(t, png, []byte(full))
	last := doc.Clips()[0].Frames[0]
	if doc.AddFrame(0, last) == nil || doc.AddClip("move", last) == nil || doc.Dirty() ||
		doc.Undo() || len(doc.Clips()[0].Frames) != spriteformat.MaxFrames {
		t.Fatal("exceeding the total frame budget changed the document or history")
	}
}

func requireEdit(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
