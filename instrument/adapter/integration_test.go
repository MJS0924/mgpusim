package adapter

// Integration tests I1~I7: verify that each adapter correctly routes events
// into PhaseMetrics without spinning up a full akita simulation engine.
// All tests call On* methods directly (unit-style) or exercise the full
// sim.Hook.Func dispatch path using synthetic HookCtx values.

import (
	"testing"

	"github.com/sarchlab/akita/v4/mem/cache/writebackcoh"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/amd/timing/wavefront"
	"github.com/sarchlab/mgpusim/v4/coherence"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// ─── I1: L2Adapter.OnL2Access routes hit/miss into PhaseMetrics ──────────────

func TestI1_L2Adapter_OnL2Access(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m)

	a.OnL2Access(true)
	a.OnL2Access(true)
	a.OnL2Access(false)

	if m.L2Hits != 2 {
		t.Errorf("I1: want L2Hits=2; got %d", m.L2Hits)
	}
	if m.L2Misses != 1 {
		t.Errorf("I1: want L2Misses=1; got %d", m.L2Misses)
	}
}

// ─── I2: L2Adapter.OnRegionFetch routes fetch into PhaseMetrics ──────────────

func TestI2_L2Adapter_OnRegionFetch(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m)

	a.OnRegionFetch(0x1000, 64)
	a.OnRegionFetch(0x2000, 64)

	if m.RegionFetchedBytes != 128 {
		t.Errorf("I2: want RegionFetchedBytes=128; got %d", m.RegionFetchedBytes)
	}
}

// ─── I3: CUAdapter.OnRegionAccess succeeds after prior fetch ─────────────────

func TestI3_CUAdapter_OnRegionAccess_AfterFetch(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	l2 := NewL2Adapter(m)
	cuA := NewCUAdapter(m)

	l2.OnRegionFetch(0x1000, 64) // seed the region
	cuA.OnRegionAccess(0x1000)   // access should not error (V12 layer 1)

	// Verify the access was recorded by flushing and checking snapshot.
	m.PhaseID = instrument.PhaseID{}
	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("I3: unexpected Flush error: %v", err)
	}
	if snap.RegionAccessedBytes == 0 {
		t.Errorf("I3: want RegionAccessedBytes>0 after access; got 0")
	}
}

// ─── I4: CUAdapter.OnInstructionRetired increments counter ───────────────────

func TestI4_CUAdapter_OnInstructionRetired(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewCUAdapter(m)

	a.OnInstructionRetired(3)
	a.OnInstructionRetired(7)

	if m.RetiredInstructions != 10 {
		t.Errorf("I4: want RetiredInstructions=10; got %d", m.RetiredInstructions)
	}
}

// ─── I5: DirectoryAdapter.OnSharerUpdate records sharer set ──────────────────

func TestI5_DirectoryAdapter_OnSharerUpdate(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	da := NewDirectoryAdapter(m)

	sharers := coherence.SharerSet(0).Add(coherence.GPUID(0)).Add(coherence.GPUID(1))
	da.OnSharerUpdate(0x4000, 2, sharers)

	// Verify via Flush snapshot SharerConsistentRegions (region has sharers recorded).
	m.AddRegionFetch(0x4000, 64) // seed the region for V12
	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("I5: unexpected Flush error: %v", err)
	}
	if snap.SharerConsistentRegions != 1 {
		t.Errorf("I5: want SharerConsistentRegions=1; got %d", snap.SharerConsistentRegions)
	}
}

// ─── I6: DirectoryAdapter.OnInvalidation increments the right counter ────────

func TestI6_DirectoryAdapter_OnInvalidation(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	da := NewDirectoryAdapter(m)

	da.OnInvalidation(instrument.InvSourceWriteInit)
	da.OnInvalidation(instrument.InvSourceWriteInit)
	da.OnInvalidation(instrument.InvSourceEvictInit)

	if m.WriteInitInvalidations != 2 {
		t.Errorf("I6: want WriteInitInvalidations=2; got %d", m.WriteInitInvalidations)
	}
	if m.EvictInitInvalidations != 1 {
		t.Errorf("I6: want EvictInitInvalidations=1; got %d", m.EvictInitInvalidations)
	}
}

// ─── I7: RegisterPhaseLifecycle flushes into sink on window boundary ─────────

