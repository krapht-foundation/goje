// Package diagnostics describes local runtime diagnostics collected by the hosts.
package diagnostics

import "time"

// Phase identifies a CPU phase recorded by the host.
type Phase string

const (
	// Update covers one runtime step, including any scene transition.
	Update Phase = "update"
	// Draw covers recording one frame's draw batch.
	Draw Phase = "draw"
	// Submit covers the host's CPU-side submission, not GPU execution time.
	Submit Phase = "submit"
)

// Record measures one attempted phase. Start and Duration encode as nanoseconds in JSON.
type Record struct {
	// Phase identifies the work that was timed.
	Phase Phase `json:"phase"`
	// Tick is the one-based simulation tick for an Update and zero otherwise.
	Tick uint64 `json:"tick"`
	// Frame is the one-based frame attempt associated with the phase.
	Frame uint64 `json:"frame"`
	// Start is elapsed time since the first recorded phase began.
	Start time.Duration `json:"start"`
	// Duration is elapsed CPU-side time for the phase.
	Duration time.Duration `json:"duration"`
}

// Report contains the local counters and trace from one run.
type Report struct {
	// Ticks is the number of completed simulation steps.
	Ticks uint64 `json:"ticks"`
	// Frames is the number of successfully presented frames.
	Frames uint64 `json:"frames"`
	// DroppedTicks is the number of whole ticks discarded by the catch-up limit.
	DroppedTicks uint64 `json:"dropped_ticks"`
	// OmittedRecords counts phases not recorded because enabled trace storage was full.
	OmittedRecords uint64 `json:"omitted_records"`
	// Trace contains recorded phases in execution order. The recipient owns the slice.
	Trace []Record `json:"trace"`
}
