package adapter

import (
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/coherence"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// CUAdapter routes ComputeUnit events into PhaseMetrics.
//
// As a sim.Hook it can be registered with ComputeUnit.AcceptHook; its Func
// method dispatches HookPosCUVectorMemAccess and WfCompletionEvent events.
// The On* methods may also be called directly (e.g., in integration tests).
//
// OnRegionAccess uses AddressMapper (injected via cfg) to translate raw memory
// addresses into (regionTag, cachelineOffset) pairs consistent with L2Adapter.
// If AddRegionAccess returns an error (V12: no prior AddRegionFetch for this
// region), warningCount is incremented rather than panicking — this handles
// the real-simulation ordering where CU sends requests before L2 fetches.
type CUAdapter struct {
	metrics      *instrument.PhaseMetrics
	mapper       coherence.AddressMapper
	warningCount uint64
}

// NewCUAdapter returns a CUAdapter using cfg for address mapping.
// cfg must satisfy coherence.DirectoryConfig.Validate() (panics on invalid cfg).
func NewCUAdapter(m *instrument.PhaseMetrics, cfg coherence.DirectoryConfig) *CUAdapter {
	return &CUAdapter{
		metrics: m,
		mapper:  coherence.NewAddressMapper(cfg),
	}
}

// OnRegionAccess records one vector memory access at addr.
// addr is mapped to (regionTag, cachelineOffset) via AddressMapper.
// V12 errors (access before fetch) are counted in WarningCount.
func (a *CUAdapter) OnRegionAccess(addr uint64) {
	regionTag := a.mapper.EntryTag(addr)
	subOffset := uint32(a.mapper.SubOffset(addr))
	if err := a.metrics.AddRegionAccess(regionTag, subOffset); err != nil {
		a.warningCount++
	}
}

// OnInstructionRetired increments the retired-instruction counter by n.
func (a *CUAdapter) OnInstructionRetired(n uint64) {
	a.metrics.AddRetiredInstructions(n)
}

// WarningCount returns the number of V12 ordering warnings accumulated
// (OnRegionAccess called before corresponding OnL2Access/OnRegionFetch).
func (a *CUAdapter) WarningCount() uint64 {
	return a.warningCount
}

// Func implements sim.Hook. It dispatches:
//   - HookPosCUVectorMemAccess → OnRegionAccess
//   - HookPosWfRetired → OnInstructionRetired(1)
func (a *CUAdapter) Func(ctx sim.HookCtx) {
	switch ctx.Pos {
	case cu.HookPosCUVectorMemAccess:
		d := ctx.Detail.(cu.CUVectorMemAccessDetail)
		a.OnRegionAccess(d.Addr)
	case cu.HookPosWfRetired:
		a.OnInstructionRetired(1)
	}
}
