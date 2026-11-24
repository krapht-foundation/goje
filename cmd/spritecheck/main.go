// Command spritecheck validates a game's PNG sprite sheet and JSON clips.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/krapht-foundation/goje/sprite"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: spritecheck SHEET.png CLIPS.json")
		os.Exit(2)
	}
	if err := check(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintf(os.Stderr, "spritecheck: %v\n", err)
		os.Exit(1)
	}
}

func check(pngPath, jsonPath string) (err error) {
	pixels, err := os.Open(pngPath) // #nosec G304 G703 -- User-selected file.
	if err != nil {
		return fmt.Errorf("open PNG: %w", err)
	}
	defer func() { err = errors.Join(err, pixels.Close()) }()
	clips, err := os.Open(jsonPath) // #nosec G304 G703 -- User-selected file.
	if err != nil {
		return fmt.Errorf("open clips: %w", err)
	}
	defer func() { err = errors.Join(err, clips.Close()) }()
	sheet, err := sprite.Decode(pixels, clips)
	if err != nil {
		return fmt.Errorf("validate %s and %s: %w", pngPath, jsonPath, err)
	}
	bounds := sheet.Image().Bounds()
	if _, err := fmt.Printf("%s and %s: valid sprite sheet (%d × %d pixels)\n",
		pngPath, jsonPath, bounds.Dx(), bounds.Dy()); err != nil {
		return fmt.Errorf("write validation result: %w", err)
	}
	return nil
}
