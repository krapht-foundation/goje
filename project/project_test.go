package project_test

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/krapht-foundation/goje/project"
)

//go:embed testdata/valid
var assets embed.FS

const validManifest = `{"version":1,"level":"levels/arena.json","ground":"art/stone.png"}`

func TestLoadEmbedded(t *testing.T) {
	root, err := fs.Sub(assets, "testdata/valid")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := project.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	width, height := loaded.Level.Size()
	if width != 2 || height != 2 || loaded.Level.Walkable(1, 0) {
		t.Fatalf("wrong level: %d × %d, wall walkable = %t", width, height, loaded.Level.Walkable(1, 0))
	}
	foundBeacon := false
	for p := range loaded.Level.Placements() {
		foundBeacon = p.Kind == "beacon" && p.X == 1 && p.Y == 1
	}
	if !foundBeacon {
		t.Fatal("game-defined beacon placement did not survive the project load")
	}
	red, green, blue, alpha := loaded.Ground.At(13, 5).RGBA()
	_, _, _, cornerAlpha := loaded.Ground.At(0, 0).RGBA()
	accent := [4]uint32{red >> 8, green >> 8, blue >> 8, alpha >> 8}
	if accent != [4]uint32{174, 92, 214, 255} || cornerAlpha != 0 {
		t.Fatalf("wrong ground pixels: accent %v, corner alpha %04x", accent, cornerAlpha)
	}
}

func TestRejectManifest(t *testing.T) {
	for name, tc := range map[string]struct{ input, diagnostic string }{
		"malformed":         {`{"version":1`, "goje.json"},
		"non-object":        {`[]`, "expected"},
		"over budget":       {validManifest + strings.Repeat(" ", 4097-len(validManifest)), "4096 bytes"},
		"non UTF-8":         {validManifest + "\xff", "UTF-8"},
		"trailing value":    {validManifest + ` {}`, "unexpected data"},
		"missing ground":    {`{"version":1,"level":"arena.json"}`, `missing field "ground"`},
		"duplicate version": {`{"version":1,"version":1}`, `duplicate field "version"`},
		"duplicate path":    {`{"level":"arena.json","\u006cevel":"other.json"}`, `duplicate field "level"`},
		"wrong-case key":    {`{"Version":1}`, `unknown field "Version"`},
		"unknown nested":    {`{"extra":{"version":1}}`, `unknown field "extra"`},
		"null path":         {`{"ground":null}`, "ground must be"},
		"nested path":       {`{"ground":[]}`, "ground must be"},
		"unknown version":   {`{"version":2}`, "unsupported project version 2"},
		"null version":      {`{"version":null}`, "version must be a JSON integer"},
		"fractional":        {`{"version":1.0}`, "version must be a JSON integer"},
		"exponent":          {`{"version":1e0}`, "version must be a JSON integer"},
	} {
		t.Run(name, func(t *testing.T) {
			assertRejected(t, fstest.MapFS{"goje.json": {Data: []byte(tc.input)}}, tc.diagnostic)
		})
	}
}

func TestRejectPaths(t *testing.T) {
	for _, name := range []string{
		"", "/arena.json", "../arena.json", "levels/./arena.json", "levels//arena.json", `levels\arena.json`,
		"c:/arena.json", "http://host/arena.json", "levels/%2e%2e/arena.json", "levels/arena.JSON", "Arena.json",
		"levels/arena.png", "levels/.json", "_levels/arena.json", "arena file.json", "lével.json", "con/arena.json",
		"levels/lpt9.json", strings.Repeat("x", 124) + ".json",
	} {
		t.Run(name, func(t *testing.T) {
			input := fmt.Sprintf(`{"version":1,"level":%q,"ground":"art/stone.png"}`, name)
			root := fstest.MapFS{"goje.json": {Data: []byte(input)}}
			assertRejected(t, root, "level must be a portable relative")
		})
	}
}

func TestRejectAssets(t *testing.T) {
	for name, tc := range map[string]struct {
		board, ground *fstest.MapFile
		diagnostic    string
	}{
		"missing level":    {nil, nil, `stat "levels/arena.json"`},
		"directory":        {&fstest.MapFile{Mode: fs.ModeDir}, nil, `"levels/arena.json" is not a regular file`},
		"named pipe":       {&fstest.MapFile{Mode: fs.ModeNamedPipe}, nil, `"levels/arena.json" is not a regular file`},
		"malformed level":  {&fstest.MapFile{Data: []byte(`{}`)}, nil, `decode "levels/arena.json"`},
		"oversized level":  {&fstest.MapFile{Data: make([]byte, 256<<10+1)}, nil, "level exceeds"},
		"missing ground":   {validBoard(), nil, `stat "art/stone.png"`},
		"malformed ground": {validBoard(), &fstest.MapFile{Data: []byte("not a PNG")}, `decode "art/stone.png"`},
		"oversized ground": {validBoard(), &fstest.MapFile{Data: make([]byte, 64<<10+1)}, "tile PNG exceeds"},
	} {
		t.Run(name, func(t *testing.T) {
			root := fstest.MapFS{"goje.json": {Data: []byte(validManifest)}}
			if tc.board != nil {
				root["levels/arena.json"] = tc.board
			}
			if tc.ground != nil {
				root["art/stone.png"] = tc.ground
			}
			assertRejected(t, root, tc.diagnostic)
		})
	}
}

func validBoard() *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(`{"version":1,"rows":["."],"placements":[]}`)}
}

func assertRejected(t *testing.T, root fs.FS, diagnostic string) {
	t.Helper()
	loaded, err := project.Load(root)
	if err == nil || !strings.Contains(err.Error(), diagnostic) {
		t.Fatalf("load error = %v, want %q", err, diagnostic)
	}
	if width, height := loaded.Level.Size(); width != 0 || height != 0 || loaded.Ground != nil {
		t.Fatal("load exposed a partial project")
	}
}
