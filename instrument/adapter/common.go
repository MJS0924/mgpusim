package adapter

import "github.com/sarchlab/mgpusim/v4/instrument"

// SnapshotSink receives completed phase snapshots from RegisterPhaseLifecycle.
// Implementations must be safe to call from the akita event loop (single goroutine).
type SnapshotSink interface {
	PushSnapshot(snap instrument.PhaseMetrics) error
}

// InMemorySink stores phase snapshots in a slice for tests and offline analysis.
type InMemorySink struct {
	Snapshots []instrument.PhaseMetrics
}

// PushSnapshot appends snap to Snapshots.
func (s *InMemorySink) PushSnapshot(snap instrument.PhaseMetrics) error {
	s.Snapshots = append(s.Snapshots, snap)
	return nil
}