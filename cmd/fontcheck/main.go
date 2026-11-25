// Command fontcheck validates a game's PNG font sheet and JSON description.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/krapht-foundation/goje/font"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: fontcheck SHEET.png FONT.json")
		os.Exit(2)
	}
	if err := check(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "fontcheck: %v\n", err)
		os.Exit(1)
	}
}

func check(pngPath, jsonPath string) (err error) {
	pixels, err := os.Open(pngPath) // #nosec G304 G703 -- User-selected file.
	if err != nil {
		return fmt.Errorf("open PNG: %w", err)
	}
	defer func() { err = errors.Join(err, pixels.Close()) }()
	metadata, err := os.Open(jsonPath) // #nosec G304 G703 -- User-selected file.
	if err != nil {
		return fmt.Errorf("open font description: %w", err)
	}
	defer func() { err = errors.Join(err, metadata.Close()) }()
	face, err := font.Decode(pixels, metadata)
	if err != nil {
		return fmt.Errorf("validate %s and %s: %w", pngPath, jsonPath, err)
	}
	bounds := face.Image().Bounds()
	if _, err := fmt.Printf("%s and %s: valid font sheet (%d × %d pixels, line height %d)\n",
		pngPath, jsonPath, bounds.Dx(), bounds.Dy(), face.LineHeight()); err != nil {
		return fmt.Errorf("write validation result: %w", err)
	}
	return nil
}
