package main

import (
	"testing"

	"github.com/krapht-foundation/goje/engine"
)

func TestCourtyardPressUsesStartingWorldAndCamera(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input engine.Input
	}{
		{
			name: "patrol starts on the same tick",
			input: engine.Input{
				Pressed: engine.KeySpace, PrimaryPress: engine.MousePress{X: 154.125, Y: 84, Valid: true},
			},
		},
		{
			name: "camera pans on the same tick",
			input: engine.Input{
				Held: engine.KeyArrowRight, PrimaryPress: engine.MousePress{X: 165.5, Y: 84, Valid: true},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newCourtyard().(*courtyard)
			if _, err := s.Update(engine.Tick{Input: tc.input}); err != nil {
				t.Fatal(err)
			}
			if s.selected != courtyardAmber {
				t.Fatalf("press on the starting amber unit selected %v", s.selected)
			}
			if _, err := s.Update(
				engine.Tick{Input: engine.Input{PrimaryPress: tc.input.PrimaryPress}},
			); err != nil {
				t.Fatal(err)
			}
			if s.selected != 0 {
				t.Fatalf("press at the same point after movement selected %v", s.selected)
			}
		})
	}
}
