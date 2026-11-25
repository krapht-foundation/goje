package skirmishbench_test

import (
	"testing"

	"github.com/krapht-foundation/goje/internal/skirmishbench"
)

func TestBattleIsDeterministicAndNeverBreaksARule(t *testing.T) {
	t.Parallel()

	run := func() uint64 {
		scene, err := skirmishbench.New()
		if err != nil {
			t.Fatal(err)
		}
		// Step reports an error if two bodies ever overlap.
		for range 400 {
			if _, err := scene.Step(); err != nil {
				t.Fatal(err)
			}
		}
		return scene.Trace()
	}
	if first, second := run(), run(); first != second {
		t.Errorf("traces differ between identical runs: %#x and %#x", first, second)
	}
}

func TestStepDoesNotAllocateOnceTheCrowdHasSpread(t *testing.T) {
	scene, err := skirmishbench.New()
	if err != nil {
		t.Fatal(err)
	}
	for range 300 {
		if _, err := scene.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if allocs := testing.AllocsPerRun(50, func() {
		if _, err := scene.Step(); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Errorf("Step allocated %v times per tick, want 0", allocs)
	}
}

// wantTrace is the trace after the 900 ticks the benchmark program runs. Native,
// Node WebAssembly, and Chromium must all reproduce it; it changes only when the rules do.
const wantTrace uint64 = 1130702823205054636

func TestBattleTraceIsPinned(t *testing.T) {
	t.Parallel()

	report, err := skirmishbench.Run(300, 600)
	if err != nil {
		t.Fatal(err)
	}
	if report.Trace != wantTrace {
		t.Errorf("trace = %d, want %d", report.Trace, wantTrace)
	}
	if report.Scouts == 0 || report.Guards == 0 || report.Scouts+report.Guards >= report.Units {
		t.Errorf(
			"the battle should leave both sides standing with casualties: %d scouts, %d guards of %d",
			report.Scouts,
			report.Guards,
			report.Units,
		)
	}
}

// wantOrdersTrace is the trace after the 900 ticks the benchmark program runs in
// the orders mode. It changes only when the rules or the squad orders do.
const wantOrdersTrace uint64 = 13621971378015380758

func TestOrdersBattleIsDeterministicAndPinned(t *testing.T) {
	t.Parallel()

	report, err := skirmishbench.RunOrders(300, 600)
	if err != nil {
		t.Fatal(err)
	}
	if report.Trace != wantOrdersTrace {
		t.Errorf("trace = %d, want %d", report.Trace, wantOrdersTrace)
	}
	if report.Scouts == 0 || report.Guards == 0 || report.Scouts+report.Guards >= report.Units {
		t.Errorf(
			"the battle should leave both sides standing with casualties: %d scouts, %d guards of %d",
			report.Scouts,
			report.Guards,
			report.Units,
		)
	}
}

// wantVisionTrace is the trace after the 900 ticks the benchmark program runs in
// the vision mode. It changes only when the rules, the squad orders, or the
// vision rules do.
const wantVisionTrace uint64 = 15294946299312324755

func TestVisionBattleIsDeterministicAndPinned(t *testing.T) {
	t.Parallel()

	report, err := skirmishbench.RunVision(300, 600)
	if err != nil {
		t.Fatal(err)
	}
	if report.Trace != wantVisionTrace {
		t.Errorf("trace = %d, want %d", report.Trace, wantVisionTrace)
	}
	if report.Scouts == 0 || report.Guards == 0 || report.Scouts+report.Guards >= report.Units {
		t.Errorf(
			"the battle should leave both sides standing with casualties: %d scouts, %d guards of %d",
			report.Scouts,
			report.Guards,
			report.Units,
		)
	}
}

// TestVisionStepDoesNotAllocateOnceTheCrowdHasSpread runs the vision mode
// without its group orders, which route units into new collision buckets.
func TestVisionStepDoesNotAllocateOnceTheCrowdHasSpread(t *testing.T) {
	scene, err := skirmishbench.NewVision()
	if err != nil {
		t.Fatal(err)
	}
	for range 300 {
		if _, err := scene.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if allocs := testing.AllocsPerRun(50, func() {
		if err := scene.Sim.Step(); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Errorf("Step with vision allocated %v times per tick", allocs)
	}
}
