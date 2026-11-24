package level_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/level"
)

const valid = `{"placements":[{"kind":"scout","id":4294967295,"x":2,"y":0},{"id":1,"kind":"a_2","x":0,"y":1}],"rows":[".#.","..."],"version":1}`

func TestDecodeBoard(t *testing.T) {
	board, err := level.Decode(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := board.Size(); w != 3 || h != 2 {
		t.Fatalf("board size = %d×%d, want 3×2", w, h)
	}
	if board.Walkable(1, 0) || board.Walkable(-1, 0) || board.Walkable(0, 2) ||
		!board.Walkable(2, 0) {
		t.Fatal("walkability should exclude walls and outside cells, not placements")
	}
	want := []level.Placement{{ID: 4294967295, Kind: "scout", X: 2}, {ID: 1, Kind: "a_2", Y: 1}}
	if got := slices.Collect(board.Placements()); !slices.Equal(got, want) {
		t.Fatalf("placements = %+v, want %+v", got, want)
	}
	opened, err := level.Decode(strings.NewReader(strings.Replace(valid, ".#.", "...", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Walkable(1, 0) || board.Walkable(1, 0) {
		t.Fatal("editing the authored row should open the cell only in the revised board")
	}
}

func TestDecodeRejectsInvalidLevels(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement, diagnostic string }{
		{"version", `"version":1`, `"version":3`, "unsupported level version 3"},
		{"duplicate root", `"version":1`, `"version":1,"vers\u0069on":1`, `duplicate field "version"`},
		{"missing field", `,"version":1`, ``, `missing field "version"`},
		{"unknown field", `"version":1`, `"version":1,"path":"file"`, `unknown field "path"`},
		{"second value", valid, valid + ` null`, "second JSON value"},
		{"trailing junk", valid, valid + ` !`, "trailing data"},
		{"ragged row", `".#."`, `".."`, "row 1"},
		{"illegal cell", `".#."`, `"é."`, "cell (0, 0)"},
		{"invalid UTF-8", `".#."`, "\".\xff.\"", "must be UTF-8"},
		{"nested row", `".#."`, `[[["..."]]]`, "expected a string"},
		{"empty rows", `[".#.","..."]`, `[]`, "at least one row"},
		{"null placements", strings.Split(valid, `,"rows"`)[0], `{"placements":null`, "placements"},
		{"excess rows", `[".#.","..."]`, `[` + strings.Repeat(`"...",`, 128) + `"..."]`, "at most 128 rows"},
		{"excess placements", strings.Split(valid, `,"rows"`)[0], `{"placements":[` + strings.Repeat(`{"id":1,"kind":"scout","x":0,"y":0},`, 1024) + `{ }]`, "at most 1024 placements"},
		{"duplicate placement field", `"id":1`, `"id":1,"id":2`, `duplicate field "id"`},
		{"duplicate ID", `"id":1`, `"id":4294967295`, "duplicate placement ID"},
		{"oversized ID", `4294967295`, `4294967296`, "32-bit integer"},
		{"zero ID", `4294967295`, `0`, "must be positive"},
		{"occupied", `"x":0,"y":1`, `"x":2,"y":0`, "occupied"},
		{"wall", `"x":2`, `"x":1`, "blocked"},
		{"outside", `"x":2`, `"x":3`, "outside the board"},
		{"exponent coordinate", `"x":2`, `"x":2e0`, "integer"},
		{"invalid kind", `"scout"`, `"../scout"`, "identifier"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Replace(valid, tc.old, tc.replacement, 1)
			board, err := level.Decode(strings.NewReader(input))
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Fatalf("error = %v, want %q", err, tc.diagnostic)
			}
			if w, h := board.Size(); w != 0 || h != 0 {
				t.Fatalf("error returned a partial board: %d×%d", w, h)
			}
		})
	}
}

func TestDecodeByteLimitIncludesWhitespace(t *testing.T) {
	const limit = 256 << 10
	atLimit := valid + strings.Repeat(" ", limit-len(valid))
	if _, err := level.Decode(strings.NewReader(atLimit)); err != nil {
		t.Fatalf("document at limit: %v", err)
	}
	_, err := level.Decode(strings.NewReader(atLimit + " "))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("document above limit: %v", err)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(valid)
	f.Add(`{"version":1,"rows":["."],"placements":[]}`)
	f.Add(`{"version":1,"rows":[[[null]]],"placements":[]}`)
	f.Fuzz(func(t *testing.T, input string) {
		board, err := level.Decode(strings.NewReader(input))
		if err != nil {
			return
		}
		w, h := board.Size()
		if w < 1 || w > 128 || h < 1 || h > 128 || len(input) > 256<<10 {
			t.Fatalf("accepted board with invalid bounds: %d×%d, %d bytes", w, h, len(input))
		}
		ids := make(map[uint32]bool)
		cells := make(map[[2]int]bool)
		for p := range board.Placements() {
			cell := [2]int{p.X, p.Y}
			if p.ID == 0 || ids[p.ID] || cells[cell] || !board.Walkable(p.X, p.Y) {
				t.Fatalf("accepted invalid placement: %+v", p)
			}
			ids[p.ID], cells[cell] = true, true
		}
	})
}
