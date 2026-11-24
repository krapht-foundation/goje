package collidebench_test

import (
	"testing"

	"github.com/krapht-foundation/goje/internal/collidebench"
)

func TestSceneIsDeterministicAndKeepsBodiesOutOfSolids(t *testing.T) {
	t.Parallel()

	run := func() uint64 {
		scene, err := collidebench.New()
		if err != nil {
			t.Fatal(err)
		}
		// Step reports an error if any body ever overlaps a solid.
		for range 200 {
			if _, _, err := scene.Step(); err != nil {
				t.Fatal(err)
			}
		}
		return scene.Hash()
	}
	if first, second := run(), run(); first != second {
		t.Errorf("hashes differ between identical runs: %#x and %#x", first, second)
	}
}

func TestStepDoesNotAllocateOnceWarm(t *testing.T) {
	scene, err := collidebench.New()
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if _, _, err := scene.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if allocs := testing.AllocsPerRun(20, func() {
		if _, _, err := scene.Step(); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Errorf("Step allocated %v times per tick, want 0", allocs)
	}
}
