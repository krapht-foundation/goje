// Package project pairs a Goje level and ground tile without owning a host or game rules.
package project

import (
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"

	"github.com/krapht-foundation/goje/level"
	"github.com/krapht-foundation/goje/tile"
)

const manifestName = "goje.json"

// Project holds the decoded level and ground tile. The caller owns the result.
type Project struct {
	Level  level.Board
	Ground image.Image
}

// Load reads a v1 project from a filesystem rooted at the directory containing
// goje.json. The filesystem must enforce its own root boundary. Load closes
// every file it opens and returns a zero Project if any file fails validation.
func Load(root fs.FS) (Project, error) {
	manifest, err := readRegular(root, manifestName, decodeManifest)
	if err != nil {
		return Project{}, err
	}
	board, err := readRegular(root, manifest.level, level.Decode)
	if err != nil {
		return Project{}, fmt.Errorf("level: %w", err)
	}
	ground, err := readRegular(root, manifest.ground, tile.Decode)
	if err != nil {
		return Project{}, fmt.Errorf("ground: %w", err)
	}
	return Project{Level: board, Ground: ground}, nil
}

func readRegular[T any](
	root fs.FS,
	name string,
	decode func(io.Reader) (T, error),
) (value T, err error) {
	// The native rooted filesystem can reject a stable FIFO or device before
	// Open, which may block or have side effects for those files. Immutable
	// filesystems such as embed.FS can check the opened file directly.
	if statFS, ok := root.(fs.StatFS); ok {
		info, statErr := statFS.Stat(name)
		if statErr != nil {
			return value, fmt.Errorf("stat %q: %w", name, statErr)
		}
		if !info.Mode().IsRegular() {
			return value, fmt.Errorf("%q is not a regular file", name)
		}
	}
	file, err := root.Open(name)
	if err != nil {
		return value, fmt.Errorf("open %q: %w", name, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %q: %w", name, closeErr))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return value, fmt.Errorf("stat opened %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return value, fmt.Errorf("%q is not a regular file", name)
	}
	value, err = decode(file)
	if err != nil {
		return value, fmt.Errorf("decode %q: %w", name, err)
	}
	return value, nil
}
