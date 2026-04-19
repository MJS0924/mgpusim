package instrument

import (
	"testing"

	"github.com/sarchlab/mgpusim/v4/coherence"
)

// ─── M1: Single region fetch + partial access → utilization correct ──────────

func TestM1_RegionUtilization(t *testing.T) {
	m := NewPhaseMetrics()
	// 256B region = 4 cachelines of 64B each.
	m.AddRegionFetch(0x1000, 256)
	if err := m.AddRegionAccess(0x1000, 0); err != nil {
		t.Fatalf("AddRegionAccess: %v", err)
	}
	if err := m.AddRegionAccess(0x1000, 1); err != nil {
		t.Fatalf("AddRegionAccess: %v", err)
	}

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.RegionFetchedBytes != 256 {
		t.Errorf("RegionFetchedBytes: want 256; got %d", snap.RegionFetchedBytes)
	}
	if snap.RegionAccessedBytes != 128 {
		t.Errorf("RegionAccessedBytes: want 128 (2×64); got %d", snap.RegionAccessedBytes)
	}
	if snap.ActiveRegions != 1 {
		t.Errorf("ActiveRegions: want 1; got %d", snap.ActiveRegions)
	}
}

// ─── M2: Same cacheline accessed twice → idempotent (accessed count = 1) ─────

func TestM2_IdempotentAccess(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddRegionFetch(0x1000, 256)
	for i := 0; i < 5; i++ {
		if err := m.AddRegionAccess(0x1000, 0); err != nil {
			t.Fatalf("AddRegionAccess iter %d: %v", i, err)
		}
	}

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.RegionAccessedBytes != 64 {
		t.Errorf("M2: idempotency broken — want RegionAccessedBytes=64; got %d",
			snap.RegionAccessedBytes)
	}
}

// ─── M3: Invalidate + re-fetch → Option A resets bitmap ─────────────────────
// First fetch: access cacheline 0. Re-fetch (simulating Invalidate+re-insert):
// access cacheline 1 only. RegionAccessedBytes must reflect post-reset accesses.

func TestM3_OptionA_ResetOnReFetch(t *testing.T) {
	m := NewPhaseMetrics()

	// First fetch: 256B, 1 cacheline accessed.
	m.AddRegionFetch(0x2000, 256)
	if err := m.AddRegionAccess(0x2000, 0); err != nil {
		t.Fatalf("first access: %v", err)
	}

	// Re-fetch (Option A: bitmap reset). RegionFetchedBytes += 256 → 512.
	m.AddRegionFetch(0x2000, 256)
	// Old cacheline 0 access is forgotten. New access: cacheline 1 only.
	if err := m.AddRegionAccess(0x2000, 1); err != nil {
		t.Fatalf("post-refetch access: %v", err)
	}

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.RegionFetchedBytes != 512 {
		t.Errorf("Option A: RegionFetchedBytes want 512 (2 fetches); got %d",
			snap.RegionFetchedBytes)
	}
	// After bitmap reset, only cacheline 1 is recorded → 1×64 = 64.
	if snap.RegionAccessedBytes != 64 {
		t.Errorf("Option A: RegionAccessedBytes want 64 (post-reset only); got %d",
			snap.RegionAccessedBytes)
	}
	// V12 must hold: 64 ≤ 512.
	if snap.RegionAccessedBytes > snap.RegionFetchedBytes {
		t.Errorf("Option A V12 violation: accessed=%d > fetched=%d",
			snap.RegionAccessedBytes, snap.RegionFetchedBytes)
	}
}

// ─── M4: All cachelines have same sharer set → consistent ────────────────────

func TestM4_SharerConsistency_AllSame(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddRegionFetch(0x3000, 256)

	shared := coherence.SharerSet(0).Add(0).Add(1) // GPU0 + GPU1
	m.UpdateSharerSet(0x3000, 0, shared)
	m.UpdateSharerSet(0x3000, 1, shared)
	m.UpdateSharerSet(0x3000, 2, shared)

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.ActiveRegions != 1 {
		t.Errorf("ActiveRegions: want 1; got %d", snap.ActiveRegions)
	}
	if snap.SharerConsistentRegions != 1 {
		t.Errorf("SharerConsistentRegions: want 1 (all same); got %d",
			snap.SharerConsistentRegions)
	}
}

// ─── M5: Cachelines have different sharer sets → inconsistent ────────────────

