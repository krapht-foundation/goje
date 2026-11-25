package engine_test

import (
	"testing"

	"github.com/krapht-foundation/goje/engine"
)

func TestTransitionReplacement(t *testing.T) {
	if _, ok := (engine.Transition{}).Replacement(); ok {
		t.Error("zero transition reported a replacement")
	}
	if _, ok := engine.Restart().Replacement(); ok {
		t.Error("restart reported a replacement")
	}
	entered := false
	factory := func() engine.Scene {
		entered = true
		return nil
	}
	got, ok := engine.Replace(factory).Replacement()
	if !ok || got == nil {
		t.Fatal("replace did not report its factory")
	}
	got()
	if !entered {
		t.Error("replacement is not the factory passed to Replace")
	}
}
