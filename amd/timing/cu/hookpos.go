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

// HookPosInstructionRetired fires when a single instruction completes execution
// in the ComputeUnit. Triggered from ComputeUnit.logInstTask() when
// completed=true (i.e., when tracing.EndTask is called for an instruction).
//
// Fires for every instruction type: VALU, VMem, Scalar, Branch, LDS, Special.
// ctx.Domain is *ComputeUnit. ctx.Item is the completing *wavefront.Inst.
// Use this hook to compute IPC: RetiredInstructions / (EndCycle - StartCycle).
var HookPosInstructionRetired = &sim.HookPos{Name: "InstructionRetired"}