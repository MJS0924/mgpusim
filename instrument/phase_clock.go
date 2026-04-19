package instrument

import "fmt"

// PhaseID uniquely identifies a phase in the M1 v1.2 sweep.
// ConfigID × WorkloadID determines the joint-entropy alignment axis (M1-P5).
// Index is strictly monotonically increasing within a simulation run.
type PhaseID struct {
	ConfigID   uint16
	WorkloadID uint16
	Index      uint32
}

// String returns "C<c>-W<w>-P<i>".
func (p PhaseID) String() string {
	return fmt.Sprintf("C%d-W%d-P%d", p.ConfigID, p.WorkloadID, p.Index)
}

// PhaseClock tracks phase boundaries from two orthogonal triggers:
//   - Fixed-width windows: fires every windowCycles ticks (windowCycles=0 disables).
//   - Kernel completions: fires on each SignalKernelBoundary call.
//
// akita isolation (B-1 §4): cycle time is injected via Tick only; no
// simulator import. Track B offline compatibility: PhaseClock is driven
// entirely from trace-derived Tick / SignalKernelBoundary calls.
//
// Monotonic guarantees (B-1 §3):
//   - PhaseID.Index strictly increases across every boundary, regardless of trigger.
//   - Consecutive phases share the boundary cycle: EndCycle_N == StartCycle_{N+1}
//     (no gap, no overlap).
//
// Simultaneous-event ordering: when a kernel and a window boundary coincide
// at the same cycle (via SignalKernelBoundary), kernel listeners are called
// first and window listeners second. This ordering is deterministic and
// race-free (single-goroutine model; callers must not call Tick/Signal concurrently).
type PhaseClock struct {
	current      PhaseID
	startCycle   uint64
	lastCycle    uint64
	windowCycles uint64

	windowListeners []func(old, new PhaseID)
	kernelListeners []func(kernelID string, old, new PhaseID)
}

// NewPhaseClock creates a PhaseClock with the given window size and initial phase.
// windowCycles=0 disables window-based boundaries; kernel-only mode remains active.
func NewPhaseClock(windowCycles uint64, initial PhaseID) *PhaseClock {
	return &PhaseClock{
		current:      initial,
		windowCycles: windowCycles,
	}
}

// OnWindowBoundary registers cb as a listener for window-based phase boundaries.
// cb(old, new) is called for every window fire; multiple listeners are called
// in registration order.
func (c *PhaseClock) OnWindowBoundary(cb func(old, new PhaseID)) {
	c.windowListeners = append(c.windowListeners, cb)
}

// OnKernelBoundary registers cb as a listener for kernel-completion boundaries.
// cb(kernelID, old, new) is called for every kernel fire; multiple listeners
// are called in registration order.
func (c *PhaseClock) OnKernelBoundary(cb func(kernelID string, old, new PhaseID)) {
	c.kernelListeners = append(c.kernelListeners, cb)
}

// Current returns a snapshot of the current PhaseID.
func (c *PhaseClock) Current() PhaseID {
	return c.current
}

// CurrentStartCycle returns the cycle at which the current phase began.
// The invariant EndCycle_N == StartCycle_{N+1} is verifiable via this method.
func (c *PhaseClock) CurrentStartCycle() uint64 {
	return c.startCycle
}

// Tick advances the clock to cycle and fires window boundaries for each
// complete window elapsed since the current phase start.
//
// If cycle skips past multiple window thresholds (e.g., offline trace replay
// with sparse samples), each boundary is fired in order; startCycle advances
// by exactly windowCycles per boundary to preserve the no-gap guarantee.
//
// cycle must be monotonically non-decreasing across Tick and SignalKernelBoundary calls.
func (c *PhaseClock) Tick(cycle uint64) {
	for c.windowCycles > 0 && (cycle-c.startCycle) >= c.windowCycles {
		old := c.current
		c.current.Index++
		c.startCycle += c.windowCycles
		for _, cb := range c.windowListeners {
			cb(old, c.current)
		}
	}
	c.lastCycle = cycle
}

// SignalKernelBoundary signals a kernel completion at cycle.
//
// Ordering guarantee: kernel listeners fire first. If the window threshold is
// also met at cycle (simultaneous event), window listeners fire immediately
// after — always kernel-before-window, deterministically.
//
// The current phase's startCycle is set to cycle in all cases, so
// EndCycle(old) == StartCycle(new) regardless of trigger type.
//
// cycle must be >= the last cycle seen by Tick or SignalKernelBoundary.
func (c *PhaseClock) SignalKernelBoundary(kernelID string, cycle uint64) {
	// Capture window condition BEFORE kernel advances startCycle.
	windowAlsoFires := c.windowCycles > 0 && (cycle-c.startCycle) >= c.windowCycles

	// Kernel fires first.
	old := c.current
	c.current.Index++
	c.startCycle = cycle
	for _, cb := range c.kernelListeners {
		cb(kernelID, old, c.current)
	}

	// Window fires second at the same cycle if threshold was already met.
	if windowAlsoFires {
		old2 := c.current
		c.current.Index++
		c.startCycle = cycle
		for _, cb := range c.windowListeners {
			cb(old2, c.current)
		}
	}

	c.lastCycle = cycle
}
