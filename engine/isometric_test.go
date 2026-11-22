package engine_test

import (
	"testing"

	"github.com/krapht-foundation/goje/engine"
)

func TestIsometricProject(t *testing.T) {
	projection := engine.Isometric{HalfTileWidth: 16, HalfTileHeight: 8}
	for _, tc := range []struct {
		name                    string
		x, y, elevation, sx, sy float64
	}{
		{name: "x axis", x: 1, sx: 16, sy: 8},
		{name: "y axis", y: 1, sx: -16, sy: 8},
		{name: "elevated off center", x: -1, y: 0.5, elevation: 12, sx: -24, sy: -16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y := projection.Project(tc.x, tc.y, tc.elevation)
			if x != tc.sx || y != tc.sy {
				t.Fatalf("Project() = (%v, %v); want (%v, %v)", x, y, tc.sx, tc.sy)
			}
		})
	}
}