func TestI7_RegisterPhaseLifecycle_WindowBoundary(t *testing.T) {
	clock := instrument.NewPhaseClock(100, instrument.PhaseID{})
	m := instrument.NewPhaseMetrics()
	sink := &InMemorySink{}

	RegisterPhaseLifecycle(clock, m, sink)

	// Accumulate some data then cross window boundary at tick 100.
	m.AddL2Access(true)
	m.AddL2Access(false)
	clock.Tick(100)

	if len(sink.Snapshots) != 1 {
		t.Fatalf("I7: want 1 snapshot after window boundary; got %d", len(sink.Snapshots))
	}
	snap := sink.Snapshots[0]
	if snap.L2Hits != 1 {
		t.Errorf("I7: snap.L2Hits: want 1; got %d", snap.L2Hits)
	}
	if snap.L2Misses != 1 {
		t.Errorf("I7: snap.L2Misses: want 1; got %d", snap.L2Misses)
	}
	if snap.EndCycle != 100 {
		t.Errorf("I7: snap.EndCycle: want 100; got %d", snap.EndCycle)
	}
}

// ─── Hook dispatch tests: verify Func() routes ctx.Pos correctly ─────────────

// TestL2Adapter_Func_L2Access verifies HookPosL2Access dispatch.
func TestL2Adapter_Func_L2Access(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m)

	a.Func(sim.HookCtx{
		Pos:    writebackcoh.HookPosL2Access,
		Detail: writebackcoh.L2AccessDetail{Hit: true, Addr: 0x100},
	})
	a.Func(sim.HookCtx{
		Pos:    writebackcoh.HookPosL2Access,
		Detail: writebackcoh.L2AccessDetail{Hit: false, Addr: 0x200},
	})

	if m.L2Hits != 1 || m.L2Misses != 1 {
		t.Errorf("L2Adapter.Func: want H=1 M=1; got H=%d M=%d", m.L2Hits, m.L2Misses)
	}
}

// TestL2Adapter_Func_RegionFetch verifies HookPosRegionFetch dispatch.
func TestL2Adapter_Func_RegionFetch(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m)

	a.Func(sim.HookCtx{
		Pos:    writebackcoh.HookPosRegionFetch,
		Detail: writebackcoh.RegionFetchDetail{RegionTag: 0x1000, RegionSizeBytes: 64},
	})

	if m.RegionFetchedBytes != 64 {
		t.Errorf("L2Adapter.Func RegionFetch: want 64; got %d", m.RegionFetchedBytes)
	}
}

// TestCUAdapter_Func_VectorMemAccess verifies HookPosCUVectorMemAccess dispatch.
func TestCUAdapter_Func_VectorMemAccess(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	m.AddRegionFetch(0x5000, 64) // seed for V12
	a := NewCUAdapter(m)

	a.Func(sim.HookCtx{
		Pos:    cu.HookPosCUVectorMemAccess,
		Detail: cu.CUVectorMemAccessDetail{Addr: 0x5000},
	})

	// Check access was recorded (will show in RegionAccessedBytes after Flush).
	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("unexpected Flush error: %v", err)
	}
	if snap.RegionAccessedBytes == 0 {
		t.Error("CUAdapter.Func VectorMemAccess: want RegionAccessedBytes>0")
	}
}

// TestCUAdapter_Func_WfCompletion verifies WfCompletionEvent dispatch.
func TestCUAdapter_Func_WfCompletion(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewCUAdapter(m)

	evt := &wavefront.WfCompletionEvent{}
	a.Func(sim.HookCtx{
		Pos:  sim.HookPosBeforeEvent,
		Item: evt,
	})

	if m.RetiredInstructions != 1 {
		t.Errorf("CUAdapter.Func WfCompletion: want RetiredInstructions=1; got %d",
			m.RetiredInstructions)
	}
}

// TestDirectoryAdapter_SharerEventCallback verifies full callback dispatch.
func TestDirectoryAdapter_SharerEventCallback(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	da := NewDirectoryAdapter(m)
	cb := da.SharerEventCallback()

	sharers := coherence.SharerSet(0).Add(coherence.GPUID(3))

	cb(coherence.SharerEvent{
		Kind:            coherence.SharerEventKindSharerUpdate,
		RegionTag:       0x8000,
		CachelineOffset: 0,
		Sharers:         sharers,
	})
	cb(coherence.SharerEvent{
		Kind:      coherence.SharerEventKindWriteInvalidate,
		RegionTag: 0x8000,
	})
	cb(coherence.SharerEvent{
		Kind:      coherence.SharerEventKindEvictInvalidate,
		RegionTag: 0x8000,
	})

	if m.WriteInitInvalidations != 1 {
		t.Errorf("callback WriteInvalidate: want 1; got %d", m.WriteInitInvalidations)
	}
	if m.EvictInitInvalidations != 1 {
		t.Errorf("callback EvictInvalidate: want 1; got %d", m.EvictInitInvalidations)
	}
}