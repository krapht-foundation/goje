package leveleditor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/internal/leveleditor"
	"github.com/krapht-foundation/goje/level"
)

func TestSightIsSetUndoneAndExported(t *testing.T) {
	doc := openZoneLevel(t, zoneLevel)
	v1 := export(t, doc)
	if doc.Sight() != 0 {
		t.Fatalf("Sight() = %d on a version 1 level", doc.Sight())
	}
	for _, bad := range []int{-1, 182} {
		if err := doc.SetSight(bad); err == nil || doc.CanUndo() {
			t.Fatalf(
				"SetSight(%d) = %v, undo %v; want a rejection with no history",
				bad,
				err,
				doc.CanUndo(),
			)
		}
	}
	if err := doc.SetSight(4); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetSight(4); err != nil || !doc.Dirty() {
		t.Fatalf("repeat SetSight = %v", err)
	}

	// A sight radius alone makes version 3, with the empty zones array it requires.
	board, err := level.Decode(bytes.NewReader(export(t, doc)))
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := board.Sight(); r != 4 || !ok {
		t.Fatalf("exported sight = %d, %v", r, ok)
	}

	hedge := level.Zone{
		Kind: level.SightBlockKind,
		Role: level.RoleTrigger,
		X:    1,
		Y:    0,
		W:    1,
		H:    2,
		Top:  32,
	}
	if _, err = doc.AddZone(hedge); err != nil {
		t.Fatal(err)
	}
	board, err = level.Decode(bytes.NewReader(export(t, doc)))
	if err != nil {
		t.Fatal(err)
	}
	if !board.SightBlocked(1, 1) || board.SightBlocked(2, 1) {
		t.Fatal("the exported hedge does not block the cells it covers")
	}

	// A solid sight_block zone is the author's mistake to fix, so export reports it.
	hedge.ID, hedge.Role = 1, level.RoleSolid
	if err := doc.EditZone(hedge); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Export(); err == nil ||
		!strings.Contains(err.Error(), `must have role "trigger"`) {
		t.Fatalf("export with a solid sight_block = %v", err)
	}

	// Undo walks back through the zone edits and the radius to the opened bytes.
	for range 3 {
		if !doc.Undo() {
			t.Fatal("undo had nothing to apply")
		}
	}
	if doc.Sight() != 0 || doc.Dirty() || !bytes.Equal(export(t, doc), v1) {
		t.Fatalf("after undo sight = %d, dirty = %v; want the opened level", doc.Sight(), doc.Dirty())
	}
	if !doc.Redo() || doc.Sight() != 4 {
		t.Fatalf("redo sight = %d, want 4", doc.Sight())
	}
	if err := doc.SetSight(0); err != nil || doc.Sight() != 0 {
		t.Fatalf("SetSight(0) = %v, sight %d", err, doc.Sight())
	}
	if !bytes.Equal(export(t, doc), v1) {
		t.Fatal("removing the radius must restore the version 1 bytes")
	}
}

func TestSightOpensAndRoundTrips(t *testing.T) {
	const v3 = "{\n  \"version\": 3,\n  \"rows\": [\n    \"..\"\n  ],\n  \"placements\": [],\n" +
		"  \"zones\": [],\n  \"sight\": {\"radius\": 7}\n}\n"
	doc := openZoneLevel(t, v3)
	if doc.Sight() != 7 {
		t.Fatalf("Sight() = %d, want 7", doc.Sight())
	}
	if got := export(t, doc); string(got) != v3 {
		t.Fatalf("round trip =\n%s\nwant\n%s", got, v3)
	}
	var unopened leveleditor.Document
	if err := unopened.SetSight(4); err == nil {
		t.Fatal("SetSight on an unopened document must fail")
	}
}
