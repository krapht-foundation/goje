package skirmish

import (
	"errors"
	"fmt"

	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/engine/vision"
)

// VisionConfig turns on line of sight. See EnableVision.
type VisionConfig struct {
	// Radius is how far a unit sees, in cells, from 1 to vision.MaxRadius. The
	// outpost uses 7.
	Radius int
}

// EnableVision turns on line of sight and fog of war for the rest of the
// simulation. It must be called before the first Step. Without it, every unit
// sees the whole board and the simulation runs as specs/skirmish-m13.md
// describes; with it, the rules of specs/vision-m15.md apply: only walls block
// sight, a unit fires at and an attack-move stops for an enemy only while that
// enemy stands in a cell its team sees, an attack needs its target in sight,
// and a guard wakes when its team sees a scout instead of by distance. The
// scouts' team remembers every cell it has seen; Explored reports it.
func (s *Sim) EnableVision(cfg VisionConfig) error {
	switch {
	case s.tick != 0:
		return errors.New("skirmish: vision must be enabled before the first Step")
	case s.radius != 0:
		return errors.New("skirmish: vision is already enabled")
	case cfg.Radius < 1 || cfg.Radius > vision.MaxRadius:
		return fmt.Errorf("skirmish: sight radius %d: must be 1–%d", cfg.Radius, vision.MaxRadius)
	}
	blocked := func(x, y int) bool { return s.wall[y*s.width+x] }
	for i := range s.sight {
		f, err := vision.NewField(s.width, s.height, max(len(s.units), 1), cfg.Radius, blocked)
		if err != nil {
			return fmt.Errorf("skirmish: %w", err)
		}
		s.sight[i] = f
	}
	s.radius = cfg.Radius
	s.explored = make([]uint64, (s.width*s.height+63)/64)
	s.see()
	return nil
}

// Visible reports whether a unit of team sees cell. Without vision, every
// cell on the board is visible. Cells off the board, and any team other than
// Scouts and Guards, never are.
func (s *Sim) Visible(team Team, cell grid.Cell) bool {
	if !s.onBoard(cell) || (team != Scouts && team != Guards) {
		return false
	}
	return s.radius == 0 || s.sight[team-1].Visible(cell.X, cell.Y)
}

// Explored reports whether a scout has ever seen cell. Without vision, every
// cell on the board is explored. Cells off the board never are.
func (s *Sim) Explored(cell grid.Cell) bool {
	if !s.onBoard(cell) {
		return false
	}
	c := cell.Y*s.width + cell.X
	return s.radius == 0 || s.explored[c/64]&(1<<(c%64)) != 0
}

func (s *Sim) onBoard(c grid.Cell) bool {
	return c.X >= 0 && c.Y >= 0 && c.X < s.width && c.Y < s.height
}

// see brings the sight of both teams up to date with where the units stand: a
// living unit views from the cell that holds its center, and a removed one
// views nothing. A Field recasts only units whose cell changed. The scouts'
// explored set gains every cell a scout's view adds. Step calls it at the end
// of each tick, after the dead are removed, so the next tick's decisions and
// the orders given between ticks see the board as it stands.
func (s *Sim) see() {
	for i := range s.units {
		u := &s.units[i]
		f := s.sight[u.Team-1]
		if !u.Alive {
			f.Remove(i)
			continue
		}
		c := s.cellOf(u.Center())
		if f.Move(i, grid.Cell{X: c % s.width, Y: c / s.width}) && u.Team == Scouts {
			for _, c := range f.Cells(i) {
				s.explored[c/64] |= 1 << (c % 64)
			}
		}
	}
}

// sees reports whether u's team sees the cell that holds t's center. Without
// vision it is always true.
func (s *Sim) sees(u, t *Unit) bool {
	if s.radius == 0 {
		return true
	}
	c := s.cellOf(t.Center())
	return s.sight[u.Team-1].Visible(c%s.width, c/s.width)
}

// lose sends an attacking unit whose target is out of sight to the cell where
// it last saw the target. The order ends there, or at once when no route
// reaches that cell, and the repath rule ends a route that stays blocked.
func (s *Sim) lose(u *Unit) {
	if u.Moving() && u.goal == u.lastSeen {
		return
	}
	if !s.offCenter(u) && u.cell == u.lastSeen {
		s.stand(u)
		return
	}
	// A new route must not hide how long the unit has been stuck behind others.
	blocked, repaths := u.blocked, u.repaths
	s.mark(u, true)
	ok := s.order(u, u.lastSeen, false)
	s.unmark()
	if !ok {
		s.stand(u)
		return
	}
	u.blocked, u.repaths = blocked, repaths
}
