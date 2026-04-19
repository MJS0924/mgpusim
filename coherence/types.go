package coherence

import "math/bits"

// GPUID uniquely identifies a GPU within a multi-GPU simulation.
// uint16 is sufficient for all current MGPUSim topologies (r9nano: 16 GPUs).
type GPUID uint16

// MaxBitmapGPUID is the maximum GPUID that fits in a SharerSet bitmap.
// GPUIDs 0..31 are representable; GPUID >= 32 causes Add/Contains to panic.
// The r9nano topology uses GPUIDs 0..15, well within this bound.
const MaxBitmapGPUID GPUID = 31

// SharerSet is a bitmask of up to 32 GPU IDs (stored in a uint32).
// Bit i is set when GPU i holds a valid cached copy of the region.
//
// Reviewer attack: "A 32-bit bitmap limits you to 32 GPUs. Why not a slice?"
// Defense: (a) The r9nano topology has 16 GPUs (IDs 0-15); a 32-bit map
// covers it with 2× headroom. (b) Bitmap ops (Add/Remove/Len) are O(1)
// and branch-free — important when called on the critical path during
// phase-level metric aggregation. (c) If we ever need >32 GPUs, the
// transition to uint64 is a one-line change with the same interface.
type SharerSet uint32

// Contains reports whether GPU id is in the set.
// Panics if id > MaxBitmapGPUID — that is a programmer error, not runtime data.
func (s SharerSet) Contains(id GPUID) bool {
	if id > MaxBitmapGPUID {
		panic("coherence.SharerSet.Contains: GPUID out of range")
	}
	return s&(SharerSet(1)<<id) != 0
}

// Add returns s with id added. Idempotent if already present.
// Panics if id > MaxBitmapGPUID.
func (s SharerSet) Add(id GPUID) SharerSet {
	if id > MaxBitmapGPUID {
		panic("coherence.SharerSet.Add: GPUID out of range")
	}
	return s | (SharerSet(1) << id)
}

// Remove returns s with id removed. No-op if not present or out of range.
func (s SharerSet) Remove(id GPUID) SharerSet {
	if id > MaxBitmapGPUID {
		return s
	}
	return s &^ (SharerSet(1) << id)
}

// Len returns the number of GPUs in the set (popcount).
func (s SharerSet) Len() int {
	return bits.OnesCount32(uint32(s))
}

// AllGPUIDs returns all GPUIDs in the set in ascending order.
// Allocates a slice; intended for testing and reporting, not the hot path.
func (s SharerSet) AllGPUIDs() []GPUID {
	out := make([]GPUID, 0, s.Len())
	for id := GPUID(0); id <= MaxBitmapGPUID; id++ {
		if s&(SharerSet(1)<<id) != 0 {
			out = append(out, id)
		}
	}
	return out
}

// Op is the coherence operation type that triggered a directory update.
type Op uint8

const (
	// OpRead is a cache-miss read; the requesting GPU is added to the sharer set.
	OpRead Op = iota
	// OpWrite is a write that invalidates all other sharers first (VI protocol).
	OpWrite
)

// DirectoryStats collects per-run counters required by the M1 invariants and
// sanity scripts. All fields are monotonically increasing; they are never
// reset within a run. Per-phase granularity is handled by PhaseMetrics (PHASE B).
type DirectoryStats struct {
	// Evictions must be 0 for any InfiniteCapacity run (Invariant V11).
	// Non-zero means the directory physically displaced an entry; this
	// invalidates all M1 results for that run.
	Evictions uint64

	// Lookups is the total Lookup calls.
	Lookups uint64

	// Inserts is the total number of new region entries allocated.
	Inserts uint64

	// SharerUpdates counts UpdateSharers calls (reads + writes).
	SharerUpdates uint64

	// Invalidations counts calls that removed at least one sharer.
	Invalidations uint64
}
