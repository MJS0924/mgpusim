package coherence

import "fmt"

// PlainVIDirectory implements the Directory interface with:
//   - Infinite capacity (hash-map entry store, no evictions).
//   - Standard 2-state VI coherence protocol (Valid / Invalid).
//   - No REC coalescing, no sub-entry structure, no promotion/demotion.
//
// This is the sole directory implementation used in the M1 v1.2 experiment.
// Its simplicity is intentional: any phase × DS variation in "optimal region
// size" observed here is attributable to workload access patterns, not to
// directory-management artefacts (§2.5.1 "intrinsic" definition).
//
// Import contract (B-0.5): this file imports ONLY the coherence package
// itself (same package) and stdlib. No akita, no REC, no HMG dependency.
// Verified by: grep -E "akita|/rec|/hmg|REC|HMG" coherence/plain_vi*.go
//
// VI state machine (2 states only):
//
//	Read(gpu):  if no entry → Insert; sharers ∪= {gpu}; state=Valid.
//	Write(gpu): if no entry → Insert; Invalidate others; sharers={gpu}; state=Valid.
//	No Modified state (single-level VI, matches MGPUSim's baseline protocol).
type PlainVIDirectory struct {
	cfg     DirectoryConfig
	mapper  AddressMapper
	entries map[uint64]*Entry // key: AddressMapper.EntryTag(addr)
	stats   DirectoryStats
}

// NewPlainVIDirectory creates a PlainVIDirectory from cfg.
//
// R_A1 defense: returns an error (not panic) if the config does not satisfy
// the M1 v1.2 baseline contract (InfiniteCapacity=true, CoalescingEnabled=false).
// Error over panic makes this testable (Scenario 6 in B-0.4).
//
// No capacity pre-allocation: the hash map grows on demand. Peak memory is
// measured in the PHASE C pilot (Appendix B #13, R_A2).
func NewPlainVIDirectory(cfg DirectoryConfig) (*PlainVIDirectory, error) {
	if !cfg.InfiniteCapacity {
		return nil, fmt.Errorf(
			"PlainVIDirectory requires InfiniteCapacity=true (Invariant V11); "+
				"got InfiniteCapacity=false — use a finite-capacity directory "+
				"for PHASE 2 P1 comparisons, not for M1",
		)
	}
	if cfg.CoalescingEnabled {
		return nil, fmt.Errorf(
			"PlainVIDirectory requires CoalescingEnabled=false (§6 REC-exclusion); "+
				"coalescing belongs to the REC implementation (PHASE 2 P1 only)",
		)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("PlainVIDirectory: invalid config: %w", err)
	}
	return &PlainVIDirectory{
		cfg:     cfg,
		mapper:  NewAddressMapper(cfg),
		entries: make(map[uint64]*Entry),
	}, nil
}

// ─── Directory interface ─────────────────────────────────────────────────────

// Lookup returns the entry for the region containing addr, or (nil, false)
// on a miss / invalid entry.
func (d *PlainVIDirectory) Lookup(addr uint64) (*Entry, bool) {
	d.stats.Lookups++
	tag := d.mapper.EntryTag(addr)
	e, ok := d.entries[tag]
	if !ok || !e.IsValid {
		return nil, false
	}
	return e, true
}

// Insert allocates a new entry for the region containing addr with
// initialSharers as the starting sharer set. Returns an error if the
// region already has a valid entry (caller must Lookup first to avoid
// double-insert, which would silently discard sharer state).
//
// V11 contract: Insert never evicts. Under infinite capacity, the map just
// grows. If we somehow reach this path when it should evict, we panic with
// "V11 violation" — that would be a programmer error in this codebase.
func (d *PlainVIDirectory) Insert(addr uint64, initialSharers SharerSet) error {
	tag := d.mapper.EntryTag(addr)
	if e, ok := d.entries[tag]; ok && e.IsValid {
		return fmt.Errorf(
			"Insert: region tag 0x%x (addr 0x%x) already has a valid entry; "+
				"call Lookup before Insert",
			tag, addr,
		)
	}

	n := d.cfg.CachelinesPerRegion()
	e := &Entry{
		Tag:          tag,
		IsValid:      true,
		Sharers:      initialSharers,
		AccessBitmap: make([]bool, n),
	}
	e.MarkAccess(d.mapper.SubOffset(addr))
	d.entries[tag] = e
	d.stats.Inserts++

	// Explicit V11 guard: an infinite directory must never evict.
	// If this insert somehow causes a displacement (it cannot in a plain map,
	// but guard against future refactors that add capacity logic):
	if d.stats.Evictions != 0 {
		panic("V11 violation: PlainVIDirectory recorded an eviction — " +
			"infinite capacity invariant broken")
	}
	return nil
}

