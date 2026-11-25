package leveleditor_test

import (
	"bytes"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/internal/leveleditor"
	"github.com/krapht-foundation/goje/level"
)

// A 4 × 3 board with a wall at (3, 2) and the courtyard's two placements.
const placementLevel = `{"version":1,"rows":["....","....","...#"],"placements":[` +
	`{"id":1,"kind":"scout","x":0,"y":0},{"id":2,"kind":"lookout","x":1,"y":0}]}`

func decodedPlacements(t *testing.T, doc *leveleditor.Document) []level.Placement {
	t.Helper()
	board, err := level.Decode(bytes.NewReader(export(t, doc)))
	if err != nil {
		t.Fatal(err)
	}
	return slices.Collect(board.Placements())
}

func TestPlacementsAreAddedRekindedDeletedAndUndone(t *testing.T) {
	doc := openZoneLevel(t, placementLevel)
	original := export(t, doc)

	id, err := doc.AddPlacement(level.Placement{Kind: "guard", X: 2, Y: 1})
	if err != nil || id != 3 {
		t.Fatalf("AddPlacement = %d, %v; want ID 3", id, err)
	}
	if id, err = doc.AddPlacement(
		level.Placement{ID: 40, Kind: "scout", X: 0, Y: 2},
	); err != nil ||
		id != 40 {
		t.Fatalf("AddPlacement with an authored ID = %d, %v; want 40", id, err)
	}
	if err := doc.SetPlacementKind(3, "scout"); err != nil {
		t.Fatal(err)
	}
	if err := doc.DeletePlacement(1); err != nil {
		t.Fatal(err)
	}
	// Moving the placement after the deleted one checks that occupancy follows the shifted order.
	if err := doc.Move(2, 1, 3, 1); err != nil {
		t.Fatal(err)
	}
	want := []level.Placement{
		{ID: 2, Kind: "lookout", X: 1, Y: 0},
		{ID: 3, Kind: "scout", X: 3, Y: 1},
		{ID: 40, Kind: "scout", X: 0, Y: 2},
	}
	if got := decodedPlacements(t, doc); !slices.Equal(got, want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
	if _, p := doc.Cell(0, 0); p.ID != 0 {
		t.Fatalf("deleted placement still occupies its cell: %+v", p)
	}
	edited := export(t, doc)

	for range 5 {
		if !doc.Undo() {
			t.Fatal("undo failed")
		}
	}
	if doc.Undo() || !bytes.Equal(export(t, doc), original) {
		t.Fatal("undoing every placement action must restore the level byte for byte")
	}
	if _, p := doc.Cell(0, 0); p.ID != 1 {
		t.Fatal("undoing a deletion must return the placement to its cell")
	}
	for range 5 {
		if !doc.Redo() {
			t.Fatal("redo failed")
		}
	}
	if !bytes.Equal(export(t, doc), edited) {
		t.Fatal("redo must reproduce the edited level byte for byte")
	}
}

func TestInvalidPlacementEditsAreRejectedAndLeaveNoHistory(t *testing.T) {
	doc := openZoneLevel(t, placementLevel)
	original := export(t, doc)
	add := func(p level.Placement) error { _, err := doc.AddPlacement(p); return err }
	for name, err := range map[string]error{
		"occupied cell":   add(level.Placement{Kind: "guard", X: 1, Y: 0}),
		"wall cell":       add(level.Placement{Kind: "guard", X: 3, Y: 2}),
		"outside":         add(level.Placement{Kind: "guard", X: 4, Y: 0}),
		"negative cell":   add(level.Placement{Kind: "guard", X: -1, Y: 0}),
		"duplicate ID":    add(level.Placement{ID: 2, Kind: "guard", X: 2, Y: 2}),
		"empty kind":      add(level.Placement{Kind: "", X: 2, Y: 2}),
		"capital kind":    add(level.Placement{Kind: "Guard", X: 2, Y: 2}),
		"digit first":     add(level.Placement{Kind: "1guard", X: 2, Y: 2}),
		"punctuation":     add(level.Placement{Kind: "guard-1", X: 2, Y: 2}),
		"long kind":       add(level.Placement{Kind: strings.Repeat("g", 33), X: 2, Y: 2}),
		"rekind invalid":  doc.SetPlacementKind(1, "Scout"),
		"rekind missing":  doc.SetPlacementKind(9, "scout"),
		"delete missing":  doc.DeletePlacement(9),
		"delete ID zero":  doc.DeletePlacement(0),
		"rekind ID zero":  doc.SetPlacementKind(0, "scout"),
		"rekind too long": doc.SetPlacementKind(2, strings.Repeat("g", 33)),
	} {
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	// The longest valid kind and an identical kind are accepted; the second is a no-op.
	if err := add(
		level.Placement{Kind: "a" + strings.Repeat("_9", 15) + "z", X: 2, Y: 2},
	); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetPlacementKind(1, "scout"); err != nil {
		t.Fatal(err)
	}
	if !doc.Undo() || doc.CanUndo() || !bytes.Equal(export(t, doc), original) {
		t.Fatal("rejected and identical edits must not enter the history")
	}

	if err := doc.BeginStroke(leveleditor.Wall); err != nil {
		t.Fatal(err)
	}
	if add(level.Placement{Kind: "guard", X: 2, Y: 2}) == nil || doc.DeletePlacement(1) == nil ||
		doc.SetPlacementKind(1, "guard") == nil {
		t.Error("placement edits during a stroke must be rejected")
	}
	doc.EndStroke(false)
	var unopened leveleditor.Document
	if _, err := unopened.AddPlacement(level.Placement{Kind: "guard"}); err == nil {
		t.Error("an unopened document must reject placement edits")
	}
}

func TestPlacementIDsFollowTheLargest(t *testing.T) {
	doc := openZoneLevel(t, placementLevel)
	if id, err := doc.AddPlacement(
		level.Placement{ID: 7, Kind: "guard", X: 2, Y: 0},
	); err != nil ||
		id != 7 {
		t.Fatalf("authored ID = %d, %v", id, err)
	}
	if id, err := doc.AddPlacement(
		level.Placement{Kind: "guard", X: 3, Y: 0},
	); err != nil ||
		id != 8 {
		t.Fatalf("next ID = %d, %v; want 8", id, err)
	}
	// IDs below the largest stay free for authors, and deleting the largest lets its ID return.
	if id, err := doc.AddPlacement(
		level.Placement{ID: 5, Kind: "guard", X: 0, Y: 1},
	); err != nil ||
		id != 5 {
		t.Fatalf("authored gap ID = %d, %v", id, err)
	}
	if err := doc.DeletePlacement(8); err != nil {
		t.Fatal(err)
	}
	if id, err := doc.AddPlacement(
		level.Placement{Kind: "guard", X: 3, Y: 0},
	); err != nil ||
		id != 8 {
		t.Fatalf("ID after deleting the largest = %d, %v; want 8", id, err)
	}
	if _, err := doc.AddPlacement(
		level.Placement{ID: math.MaxUint32, Kind: "guard", X: 1, Y: 1},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.AddPlacement(level.Placement{Kind: "guard", X: 2, Y: 1}); err == nil {
		t.Fatal("no ID is above 4,294,967,295")
	}
	if id, err := doc.AddPlacement(
		level.Placement{ID: 6, Kind: "guard", X: 2, Y: 1},
	); err != nil ||
		id != 6 {
		t.Fatalf("an authored ID must still work at the top of the range: %d, %v", id, err)
	}
	if got := decodedPlacements(t, doc); len(got) != 7 || got[5].ID != math.MaxUint32 {
		t.Fatalf("placements did not round trip: %+v", got)
	}
}

func TestPlacementLimitHolds(t *testing.T) {
	rows := make([]string, 32)
	for y := range rows {
		rows[y] = strings.Repeat(".", 33)
	}
	data, err := level.Encode(rows, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc leveleditor.Document
	if err := doc.Open(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	for i := range level.MaxPlacements {
		if _, err := doc.AddPlacement(
			level.Placement{Kind: "guard", X: i % 33, Y: i / 33},
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := doc.AddPlacement(level.Placement{Kind: "guard", X: 32, Y: 31}); err == nil {
		t.Fatal("a level holds at most 1,024 placements")
	}
	if got := decodedPlacements(t, &doc); len(got) != level.MaxPlacements || got[1023].ID != 1024 {
		t.Fatalf("exported %d placements", len(got))
	}
}

// TestRandomPlacementHistory applies seeded random edits, undos, and redos and
// checks every state against the export it had when it was first reached.
func TestRandomPlacementHistory(t *testing.T) {
	const side = 6
	rows := []string{"......", "..#...", "......", "...#..", "......", "......"}
	for seed := range uint64(8) {
		data, err := level.Encode(rows, []level.Placement{{ID: 1, Kind: "scout", X: 0, Y: 0}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var doc leveleditor.Document
		if err := doc.Open(bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewPCG(seed, 15))
		states := [][]byte{data} // states[pos] is the current export; earlier ones can be undone.
		pos := 0
		kinds := []string{"scout", "guard", "lookout"}
		for step := range 400 {
			placements := decodedPlacements(t, &doc)
			var some level.Placement
			if len(placements) != 0 {
				some = placements[rng.IntN(len(placements))]
			}
			var err error
			switch rng.IntN(6) {
			case 0:
				_, err = doc.AddPlacement(level.Placement{
					Kind: kinds[rng.IntN(len(kinds))], X: rng.IntN(side), Y: rng.IntN(side),
				})
			case 1:
				err = doc.DeletePlacement(some.ID)
			case 2:
				err = doc.SetPlacementKind(some.ID, kinds[rng.IntN(len(kinds))])
			case 3:
				err = doc.Move(some.X, some.Y, rng.IntN(side), rng.IntN(side))
			case 4:
				if doc.Undo() != (pos > 0) {
					t.Fatalf("seed %d step %d: undo availability is wrong", seed, step)
				}
				pos = max(0, pos-1)
			default:
				if doc.Redo() != (pos < len(states)-1) {
					t.Fatalf("seed %d step %d: redo availability is wrong", seed, step)
				}
				pos = min(len(states)-1, pos+1)
			}
			current := export(t, &doc)
			if err == nil && !bytes.Equal(current, states[pos]) {
				// A committed edit replaces the redo branch, and the history keeps 64 actions.
				states = append(states[:pos+1], current)
				pos++
				if len(states) > 65 {
					states = states[1:]
					pos--
				}
			}
			if !bytes.Equal(current, states[pos]) {
				t.Fatalf("seed %d step %d: state differs from the one recorded for it", seed, step)
			}
			for _, p := range decodedPlacements(t, &doc) {
				if _, at := doc.Cell(p.X, p.Y); at != p {
					t.Fatalf(
						"seed %d step %d: cell (%d, %d) holds %+v, want %+v",
						seed,
						step,
						p.X,
						p.Y,
						at,
						p,
					)
				}
			}
		}
	}
}
