package adapter

import (
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/amd/timing/wavefront"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// CUAdapter routes ComputeUnit events into PhaseMetrics.
//
// As a sim.Hook it can be registered with ComputeUnit.AcceptHook; its Func
// method dispatches HookPosCUVectorMemAccess and WfCompletionEvent events.
// The On* methods may also be called directly (e.g., in integration tests).
type CUAdapter struct {
	metrics *instrument.PhaseMetrics
}

// NewCUAdapter returns a CUAdapter that accumulates into m.
func NewCUAdapter(m *instrument.PhaseMetrics) *CUAdapter {
	return &CUAdapter{metrics: m}
}

// OnRegionAccess records one vector memory access at addr.
// addr is treated as regionTag (cacheline granularity); cachelineOffset=0.
func (a *CUAdapter) OnRegionAccess(addr uint64) {
	_ = a.metrics.AddRegionAccess(addr, 0)
}

// OnInstructionRetired increments the retired-instruction counter by n.
func (a *CUAdapter) OnInstructionRetired(n uint64) {
	a.metrics.AddRetiredInstructions(n)
}

// Func implements sim.Hook. It dispatches:
//   - HookPosCUVectorMemAccess → OnRegionAccess
//   - HookPosBeforeEvent + *wavefront.WfCompletionEvent → OnInstructionRetired(1)
func (a *CUAdapter) Func(ctx sim.HookCtx) {
	switch ctx.Pos {
	case cu.HookPosCUVectorMemAccess:
		d := ctx.Detail.(cu.CUVectorMemAccessDetail)
		a.OnRegionAccess(d.Addr)
	case sim.HookPosBeforeEvent:
		if _, ok := ctx.Item.(*wavefront.WfCompletionEvent); ok {
			a.OnInstructionRetired(1)
		}
	}
}