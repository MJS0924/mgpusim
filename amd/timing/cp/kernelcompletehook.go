package cp

import "github.com/sarchlab/akita/v4/sim"

// HookPosKernelComplete fires on the CommandProcessor when one of its
// dispatchers finishes sending LaunchKernelRsp to the driver.
// Per-GPU: each GPU's CommandProcessor fires independently.
//
// Registered via cp.AcceptHook(h). Check ctx.Pos == HookPosKernelComplete.
var HookPosKernelComplete = &sim.HookPos{Name: "KernelComplete"}

// KernelCompleteHookInfo is the ctx.Item type for HookPosKernelComplete.
type KernelCompleteHookInfo struct {
	// KernelID is the LaunchKernelReq.ID for the completed kernel.
	KernelID string
}
