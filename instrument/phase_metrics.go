package instrument

// Risk register note (M1 v1.2):
//
// R_B3 (sharer consistency memory growth) has been REMOVED from the B-phase
// risk table and relocated to PHASE C pilot 선결 확인 항목 (open questions).
// Rationale: regionSharerSets growth is bounded by the number of distinct
// (regionTag, cachelineOffset) pairs accessed per phase; under realistic
// workloads this is proportional to working-set size, not unbounded.
// Measurement deferred to PHASE C pilot run — not a blocking B-phase risk.

import (
	"fmt"

	"github.com/sarchlab/mgpusim/v4/coherence"
)

// InvSource identifies the cause of an invalidation event.
type InvSource uint8

const (
	// InvSourceWriteInit: write-invalidate triggered by the VI protocol.
	InvSourceWriteInit InvSource = iota
	// InvSourceEvictInit: directory-eviction-triggered invalidation.
	// Under InfiniteCapacity (Invariant V11) this path must never fire.
	InvSourceEvictInit
)

// PhaseMetrics accumulates all counters and intrinsic metrics for a single
// execution phase. It pairs with PhaseClock: an OnWindowBoundary listener
// calls Flush() to capture a snapshot and reset for the next phase.
//
// V12 2-layer defense (B-2 §보강1):
//
//	Layer 1 (structural): AddRegionAccess returns error when called before
//	  AddRegionFetch for the same regionTag — prevents accessed bytes
//	  accumulating without a corresponding fetch record.
//	Layer 2 (detection): Flush() verifies RegionAccessedBytes ≤ RegionFetchedBytes
//	  and returns a non-nil error on violation; the snapshot is invalid.
//
// Option A (B-2 §보강3): AddRegionFetch resets the access bitmap when a
// region is re-fetched within the same phase (after Invalidate+re-insert).
// RegionFetchedBytes accumulates; RegionAccessedBytes counts only accesses
// since the most recent fetch. Interpretation: "of the last fetch, what
// fraction was actually used?" — conservative, suitable for worst-case analysis.
//
// Sharer consistency (B-2 §보강4): UpdateSharerSet records per-cacheline
// sharer sets; Flush() counts regions where every tracked cacheline carries
// an identical SharerSet as SharerConsistentRegions.
//
// akita isolation: imports coherence (stdlib-only package) and fmt only.
type PhaseMetrics struct {
	// ── Basic counters ────────────────────────────────────────────────────
	L2Hits   uint64
	L2Misses uint64

	WriteInitInvalidations uint64
	EvictInitInvalidations uint64

	// DirectoryEvictions must be 0 for InfiniteCapacity runs (Invariant V11).
	// This field is informational; V11 enforcement is via
	// coherence.DirectoryConfig.AssertNoEviction().
	DirectoryEvictions  uint64
	// RetiredWavefronts counts wavefronts that reached WfCompleted state this phase.
	// Unit: wavefronts (not individual instructions). Each wavefront is 64 threads.
	RetiredWavefronts uint64

	// ── Intrinsic region-utilization metrics ─────────────────────────────
	RegionFetchedBytes  uint64 // cumulative bytes fetched this phase
	RegionAccessedBytes uint64 // bytes accessed since last fetch (Option A)
	ActiveRegions       uint64 // distinct regions fetched this phase

	// ── Sharer consistency ────────────────────────────────────────────────
	// SharerConsistentRegions / ActiveRegions = sharer consistency rate.
	SharerConsistentRegions uint64

	// ── DS axis placeholder (B-7/8) ──────────────────────────────────────
	// DSAccesses maps data-structure ID → access count.
	// Populated in B-7/8; present here to avoid struct churn.
	DSAccesses map[uint16]uint64

	// ── Address bucket ────────────────────────────────────────────────────
	AddrBucketAccesses map[uint64]uint64

	// ── Phase identification ──────────────────────────────────────────────
	PhaseID    PhaseID
	StartCycle uint64
	EndCycle   uint64

	// ── Internal per-region tracking (opaque; cleared by Flush/Reset) ────
	//
	// regionFetched: regionTag → cumulative fetched bytes.
	// Grows on re-fetch within the same phase (Option A double-count).
	regionFetched map[uint64]uint64

	// regionAccessedBits: regionTag → set of accessed cacheline offsets.
	// The map value is re-initialised (emptied) on each AddRegionFetch call
	// for the same tag (Option A bitmap reset).
	regionAccessedBits map[uint64]map[uint32]bool

	// regionSharerSets: regionTag → cachelineOffset → SharerSet.
	// Populated by UpdateSharerSet; consumed by Flush() for consistency check.
	regionSharerSets map[uint64]map[uint32]coherence.SharerSet
}