// UpdateSharers records a coherence access (OpRead or OpWrite) by gpu on
// the region containing addr. Auto-inserts the entry on a miss.
//
// OpRead:  sharers ∪= {gpu}. All current sharers keep their copies.
// OpWrite: sharers ← {gpu}. All other current sharers are invalidated
//
//	(write-invalidate VI protocol). Entry.IsDirty is set.
func (d *PlainVIDirectory) UpdateSharers(addr uint64, gpu GPUID, op Op) error {
	tag := d.mapper.EntryTag(addr)
	e, ok := d.entries[tag]

	if !ok || !e.IsValid {
		// Auto-insert on miss (both reads and writes create a new entry).
		n := d.cfg.CachelinesPerRegion()
		e = &Entry{
			Tag:          tag,
			IsValid:      true,
			AccessBitmap: make([]bool, n),
		}
		d.entries[tag] = e
		d.stats.Inserts++
	}

	// V12: mark the specific sub-offset accessed.
	e.MarkAccess(d.mapper.SubOffset(addr))
	d.stats.SharerUpdates++

	switch op {
	case OpRead:
		e.Sharers = e.Sharers.Add(gpu)

	case OpWrite:
		// Write-invalidate: sole sharer becomes the writer.
		// If there were other sharers, they are invalidated.
		if e.Sharers.Len() > 0 && !(e.Sharers.Len() == 1 && e.Sharers.Contains(gpu)) {
			d.stats.Invalidations++
		}
		e.Sharers = SharerSet(0).Add(gpu)
		e.IsDirty = true
	}
	return nil
}

// Invalidate removes all sharers EXCEPT excludeGPU from the region.
// If excludeGPU == InvalidGPUID, all sharers are removed and the entry
// becomes invalid. No-op on a miss.
func (d *PlainVIDirectory) Invalidate(addr uint64, excludeGPU GPUID) error {
	tag := d.mapper.EntryTag(addr)
	e, ok := d.entries[tag]
	if !ok || !e.IsValid {
		return nil
	}

	if excludeGPU == InvalidGPUID {
		// Full invalidation: entry transitions to Invalid state.
		// AccessBitmap is preserved — phase-boundary reporting reads it before
		// resetting (V12 invariant; see PhaseMetrics in PHASE B).
		e.IsValid = false
		e.IsDirty = false
		e.Sharers = 0
		d.stats.Invalidations++
		return nil
	}

	// Partial invalidation: keep only excludeGPU.
	if e.Sharers.Contains(excludeGPU) {
		e.Sharers = SharerSet(0).Add(excludeGPU)
	} else {
		// excludeGPU was not a sharer; invalidate everyone.
		e.Sharers = 0
		e.IsValid = false
		e.IsDirty = false
	}
	if e.Sharers.Len() == 0 {
		e.IsValid = false
	}
	d.stats.Invalidations++
	return nil
}

// Stats returns a snapshot of the cumulative counters.
func (d *PlainVIDirectory) Stats() DirectoryStats {
	return d.stats
}

// ─── Compile-time interface satisfaction check ───────────────────────────────

// Ensure PlainVIDirectory satisfies the Directory interface at compile time.
// If the interface changes and PlainVIDirectory falls out of sync, this line
// produces a clear compile error rather than a runtime panic.
var _ Directory = (*PlainVIDirectory)(nil)