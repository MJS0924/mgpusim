package cu

import "github.com/sarchlab/akita/v4/sim"

// HookPosCUVectorMemAccess fires in VectorMemoryUnit.sendRequest() when a
// vector memory request is successfully sent to the memory subsystem.
// ctx.Domain is *ComputeUnit; ctx.Detail is CUVectorMemAccessDetail.
var HookPosCUVectorMemAccess = &sim.HookPos{Name: "CUVectorMemAccess"}

// CUVectorMemAccessDetail carries the physical address of a vector memory access.
type CUVectorMemAccessDetail struct {
	Addr uint64
}

// HookPosWfRetired fires when a wavefront's state transitions to WfCompleted,
// regardless of -wf-sampling mode. Triggered from Scheduler.evalSEndPgm() at
// each direct wf.State = WfCompleted assignment, not via WfCompletionEvent
// (which only exists under sampled mode or WG-completion-message retry).
//
// ctx.Domain is *ComputeUnit. ctx.Item is the retiring *wavefront.Wavefront.
var HookPosWfRetired = &sim.HookPos{Name: "WfRetired"}