func TestM5_SharerConsistency_Mixed(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddRegionFetch(0x4000, 256)

	m.UpdateSharerSet(0x4000, 0, coherence.SharerSet(0).Add(0))        // GPU0 only
	m.UpdateSharerSet(0x4000, 1, coherence.SharerSet(0).Add(0).Add(1)) // GPU0 + GPU1

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.SharerConsistentRegions != 0 {
		t.Errorf("SharerConsistentRegions: want 0 (mixed); got %d",
			snap.SharerConsistentRegions)
	}
	if snap.ActiveRegions != 1 {
		t.Errorf("ActiveRegions: want 1; got %d", snap.ActiveRegions)
	}
}

// ─── M6: V12 structural defense — Access before Fetch → error ────────────────

func TestM6_V12_StructuralDefense_NoFetch(t *testing.T) {
	m := NewPhaseMetrics()
	err := m.AddRegionAccess(0x5000, 0)
	if err == nil {
		t.Error("M6: AddRegionAccess without prior AddRegionFetch must return error; got nil")
	}
}

// ─── M7: V12 detection — accessed > fetched → Flush returns error ────────────
// We bypass the structural defense by manipulating RegionFetchedBytes directly
// after a valid access sequence, then triggering the detection layer in Flush.

func TestM7_V12_Detection_FlushError(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddRegionFetch(0x6000, 64) // 1 cacheline fetched
	if err := m.AddRegionAccess(0x6000, 0); err != nil {
		t.Fatalf("AddRegionAccess: %v", err)
	}
	// Artificially zero RegionFetchedBytes to simulate the violation.
	// Internal map has 1 accessed cacheline (64B); fetched will read as 0.
	m.RegionFetchedBytes = 0

	_, err := m.Flush()
	if err == nil {
		t.Error("M7: Flush must return error when accessed > fetched; got nil")
	}
}

// ─── M8: V11 — DirectoryEvictions field tracks correctly ─────────────────────

func TestM8_V11_DirectoryEvictionsTracked(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddDirectoryEviction()
	m.AddDirectoryEviction()

	// No AddRegionFetch calls → RegionAccessedBytes=0, RegionFetchedBytes=0 → V12 OK.
	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.DirectoryEvictions != 2 {
		t.Errorf("DirectoryEvictions: want 2; got %d", snap.DirectoryEvictions)
	}
}

// ─── M9: Reset → all counters 0 and maps empty ───────────────────────────────

func TestM9_Reset_ClearsAll(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddL2Access(true)
	m.AddL2Access(false)
	m.AddRetiredInstructions(100)
	m.AddInvalidation(InvSourceWriteInit)
	m.AddDirectoryEviction()
	m.AddAddrBucketAccess(0xDEAD)
	m.AddDSAccess(3)
	m.AddRegionFetch(0x7000, 512)
	if err := m.AddRegionAccess(0x7000, 0); err != nil {
		t.Fatalf("AddRegionAccess: %v", err)
	}
	m.UpdateSharerSet(0x7000, 0, coherence.SharerSet(0).Add(0))
	m.PhaseID = PhaseID{Index: 7}
	m.StartCycle = 100
	m.EndCycle = 200

	m.Reset()

	if m.L2Hits != 0 || m.L2Misses != 0 {
		t.Errorf("M9: L2 counters must be 0 after Reset; got H=%d M=%d",
			m.L2Hits, m.L2Misses)
	}
	if m.RetiredInstructions != 0 {
		t.Errorf("M9: RetiredInstructions must be 0; got %d", m.RetiredInstructions)
	}
	if m.WriteInitInvalidations != 0 || m.DirectoryEvictions != 0 {
		t.Errorf("M9: invalidation/eviction counters must be 0")
	}
	if m.RegionFetchedBytes != 0 || m.RegionAccessedBytes != 0 {
		t.Errorf("M9: region counters must be 0")
	}
	if m.PhaseID != (PhaseID{}) {
		t.Errorf("M9: PhaseID must be zero after Reset; got %v", m.PhaseID)
	}
	if m.StartCycle != 0 || m.EndCycle != 0 {
		t.Errorf("M9: cycles must be 0 after Reset; got start=%d end=%d",
			m.StartCycle, m.EndCycle)
	}
	if len(m.DSAccesses) != 0 {
		t.Errorf("M9: DSAccesses must be empty; got %d entries", len(m.DSAccesses))
	}
	if len(m.AddrBucketAccesses) != 0 {
		t.Errorf("M9: AddrBucketAccesses must be empty; got %d entries",
			len(m.AddrBucketAccesses))
	}
}

