package instrument

import (
	"testing"
)

// ─── T1: Window boundary fires 5 times in 500K cycles ────────────────────────

func TestT1_WindowBoundaryFires(t *testing.T) {
	clock := NewPhaseClock(100_000, PhaseID{})
	count := 0
	clock.OnWindowBoundary(func(old, new PhaseID) { count++ })

	for i := uint64(1); i <= 500_000; i++ {
		clock.Tick(i)
	}

	if count != 5 {
		t.Errorf("T1: want 5 window boundaries; got %d", count)
	}
	if clock.Current().Index != 5 {
		t.Errorf("T1: want Index=5; got %d", clock.Current().Index)
	}
}

// ─── T2: windowCycles=0 disables window; kernel still fires ──────────────────

func TestT2_WindowDisabledWhenZero(t *testing.T) {
	clock := NewPhaseClock(0, PhaseID{})
	windowCount := 0
	kernelCount := 0
	clock.OnWindowBoundary(func(old, new PhaseID) { windowCount++ })
	clock.OnKernelBoundary(func(_ string, old, new PhaseID) { kernelCount++ })

	for i := uint64(1); i <= 100_000; i++ {
		clock.Tick(i)
	}
	clock.SignalKernelBoundary("k1", 50_000)

	if windowCount != 0 {
		t.Errorf("T2: window must not fire when windowCycles=0; got %d fires", windowCount)
	}
	if kernelCount != 1 {
		t.Errorf("T2: kernel must fire once; got %d", kernelCount)
	}
}

// ─── T3: Kernel boundary fires 3 times with correct kernelIDs ────────────────

func TestT3_KernelBoundaryListenerCalled(t *testing.T) {
	clock := NewPhaseClock(0, PhaseID{})
	var kernels []string
	clock.OnKernelBoundary(func(kernelID string, old, new PhaseID) {
		kernels = append(kernels, kernelID)
	})

	clock.SignalKernelBoundary("k1", 10)
	clock.SignalKernelBoundary("k2", 20)
	clock.SignalKernelBoundary("k3", 30)

	if len(kernels) != 3 {
		t.Fatalf("T3: want 3 kernel calls; got %d", len(kernels))
	}
	for i, want := range []string{"k1", "k2", "k3"} {
		if kernels[i] != want {
			t.Errorf("T3: kernels[%d]: want %q; got %q", i, want, kernels[i])
		}
	}
}

// ─── T4: Index strictly increases across Tick + kernel events ────────────────

func TestT4_MonotonicIndexIncrease(t *testing.T) {
	// windowCycles=100 but we only tick to 10, so no window boundary fires.
	clock := NewPhaseClock(100, PhaseID{})
	var indices []uint32
	record := func(id PhaseID) { indices = append(indices, id.Index) }
	clock.OnWindowBoundary(func(old, new PhaseID) { record(new) })
	clock.OnKernelBoundary(func(_ string, old, new PhaseID) { record(new) })

	for i := uint64(1); i <= 10; i++ {
		clock.Tick(i)
	}
	clock.SignalKernelBoundary("k1", 11)
	clock.SignalKernelBoundary("k2", 12)
	clock.SignalKernelBoundary("k3", 13)

	if len(indices) != 3 {
		t.Fatalf("T4: want 3 recorded indices (3 kernels); got %d", len(indices))
	}
	for i := 1; i < len(indices); i++ {
		if indices[i] <= indices[i-1] {
			t.Errorf("T4: Index not strictly increasing at pos %d: %d <= %d",
				i, indices[i], indices[i-1])
		}
	}
}

// ─── T5: EndCycle_N == StartCycle_{N+1} (no gap, no overlap) ─────────────────

