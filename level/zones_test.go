package level_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/level"
)

const validV2 = `{"version":2,"rows":[".#.","..."],"placements":[{"id":1,"kind":"scout","x":0,"y":1}],` +
	`"zones":[{"id":7,"kind":"deck","role":"solid","x":0,"y":0,"w":3,"h":2,"bottom":24,"top":32},` +
	`{"id":2,"kind":"door","role":"trigger","x":2,"y":0,"w":1,"h":1,"bottom":0,"top":20}]}`

func TestDecodeVersion2Zones(t *testing.T) {
	board, err := level.Decode(strings.NewReader(validV2))
	if err != nil {
		t.Fatal(err)
	}
	want := []level.Zone{
		{ID: 7, Kind: "deck", Role: level.RoleSolid, X: 0, Y: 0, W: 3, H: 2, Bottom: 24, Top: 32},
		{ID: 2, Kind: "door", Role: level.RoleTrigger, X: 2, Y: 0, W: 1, H: 1, Bottom: 0, Top: 20},
	}
	if got := slices.Collect(board.Zones()); !slices.Equal(got, want) {
		t.Fatalf("zones = %+v, want %+v in document order", got, want)
	}
	// A zone may cover walls and placements, and zones may overlap.
	if board.Walkable(1, 0) {
		t.Error("a zone must not change the terrain under it")
	}
	v1, err := level.Decode(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Collect(v1.Zones()); len(got) != 0 {
		t.Errorf("version 1 zones = %+v, want none", got)
	}
}

func TestDecodeRejectsInvalidZones(t *testing.T) {
	zone := `{"id":7,"kind":"deck","role":"solid","x":0,"y":0,"w":3,"h":2,"bottom":24,"top":32}`
	head, _, _ := strings.Cut(validV2, `,"zones"`)
	for _, tc := range []struct{ name, old, replacement, diagnostic string }{
		{"version 1 with zones", `"version":2`, `"version":1`, "zones need version 2"},
		{"misspelled zones field", `"zones":[`, `"xzones":[`, `unknown field "xzones"`},
		{"missing zones", validV2, head + `}`, `missing field "zones"`},
		{"version 4", `"version":2`, `"version":4`, "unsupported level version 4 (want 1, 2, or 3)"},
		{"duplicate zones field", `"zones":[`, `"zones":[],"zones":[`, `duplicate field "zones"`},
		{"null zones", validV2, head + `,"zones":null}`, "expected"},
		{"missing zone field", `"role":"solid",`, ``, `missing field "role"`},
		{"unknown zone field", `"kind":"deck",`, `"kind":"deck","color":"red",`, `unknown field "color"`},
		{"duplicate zone field", `"kind":"deck",`, `"kind":"deck","kind":"x",`, `duplicate field "kind"`},
		{"zero ID", `"id":7`, `"id":0`, "must be positive"},
		{"duplicate ID", `"id":2`, `"id":7`, "duplicate zone ID 7"},
		{"oversized ID", `"id":7`, `"id":4294967296`, "32-bit integer"},
		{"invalid kind", `"deck"`, `"Deck"`, "invalid kind"},
		{"invalid role", `"solid"`, `"wall"`, "role must be"},
		{"role must be text", `"role":"solid"`, `"role":1`, "expected a string"},
		{"empty width", `"w":3`, `"w":0`, "not inside"},
		{"past right edge", `"w":3`, `"w":4`, "not inside"},
		{"past bottom edge", `"h":2`, `"h":3`, "not inside"},
		{"negative x", `"x":0,"y":0,"w":3`, `"x":-1,"y":0,"w":3`, "unsigned"},
		{"fractional height", `"top":32`, `"top":32.5`, "unsigned"},
		{"exponent", `"bottom":24`, `"bottom":2.4e1`, "unsigned"},
		{"empty extent", `"top":32`, `"top":24`, "vertical extent"},
		{"top beyond limit", `"top":32`, `"top":1025`, "vertical extent"},
		{"top beyond field width", `"top":32`, `"top":2048`, "unsigned"},
		{"too many zones", `"zones":[`, `"zones":[` + strings.Repeat(zone+",", 1024), "at most 1024 zones"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Replace(validV2, tc.old, tc.replacement, 1)
			if tc.old == validV2 {
				input = tc.replacement // The case replaces the whole document.
			}
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

func rowsOf(board level.Board) []string {
	w, h := board.Size()
	rows := make([]string, h)
	for y := range rows {
		var row strings.Builder
		for x := range w {
			if board.Walkable(x, y) {
				row.WriteByte('.')
			} else {
				row.WriteByte('#')
			}
		}
		rows[y] = row.String()
	}
	return rows
}

func TestEncodeWritesTheLowestVersionThatFits(t *testing.T) {
	// No zones: byte-identical to what every earlier tool wrote.
	v1, err := level.Encode([]string{".#."}, []level.Placement{{ID: 3, Kind: "scout"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const wantV1 = "{\n  \"version\": 1,\n  \"rows\": [\n    \".#.\"\n  ],\n" +
		"  \"placements\": [\n    {\"id\": 3, \"kind\": \"scout\", \"x\": 0, \"y\": 0}\n  ]\n}\n"
	if string(v1) != wantV1 {
		t.Errorf("version 1 bytes =\n%s\nwant\n%s", v1, wantV1)
	}

	board, err := level.Decode(strings.NewReader(validV2))
	if err != nil {
		t.Fatal(err)
	}
	v2, err := level.Encode(
		rowsOf(board),
		slices.Collect(board.Placements()),
		slices.Collect(board.Zones()),
	)
	if err != nil {
		t.Fatal(err)
	}
	const wantV2 = "{\n  \"version\": 2,\n  \"rows\": [\n    \".#.\",\n    \"...\"\n  ],\n" +
		"  \"placements\": [\n    {\"id\": 1, \"kind\": \"scout\", \"x\": 0, \"y\": 1}\n  ],\n" +
		"  \"zones\": [\n" +
		"    {\"id\": 7, \"kind\": \"deck\", \"role\": \"solid\", \"x\": 0, \"y\": 0, \"w\": 3, \"h\": 2, \"bottom\": 24, \"top\": 32},\n" +
		"    {\"id\": 2, \"kind\": \"door\", \"role\": \"trigger\", \"x\": 2, \"y\": 0, \"w\": 1, \"h\": 1, \"bottom\": 0, \"top\": 20}\n" +
		"  ]\n}\n"
	if string(v2) != wantV2 {
		t.Errorf("version 2 bytes =\n%s\nwant\n%s", v2, wantV2)
	}
	// Re-encoding the decoded canonical bytes is a fixed point.
	again, err := level.Decode(bytes.NewReader(v2))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := level.Encode(
		rowsOf(again),
		slices.Collect(again.Placements()),
		slices.Collect(again.Zones()),
	); !bytes.Equal(
		got,
		v2,
	) {
		t.Error("encoding a decoded version 2 level changed its bytes")
	}
	// An empty zone list is still version 1, so no feature is claimed.
	empty, err := level.Encode([]string{"."}, nil, []level.Zone{})
	if err != nil || !strings.Contains(string(empty), `"version": 1`) {
		t.Errorf("empty zone list: %q, %v", empty, err)
	}
}

func TestEncodeRejectsInvalidZones(t *testing.T) {
	ok := level.Zone{ID: 1, Kind: "deck", Role: level.RoleSolid, W: 1, H: 1, Top: 8}
	for _, tc := range []struct {
		name  string
		zones []level.Zone
	}{
		{"zero ID", []level.Zone{{Kind: "deck", Role: level.RoleSolid, W: 1, H: 1, Top: 8}}},
		{"duplicate ID", []level.Zone{ok, ok}},
		{"bad role", []level.Zone{{ID: 1, Kind: "deck", Role: "wall", W: 1, H: 1, Top: 8}}},
		{"outside", []level.Zone{{ID: 1, Kind: "deck", Role: level.RoleSolid, X: 1, W: 1, H: 1, Top: 8}}},
		{"empty extent", []level.Zone{{ID: 1, Kind: "deck", Role: level.RoleSolid, W: 1, H: 1, Bottom: 8, Top: 8}}},
		{"too many", make([]level.Zone, 1025)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if data, err := level.Encode([]string{"."}, nil, tc.zones); err == nil || data != nil {
				t.Fatalf("invalid zones returned %q and error %v", data, err)
			}
		})
	}
}

func TestLargestVersion2LevelFitsTheByteLimit(t *testing.T) {
	rows := make([]string, 128)
	for y := range rows {
		rows[y] = strings.Repeat(".", 128)
	}
	placements := make([]level.Placement, 1024)
	for i := range placements {
		placements[i] = level.Placement{
			ID:   ^uint32(0) - uint32(i),
			Kind: strings.Repeat("k", 32),
			X:    i % 128,
			Y:    i / 128,
		}
	}
	zones := make([]level.Zone, 1024)
	for i := range zones {
		zones[i] = level.Zone{
			ID: ^uint32(0) - uint32(i), Kind: strings.Repeat("z", 32), Role: level.RoleTrigger,
			X: 127, Y: 127, W: 1, H: 1, Bottom: 1000, Top: 1024,
		}
	}
	data, err := level.Encode(rows, placements, zones)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > level.MaxBytes {
		t.Fatalf("encoded %d bytes, above the limit", len(data))
	}
	board, err := level.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(slices.Collect(board.Zones()), zones) {
		t.Fatal("maximum-size export lost or reordered zones")
	}
}

func FuzzDecodeZones(f *testing.F) {
	f.Add(validV2)
	f.Add(`{"version":2,"rows":["."],"placements":[],"zones":[]}`)
	f.Add(
		`{"version":2,"rows":["."],"placements":[],"zones":[{"id":1,"kind":"a","role":"trigger","x":0,"y":0,"w":1,"h":1,"bottom":0,"top":1}]}`,
	)
	f.Add(validV3)
	f.Add(`{"version":3,"rows":["."],"placements":[],"zones":[],"sight":{"radius":181}}`)
	f.Fuzz(func(t *testing.T, input string) {
		board, err := level.Decode(strings.NewReader(input))
		if err != nil {
			return
		}
		w, h := board.Size()
		ids := make(map[uint32]bool)
		for z := range board.Zones() {
			if ids[z.ID] || z.Check(w, h) != nil {
				t.Fatalf("accepted invalid zone: %+v on %d×%d", z, w, h)
			}
			ids[z.ID] = true
		}
		// Anything the decoder accepts, the encoder must write and the decoder read back unchanged.
		radius, _ := board.Sight()
		data, err := level.EncodeSight(
			rowsOf(board),
			slices.Collect(board.Placements()),
			slices.Collect(board.Zones()),
			radius,
		)
		if err != nil {
			t.Fatalf("encode of an accepted level: %v", err)
		}
		again, err := level.Decode(bytes.NewReader(data))
		if err != nil || !slices.Equal(slices.Collect(again.Zones()), slices.Collect(board.Zones())) {
			t.Fatalf("round trip changed zones: %v", err)
		}
		if r, ok := again.Sight(); r != radius || ok != (radius != 0) {
			t.Fatalf("round trip changed sight %d to %d", radius, r)
		}
	})
}
