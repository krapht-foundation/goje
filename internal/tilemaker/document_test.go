package tilemaker_test

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/krapht-foundation/goje/internal/tilemaker"
	"github.com/krapht-foundation/goje/tile"
)

var red = color.NRGBA{R: 230, A: 255}

func TestStrokeInterpolationAndCancellation(t *testing.T) {
	var doc tilemaker.Document
	if !doc.Begin(image.Pt(2, 2), red) {
		t.Fatal("could not start stroke")
	}
	doc.Extend(image.Pt(5, 5))
	doc.Extend(image.Pt(-1, 5))
	doc.Extend(image.Pt(9, 5))
	doc.Extend(image.Pt(7, 5))
	if !doc.Commit() {
		t.Fatal("stroke did not commit")
	}
	var want tilemaker.Pixels
	for _, p := range []image.Point{
		image.Pt(2, 2), image.Pt(3, 3), image.Pt(4, 4), image.Pt(5, 5),
		image.Pt(7, 5), image.Pt(8, 5), image.Pt(9, 5),
	} {
		i := 4 * (p.Y*tile.Width + p.X)
		want[i], want[i+3] = red.R, red.A
	}
	if doc.Snapshot() != want {
		t.Fatal("stroke skipped pixels or crossed the out-of-bounds gap")
	}
	if !doc.Undo() || doc.Dirty() || !doc.CanRedo() {
		t.Fatal("one undo must undo the whole stroke and restore the clean blank tile")
	}
	if !doc.Begin(image.Pt(0, 0), red) {
		t.Fatal("could not start cancelled stroke")
	}
	doc.Cancel()
	if doc.Dirty() || !doc.Redo() || doc.Snapshot() != want {
		t.Fatal("cancellation must restore the blank image and preserve redo")
	}
}

func TestExportRetainsImportedAlphaAndTracksSavedPixels(t *testing.T) {
	pixels := image.NewNRGBA(image.Rect(0, 0, tile.Width, tile.Height))
	accent := color.NRGBA{R: 17, G: 201, B: 33, A: 91}
	pixels.SetNRGBA(10, 4, accent)
	input, err := tile.EncodePNG(pixels)
	if err != nil {
		t.Fatal(err)
	}
	doc := open(t, input)
	paint(t, doc, image.Pt(8, 5), red)
	want := doc.Snapshot()
	data, err := doc.ExportPNG()
	if err != nil || !doc.Dirty() {
		t.Fatalf("export must not mark the tile saved before handoff: %v", err)
	}
	reopened := open(t, data)
	if reopened.Snapshot() != want || reopened.Dirty() {
		t.Fatal("reopening must preserve the edited pixels and start clean")
	}
	i := (4*tile.Width + 10) * 4
	got := reopened.Snapshot()
	if !bytes.Equal(got[i:i+4], []byte{accent.R, accent.G, accent.B, accent.A}) {
		t.Fatalf("imported intermediate-alpha pixel changed: %v", got[i:i+4])
	}
	again, err := reopened.ExportPNG()
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("re-encoding equivalent pixels changed the PNG: %v", err)
	}
	doc.MarkExported()
	if doc.Dirty() {
		t.Fatal("download handoff did not mark the image saved")
	}
	paint(t, doc, image.Pt(8, 5), color.NRGBA{})
	if !doc.Dirty() || !doc.Undo() || doc.Dirty() {
		t.Fatal("erasing must dirty the image and undoing to the saved pixels must clear it")
	}
	if !doc.Begin(image.Pt(8, 5), red) || doc.Commit() || !doc.CanRedo() {
		t.Fatal("no-op edit must preserve redo")
	}
	paint(t, doc, image.Pt(0, 0), red)
	if doc.CanRedo() {
		t.Fatal("new edit must discard the undone branch")
	}
}

func TestHistoryRetainsLast64Transactions(t *testing.T) {
	var doc tilemaker.Document
	for n := 1; n <= 70; n++ {
		paint(t, &doc, image.Point{}, color.NRGBA{R: uint8(n), A: 255})
	}
	for n := 69; n >= 6; n-- {
		if !doc.Undo() || int(doc.Snapshot()[0]) != n {
			t.Fatalf("undo did not restore state %d", n)
		}
	}
	if doc.Undo() {
		t.Fatal("undo reached an evicted state")
	}
	for n := 7; n <= 70; n++ {
		if !doc.Redo() || int(doc.Snapshot()[0]) != n {
			t.Fatalf("redo did not restore state %d", n)
		}
	}
}

func open(t *testing.T, data []byte) *tilemaker.Document {
	t.Helper()
	doc, err := tilemaker.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func paint(t *testing.T, doc *tilemaker.Document, at image.Point, ink color.NRGBA) {
	t.Helper()
	if !doc.Begin(at, ink) || !doc.Commit() {
		t.Fatalf("could not commit a pixel at %v", at)
	}
}
