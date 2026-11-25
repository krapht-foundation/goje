package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/krapht-foundation/goje/font"
)

func TestCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sheet := filepath.Join(dir, "font.png")
	file, err := os.Create(sheet) // #nosec G304 -- Test temporary file.
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, font.Builtin().Image()); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	valid := filepath.Join(dir, "font.json")
	invalid := filepath.Join(dir, "invalid.json")
	for path, data := range map[string]string{
		valid:   `{"version":1,"cellWidth":6,"cellHeight":8,"baseline":7,"first":32,"count":95}`,
		invalid: `{"version":1,"cellWidth":6,"cellHeight":8,"baseline":7,"first":32,"count":97}`,
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := check(sheet, valid); err != nil {
		t.Fatalf("the built-in sheet is invalid: %v", err)
	}
	if err := check(sheet, invalid); err == nil {
		t.Fatal("97 glyphs in 96 cells passed")
	}
	if err := check(filepath.Join(dir, "missing.png"), valid); err == nil {
		t.Fatal("a missing sheet passed")
	}
}
