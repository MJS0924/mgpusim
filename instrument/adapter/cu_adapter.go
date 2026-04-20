package adapter

import (
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/coherence"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// CUAdapter routes ComputeUnit events into PhaseMetrics.
//
// β design (extended 2026-04-20): CU access auto-registers its region if
// not yet fetched, since in real akita ordering the CU hook fires before
// the L2 hook. This preserves the "region activation" semantics and keeps
// RegionAccessedBytes populated even when the CU observes an access first.
// The L2Adapter cooperates via PhaseMetrics.IsRegionFetched — when the L2
// hook later fires for the same region, it skips its own AddRegionFetch,
// avoiding double-counted fetched bytes and bitmap reset.
//
// warningCount now increments only for a genuine post-auto-register failure
// (AddRegionAccess still fails after AddRegionFetch was just called) — this
// indicates an unrecoverable bug, not the expected CU/L2 ordering.
type CUAdapter struct {
	metrics      *instrument.PhaseMetrics
	mapper       coherence.AddressMapper
	regionSize   uint64
	warningCount uint64
}

// NewCUAdapter returns a CUAdapter using cfg for address mapping.
// cfg must satisfy coherence.DirectoryConfig.Validate() (panics on invalid cfg).
func NewCUAdapter(m *instrument.PhaseMetrics, cfg coherence.DirectoryConfig) *CUAdapter {
	return &CUAdapter{
		metrics:    m,
		mapper:     coherence.NewAddressMapper(cfg),
		regionSize: cfg.RegionSizeBytes,
	}
}

// OnRegionAccess records one vector memory access at addr.
// If the region has not been fetched yet (CU hook fires before L2 hook),
// the region is auto-registered via AddRegionFetch and the access is retried.
func (a *CUAdapter) OnRegionAccess(addr uint64) {
	regionTag := a.mapper.EntryTag(addr)
	subOffset := uint32(a.mapper.SubOffset(addr))
	if err := a.metrics.AddRegionAccess(regionTag, subOffset); err != nil {
		a.metrics.AddRegionFetch(regionTag, a.regionSize)
		if err2 := a.metrics.AddRegionAccess(regionTag, subOffset); err2 != nil {
			a.warningCount++
		}
	}
}

// OnWavefrontRetired increments the retired-wavefront counter by n.
func (a *CUAdapter) OnWavefrontRetired(n uint64) {
	a.metrics.AddRetiredWavefronts(n)
}

// WarningCount returns the number of unrecoverable access failures (post
// auto-register retry). Under β extended semantics this should be 0 in a
// well-formed run; non-zero indicates a genuine bug, not CU/L2 ordering.
func (a *CUAdapter) WarningCount() uint64 {
	return a.warningCount
}

// Func implements sim.Hook. It dispatches:
//   - HookPosCUVectorMemAccess → OnRegionAccess
//   - HookPosWfRetired → OnWavefrontRetired(1)
func (a *CUAdapter) Func(ctx sim.HookCtx) {
	switch ctx.Pos {
	case cu.HookPosCUVectorMemAccess:
		d := ctx.Detail.(cu.CUVectorMemAccessDetail)
		a.OnRegionAccess(d.Addr)
	case cu.HookPosWfRetired:
		a.OnWavefrontRetired(1)
	}
}
