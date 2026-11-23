package driver

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/krapht-foundation/goje/diagnostics"
	"github.com/krapht-foundation/goje/engine"
)

type testScene struct {
	updates uint64
	draws   uint64
	err     error
}

func (*testScene) Enter() error { return nil }
func (*testScene) Exit()        {}

func (s *testScene) Update(tick engine.Tick) (engine.Transition, error) {
	s.updates = tick.Number
	return engine.Transition{}, s.err
}

func (s *testScene) Draw(engine.Canvas) { s.draws++ }

func startTestSession(tb testing.TB, capture *Capture) (*Session, *testScene) {
	tb.Helper()
	scene := &testScene{}
	session, err := Start(
		tb.Context(),
		func() engine.Scene { return scene },
		Config{Capture: capture},
	)
	if err != nil {
		tb.Fatal(err)
	}
	return session, scene
}

type clockRead struct {
	updates, draws uint64
}

func TestCapture(t *testing.T) {
	wantTrace := []diagnostics.Record{
		{Phase: diagnostics.Draw, Frame: 1, Duration: time.Nanosecond},
		{Phase: diagnostics.Submit, Frame: 1, Start: 2, Duration: time.Nanosecond},
		{Phase: diagnostics.Update, Tick: 1, Frame: 2, Start: 4, Duration: time.Nanosecond},
		{Phase: diagnostics.Update, Tick: 2, Frame: 2, Start: 6, Duration: time.Nanosecond},
		{Phase: diagnostics.Update, Tick: 3, Frame: 2, Start: 8, Duration: time.Nanosecond},
		{Phase: diagnostics.Update, Tick: 4, Frame: 2, Start: 10, Duration: time.Nanosecond},
		{Phase: diagnostics.Draw, Frame: 2, Start: 12, Duration: time.Nanosecond},
		{Phase: diagnostics.Submit, Frame: 2, Start: 14, Duration: time.Nanosecond},
		{Phase: diagnostics.Update, Tick: 5, Frame: 3, Start: 16, Duration: time.Nanosecond},
		{Phase: diagnostics.Draw, Frame: 3, Start: 18, Duration: time.Nanosecond},
		{Phase: diagnostics.Submit, Frame: 3, Start: 20, Duration: time.Nanosecond},
	}
	// Updates and draws must execute between their own start and end reads.
	wantReads := []clockRead{
		{0, 0},
		{0, 1},
		{0, 1},
		{0, 1},
		{0, 1},
		{1, 1},
		{1, 1},
		{2, 1},
		{2, 1},
		{3, 1},
		{3, 1},
		{4, 1},
		{4, 1},
		{4, 2},
		{4, 2},
		{4, 2},
		{4, 2},
		{5, 2},
		{5, 2},
		{5, 3},
		{5, 3},
		{5, 3},
	}
	for _, tc := range []struct {
		name     string
		capacity int
		omitted  uint64
	}{
		{name: "disabled", capacity: -1},
		{name: "counters only"},
		{name: "overflow", capacity: 4, omitted: 7},
		{name: "trace", capacity: 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var capture *Capture
			if tc.capacity >= 0 {
				var err error
				capture, err = NewCapture(tc.capacity)
				if err != nil {
					t.Fatal(err)
				}
			}
			session, scene := startTestSession(t, capture)
			reads := make([]clockRead, 0, len(wantReads))
			if capture != nil {
				capture.now = func() time.Time {
					stamp := time.Unix(0, int64(len(reads)))
					reads = append(reads, clockRead{scene.updates, scene.draws})
					return stamp
				}
			}
			for i, stamp := range []time.Duration{0, 100 * time.Millisecond, 117 * time.Millisecond} {
				if _, err := session.Advance(stamp); err != nil {
					t.Fatal(err)
				}
				start := session.StartSubmission()
				if tc.capacity <= 0 && !start.IsZero() {
					t.Fatalf("submission unexpectedly started a timer: %v", start)
				}
				session.FinishSubmission(start)
				// A failed submission is timed, but does not count as a presentation.
				if i != 1 {
					session.Presented()
				}
			}
			if err := session.Stop(); err != nil {
				t.Fatal(err)
			}
			if scene.updates != 5 || scene.draws != 3 {
				t.Fatalf("workload changed: %d updates and %d draws", scene.updates, scene.draws)
			}
			count := min(max(tc.capacity, 0), len(wantTrace))
			if !slices.Equal(reads, wantReads[:2*count]) {
				t.Fatalf("clock reads = %v, want %v", reads, wantReads[:2*count])
			}
			if capture == nil {
				return
			}
			report := capture.Report()
			if report.Ticks != 5 || report.Frames != 2 || report.DroppedTicks != 2 ||
				report.OmittedRecords != tc.omitted {
				t.Fatalf("unexpected counters: %+v", report)
			}
			if !slices.Equal(report.Trace, wantTrace[:count]) || cap(report.Trace) != count {
				t.Fatalf(
					"trace = %+v (cap %d), want %+v",
					report.Trace,
					cap(report.Trace),
					wantTrace[:count],
				)
			}
			if next := capture.Report(); next.Trace != nil || next.Ticks != report.Ticks {
				t.Fatalf("trace ownership not transferred: %+v", next)
			}
		})
	}
}

func TestCaptureFailedUpdate(t *testing.T) {
	capture, err := NewCapture(2)
	if err != nil {
		t.Fatal(err)
	}
	capture.now = func() time.Time { return time.Unix(0, 0) }
	session, scene := startTestSession(t, capture)
	if _, err := session.Advance(0); err != nil {
		t.Fatal(err)
	}
	scene.err = errors.New("update failed")
	if _, err := session.Advance(20 * time.Millisecond); !errors.Is(err, scene.err) {
		t.Fatalf("Advance error = %v, want %v", err, scene.err)
	}
	if err := session.Stop(); err != nil {
		t.Fatal(err)
	}
	report := capture.Report()
	wantTrace := []diagnostics.Record{
		{Phase: diagnostics.Draw, Frame: 1},
		{Phase: diagnostics.Update, Tick: 1, Frame: 2},
	}
	if report.Ticks != 0 || report.Frames != 0 || !slices.Equal(report.Trace, wantTrace) {
		t.Fatalf("unexpected report after failed update: %+v", report)
	}
}

func TestNewCaptureBounds(t *testing.T) {
	for _, capacity := range []int{-1, maxTraceCapacity + 1} {
		if capture, err := NewCapture(capacity); capture != nil || err == nil {
			t.Fatalf("NewCapture(%d) = %v, %v; want nil capture and error", capacity, capture, err)
		}
	}
}

func BenchmarkAdvanceDiagnosticsDisabled(b *testing.B) {
	session, _ := startTestSession(b, nil)
	if _, err := session.Advance(0); err != nil {
		b.Fatal(err)
	}
	var stamp time.Duration
	b.ReportAllocs()
	for b.Loop() {
		stamp += 20 * time.Millisecond
		if _, err := session.Advance(stamp); err != nil {
			b.Fatal(err)
		}
		start := session.StartSubmission()
		session.FinishSubmission(start)
		session.Presented()
	}
	if err := session.Stop(); err != nil {
		b.Fatal(err)
	}
}
