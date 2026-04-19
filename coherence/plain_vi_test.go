package coherence

import (
	"fmt"
	"testing"
)

// newTestDir is a helper that creates a PlainVIDirectory for the given region
// size, using the M1 v1.2 baseline flags. Fails immediately on error so test
// bodies stay clean.
func newTestDir(t *testing.T, regionBytes uint64) *PlainVIDirectory {
	t.Helper()
	cfg := DirectoryConfig{
		RegionSizeBytes:   regionBytes,
		InfiniteCapacity:  true,
		CoalescingEnabled: false,
	}
	d, err := NewPlainVIDirectory(cfg)
	if err != nil {
		t.Fatalf("NewPlainVIDirectory(R=%d): %v", regionBytes, err)
	}
	return d
}

// assertV11 checks Invariant V11: zero evictions for the run.
func assertV11(t *testing.T, d *PlainVIDirectory, ctx string) {
	t.Helper()
	if ev := d.Stats().Evictions; ev != 0 {
		t.Errorf("V11 FAIL [%s]: Evictions=%d, must be 0", ctx, ev)
	}
}

// regionAlignedAddr returns a region-aligned address that is non-zero and
// large enough to avoid aliasing with region 0. Used to keep tests
// deterministic regardless of region size.
func regionAlignedAddr(regionBytes uint64) uint64 {
	return regionBytes * 16 // 16th region, always aligned
}

// ─── Scenario 1 ──────────────────────────────────────────────────────────────
// GPU0 read on empty directory → entry created, sharer={GPU0}, Valid.

func TestScenario1_ReadCreatesEntry(t *testing.T) {
	for _, r := range ValidRegionSizes {
		r := r
		t.Run(fmt.Sprintf("R=%d", r), func(t *testing.T) {
			d := newTestDir(t, r)
			addr := regionAlignedAddr(r) + 64 // mid-region address

			if err := d.UpdateSharers(addr, 0, OpRead); err != nil {
				t.Fatalf("UpdateSharers: %v", err)
			}

			e, ok := d.Lookup(addr)
			if !ok {
				t.Fatal("Lookup: entry must exist after Read")
			}
			if !e.IsValid {
				t.Error("entry must be Valid after Read")
			}
			if !e.Sharers.Contains(0) {
				t.Errorf("sharer set must contain GPU0; got %032b", uint32(e.Sharers))
			}
			if e.Sharers.Len() != 1 {
				t.Errorf("sharer count must be 1; got %d", e.Sharers.Len())
			}
			if s := d.Stats(); s.Inserts != 1 {
				t.Errorf("Inserts must be 1; got %d", s.Inserts)
			}
			assertV11(t, d, "scenario1")
		})
	}
}

// ─── Scenario 2 ──────────────────────────────────────────────────────────────
// GPU0 read, then GPU1 read → sharer={GPU0, GPU1}.

func TestScenario2_SecondReaderAdded(t *testing.T) {
	for _, r := range ValidRegionSizes {
		r := r
		t.Run(fmt.Sprintf("R=%d", r), func(t *testing.T) {
			d := newTestDir(t, r)
			addr := regionAlignedAddr(r)

			d.UpdateSharers(addr, 0, OpRead) //nolint
			d.UpdateSharers(addr, 1, OpRead) //nolint

			e, ok := d.Lookup(addr)
			if !ok {
				t.Fatal("Lookup: entry must exist")
			}
			if e.Sharers.Len() != 2 {
				t.Errorf("sharer count must be 2; got %d (set=%032b)",
					e.Sharers.Len(), uint32(e.Sharers))
			}
			if !e.Sharers.Contains(0) || !e.Sharers.Contains(1) {
				t.Errorf("both GPU0 and GPU1 must be sharers; got %032b", uint32(e.Sharers))
			}
			// Only one entry must exist (same region).
			if s := d.Stats(); s.Inserts != 1 {
				t.Errorf("Inserts must be 1 (same region); got %d", s.Inserts)
			}
			assertV11(t, d, "scenario2")
		})
	}
}

// ─── Scenario 3 ──────────────────────────────────────────────────────────────
// GPU0 and GPU1 read, then GPU0 writes → GPU1 invalidated, sharer={GPU0}.

func TestScenario3_WriteInvalidatesOtherSharers(t *testing.T) {
	for _, r := range ValidRegionSizes {
		r := r
		t.Run(fmt.Sprintf("R=%d", r), func(t *testing.T) {
			d := newTestDir(t, r)
			addr := regionAlignedAddr(r)

			d.UpdateSharers(addr, 0, OpRead)  //nolint
			d.UpdateSharers(addr, 1, OpRead)  //nolint
			d.UpdateSharers(addr, 0, OpWrite) //nolint

			e, ok := d.Lookup(addr)
			if !ok {
				t.Fatal("Lookup: entry must exist after Write")
			}
			if e.Sharers.Len() != 1 {
				t.Errorf("sharer count must be 1 after Write; got %d (set=%032b)",
					e.Sharers.Len(), uint32(e.Sharers))
			}
			if !e.Sharers.Contains(0) {
				t.Error("GPU0 must remain as sole sharer after Write")
			}
			if e.Sharers.Contains(1) {
				t.Error("GPU1 must be invalidated after GPU0 Write")
			}
			if !e.IsDirty {
				t.Error("entry must be dirty after Write")
			}
			assertV11(t, d, "scenario3")
		})
	}
}