func TestT5_NoGapNoOverlap(t *testing.T) {
	const windowCycles = uint64(100)
	clock := NewPhaseClock(windowCycles, PhaseID{})

	// Capture startCycle of each new phase (including the initial phase 0).
	starts := []uint64{clock.CurrentStartCycle()}
	clock.OnWindowBoundary(func(old, new PhaseID) {
		starts = append(starts, clock.CurrentStartCycle())
	})

	for i := uint64(1); i <= 300; i++ {
		clock.Tick(i)
	}

	// Expect boundary at 100, 200, 300 → starts=[0,100,200,300].
	if len(starts) != 4 {
		t.Fatalf("T5: want 4 start cycles (initial + 3 boundaries); got %d", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		want := starts[i-1] + windowCycles
		if starts[i] != want {
			t.Errorf("T5: gap/overlap at boundary %d: StartCycle=%d, want %d "+
				"(EndCycle of prev phase)", i, starts[i], want)
		}
	}
}

// ─── T6: Simultaneous event → kernel listener called before window listener ──

func TestT6_SimultaneousKernelBeforeWindow(t *testing.T) {
	clock := NewPhaseClock(100, PhaseID{})
	order := 0
	kernelOrder := -1
	windowOrder := -1

	clock.OnKernelBoundary(func(_ string, old, new PhaseID) {
		kernelOrder = order
		order++
	})
	clock.OnWindowBoundary(func(old, new PhaseID) {
		windowOrder = order
		order++
	})

	// Advance to one cycle before the window threshold.
	for i := uint64(1); i < 100; i++ {
		clock.Tick(i)
	}
	// At cycle 100: window threshold met AND kernel signals — both must fire.
	clock.SignalKernelBoundary("k1", 100)

	if kernelOrder == -1 {
		t.Fatal("T6: kernel listener was not called")
	}
	if windowOrder == -1 {
		t.Fatal("T6: window listener was not called at simultaneous cycle")
	}
	if kernelOrder >= windowOrder {
		t.Errorf("T6: kernel must fire before window; got kernel=%d window=%d",
			kernelOrder, windowOrder)
	}
	// Both should have fired at Index 1 and 2 respectively.
	if clock.Current().Index != 2 {
		t.Errorf("T6: want Index=2 after kernel+window at same cycle; got %d",
			clock.Current().Index)
	}
}

// ─── T7: Multiple listeners for the same event are all called ────────────────

func TestT7_MultipleListenersBothCalled(t *testing.T) {
	clock := NewPhaseClock(100, PhaseID{})
	calls := 0
	clock.OnWindowBoundary(func(old, new PhaseID) { calls++ })
	clock.OnWindowBoundary(func(old, new PhaseID) { calls++ })

	for i := uint64(1); i <= 100; i++ {
		clock.Tick(i)
	}

	if calls != 2 {
		t.Errorf("T7: both listeners must be called once each; got %d total calls", calls)
	}

	// Also verify two kernel listeners.
	clock2 := NewPhaseClock(0, PhaseID{})
	kernelCalls := 0
	clock2.OnKernelBoundary(func(_ string, old, new PhaseID) { kernelCalls++ })
	clock2.OnKernelBoundary(func(_ string, old, new PhaseID) { kernelCalls++ })
	clock2.SignalKernelBoundary("k1", 1)
	if kernelCalls != 2 {
		t.Errorf("T7: both kernel listeners must be called; got %d total calls", kernelCalls)
	}
}

// ─── Additional: PhaseID.String() format ─────────────────────────────────────

func TestPhaseIDString(t *testing.T) {
	p := PhaseID{ConfigID: 3, WorkloadID: 7, Index: 42}
	want := "C3-W7-P42"
	if got := p.String(); got != want {
		t.Errorf("PhaseID.String(): want %q; got %q", want, got)
	}
}

// ─── Additional: Current() returns initial PhaseID ───────────────────────────

func TestCurrent_ReturnsInitialPhaseID(t *testing.T) {
	initial := PhaseID{ConfigID: 2, WorkloadID: 5, Index: 0}
	clock := NewPhaseClock(1000, initial)
	if got := clock.Current(); got != initial {
		t.Errorf("Current(): want %v; got %v", initial, got)
	}
}

// ─── Additional: Tick with skipped cycles fires multiple window boundaries ───

func TestTick_SkippedCyclesFiresMultipleBoundaries(t *testing.T) {
	clock := NewPhaseClock(100, PhaseID{})
	count := 0
	clock.OnWindowBoundary(func(old, new PhaseID) { count++ })

	// Jump straight from 0 to 350 — should fire 3 boundaries (100, 200, 300).
	clock.Tick(350)

	if count != 3 {
		t.Errorf("skipped-cycle Tick: want 3 boundaries; got %d", count)
	}
	if clock.CurrentStartCycle() != 300 {
		t.Errorf("startCycle after 3 fires: want 300; got %d", clock.CurrentStartCycle())
	}
}