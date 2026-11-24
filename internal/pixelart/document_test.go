package pixelart

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/internal/spriteformat"
)

func TestPNGRoundTripAndImportLimits(t *testing.T) {
	t.Parallel()
	for _, size := range []image.Point{image.Pt(0, 1), image.Pt(1, -1), image.Pt(513, 1)} {
		if doc, err := New(size); err == nil || doc != nil {
			t.Fatalf("accepted unsupported canvas %v", size)
		}
	}
	original := image.NewNRGBA(image.Rect(0, 0, 1, 512))
	translucent := color.NRGBA{R: 13, G: 91, B: 207, A: 77}
	original.SetNRGBA(0, 511, translucent)
	var source bytes.Buffer
	if err := png.Encode(&source, original); err != nil {
		t.Fatal(err)
	}
	doc, err := Open(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Size() != image.Pt(1, 512) || doc.Dirty() {
		t.Fatalf("opening introduced a resize or edit: %v", doc.Size())
	}
	strokeAt(t, doc, image.Pt(0, 0), color.NRGBA{R: 215, G: 84, B: 73, A: 1})
	encoded, err := doc.ExportPNG()
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Dirty() {
		t.Fatal("encoding marked the sheet as saved before download handoff")
	}
	again, err := doc.ExportPNG()
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatalf("same artwork produced different bytes: %v", err)
	}
	reopened, err := Open(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	original.SetNRGBA(0, 0, color.NRGBA{R: 215, G: 84, B: 73, A: 255})
	if !bytes.Equal(reopened.Pixels(), original.Pix) {
		t.Fatal("round trip lost the painted pixel, the untouched alpha, or transparent pixels")
	}

	var tooWide bytes.Buffer
	if err := png.Encode(&tooWide, image.NewNRGBA(image.Rect(0, 0, 513, 1))); err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{
		source.Bytes()[:len(source.Bytes())-5], tooWide.Bytes(),
		make([]byte, spriteformat.MaxPNGBytes+1),
	} {
		if candidate, err := Open(bytes.NewReader(input)); err == nil || candidate != nil {
			t.Fatalf("invalid input staged a document (%d bytes)", len(input))
		}
	}
	if !doc.Dirty() || !doc.CanUndo() || !bytes.Equal(doc.Pixels(), original.Pix) {
		t.Fatal("opening invalid input affected the current document")
	}
}

func TestStrokeGapsCancellationAndSavedRevision(t *testing.T) {
	t.Parallel()
	doc := blank(t, 9, 3)
	ink := color.NRGBA{G: 83, B: 41, A: 255}
	if doc.Begin(image.Pt(-1, 0), ink) || !doc.Begin(image.Pt(0, 1), ink) {
		t.Fatal("began a stroke outside the sheet or rejected an in-bounds sample")
	}
	doc.Extend(image.Pt(3, 1))
	doc.Extend(image.Pt(4, -1))
	doc.Extend(image.Pt(7, 1))
	doc.Extend(image.Pt(8, 1))
	painted := []byte{255, 255, 255, 255, 0, 0, 0, 255, 255}
	for x, a := range painted {
		if got := doc.Pixels()[(doc.Size().X+x)*4+3]; got != a {
			t.Fatalf("stroke at (%d,1) has alpha %d, want %d", x, got, a)
		}
	}
	if _, err := doc.ExportPNG(); err == nil {
		t.Fatal("export accepted a stroke before it committed")
	}
	if !doc.Commit() || !doc.Undo() || doc.Dirty() || !doc.Redo() || !doc.Dirty() {
		t.Fatal("one stroke must undo and redo as one edit")
	}
	doc.MarkExported()
	if doc.Dirty() || !doc.Undo() || !doc.Dirty() {
		t.Fatal("undo did not restore pixels relative to the new saved baseline")
	}
	doc.Begin(image.Pt(8, 2), ink)
	doc.Cancel()
	if !doc.CanRedo() || doc.Pixels()[len(doc.Pixels())-1] != 0 {
		t.Fatal("cancel left a painted pixel or dropped redo")
	}
	doc.Begin(image.Pt(8, 2), color.NRGBA{})
	if doc.Commit() || !doc.Redo() || doc.Dirty() {
		t.Fatal("a no-op dropped redo or restoring the exact artwork failed to clear dirty")
	}
	strokeAt(t, doc, image.Pt(0, 1), color.NRGBA{R: 12})
	if doc.Pixels()[4*9] != 0 || doc.Pixels()[4*9+3] != 0 || !doc.Undo() || doc.Dirty() {
		t.Fatal("eraser did not clear straight RGBA or undo did not restore it")
	}
	strokeAt(t, doc, image.Pt(8, 2), ink)
	if doc.CanRedo() {
		t.Fatal("a changed branch retained the abandoned redo")
	}
}

func TestHistoryEvictsByEditCountAndPixelData(t *testing.T) {
	t.Parallel()
	doc := blank(t, 1, 1)
	for i := byte(1); i <= 65; i++ {
		strokeAt(t, doc, image.Point{}, color.NRGBA{R: i, A: 255})
	}
	for range 64 {
		if !doc.Undo() {
			t.Fatal("evicted a single-pixel edit too early")
		}
	}
	if doc.Undo() || doc.Pixels()[0] != 1 || !doc.CanRedo() {
		t.Fatal("undo escaped the 64 retained edits")
	}

	doc = blank(t, 512, 512)
	for i := byte(1); i <= 5; i++ {
		doc.Begin(image.Point{}, color.NRGBA{R: i, A: 255})
		for y := range 512 {
			if y%2 == 0 {
				doc.Extend(image.Pt(0, y))
				doc.Extend(image.Pt(511, y))
			} else {
				doc.Extend(image.Pt(511, y))
				doc.Extend(image.Pt(0, y))
			}
		}
		if !doc.Commit() {
			t.Fatalf("full-sheet stroke %d was not retained", i)
		}
	}
	var undone byte
	for doc.Undo() {
		undone++
	}
	if undone == 0 || undone > 4 || doc.Pixels()[0] != 5-undone ||
		doc.Pixels()[len(doc.Pixels())-4] != 5-undone {
		t.Fatalf("byte bound evicted the wrong revision: undid %d strokes", undone)
	}
}

func TestOversizedExportPreservesArtwork(t *testing.T) {
	t.Parallel()
	doc := blank(t, 512, 512)
	// Pencil marks are opaque. A normalized RGBA import can have varying alpha
	// at every pixel, so seed that state to force the encoder past one MiB.
	rng := rand.NewChaCha8([32]byte{42})
	if _, err := rng.Read(doc.pixels.Pix); err != nil {
		t.Fatal(err)
	}
	copy(doc.saved, doc.pixels.Pix)
	strokeAt(t, doc, image.Point{}, color.NRGBA{})
	out, err := doc.ExportPNG()
	if out != nil || err == nil || !strings.Contains(err.Error(), "1048576 bytes") {
		t.Fatalf("expected a v1 export size error, got %v", err)
	}
	if !doc.Dirty() || !doc.Undo() || doc.Dirty() || !bytes.Equal(doc.Pixels(), doc.saved) {
		t.Fatal("export failure lost the edit or changed the saved baseline")
	}
}

func blank(t *testing.T, width, height int) *Document {
	t.Helper()
	doc, err := New(image.Pt(width, height))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func strokeAt(t *testing.T, doc *Document, at image.Point, ink color.NRGBA) {
	t.Helper()
	if !doc.Begin(at, ink) || !doc.Commit() {
		t.Fatalf("failed to commit stroke at %v", at)
	}
}
