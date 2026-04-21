package adapter

import (
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/coherence"
)

// ShadowDirHook feeds CU vector-memory access addresses into a shadow
// PlainVIDirectory. One instance per CU; gpuID distinguishes CUs so the
// directory's sharer sets reflect multi-CU sharing.
//
// Attach via cuComp.AcceptHook(hook). Only HookPosCUVectorMemAccess events
// are processed; all other hook positions are silently ignored.
//
// All accesses are recorded as OpRead. Write-initiated invalidations are
// out-of-scope for the B-7.0 capacity-effect test (we measure eviction
// pressure, not write-invalidation frequency).
type ShadowDirHook struct {
	dir   *coherence.PlainVIDirectory
	gpuID coherence.GPUID
}

// NewShadowDirHook creates a hook that forwards CU accesses to dir using gpuID.
func NewShadowDirHook(dir *coherence.PlainVIDirectory, gpuID coherence.GPUID) *ShadowDirHook {
	return &ShadowDirHook{dir: dir, gpuID: gpuID}
}

// Func implements sim.Hook.
func (h *ShadowDirHook) Func(ctx sim.HookCtx) {
	if ctx.Pos != cu.HookPosCUVectorMemAccess {
		return
	}
	d := ctx.Detail.(cu.CUVectorMemAccessDetail)
	_ = h.dir.UpdateSharers(d.Addr, h.gpuID, coherence.OpRead)
}