// ─── Scenario 4 ──────────────────────────────────────────────────────────────
// Same address read repeatedly → no new entries, sharer set stable.

func TestScenario4_RepeatedReadNoNewEntry(t *testing.T) {
	for _, r := range ValidRegionSizes {
		r := r
		t.Run(fmt.Sprintf("R=%d", r), func(t *testing.T) {
			d := newTestDir(t, r)
			addr := regionAlignedAddr(r) + 64

			for i := 0; i < 10; i++ {
				d.UpdateSharers(addr, 0, OpRead) //nolint
			}

			e, ok := d.Lookup(addr)
			if !ok {
				t.Fatal("Lookup: entry must exist")
			}
			if e.Sharers.Len() != 1 {
				t.Errorf("repeated reads must not increase sharer count; got %d", e.Sharers.Len())
			}
			// Only one insert despite 10 reads (entry was reused).
			if s := d.Stats(); s.Inserts != 1 {
				t.Errorf("Inserts must be 1 after repeated reads; got %d", s.Inserts)
			}
			// SharerUpdates must equal the number of UpdateSharers calls (10).
			if s := d.Stats(); s.SharerUpdates != 10 {
				t.Errorf("SharerUpdates must be 10; got %d", s.SharerUpdates)
			}
			assertV11(t, d, "scenario4")
		})
	}
}

// ─── Scenario 5 ──────────────────────────────────────────────────────────────
// Invalidate(all) then Read → new entry created from scratch.

func TestScenario5_AfterFullInvalidateNewEntryCreated(t *testing.T) {
	for _, r := range ValidRegionSizes {
		r := r
		t.Run(fmt.Sprintf("R=%d", r), func(t *testing.T) {
			d := newTestDir(t, r)
			addr := regionAlignedAddr(r)

			// First access: GPU0 reads.
			d.UpdateSharers(addr, 0, OpRead) //nolint

			// Full invalidation (pass InvalidGPUID to invalidate all).
			if err := d.Invalidate(addr, InvalidGPUID); err != nil {
				t.Fatalf("Invalidate: %v", err)
			}
			if _, ok := d.Lookup(addr); ok {
				t.Error("Lookup must return miss after full Invalidate")
			}

			// Second access: GPU1 reads after invalidation.
			d.UpdateSharers(addr, 1, OpRead) //nolint

			e, ok := d.Lookup(addr)
			if !ok {
				t.Fatal("Lookup: entry must exist after second Read")
			}
			if e.Sharers.Len() != 1 {
				t.Errorf("sharer count must be 1 after re-insert; got %d", e.Sharers.Len())
			}
			if !e.Sharers.Contains(1) {
				t.Errorf("GPU1 must be the sharer after re-insert; got %032b", uint32(e.Sharers))
			}
			// Two inserts expected: one before and one after invalidation.
			if s := d.Stats(); s.Inserts != 2 {
				t.Errorf("Inserts must be 2 (initial + re-insert); got %d", s.Inserts)
			}
			assertV11(t, d, "scenario5")
		})
	}
}

// ─── Scenario 6 ──────────────────────────────────────────────────────────────
// R_A1 defense: NewPlainVIDirectory must return an error (not panic) when
// InfiniteCapacity=false or CoalescingEnabled=true.

func TestScenario6_ConstructorRejectsInvalidFlags(t *testing.T) {
	badCfgs := []struct {
		cfg  DirectoryConfig
		desc string
	}{
		{
			DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: false, CoalescingEnabled: false},
			"InfiniteCapacity=false",
		},
		{
			DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: true, CoalescingEnabled: true},
			"CoalescingEnabled=true",
		},
		{
			DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: false, CoalescingEnabled: true},
			"both flags off-spec",
		},
	}

	for _, tc := range badCfgs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			_, err := NewPlainVIDirectory(tc.cfg)
			if err == nil {
				t.Errorf("NewPlainVIDirectory(%s) must return error; got nil", tc.desc)
			}
		})
	}

	// All five region sizes must construct successfully under the M1 contract.
	for _, r := range ValidRegionSizes {
		r := r
		t.Run(fmt.Sprintf("valid_R=%d", r), func(t *testing.T) {
			d := newTestDir(t, r)
			if d == nil {
				t.Fatal("NewPlainVIDirectory returned nil")
			}
			if ev := d.Stats().Evictions; ev != 0 {
				t.Errorf("V11: fresh directory Evictions=%d, must be 0", ev)
			}
		})
	}
}