// ─── M10: PhaseClock integration — Flush triggered by window boundary ─────────
// Phase 0: fetch + access region A. Tick to window boundary → Flush captures.
// Phase 1: fetch + access region B. Verify snapshots are independent.

func TestM10_PhaseBoundaryIntegration(t *testing.T) {
	const windowCycles = uint64(1000)
	clock := NewPhaseClock(windowCycles, PhaseID{})
	m := NewPhaseMetrics()
	m.StartCycle = 0

	var snapshots []PhaseMetrics

	clock.OnWindowBoundary(func(old, new PhaseID) {
		m.PhaseID = old
		m.EndCycle = clock.CurrentStartCycle()
		snap, err := m.Flush()
		if err != nil {
			t.Errorf("M10: Flush at boundary: %v", err)
		}
		snapshots = append(snapshots, snap)
		// Prepare for next phase.
		m.PhaseID = new
		m.StartCycle = clock.CurrentStartCycle()
	})

	// Phase 0 data.
	m.AddRegionFetch(0xA000, 256) // 4 cachelines
	if err := m.AddRegionAccess(0xA000, 0); err != nil {
		t.Fatalf("Phase 0 access: %v", err)
	}

	// Advance to first window boundary.
	for i := uint64(1); i <= windowCycles; i++ {
		clock.Tick(i)
	}
	if len(snapshots) != 1 {
		t.Fatalf("M10: want 1 snapshot after first boundary; got %d", len(snapshots))
	}

	// Phase 1 data.
	m.AddRegionFetch(0xB000, 128) // 2 cachelines
	if err := m.AddRegionAccess(0xB000, 0); err != nil {
		t.Fatalf("Phase 1 access: %v", err)
	}
	if err := m.AddRegionAccess(0xB000, 1); err != nil {
		t.Fatalf("Phase 1 access: %v", err)
	}

	// Advance to second window boundary.
	for i := windowCycles + 1; i <= 2*windowCycles; i++ {
		clock.Tick(i)
	}
	if len(snapshots) != 2 {
		t.Fatalf("M10: want 2 snapshots after second boundary; got %d", len(snapshots))
	}

	// Verify Phase 0 snapshot.
	s0 := snapshots[0]
	if s0.RegionFetchedBytes != 256 {
		t.Errorf("M10 Phase 0: RegionFetchedBytes want 256; got %d", s0.RegionFetchedBytes)
	}
	if s0.RegionAccessedBytes != 64 {
		t.Errorf("M10 Phase 0: RegionAccessedBytes want 64; got %d", s0.RegionAccessedBytes)
	}
	if s0.ActiveRegions != 1 {
		t.Errorf("M10 Phase 0: ActiveRegions want 1; got %d", s0.ActiveRegions)
	}
	// Trivially consistent (no sharer updates).
	if s0.SharerConsistentRegions != 1 {
		t.Errorf("M10 Phase 0: SharerConsistentRegions want 1; got %d",
			s0.SharerConsistentRegions)
	}

	// Verify Phase 1 snapshot.
	s1 := snapshots[1]
	if s1.RegionFetchedBytes != 128 {
		t.Errorf("M10 Phase 1: RegionFetchedBytes want 128; got %d", s1.RegionFetchedBytes)
	}
	if s1.RegionAccessedBytes != 128 {
		t.Errorf("M10 Phase 1: RegionAccessedBytes want 128; got %d", s1.RegionAccessedBytes)
	}
	if s1.ActiveRegions != 1 {
		t.Errorf("M10 Phase 1: ActiveRegions want 1; got %d", s1.ActiveRegions)
	}

	// Verify snapshots are independent (phase 0 data not in phase 1).
	if s0.RegionFetchedBytes == s1.RegionFetchedBytes {
		t.Errorf("M10: snapshots share same RegionFetchedBytes (%d); "+
			"phase isolation broken", s0.RegionFetchedBytes)
	}

	// Verify cycle tracking.
	if s0.EndCycle != windowCycles {
		t.Errorf("M10 Phase 0: EndCycle want %d; got %d", windowCycles, s0.EndCycle)
	}
	if s1.StartCycle != windowCycles {
		t.Errorf("M10 Phase 1: StartCycle want %d; got %d", windowCycles, s1.StartCycle)
	}
}

// ─── Additional: L2 hit/miss and invalidation counters ───────────────────────

