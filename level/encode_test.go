package level_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/level"
)

func TestEncodeLargestBoard(t *testing.T) {
	rows := make([]string, 128)
	for y := range rows {
		rows[y] = strings.Repeat(".", 128)
	}
	placements := make([]level.Placement, 1024)
	for i := range placements {
		placements[i] = level.Placement{
			ID: ^uint32(0) - uint32(i), Kind: strings.Repeat("k", 32), X: 120 + i%8, Y: i / 8,
		}
	}
	data, err := level.Encode(rows, placements, nil)
	if err != nil {
		t.Fatal(err)
	}
	board, err := level.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := board.Size(); w != 128 || h != 128 ||
		!slices.Equal(slices.Collect(board.Placements()), placements) {
		t.Fatal("maximum-size export lost geometry or reordered placements")
	}
}

func TestEncodeRejectsInvalidContent(t *testing.T) {
	p := level.Placement{ID: 7, Kind: "scout"}
	for _, tc := range []struct {
		name       string
		rows       []string
		placements []level.Placement
	}{
		{"no rows", nil, nil},
		{"ragged rows", []string{"..", "."}, nil},
		{"invalid terrain", []string{`\"`}, nil},
		{"too many placements", []string{"."}, make([]level.Placement, 1025)},
		{"invalid id", []string{"."}, []level.Placement{{Kind: "scout"}}},
		{"invalid kind", []string{"."}, []level.Placement{{ID: 7, Kind: `bad"name`}}},
		{"negative cell", []string{"."}, []level.Placement{{ID: 7, Kind: "scout", X: -1}}},
		{"duplicate ID", []string{".."}, []level.Placement{p, {ID: 7, Kind: "scout", X: 1}}},
		{"occupied", []string{".."}, []level.Placement{p, {ID: 8, Kind: "scout"}}},
		{"blocked", []string{"#"}, []level.Placement{p}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if data, err := level.Encode(tc.rows, tc.placements, nil); err == nil || data != nil {
				t.Fatalf("invalid level returned %q and error %v", data, err)
			}
		})
	}
}
