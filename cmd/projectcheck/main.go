//go:build !wasm

// Command projectcheck validates a Goje project without applying any game's rules.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/krapht-foundation/goje/project"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: projectcheck PROJECT_DIR")
		os.Exit(2)
	}
	if err := check(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "projectcheck: %v\n", err)
		os.Exit(1)
	}
}

func check(directory string) (err error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	loaded, err := project.Load(root.FS())
	if err != nil {
		return fmt.Errorf("%s: %w", directory, err)
	}
	width, height := loaded.Level.Size()
	bounds := loaded.Ground.Bounds()
	if _, err := fmt.Printf("%s: valid v1 project (level %d × %d cells, ground %d × %d pixels)\n",
		directory, width, height, bounds.Dx(), bounds.Dy()); err != nil {
		return fmt.Errorf("write validation result: %w", err)
	}
	return nil
}
