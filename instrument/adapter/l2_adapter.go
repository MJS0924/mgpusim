package adapter

// L2Adapter (β design, 2026-04-19):
//
// Region activation is addr-driven and deduplicated per phase:
//   - registerRegionIfNew(addr) uses AddressMapper.EntryTag to map raw addresses
//     to region-level tags; calls AddRegionFetch exactly once per (phase, region).
//   - OnL2Access triggers region activation on every L2 hit or miss; the
//     dedup map ensures AddRegionFetch is called at most once per region per phase.
//   - ResetPhase clears the dedup map at phase boundaries.
//
// This mirrors the Adaptive Region-Size Directory's per-region entry semantics:
// each region is tracked exactly once per phase regardless of how many cachelines
// within it are accessed. β dedup ≡ directory's 1-entry-per-region constraint.
//
// akita isolation: imports coherence (akita-free) + akita/sim + writebackcoh.

import (
	"github.com/sarchlab/akita/v4/mem/cache/writebackcoh"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/coherence"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// L2Adapter routes writebackcoh L2 events into PhaseMetrics with
// per-phase region deduplication (β design).
//
// As a sim.Hook it can be registered with writebackcoh.Comp.AcceptHook; its
// Func method dispatches HookPosL2Access and HookPosRegionFetch events.
// The On* methods may also be called directly (e.g., in integration tests).
type L2Adapter struct {
	metrics             *instrument.PhaseMetrics
	mapper              coherence.AddressMapper
	regionSize          uint64
	currentPhaseRegions map[uint64]bool
}

// NewL2Adapter returns an L2Adapter using cfg for region-level address mapping.
// cfg must satisfy coherence.DirectoryConfig.Validate() (panics on invalid cfg).
func NewL2Adapter(m *instrument.PhaseMetrics, cfg coherence.DirectoryConfig) *L2Adapter {
	return &L2Adapter{
		metrics:             m,
		mapper:              coherence.NewAddressMapper(cfg),
		regionSize:          cfg.RegionSizeBytes,
		currentPhaseRegions: make(map[uint64]bool),
	}
}

// registerRegionIfNew maps addr to a region tag and, if the region has not
// been seen this phase, calls AddRegionFetch exactly once.
func (a *L2Adapter) registerRegionIfNew(addr uint64) {
	tag := a.mapper.EntryTag(addr)
	if a.currentPhaseRegions[tag] {
		return
	}
	a.currentPhaseRegions[tag] = true
	a.metrics.AddRegionFetch(tag, a.regionSize)
}

// OnL2Access records one L2 hit (hit=true) or miss (hit=false) and activates
// the region containing addr for the current phase (deduped).
func (a *L2Adapter) OnL2Access(hit bool, addr uint64) {
	a.metrics.AddL2Access(hit)
	a.registerRegionIfNew(addr)
}

// OnRegionFetch is kept for backward compatibility and as the L2 miss hook path.
// In β design, region activation is handled by OnL2Access; this function
// delegates to registerRegionIfNew so that double-registration is prevented.
// The sizeBytes parameter is ignored — a.regionSize (from cfg) is authoritative.
func (a *L2Adapter) OnRegionFetch(addr uint64, sizeBytes uint64) {
	a.registerRegionIfNew(addr)
}

// ResetPhase clears the per-phase region dedup map. Must be called at every
// phase boundary (RegisterPhaseLifecycle handles this automatically when
// L2Adapter is passed as a PhaseResetable).
func (a *L2Adapter) ResetPhase() {
	a.currentPhaseRegions = make(map[uint64]bool)
}

// Func implements sim.Hook. It dispatches:
//   - HookPosL2Access  → OnL2Access(detail.Hit, detail.Addr)
//   - HookPosRegionFetch → OnRegionFetch(detail.RegionTag, detail.RegionSizeBytes)
func (a *L2Adapter) Func(ctx sim.HookCtx) {
	switch ctx.Pos {
	case writebackcoh.HookPosL2Access:
		d := ctx.Detail.(writebackcoh.L2AccessDetail)
		a.OnL2Access(d.Hit, d.Addr)
	case writebackcoh.HookPosRegionFetch:
		d := ctx.Detail.(writebackcoh.RegionFetchDetail)
		a.OnRegionFetch(d.RegionTag, d.RegionSizeBytes)
	}
}
