package loop_test

import (
	"testing"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/loop"
)

func TestFramePacing(t *testing.T) {
	var host loop.Loop
	for _, tc := range []struct {
		at   time.Duration
		want loop.Budget
	}{
		{at: 0},
		{at: 16 * time.Millisecond},
		{at: 17 * time.Millisecond, want: loop.Budget{Updates: 1}},
		{at: time.Second + 32*time.Millisecond, want: loop.Budget{Updates: 4, Dropped: 56}},
		{at: time.Second + 33*time.Millisecond},
		{at: time.Second + 34*time.Millisecond, want: loop.Budget{Updates: 1}},
	} {
		if got := host.Frame(tc.at); got != tc.want {
			t.Fatalf("Frame(%s) = %+v; want %+v", tc.at, got, tc.want)
		}
	}
}

func TestFrameDoesNotDriftWithDisplayRate(t *testing.T) {
	for _, refresh := range []int{30, 60, 144} {
		var host loop.Loop
		var updates int
		for frame := range refresh*10 + 1 {
			budget := host.Frame(time.Duration(frame) * time.Second / time.Duration(refresh))
			updates += budget.Updates
			if budget.Dropped != 0 {
				t.Fatalf("%d Hz: dropped %d ticks at frame %d", refresh, budget.Dropped, frame)
			}
		}
		if updates != 600 {
			t.Fatalf("%d Hz: ran %d ticks in ten seconds; want 600", refresh, updates)
		}
	}
}

func TestInputAcrossPresentationAndSuspension(t *testing.T) {
	var host loop.Loop
	host.Frame(0)
	host.KeyDown(engine.KeyR, false)
	host.KeyUp(engine.KeyR)
	host.KeyDown(engine.KeyD, false)
	host.PressSecondary(50.5, 33.25)
	host.PressPrimary(0, 0)
	if got := host.Frame(8 * time.Millisecond); got.Updates != 0 {
		t.Fatalf("early frame scheduled %+v; want no update", got)
	}
	if got := host.Frame(50 * time.Millisecond); got.Updates != 3 {
		t.Fatalf("catch-up frame scheduled %+v; want three updates", got)
	}
	// Each button retains its first press through the zero-update frame.
	// Only the first catch-up tick sees the two independent edges.
	host.PressPrimary(70, 25)
	host.PressSecondary(75, 46)
	want := engine.Input{
		Held:           engine.KeyD,
		Pressed:        engine.KeyR | engine.KeyD,
		PrimaryPress:   engine.MousePress{Valid: true},
		SecondaryPress: engine.MousePress{X: 50.5, Y: 33.25, Valid: true},
	}
	if got := host.NextInput(); got != want {
		t.Fatalf("first step input = %+v; want %+v", got, want)
	}
	for range 2 {
		host.KeyDown(engine.KeyD, true)
		if got := host.NextInput(); got != (engine.Input{Held: engine.KeyD}) {
			t.Fatalf("catch-up step input = %+v; want held D with no edge", got)
		}
	}
	// Suspend with both mouse edges pending and partial tick time.
	host.Frame(62 * time.Millisecond)
	host.KeyDown(engine.KeyW, false)
	host.PressPrimary(8.5, 30.25)
	host.PressSecondary(10.5, 40.25)
	host.SetActive(false)
	host.KeyDown(engine.KeyA, false)
	host.PressPrimary(60, 75)
	host.PressSecondary(80, 95)
	if got := host.Frame(5 * time.Second); got != (loop.Budget{}) {
		t.Fatalf("suspended frame = %+v; want no updates or drops", got)
	}
	host.SetActive(true)
	host.KeyDown(engine.KeyD, true)
	if got := host.Frame(6 * time.Second); got != (loop.Budget{}) {
		t.Fatalf("resume frame = %+v; want no updates or drops", got)
	}
	if got := host.Frame(6*time.Second + 8*time.Millisecond); got.Updates != 0 {
		t.Fatalf("resumed loop reused partial tick time: %+v", got)
	}
	if got := host.Frame(6*time.Second + 17*time.Millisecond); got.Updates != 1 {
		t.Fatalf("resumed loop scheduled %+v; want one update", got)
	}
	if got := host.NextInput(); got != (engine.Input{}) {
		t.Fatalf("resumed step inherited stale input: %+v", got)
	}
}

func TestSecondaryPressCanBeBufferedAgainAfterConsumption(t *testing.T) {
	var host loop.Loop
	for _, point := range []engine.MousePress{
		{X: 5, Y: 10, Valid: true},
		{X: 20, Y: 15, Valid: true},
	} {
		host.PressSecondary(point.X, point.Y)
		want := engine.Input{SecondaryPress: point}
		if got := host.NextInput(); got != want {
			t.Fatalf("input = %+v; want %+v", got, want)
		}
	}
}
