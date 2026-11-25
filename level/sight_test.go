package level_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine/vision"
	"github.com/krapht-foundation/goje/level"
)

// The canonical encoding of a version 3 level with a hedge and an unrelated trigger.
const validV3 = "{\n  \"version\": 3,\n  \"rows\": [\n    \"....\",\n    \"..#.\"\n  ],\n" +
	"  \"placements\": [\n    {\"id\": 1, \"kind\": \"scout\", \"x\": 0, \"y\": 0}\n  ],\n" +
	"  \"zones\": [\n" +
	"    {\"id\": 1, \"kind\": \"sight_block\", \"role\": \"trigger\", \"x\": 1, \"y\": 0, \"w\": 1, \"h\": 2, \"bottom\": 0, \"top\": 32},\n" +
	"    {\"id\": 2, \"kind\": \"door\", \"role\": \"trigger\", \"x\": 3, \"y\": 0, \"w\": 1, \"h\": 1, \"bottom\": 0, \"top\": 32}\n" +
	"  ],\n  \"sight\": {\"radius\": 4}\n}\n"

func TestDecodeVersion3Sight(t *testing.T) {
	board, err := level.Decode(strings.NewReader(validV3))
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := board.Sight(); r != 4 || !ok {
		t.Fatalf("Sight() = %d, %v, want 4, true", r, ok)
	}
	var blocked []int
	for y := range 2 {
		for x := range 4 {
			if board.SightBlocked(x, y) {
				blocked = append(blocked, y*4+x)
			}
		}
	}
	// Only the sight_block zone blocks: not the door trigger and not the wall.
	if want := []int{1, 5}; !slices.Equal(blocked, want) {
		t.Fatalf("blocked cells = %v, want %v", blocked, want)
	}
	if board.SightBlocked(-1, 0) || board.SightBlocked(1, 2) {
		t.Error("a cell outside the board must not be blocked")
	}
	if !board.Walkable(1, 0) {
		t.Error("a sight_block zone must not change the terrain under it")
	}

	// Without a sight object, sight_block is an ordinary kind with no meaning.
	v2, err := level.Decode(strings.NewReader(
		`{"version":2,"rows":[".."],"placements":[],"zones":[{"id":1,"kind":"sight_block","role":"solid","x":0,"y":0,"w":1,"h":1,"bottom":0,"top":32}]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := v2.Sight(); r != 0 || ok || v2.SightBlocked(0, 0) {
		t.Fatalf("version 2 sight = %d, %v, blocked %v; want none", r, ok, v2.SightBlocked(0, 0))
	}
}

func TestDecodeRejectsInvalidSight(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement, diagnostic string }{
		{"missing sight", `,
  "sight": {"radius": 4}`, ``, `missing field "sight"`},
		{"sight in version 2", `"version": 3`, `"version": 2`, "sight needs version 3"},
		{"sight in version 1", `"version": 3`, `"version": 1`, "zones need version 2"},
		{"misspelled zones", `"zones": [`, `"xzones": [`, `unknown field "xzones"`},
		{"sight without radius", `{"radius": 4}`, `{}`, `missing field "radius"`},
		{"null sight", `{"radius": 4}`, `null`, "expected"},
		{"unknown sight field", `{"radius": 4}`, `{"radius": 4, "range": 2}`, `unknown field "range"`},
		{"duplicate sight field", `"sight": {`, `"sight": {"radius": 3}, "sight": {`, `duplicate field "sight"`},
		{"duplicate radius", `{"radius": 4}`, `{"radius": 4, "radius": 4}`, `duplicate field "radius"`},
		{"zero radius", `"radius": 4`, `"radius": 0`, "sight radius 0 must be 1–181"},
		{"radius above the limit", `"radius": 4`, `"radius": 182`, "sight radius 182 must be 1–181"},
		{"radius beyond field width", `"radius": 4`, `"radius": 256`, "unsigned"},
		{"negative radius", `"radius": 4`, `"radius": -1`, "unsigned"},
		{"fractional radius", `"radius": 4`, `"radius": 4.5`, "unsigned"},
		{"solid sight_block", `"sight_block", "role": "trigger"`, `"sight_block", "role": "solid"`, `must have role "trigger"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Replace(validV3, tc.old, tc.replacement, 1)
			if input == validV3 {
				t.Fatal("the case does not change the document")
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

func TestEncodeSight(t *testing.T) {
	board, err := level.Decode(strings.NewReader(validV3))
	if err != nil {
		t.Fatal(err)
	}
	rows, placements, zones := rowsOf(
		board,
	), slices.Collect(
		board.Placements(),
	), slices.Collect(
		board.Zones(),
	)
	data, err := level.EncodeSight(rows, placements, zones, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != validV3 {
		t.Fatalf("version 3 bytes =\n%s\nwant\n%s", data, validV3)
	}

	// A radius of 0 is Encode: versions 1 and 2 keep their bytes.
	for _, z := range [][]level.Zone{nil, zones} {
		want, encodeErr := level.Encode(rows, placements, z)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		got, encodeErr := level.EncodeSight(rows, placements, z, 0)
		if encodeErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("EncodeSight with radius 0 = %q, %v; want %q", got, encodeErr, want)
		}
	}

	// A version 3 level without zones still writes the array its decoder requires.
	empty, err := level.EncodeSight([]string{"."}, nil, nil, vision.MaxRadius)
	if err != nil {
		t.Fatal(err)
	}
	const wantEmpty = "{\n  \"version\": 3,\n  \"rows\": [\n    \".\"\n  ],\n  \"placements\": [],\n" +
		"  \"zones\": [],\n  \"sight\": {\"radius\": 181}\n}\n"
	if string(empty) != wantEmpty {
		t.Fatalf("empty version 3 bytes =\n%s\nwant\n%s", empty, wantEmpty)
	}
	if b, err := level.Decode(bytes.NewReader(empty)); err != nil {
		t.Fatal(err)
	} else if r, _ := b.Sight(); r != vision.MaxRadius {
		t.Fatalf("radius = %d after a round trip", r)
	}

	solid := slices.Clone(zones)
	solid[0].Role = level.RoleSolid
	for _, tc := range []struct {
		name   string
		zones  []level.Zone
		radius int
	}{
		{"negative radius", zones, -1},
		{"radius above the limit", zones, vision.MaxRadius + 1},
		{"solid sight_block", solid, 4},
	} {
		if data, err := level.EncodeSight(
			rows,
			placements,
			tc.zones,
			tc.radius,
		); err == nil ||
			data != nil {
			t.Errorf("%s: EncodeSight = %q, %v; want an error and no bytes", tc.name, data, err)
		}
	}
	// The role rule belongs to version 3; version 2 keeps accepting the zone.
	if _, err := level.Encode(rows, placements, solid); err != nil {
		t.Errorf("version 2 with a solid sight_block zone: %v", err)
	}
}

func TestLargestVersion3LevelFitsTheByteLimit(t *testing.T) {
	rows := make([]string, 128)
	for y := range rows {
		rows[y] = strings.Repeat(".", 128)
	}
	placements := make([]level.Placement, 1024)
	zones := make([]level.Zone, 1024)
	for i := range placements {
		placements[i] = level.Placement{
			ID:   ^uint32(0) - uint32(i),
			Kind: strings.Repeat("k", 32),
			X:    i % 128,
			Y:    i / 128,
		}
		zones[i] = level.Zone{
			ID: ^uint32(0) - uint32(i), Kind: strings.Repeat("z", 32), Role: level.RoleTrigger,
			X: 127, Y: 127, W: 1, H: 1, Bottom: 1000, Top: 1024,
		}
	}
	data, err := level.EncodeSight(rows, placements, zones, vision.MaxRadius)
	if err != nil {
		t.Fatal(err)
	}
	board, err := level.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := board.Sight(); r != vision.MaxRadius || !ok ||
		!slices.Equal(slices.Collect(board.Zones()), zones) {
		t.Fatal("maximum-size version 3 export lost its sight or zones")
	}
}
