// Package courtyard defines the game-specific level rules for the Goje reference playground.
package courtyard

import (
	"errors"
	"fmt"
	"io"

	"github.com/krapht-foundation/goje/level"
)

// Layout contains validated terrain and the reference encounter's two spawns.
// The reference scene needs one movable scout and one stationary lookout.
type Layout struct {
	Board          level.Board
	Scout, Lookout level.Placement
}

// Decode loads the v1 board and checks that the reference scene can use it.
func Decode(r io.Reader) (Layout, error) {
	board, err := level.Decode(r)
	if err != nil {
		return Layout{}, fmt.Errorf("decode courtyard: %w", err)
	}
	layout := Layout{Board: board}
	for p := range board.Placements() {
		var spawn *level.Placement
		switch p.Kind {
		case "scout":
			spawn = &layout.Scout
		case "lookout":
			spawn = &layout.Lookout
		default:
			return Layout{}, fmt.Errorf("placement ID %d: unknown reference kind %q", p.ID, p.Kind)
		}
		if spawn.ID != 0 {
			return Layout{}, fmt.Errorf("placement ID %d: unexpected second %s", p.ID, p.Kind)
		}
		*spawn = p
	}
	if layout.Scout.ID == 0 || layout.Lookout.ID == 0 {
		return Layout{}, errors.New("the reference courtyard needs one scout and one lookout")
	}
	return layout, nil
}