func TestCounterAccumulation(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddL2Access(true)
	m.AddL2Access(true)
	m.AddL2Access(false)
	m.AddInvalidation(InvSourceWriteInit)
	m.AddInvalidation(InvSourceWriteInit)
	m.AddInvalidation(InvSourceEvictInit)
	m.AddRetiredInstructions(42)

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.L2Hits != 2 {
		t.Errorf("L2Hits: want 2; got %d", snap.L2Hits)
	}
	if snap.L2Misses != 1 {
		t.Errorf("L2Misses: want 1; got %d", snap.L2Misses)
	}
	if snap.WriteInitInvalidations != 2 {
		t.Errorf("WriteInitInvalidations: want 2; got %d", snap.WriteInitInvalidations)
	}
	if snap.EvictInitInvalidations != 1 {
		t.Errorf("EvictInitInvalidations: want 1; got %d", snap.EvictInitInvalidations)
	}
	if snap.RetiredInstructions != 42 {
		t.Errorf("RetiredInstructions: want 42; got %d", snap.RetiredInstructions)
	}
}

// ─── Additional: Flush resets accumulator; counters start fresh ──────────────

func TestFlushResetsForNextPhase(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddL2Access(true)
	m.AddRegionFetch(0xC000, 64)
	if err := m.AddRegionAccess(0xC000, 0); err != nil {
		t.Fatalf("AddRegionAccess: %v", err)
	}

	snap1, err := m.Flush()
	if err != nil {
		t.Fatalf("first Flush: %v", err)
	}
	if snap1.L2Hits != 1 {
		t.Fatalf("snap1 L2Hits: want 1; got %d", snap1.L2Hits)
	}

	// After Flush, accumulator should be clean.
	if m.L2Hits != 0 || m.RegionFetchedBytes != 0 {
		t.Errorf("accumulator not reset after Flush: L2Hits=%d RegionFetched=%d",
			m.L2Hits, m.RegionFetchedBytes)
	}

	// Second flush with fresh data must not bleed from first.
	m.AddL2Access(false)
	snap2, err := m.Flush()
	if err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	if snap2.L2Hits != 0 || snap2.L2Misses != 1 {
		t.Errorf("snap2: want H=0 M=1; got H=%d M=%d", snap2.L2Hits, snap2.L2Misses)
	}
}

// ─── Additional: DSAccesses and AddrBucketAccesses survive Flush (snapshot) ──

func TestDSAndAddrBucket_InSnapshot(t *testing.T) {
	m := NewPhaseMetrics()
	m.AddDSAccess(7)
	m.AddDSAccess(7)
	m.AddDSAccess(3)
	m.AddAddrBucketAccess(0xFFFF)
	m.AddAddrBucketAccess(0xFFFF)

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.DSAccesses[7] != 2 {
		t.Errorf("DSAccesses[7]: want 2; got %d", snap.DSAccesses[7])
	}
	if snap.DSAccesses[3] != 1 {
		t.Errorf("DSAccesses[3]: want 1; got %d", snap.DSAccesses[3])
	}
	if snap.AddrBucketAccesses[0xFFFF] != 2 {
		t.Errorf("AddrBucketAccesses[0xFFFF]: want 2; got %d",
			snap.AddrBucketAccesses[0xFFFF])
	}
	// Accumulator maps must be cleared after Flush.
	if len(m.DSAccesses) != 0 {
		t.Errorf("accumulator DSAccesses not cleared after Flush; len=%d",
			len(m.DSAccesses))
	}
}

// ─── Additional: multiple regions, mixed consistency ─────────────────────────

func TestMultiRegion_SharerConsistency(t *testing.T) {
	m := NewPhaseMetrics()
	// Region A: 2 cachelines, same sharer.
	m.AddRegionFetch(0xD000, 256)
	m.UpdateSharerSet(0xD000, 0, coherence.SharerSet(0).Add(0))
	m.UpdateSharerSet(0xD000, 1, coherence.SharerSet(0).Add(0))

	// Region B: 2 cachelines, different sharers.
	m.AddRegionFetch(0xE000, 256)
	m.UpdateSharerSet(0xE000, 0, coherence.SharerSet(0).Add(0))
	m.UpdateSharerSet(0xE000, 1, coherence.SharerSet(0).Add(1))

	// Region C: fetched, no sharer update → trivially consistent.
	m.AddRegionFetch(0xF000, 64)

	snap, err := m.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if snap.ActiveRegions != 3 {
		t.Errorf("ActiveRegions: want 3; got %d", snap.ActiveRegions)
	}
	// A: consistent, B: inconsistent, C: trivially consistent → 2 consistent.
	if snap.SharerConsistentRegions != 2 {
		t.Errorf("SharerConsistentRegions: want 2 (A+C); got %d",
			snap.SharerConsistentRegions)
	}
}