// NewPhaseMetrics allocates and initialises a PhaseMetrics ready for accumulation.
func NewPhaseMetrics() *PhaseMetrics {
	return &PhaseMetrics{
		DSAccesses:         make(map[uint16]uint64),
		AddrBucketAccesses: make(map[uint64]uint64),
		regionFetched:      make(map[uint64]uint64),
		regionAccessedBits: make(map[uint64]map[uint32]bool),
		regionSharerSets:   make(map[uint64]map[uint32]coherence.SharerSet),
	}
}

// ── Basic counter methods ─────────────────────────────────────────────────────

// AddL2Access records one L2 cache access (hit or miss).
func (m *PhaseMetrics) AddL2Access(hit bool) {
	if hit {
		m.L2Hits++
	} else {
		m.L2Misses++
	}
}

// AddRetiredWavefronts increments the retired-wavefront counter by n.
func (m *PhaseMetrics) AddRetiredWavefronts(n uint64) {
	m.RetiredWavefronts += n
}

// AddInvalidation records one invalidation event by source.
func (m *PhaseMetrics) AddInvalidation(source InvSource) {
	switch source {
	case InvSourceWriteInit:
		m.WriteInitInvalidations++
	case InvSourceEvictInit:
		m.EvictInitInvalidations++
	}
}

// AddDirectoryEviction records one directory eviction.
// Under InfiniteCapacity (V11) the count must remain 0 throughout the run.
func (m *PhaseMetrics) AddDirectoryEviction() {
	m.DirectoryEvictions++
}

// AddAddrBucketAccess increments the access count for the given address key.
func (m *PhaseMetrics) AddAddrBucketAccess(addr uint64) {
	m.AddrBucketAccesses[addr]++
}

// AddDSAccess increments the access counter for data-structure dsID.
// B-7/8 placeholder; DSAccesses map is always non-nil.
func (m *PhaseMetrics) AddDSAccess(dsID uint16) {
	m.DSAccesses[dsID]++
}

// ── Region-utilization methods ────────────────────────────────────────────────

// AddRegionFetch records a region fetch of regionSizeBytes bytes.
//
// Option A (B-2 §보강3): if regionTag was already fetched in this phase
// (re-fetch after Invalidate+re-insert), the access bitmap for that tag is
// reset so that RegionAccessedBytes reflects only post-re-fetch accesses.
// RegionFetchedBytes accumulates regardless (+= regionSizeBytes each call).
func (m *PhaseMetrics) AddRegionFetch(regionTag uint64, regionSizeBytes uint64) {
	m.RegionFetchedBytes += regionSizeBytes
	m.regionFetched[regionTag] += regionSizeBytes
	// Option A: always reset (or initialise) the access bitmap for this tag.
	m.regionAccessedBits[regionTag] = make(map[uint32]bool)
}

// AddRegionAccess records access to the cacheline at cachelineOffset within regionTag.
//
// V12 structural defense (layer 1): returns a non-nil error if AddRegionFetch
// was not called for regionTag in this phase. This prevents RegionAccessedBytes
// accumulating without a corresponding fetch record.
//
// Idempotent: re-accessing the same cacheline offset does not double-count
// (bool-map assignment, equivalent to bitwise OR).
func (m *PhaseMetrics) AddRegionAccess(regionTag uint64, cachelineOffset uint32) error {
	if _, ok := m.regionFetched[regionTag]; !ok {
		return fmt.Errorf(
			"V12 structural defense: AddRegionAccess on region 0x%x "+
				"without prior AddRegionFetch in this phase",
			regionTag,
		)
	}
	m.regionAccessedBits[regionTag][cachelineOffset] = true
	return nil
}

// ── Sharer-consistency methods ────────────────────────────────────────────────

// UpdateSharerSet records the sharer set for cachelineOffset within regionTag.
// Called when a GPU accesses or invalidates a cacheline. Used by Flush() to
// compute SharerConsistentRegions across all tracked cachelines per region.
func (m *PhaseMetrics) UpdateSharerSet(
	regionTag uint64,
	cachelineOffset uint32,
	sharers coherence.SharerSet,
) {
	if _, ok := m.regionSharerSets[regionTag]; !ok {
		m.regionSharerSets[regionTag] = make(map[uint32]coherence.SharerSet)
	}
	m.regionSharerSets[regionTag][cachelineOffset] = sharers
}

// ── Flush / Reset ─────────────────────────────────────────────────────────────

