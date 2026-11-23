// Command tilecheck validates a ground tile PNG for the reference playground.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/krapht-foundation/goje/tile"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tilecheck TILE.png")
		os.Exit(2)
	}
	if err := check(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "tilecheck: %v\n", err)
		os.Exit(1)
	}
}

func check(path string) (err error) {
	file, err := os.Open(path) // #nosec G304 G703 -- User-selected file.
	if err != nil {
		return fmt.Errorf("open tile: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	pixels, err := tile.Decode(file)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	bounds := pixels.Bounds()
	_, err = fmt.Printf("%s: valid ground tile (%d × %d pixels)\n", path, bounds.Dx(), bounds.Dy())
	if err != nil {
		return fmt.Errorf("write validation result: %w", err)
	}
	return nil
}
