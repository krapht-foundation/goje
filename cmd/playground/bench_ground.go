//go:build goje_bench && !goje_smoke

package main

import (
	"bytes"
	"strings"

	"github.com/krapht-foundation/goje/engine"
	courtyardlevel "github.com/krapht-foundation/goje/examples/courtyard"
	"github.com/krapht-foundation/goje/level"
)

const denseGroundCells = 128 * 128

// Both modes use the same board, camera and atlas. Only tile quads differ.
func groundBenchmarkFactory(
	project authoredProject,
	dense, tiles bool,
) (engine.SceneFactory, error) {
	if dense {
		data, err := denseGroundLevel()
		if err != nil {
			return nil, err
		}
		project.layout, err = courtyardlevel.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
	}
	return func() engine.Scene {
		scene := &authoredCourtyard{layout: project.layout, idle: project.idle, move: project.move}
		return &groundBenchmark{scene, tiles}
	}, nil
}

func denseGroundLevel() ([]byte, error) {
	var rows [128]string
	row := strings.Repeat(".", len(rows))
	for y := range rows {
		rows[y] = row
	}
	return level.Encode(rows[:], []level.Placement{
		{ID: 1, Kind: "scout", X: 63, Y: 63},
		{ID: 2, Kind: "lookout", X: 64, Y: 64},
	}, nil)
}

type groundBenchmark struct {
	*authoredCourtyard
	tiles bool
}

func (s *groundBenchmark) Enter() error {
	if err := s.authoredCourtyard.Enter(); err != nil {
		return err
	}
	if !s.tiles {
		s.ground = nil
	}
	return nil
}
