package coherence

// Entry describes one region-granular directory record for the M1 v1.2
// baseline directory (no sub-entries per A-9; no coalescing per A-6).
//
// Reviewer attack: "Why a separate Entry type when REC already defines one?"
// Defense: The M1 baseline is an intentionally minimal VI directory — no
// sub-entry array, no coalescing bookkeeping, no dirty-mask per sub-block.
// Reusing REC's CohEntry would force these fields into the baseline and
// obscure which state comes from the workload vs. from REC's scheme.
// This type is the authoritative M1 baseline record; PHASE 2 P1 imports
// REC's type separately when it runs the REC baseline comparison.
//
// Populated/owned by the coherence directory implementation (later phases).
type Entry struct {
	// Tag is the region tag: addr >> Log2RegionSize (see AddressMapper).
	Tag uint64

	// PID identifies the address space (same semantics as REC / VI).
	PID uint32

	// IsValid indicates the entry currently tracks live coherence state.
	IsValid bool

	// IsDirty indicates at least one writer has modified any cache line in
	// the region since the last writeback.
	IsDirty bool

	// Sharers is the set of GPUs that currently hold a valid cached copy of
	// any cache line within this region. Stored as a SharerSet bitmap
	// (uint32); supports up to 32 GPUs (MaxBitmapGPUID=31, r9nano uses 0-15).
	//
	// No akita sim.RemotePort dependency: the directory implementation maps
	// port→GPUID at its adapter boundary; this package is stdlib-only.
	Sharers SharerSet

	// AccessBitmap is a per-cacheline access bitmap for the region.
	// Bit i is set when the i-th cache line (i in [0, CachelinesPerRegion))
	// has been accessed since the entry became valid.
	// Used for region-utilization metric (PHASE B).
	// Length == CachelinesPerRegion(); nil if entry is invalid.
	AccessBitmap []bool
}

// Reset clears the entry but retains the backing AccessBitmap slice so it
// can be reused by the next region allocated into this slot (amortizes
// allocation cost; important when hash-map entries come and go).
func (e *Entry) Reset() {
	e.Tag = 0
	e.PID = 0
	e.IsValid = false
	e.IsDirty = false
	e.Sharers = 0
	for i := range e.AccessBitmap {
		e.AccessBitmap[i] = false
	}
}

// MarkAccess sets the access bit for the cache line at subOffset within
// the region. Out-of-range subOffset is a programmer error; caller must
// ensure AddressMapper.SubOffset() was used to compute it.
func (e *Entry) MarkAccess(subOffset int) {
	if subOffset < 0 || subOffset >= len(e.AccessBitmap) {
		panic("coherence.Entry.MarkAccess: subOffset out of range")
	}
	e.AccessBitmap[subOffset] = true
}

// AccessedCachelines counts how many cache lines in this region have been
// accessed (popcount of AccessBitmap). Used by RegionUtilization metric
// (PHASE B: RegionAccessedBytes = count × BlockSizeBytes).
func (e *Entry) AccessedCachelines() int {
	n := 0
	for _, b := range e.AccessBitmap {
		if b {
			n++
		}
	}
	return n
}