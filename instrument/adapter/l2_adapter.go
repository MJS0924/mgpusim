package adapter

import (
	"github.com/sarchlab/akita/v4/mem/cache/writebackcoh"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// L2Adapter routes writebackcoh L2 events into PhaseMetrics.
//
// As a sim.Hook it can be registered with writebackcoh.Comp.AcceptHook; its
// Func method dispatches HookPosL2Access and HookPosRegionFetch events.
// The On* methods may also be called directly (e.g., in integration tests).
type L2Adapter struct {
	metrics *instrument.PhaseMetrics
}

// NewL2Adapter returns an L2Adapter that accumulates into m.
func NewL2Adapter(m *instrument.PhaseMetrics) *L2Adapter {
	return &L2Adapter{metrics: m}
}

// OnL2Access records one L2 hit (hit=true) or miss (hit=false).
func (a *L2Adapter) OnL2Access(hit bool) {
	a.metrics.AddL2Access(hit)
}

// OnRegionFetch records a cache-miss fetch of sizeBytes bytes for regionTag.
func (a *L2Adapter) OnRegionFetch(regionTag uint64, sizeBytes uint64) {
	a.metrics.AddRegionFetch(regionTag, sizeBytes)
}

// Func implements sim.Hook. It dispatches:
//   - HookPosL2Access → OnL2Access
//   - HookPosRegionFetch → OnRegionFetch
func (a *L2Adapter) Func(ctx sim.HookCtx) {
	switch ctx.Pos {
	case writebackcoh.HookPosL2Access:
		d := ctx.Detail.(writebackcoh.L2AccessDetail)
		a.OnL2Access(d.Hit)
	case writebackcoh.HookPosRegionFetch:
		d := ctx.Detail.(writebackcoh.RegionFetchDetail)
		a.OnRegionFetch(d.RegionTag, d.RegionSizeBytes)
	}
}