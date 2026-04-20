package adapter

// Integration tests I1~I11: verify that each adapter correctly routes events
// into PhaseMetrics without spinning up a full akita simulation engine.
// All tests call On* methods directly (unit-style) or exercise the full
// sim.Hook.Func dispatch path using synthetic HookCtx values.
//
// testCfg(r) returns the M1 baseline DirectoryConfig with RegionSizeBytes=r.

import (
	"testing"

	"github.com/sarchlab/akita/v4/mem/cache/writebackcoh"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/amd/timing/wavefront"
	"github.com/sarchlab/mgpusim/v4/coherence"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// testCfg returns an M1-baseline DirectoryConfig with the given region size.
func testCfg(regionSizeBytes uint64) coherence.DirectoryConfig {
	return coherence.DirectoryConfig{
		RegionSizeBytes:   regionSizeBytes,
		InfiniteCapacity:  true,
		CoalescingEnabled: false,
	}
}

// defaultCfg is the 64B-region config used by most tests.
var defaultCfg = testCfg(64)

// ─── I1: L2Adapter.OnL2Access routes hit/miss into PhaseMetrics ──────────────

func TestI1_L2Adapter_OnL2Access(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m, defaultCfg)

	a.OnL2Access(true, 0x1000)
	a.OnL2Access(true, 0x2000)
	a.OnL2Access(false, 0x3000)

	if m.L2Hits != 2 {
		t.Errorf("I1: want L2Hits=2; got %d", m.L2Hits)
	}
	if m.L2Misses != 1 {
		t.Errorf("I1: want L2Misses=1; got %d", m.L2Misses)
	}
}

// ─── I2: L2Adapter.OnRegionFetch records fetch in PhaseMetrics ───────────────

func TestI2_L2Adapter_OnRegionFetch(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m, defaultCfg)

	a.OnRegionFetch(0x1000, 64) // activates region EntryTag(0x1000)
	a.OnRegionFetch(0x2000, 64) // activates region EntryTag(0x2000) — different region

	if m.RegionFetchedBytes != 128 {
		t.Errorf("I2: want RegionFetchedBytes=128; got %d", m.RegionFetchedBytes)
	}
}

// ─── I3: CUAdapter.OnRegionAccess succeeds after prior fetch ─────────────────

func TestI3_CUAdapter_OnRegionAccess_AfterFetch(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	l2 := NewL2Adapter(m, defaultCfg)
	cuA := NewCUAdapter(m, defaultCfg)

	l2.OnRegionFetch(0x1000, 64) // seed: AddRegionFetch(EntryTag(0x1000), 64)
	cuA.OnRegionAccess(0x1000)   // access: AddRegionAccess(EntryTag(0x1000), SubOffset(0x1000))

	if cuA.WarningCount() != 0 {
		t.Errorf("I3: expected 0 warnings after prior fetch; got %d", cuA.WarningCount())
	}

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
	a := NewCUAdapter(m, defaultCfg)

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

	m.AddRegionFetch(0x4000, 64)
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

	RegisterPhaseLifecycle(clock, m, sink) // no resetables

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

// ─── I8: L2Adapter deduplicates multiple accesses within one region ───────────

func TestI8_L2Adapter_RegionDedup_WithinPhase(t *testing.T) {
	cfg := testCfg(1024) // 1KB region = 16 cachelines
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m, cfg)

	// Three cacheline addresses within the same 1KB region (0x0000 ~ 0x03FF).
	a.OnL2Access(true, 0x0000)
	a.OnL2Access(true, 0x0040)
	a.OnL2Access(false, 0x0080)

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("I8: unexpected Flush error: %v", err)
	}
	if snap.RegionFetchedBytes != 1024 {
		t.Errorf("I8: want RegionFetchedBytes=1024 (1 region × 1KB); got %d",
			snap.RegionFetchedBytes)
	}
	if snap.ActiveRegions != 1 {
		t.Errorf("I8: want ActiveRegions=1; got %d", snap.ActiveRegions)
	}
}

// ─── I9: L2Adapter.ResetPhase clears dedup between phases ────────────────────

func TestI9_L2Adapter_ResetPhase_ClearsDedup(t *testing.T) {
	cfg := testCfg(1024)
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m, cfg)

	// Phase 1: activate region 0.
	a.OnL2Access(true, 0x0000)
	snap1, err := m.Flush()
	if err != nil {
		t.Fatalf("I9 phase1 Flush: %v", err)
	}
	if snap1.RegionFetchedBytes != 1024 {
		t.Errorf("I9 phase1: want 1024; got %d", snap1.RegionFetchedBytes)
	}

	// Reset for phase 2.
	a.ResetPhase()

	// Phase 2: same region again — must re-register after reset.
	a.OnL2Access(true, 0x0000)
	snap2, err := m.Flush()
	if err != nil {
		t.Fatalf("I9 phase2 Flush: %v", err)
	}
	if snap2.RegionFetchedBytes != 1024 {
		t.Errorf("I9 phase2: want 1024 (re-registered after reset); got %d",
			snap2.RegionFetchedBytes)
	}
}