// ─── Additional: partial Invalidate (keep one GPU) ──────────────────────────

// TestPartialInvalidate_KeepExcludedGPU verifies that Invalidate(addr, GPU0)
// keeps GPU0 and removes all other sharers. This is the write-invalidate path
// (caller: GPU0 is the writer that survives; GPU1 is evicted from sharer set).
func TestPartialInvalidate_KeepExcludedGPU(t *testing.T) {
	for _, r := range ValidRegionSizes {
		r := r
		t.Run(fmt.Sprintf("R=%d", r), func(t *testing.T) {
			d := newTestDir(t, r)
			addr := regionAlignedAddr(r)

			d.UpdateSharers(addr, 0, OpRead) //nolint
			d.UpdateSharers(addr, 1, OpRead) //nolint
			d.UpdateSharers(addr, 2, OpRead) //nolint

			// Keep GPU0, remove GPU1 and GPU2.
			if err := d.Invalidate(addr, 0); err != nil {
				t.Fatalf("Invalidate: %v", err)
			}

			e, ok := d.Lookup(addr)
			if !ok {
				t.Fatal("Lookup: entry must remain valid after partial Invalidate")
			}
			if !e.Sharers.Contains(0) {
				t.Error("GPU0 (excludeGPU) must remain after partial Invalidate")
			}
			if e.Sharers.Contains(1) || e.Sharers.Contains(2) {
				t.Errorf("GPU1,GPU2 must be removed; got %032b", uint32(e.Sharers))
			}
			assertV11(t, d, "partial_invalidate")
		})
	}
}

// ─── V12 access bitmap across region sizes ───────────────────────────────────

// TestV12_AccessBitmapAllRegionSizes verifies that the AccessBitmap correctly
// tracks sub-offset accesses for every config. The number of set bits must
// equal the number of distinct sub-offsets accessed (V12: accessed ≤ fetched).
func TestV12_AccessBitmapAllRegionSizes(t *testing.T) {
	for _, r := range ValidRegionSizes {
		r := r
		cfg := DirectoryConfig{
			RegionSizeBytes:   r,
			InfiniteCapacity:  true,
			CoalescingEnabled: false,
		}
		mapper := NewAddressMapper(cfg)
		t.Run(fmt.Sprintf("R=%d", r), func(t *testing.T) {
			d := newTestDir(t, r)
			base := regionAlignedAddr(r)
			n := cfg.CachelinesPerRegion()

			// Access every other sub-offset.
			accessed := 0
			for i := 0; i < n; i += 2 {
				addr := base + uint64(i)*64
				d.UpdateSharers(addr, 0, OpRead) //nolint
				accessed++
			}

			e, ok := d.Lookup(base)
			if !ok {
				t.Fatal("Lookup: must find entry")
			}
			got := e.AccessedCachelines()
			if got != accessed {
				t.Errorf("AccessedCachelines: want %d got %d", accessed, got)
			}
			// V12: accessed ≤ fetched (fetched = n = total lines in region).
			if got > n {
				t.Errorf("V12 fail: AccessedCachelines=%d > CachelinesPerRegion=%d",
					got, n)
			}

			// Verify bitmap correctness: only even-indexed lines set.
			for i, b := range e.AccessBitmap {
				want := (i%2 == 0) && (i < n)
				if b != want {
					t.Errorf("AccessBitmap[%d]: want %v got %v", i, want, b)
				}
			}
			assertV11(t, d, fmt.Sprintf("V12_R=%d", r))
		})

		_ = mapper // used indirectly through UpdateSharers
	}
}

// ─── Stats counter consistency ───────────────────────────────────────────────

// TestStatsConsistency verifies that Stats() counters increment correctly
// across a sequence of reads, a write, and an invalidation.
func TestStatsConsistency(t *testing.T) {
	d := newTestDir(t, 1024) // arbitrary R=1KB
	addr := uint64(1024 * 8)

	// 2 reads by GPU0 and GPU1 → 1 insert (first access), 2 sharer updates.
	d.UpdateSharers(addr, 0, OpRead) //nolint
	d.UpdateSharers(addr, 1, OpRead) //nolint

	// 1 write by GPU0 → 1 sharer update + 1 invalidation (GPU1 removed).
	d.UpdateSharers(addr, 0, OpWrite) //nolint

	// 1 full invalidate.
	d.Invalidate(addr, InvalidGPUID) //nolint

	s := d.Stats()
	if s.Inserts != 1 {
		t.Errorf("Inserts: want 1 got %d", s.Inserts)
	}
	if s.SharerUpdates != 3 {
		t.Errorf("SharerUpdates: want 3 got %d", s.SharerUpdates)
	}
	// 1 invalidation from Write + 1 from explicit Invalidate = 2.
	if s.Invalidations != 2 {
		t.Errorf("Invalidations: want 2 got %d", s.Invalidations)
	}
	if s.Evictions != 0 {
		t.Errorf("V11: Evictions must be 0; got %d", s.Evictions)
	}
}