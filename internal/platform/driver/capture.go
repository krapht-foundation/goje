package driver

import (
	"fmt"
	"slices"
	"time"

	"github.com/krapht-foundation/goje/diagnostics"
)

const maxTraceCapacity = 1 << 20

// Capture holds diagnostics privately on the host loop for a single run.
// A Capture must not be reused or read while its run is active.
type Capture struct {
	report diagnostics.Report
	frame  uint64
	origin time.Time
	now    func() time.Time
}

// NewCapture starts counters and reserves storage for up to capacity phase records.
// Capacity must be between 0 and 1<<20 inclusive; zero enables only counters.
func NewCapture(capacity int) (*Capture, error) {
	if capacity < 0 || capacity > maxTraceCapacity {
		return nil, fmt.Errorf("trace capacity %d is outside [0, %d]", capacity, maxTraceCapacity)
	}
	c := &Capture{}
	if capacity > 0 {
		c.report.Trace = make([]diagnostics.Record, 0, capacity)
	}
	return c, nil
}

// Report returns the counters by value and transfers the trace to the caller.
// Call it only after the host loop has stopped. Subsequent calls return no trace.
func (c *Capture) Report() diagnostics.Report {
	report := c.report
	report.Trace = slices.Clip(report.Trace)
	c.report.Trace = nil
	return report
}

func (c *Capture) phaseStart() time.Time {
	if cap(c.report.Trace) == 0 {
		return time.Time{}
	}
	if len(c.report.Trace) == cap(c.report.Trace) {
		c.report.OmittedRecords++
		return time.Time{}
	}
	return c.readClock()
}

func (c *Capture) phaseFinish(start time.Time, phase diagnostics.Phase, tick uint64) {
	if start.IsZero() {
		return
	}
	end := c.readClock()
	if len(c.report.Trace) == 0 {
		c.origin = start
	}
	c.report.Trace = append(c.report.Trace, diagnostics.Record{
		Phase:    phase,
		Tick:     tick,
		Frame:    c.frame,
		Start:    start.Sub(c.origin),
		Duration: end.Sub(start),
	})
}

func (c *Capture) readClock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