// ─── I10: CUAdapter increments warningCount when no prior fetch ──────────────

func TestI10_CUAdapter_WarningOnMissingFetch(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewCUAdapter(m, defaultCfg)

	// No prior OnRegionFetch/OnL2Access → V12 layer 1 triggers → warningCount++.
	a.OnRegionAccess(0x5000)
	a.OnRegionAccess(0x6000)

	if a.WarningCount() != 2 {
		t.Errorf("I10: want warningCount=2; got %d", a.WarningCount())
	}
}

// ─── I11: L2 hit also triggers region activation ─────────────────────────────

func TestI11_L2Hit_TriggersRegionActivation(t *testing.T) {
	cfg := testCfg(1024)
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m, cfg)

	a.OnL2Access(true, 0x0000) // L2 hit still activates the region

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("I11: unexpected Flush error: %v", err)
	}
	if snap.RegionFetchedBytes == 0 {
		t.Error("I11: L2 hit must trigger region activation; RegionFetchedBytes=0")
	}
	if snap.L2Hits != 1 {
		t.Errorf("I11: want L2Hits=1; got %d", snap.L2Hits)
	}
}

// ─── Hook dispatch tests: verify Func() routes ctx.Pos correctly ─────────────

func TestL2Adapter_Func_L2Access(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m, defaultCfg)

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

func TestL2Adapter_Func_RegionFetch(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewL2Adapter(m, defaultCfg)

	a.Func(sim.HookCtx{
		Pos:    writebackcoh.HookPosRegionFetch,
		Detail: writebackcoh.RegionFetchDetail{RegionTag: 0x1000, RegionSizeBytes: 64},
	})

	if m.RegionFetchedBytes != 64 {
		t.Errorf("L2Adapter.Func RegionFetch: want 64; got %d", m.RegionFetchedBytes)
	}
}

func TestCUAdapter_Func_VectorMemAccess(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	m.AddRegionFetch(0, 64) // seed: EntryTag(0x5000) for R=64B = 0x5000>>6 = 320; seed 0 for simplicity
	// Actually seed the exact tag CUAdapter will compute.
	cfg := defaultCfg
	mapper := coherence.NewAddressMapper(cfg)
	tag := mapper.EntryTag(0x5000)
	m.Reset()
	m.AddRegionFetch(tag, 64)

	a := NewCUAdapter(m, cfg)

	a.Func(sim.HookCtx{
		Pos:    cu.HookPosCUVectorMemAccess,
		Detail: cu.CUVectorMemAccessDetail{Addr: 0x5000},
	})

	if a.WarningCount() != 0 {
		t.Errorf("CUAdapter.Func VectorMemAccess: unexpected warning count %d", a.WarningCount())
	}
	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("unexpected Flush error: %v", err)
	}
	if snap.RegionAccessedBytes == 0 {
		t.Error("CUAdapter.Func VectorMemAccess: want RegionAccessedBytes>0")
	}
}

func TestCUAdapter_Func_WfCompletion(t *testing.T) {
	m := instrument.NewPhaseMetrics()
	a := NewCUAdapter(m, defaultCfg)

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

// ─── RegisterPhaseLifecycle with PhaseResetable ───────────────────────────────

func TestRegisterPhaseLifecycle_ResetsL2AdapterOnBoundary(t *testing.T) {
	cfg := testCfg(1024)
	clock := instrument.NewPhaseClock(100, instrument.PhaseID{})
	m := instrument.NewPhaseMetrics()
	sink := &InMemorySink{}
	l2 := NewL2Adapter(m, cfg)

	RegisterPhaseLifecycle(clock, m, sink, l2)

	// Phase 0: activate region 0x0000.
	l2.OnL2Access(true, 0x0000)
	clock.Tick(100) // boundary → flush + ResetPhase

	if len(sink.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot; got %d", len(sink.Snapshots))
	}
	if sink.Snapshots[0].RegionFetchedBytes != 1024 {
		t.Errorf("phase0: want RegionFetchedBytes=1024; got %d",
			sink.Snapshots[0].RegionFetchedBytes)
	}

	// Phase 1: same region again — must appear in phase1 snapshot after reset.
	l2.OnL2Access(true, 0x0000)
	clock.Tick(200) // boundary → flush

	if len(sink.Snapshots) != 2 {
		t.Fatalf("want 2 snapshots; got %d", len(sink.Snapshots))
	}
	if sink.Snapshots[1].RegionFetchedBytes != 1024 {
		t.Errorf("phase1 (after reset): want RegionFetchedBytes=1024; got %d",
			sink.Snapshots[1].RegionFetchedBytes)
	}
}
