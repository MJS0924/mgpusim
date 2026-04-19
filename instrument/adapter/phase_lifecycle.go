package adapter

import "github.com/sarchlab/mgpusim/v4/instrument"

// RegisterPhaseLifecycle wires PhaseClock boundary events to Flush() → sink.
//
// On each window or kernel boundary, it:
//  1. Sets m.EndCycle to the boundary cycle (clock.CurrentStartCycle() post-advance).
//  2. Calls m.Flush() to compute aggregates and obtain a snapshot.
//  3. Pushes the snapshot to sink (skipped on V12 error).
//  4. Advances m.PhaseID and m.StartCycle to the new phase.
//
// The initial phase is initialised from clock.Current() and
// clock.CurrentStartCycle() before any events are registered.
// The caller is responsible for ensuring m is not used concurrently.
func RegisterPhaseLifecycle(
	clock *instrument.PhaseClock,
	m *instrument.PhaseMetrics,
	sink SnapshotSink,
) {
	m.PhaseID = clock.Current()
	m.StartCycle = clock.CurrentStartCycle()

	flush := func(newPhaseID instrument.PhaseID) {
		m.EndCycle = clock.CurrentStartCycle()
		snap, err := m.Flush()
		if err == nil {
			_ = sink.PushSnapshot(snap)
		}
		m.PhaseID = newPhaseID
		m.StartCycle = clock.CurrentStartCycle()
	}

	clock.OnWindowBoundary(func(old, new instrument.PhaseID) {
		flush(new)
	})
	clock.OnKernelBoundary(func(_ string, old, new instrument.PhaseID) {
		flush(new)
	})
}