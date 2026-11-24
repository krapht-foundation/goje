package leveleditor_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/internal/leveleditor"
	"github.com/krapht-foundation/goje/level"
)

const zoneLevel = `{"version":1,"rows":["....","....","...."],"placements":[{"id":1,"kind":"scout","x":0,"y":0}]}`

func openZoneLevel(t *testing.T, source string) *leveleditor.Document {
	t.Helper()
	doc := new(leveleditor.Document)
	if err := doc.Open(strings.NewReader(source)); err != nil {
		t.Fatal(err)
	}
	return doc
}

var deck = level.Zone{
	Kind:   "deck",
	Role:   level.RoleSolid,
	X:      1,
	Y:      1,
	W:      2,
	H:      1,
	Bottom: 24,
	Top:    32,
}

func TestZonesAreAddedEditedDeletedAndUndone(t *testing.T) {
	doc := openZoneLevel(t, zoneLevel)
	original := export(t, doc)

	id, err := doc.AddZone(deck)
	if err != nil || id != 1 {
		t.Fatalf("AddZone = %d, %v; want ID 1", id, err)
	}
	door := level.Zone{Kind: "door", Role: level.RoleTrigger, X: 3, Y: 2, W: 1, H: 1, Top: 20}
	if id, err = doc.AddZone(door); err != nil || id != 2 {
		t.Fatalf("second AddZone = %d, %v; want ID 2", id, err)
	}
	if !strings.Contains(string(export(t, doc)), `"version": 2`) {
		t.Fatal("a level with zones must export as version 2")
	}

	moved := doc.Zones()[0]
	moved.X, moved.Bottom = 0, 8
	if err := doc.EditZone(moved); err != nil {
		t.Fatal(err)
	}
	if err := doc.DeleteZone(2); err != nil {
		t.Fatal(err)
	}
	if got := doc.Zones(); len(got) != 1 || got[0] != moved {
		t.Fatalf("zones = %+v, want only the edited deck", got)
	}

	// Undo each action in turn, down to the original level and its exact bytes.
	for range 4 {
		if !doc.Undo() {
			t.Fatal("undo failed")
		}
	}
	if doc.Undo() || len(doc.Zones()) != 0 || !bytes.Equal(export(t, doc), original) {
		t.Fatal("undoing every zone action must restore a version 1 level byte for byte")
	}
	// Redo replays them, keeping document order.
	for range 4 {
		if !doc.Redo() {
			t.Fatal("redo failed")
		}
	}
	if got := doc.Zones(); len(got) != 1 || got[0] != moved {
		t.Fatalf("after redo zones = %+v", got)
	}
}

func TestDeletedZonesReturnToTheirPlaceOnUndo(t *testing.T) {
	doc := openZoneLevel(t, zoneLevel)
	for range 3 {
		if _, err := doc.AddZone(deck); err != nil {
			t.Fatal(err)
		}
	}
	if err := doc.DeleteZone(2); err != nil {
		t.Fatal(err)
	}
	if !doc.Undo() {
		t.Fatal("undo failed")
	}
	zones := doc.Zones()
	ids := make([]uint32, 0, len(zones))
	for _, z := range zones {
		ids = append(ids, z.ID)
	}
	if !slices.Equal(ids, []uint32{1, 2, 3}) {
		t.Fatalf("zone order after undo = %v, want [1 2 3]", ids)
	}
	// New IDs follow the largest, so deleting the last one lets its ID return.
	if err := doc.DeleteZone(3); err != nil {
		t.Fatal(err)
	}
	if id, err := doc.AddZone(deck); err != nil || id != 3 {
		t.Fatalf("AddZone after deleting the last zone = %d, %v", id, err)
	}
}

func TestInvalidZoneEditsAreRejectedAndLeaveNoHistory(t *testing.T) {
	doc := openZoneLevel(t, zoneLevel)
	if _, err := doc.AddZone(deck); err != nil {
		t.Fatal(err)
	}
	before := doc.Zones()
	bad := deck
	bad.W = 9 // Past the right edge of a four-cell board.
	for name, err := range map[string]error{
		"add outside":   func() error { _, err := doc.AddZone(bad); return err }(),
		"add bad kind":  func() error { z := deck; z.Kind = "Deck"; _, err := doc.AddZone(z); return err }(),
		"edit outside":  doc.EditZone(level.Zone{ID: 1, Kind: "deck", Role: level.RoleSolid, W: 9, H: 1, Top: 8}),
		"edit missing":  doc.EditZone(level.Zone{ID: 5, Kind: "deck", Role: level.RoleSolid, W: 1, H: 1, Top: 8}),
		"delete absent": doc.DeleteZone(5),
	} {
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if !slices.Equal(doc.Zones(), before) {
		t.Fatal("a rejected edit changed the zones")
	}
	// An identical edit is a no-op and enters no history: one undo reaches the start.
	if err := doc.EditZone(before[0]); err != nil {
		t.Fatal(err)
	}
	if !doc.Undo() || doc.CanUndo() {
		t.Fatal("rejected and identical edits must not enter the history")
	}
}

func TestZoneEditsWaitForTheStrokeAndTheLimitHolds(t *testing.T) {
	doc := openZoneLevel(t, zoneLevel)
	if err := doc.BeginStroke(leveleditor.Wall); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddZone(deck); err == nil {
		t.Error("a zone edit during a stroke must be rejected")
	}
	doc.EndStroke(false)

	small := level.Zone{Kind: "deck", Role: level.RoleSolid, W: 1, H: 1, Top: 8}
	for range level.MaxZones {
		if _, err := doc.AddZone(small); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := doc.AddZone(small); err == nil {
		t.Error("a level holds at most 1,024 zones")
	}
	var unopened leveleditor.Document
	if _, err := unopened.AddZone(small); err == nil {
		t.Error("an unopened document must reject zone edits")
	}
}

func TestOpenKeepsZonesTheEditorDoesNotTouch(t *testing.T) {
	source := `{"version":2,"rows":["...."],"placements":[],"zones":[` +
		`{"id":9,"kind":"door","role":"trigger","x":2,"y":0,"w":1,"h":1,"bottom":0,"top":20}]}`
	doc := openZoneLevel(t, source)
	stroke(t, doc, leveleditor.Wall, [][2]int{{0, 0}}, true)
	data := export(t, doc)
	board, err := level.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	zones := slices.Collect(board.Zones())
	if len(zones) != 1 || zones[0].ID != 9 || board.Walkable(0, 0) {
		t.Fatalf("terrain edit lost the zone or the wall: %+v", zones)
	}
	if id, err := doc.AddZone(
		level.Zone{Kind: "deck", Role: level.RoleSolid, W: 1, H: 1, Top: 8},
	); err != nil ||
		id != 10 {
		t.Fatalf("new zone ID = %d, %v; want 10", id, err)
	}
}
