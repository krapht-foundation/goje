// Command levelcheck validates a Goje v1 level for the reference playground.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/krapht-foundation/goje/examples/courtyard"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: levelcheck LEVEL.json")
		os.Exit(2)
	}
	if err := check(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "levelcheck: %v\n", err)
		os.Exit(1)
	}
}

func check(path string) (err error) {
	// The caller explicitly chooses the one file to validate.
	file, err := os.Open(path) // #nosec G304 G703 -- User-selected file.
	if err != nil {
		return fmt.Errorf("open level: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	layout, err := courtyard.Decode(file)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	w, h := layout.Board.Size()
	_, err = fmt.Printf("%s: valid v1 level (%d × %d cells, 2 placements)\n", path, w, h)
	if err != nil {
		return fmt.Errorf("write validation result: %w", err)
	}
	return nil
}
