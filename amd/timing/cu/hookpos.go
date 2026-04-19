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