package adapter

import (
	"github.com/sarchlab/mgpusim/v4/coherence"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// DirectoryAdapter routes PlainVIDirectory SharerEvents into PhaseMetrics.
//
// Register it with PlainVIDirectory.AddCallback(adapter.SharerEventCallback()).
// The On* methods may also be called directly (e.g., in integration tests).
type DirectoryAdapter struct {
	metrics *instrument.PhaseMetrics
}

// NewDirectoryAdapter returns a DirectoryAdapter that accumulates into m.
func NewDirectoryAdapter(m *instrument.PhaseMetrics) *DirectoryAdapter {
	return &DirectoryAdapter{metrics: m}
}

// OnSharerUpdate records a sharer-set update for the given region and offset.
func (a *DirectoryAdapter) OnSharerUpdate(
	regionTag uint64,
	cachelineOffset uint32,
	sharers coherence.SharerSet,
) {
	a.metrics.UpdateSharerSet(regionTag, cachelineOffset, sharers)
}

// OnInvalidation records one invalidation event by source.
func (a *DirectoryAdapter) OnInvalidation(source instrument.InvSource) {
	a.metrics.AddInvalidation(source)
}

// SharerEventCallback returns a func(coherence.SharerEvent) suitable for
// PlainVIDirectory.AddCallback. The callback dispatches each event kind
// to the corresponding On* method.
func (a *DirectoryAdapter) SharerEventCallback() func(coherence.SharerEvent) {
	return func(e coherence.SharerEvent) {
		switch e.Kind {
		case coherence.SharerEventKindSharerUpdate:
			a.OnSharerUpdate(e.RegionTag, e.CachelineOffset, e.Sharers)
		case coherence.SharerEventKindWriteInvalidate:
			a.OnInvalidation(instrument.InvSourceWriteInit)
		case coherence.SharerEventKindEvictInvalidate:
			a.OnInvalidation(instrument.InvSourceEvictInit)
		}
	}
}