// Flush computes aggregate metrics, validates V12, returns a snapshot of the
// current phase, and resets all counters and internal maps for the next phase.
//
// Callers must set m.PhaseID, m.StartCycle, and m.EndCycle before calling Flush;
// those values are copied into the returned snapshot unchanged.
//
// V12 detection (layer 2): returns (PhaseMetrics{}, error) if
// RegionAccessedBytes > RegionFetchedBytes. The snapshot is zero-valued on error;
// the run result MUST NOT be used (ethics clause, M1 v1.2 §2.5.6).
//
// DirectoryEvictions in the snapshot is informational; V11 enforcement
// is the caller's responsibility via coherence.DirectoryConfig.AssertNoEviction().
func (m *PhaseMetrics) Flush() (PhaseMetrics, error) {
	// ── Step 1: compute RegionAccessedBytes from per-region bitmaps ────────
	var accessedBytes uint64
	for regionTag := range m.regionFetched {
		if bits, ok := m.regionAccessedBits[regionTag]; ok {
			accessedBytes += uint64(len(bits)) * coherence.DefaultBlockSizeBytes
		}
	}
	m.RegionAccessedBytes = accessedBytes
	m.ActiveRegions = uint64(len(m.regionFetched))

	// ── Step 2: compute SharerConsistentRegions ────────────────────────────
	var consistentCount uint64
	for regionTag := range m.regionFetched {
		lineSharers, hasSharers := m.regionSharerSets[regionTag]
		if !hasSharers || len(lineSharers) == 0 {
			// No sharer data recorded → trivially consistent.
			consistentCount++
			continue
		}
		var ref coherence.SharerSet
		first := true
		consistent := true
		for _, s := range lineSharers {
			if first {
				ref = s
				first = false
			} else if s != ref {
				consistent = false
				break
			}
		}
		if consistent {
			consistentCount++
		}
	}
	m.SharerConsistentRegions = consistentCount

	// ── Step 3: V12 detection (layer 2) ───────────────────────────────────
	if m.RegionAccessedBytes > m.RegionFetchedBytes {
		return PhaseMetrics{}, fmt.Errorf(
			"V12 violation: RegionAccessedBytes=%d > RegionFetchedBytes=%d; "+
				"run result MUST NOT be used",
			m.RegionAccessedBytes, m.RegionFetchedBytes,
		)
	}

	// ── Step 4: build snapshot (exported fields + deep-copied maps) ────────
	snap := PhaseMetrics{
		L2Hits:                  m.L2Hits,
		L2Misses:                m.L2Misses,
		WriteInitInvalidations:  m.WriteInitInvalidations,
		EvictInitInvalidations:  m.EvictInitInvalidations,
		DirectoryEvictions:      m.DirectoryEvictions,
		RetiredWavefronts:     m.RetiredWavefronts,
		RegionFetchedBytes:      m.RegionFetchedBytes,
		RegionAccessedBytes:     m.RegionAccessedBytes,
		ActiveRegions:           m.ActiveRegions,
		SharerConsistentRegions: m.SharerConsistentRegions,
		PhaseID:                 m.PhaseID,
		StartCycle:              m.StartCycle,
		EndCycle:                m.EndCycle,
		DSAccesses:              copyMap16u64(m.DSAccesses),
		AddrBucketAccesses:      copyMap64u64(m.AddrBucketAccesses),
	}

	// ── Step 5: reset for next phase (PhaseID/Cycle left for caller to set) ─
	m.resetInternal()
	return snap, nil
}

// Reset clears all counters, internal maps, PhaseID, and cycle fields.
func (m *PhaseMetrics) Reset() {
	m.PhaseID = PhaseID{}
	m.StartCycle = 0
	m.EndCycle = 0
	m.resetInternal()
}

// resetInternal clears all counters and per-region maps.
// PhaseID, StartCycle, and EndCycle are NOT touched; the caller updates them
// between Flush() and the start of the next accumulation window.
func (m *PhaseMetrics) resetInternal() {
	m.L2Hits = 0
	m.L2Misses = 0
	m.WriteInitInvalidations = 0
	m.EvictInitInvalidations = 0
	m.DirectoryEvictions = 0
	m.RetiredWavefronts = 0
	m.RegionFetchedBytes = 0
	m.RegionAccessedBytes = 0
	m.ActiveRegions = 0
	m.SharerConsistentRegions = 0

	for k := range m.DSAccesses {
		delete(m.DSAccesses, k)
	}
	for k := range m.AddrBucketAccesses {
		delete(m.AddrBucketAccesses, k)
	}
	for k := range m.regionFetched {
		delete(m.regionFetched, k)
	}
	for k := range m.regionAccessedBits {
		delete(m.regionAccessedBits, k)
	}
	for k := range m.regionSharerSets {
		delete(m.regionSharerSets, k)
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func copyMap16u64(src map[uint16]uint64) map[uint16]uint64 {
	dst := make(map[uint16]uint64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyMap64u64(src map[uint64]uint64) map[uint64]uint64 {
	dst := make(map[uint64]uint64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
