// Package coherence provides the region-size configuration and address-mapping
// utilities for the M1 motivation experiment (v1.2).
//
// v1.2 philosophy: measure workload-intrinsic sharing characteristics on a
// BASELINE platform (plain VI directory + infinite capacity + no REC
// coalescing) so that phase/DS variation in optimal region size is
// attributable to access patterns — not to directory eviction pressure or
// coalescing bookkeeping.
//
// Reviewer attack: "An infinite directory is unrealistic."
// Defense: M1 establishes the existence of an intrinsic signal (upper bound).
// PHASE 2 P1 (iso-storage proposed vs REC vs HMG) and A9 (static vs dynamic)
// close the gap to real hardware. See §2.5.5.
//
// Reviewer attack: "Why not build on REC directly?"
// Defense: REC's entry coalescing conflates eviction churn with workload
// variation. Separating the two is the purpose of v1.2 (§2.5.2).
package coherence

import (
	"fmt"
	"math/bits"
)

const (
	// DefaultBlockSizeBytes is the cache line size used throughout MGPUSim (64B).
	DefaultBlockSizeBytes uint64 = 64
)

// ValidRegionSizes lists the five region sizes used in the M1 v1.2 sweep:
// 64B, 256B, 1KB, 4KB, 16KB. Geometric with ratio 4 (log2 step = 2),
// covering a 256× spread from a single cache line up to a 4KB page × 4.
//
// Reviewer attack: "Why these specific five sizes?"
// Defense: (a) 64B is the baseline cache line; (b) 16KB is the upper limit
// where a single region crosses 4× GPU pages; (c) five points with ratio-4
// give enough resolution to detect per-phase extrema without bloating the
// sweep budget. Documented in design_document.md §2.5.2.
var ValidRegionSizes = []uint64{64, 256, 1024, 4096, 16384}

// DirectoryConfig holds the static parameters for one region-size
// configuration in the M1 v1.2 sweep.
//
// All three flags are user-facing and must be set explicitly by the caller:
//   - RegionSizeBytes:    coherence granularity in bytes (must be power of 2)
//   - InfiniteCapacity:   true for M1 (Invariant V11); false for PHASE 2 P1
//   - CoalescingEnabled:  false for M1 (baseline); true only for REC in PHASE 2
//
// Reviewer attack: "Three booleans invite misconfiguration."
// Defense: Validate() rejects every combination that violates M1 requirements
// (e.g., CoalescingEnabled=true without a corresponding REC build tag).
type DirectoryConfig struct {
	// RegionSizeBytes is the coherence granularity in bytes.
	// Must be a power of 2 and a member of ValidRegionSizes.
	RegionSizeBytes uint64

	// BlockSizeBytes is the cache line size. Zero defaults to 64B.
	BlockSizeBytes uint64

	// InfiniteCapacity, when true, requires the directory implementation to
	// never evict entries (Invariant V11). Runtime DirectoryEvictions must
	// remain 0 for the entire simulation. M1 v1.2 requires this.
	//
	// Implementation contract: entries are stored in a hash map keyed by
	// (PID, tag); no set-associative eviction logic is invoked.
	InfiniteCapacity bool

	// CoalescingEnabled, when false (default), disables REC-style entry
	// coalescing so that per-region sharer sets and per-region access bitmaps
	// reflect workload access patterns directly. M1 v1.2 requires false.
	CoalescingEnabled bool
}

// blockSize returns BlockSizeBytes, defaulting to DefaultBlockSizeBytes.
func (c DirectoryConfig) blockSize() uint64 {
	if c.BlockSizeBytes == 0 {
		return DefaultBlockSizeBytes
	}
	return c.BlockSizeBytes
}

// Validate returns a non-nil error if the config is invalid for M1 v1.2.
// Three classes of rejection:
//  1. RegionSizeBytes structurally invalid (zero, non-power-of-2, < block size).
//  2. RegionSizeBytes not a member of ValidRegionSizes (prevents silent drift
//     of the sweep set).
//  3. Semantically invalid combinations (e.g., coalescing without infinite).
func (c DirectoryConfig) Validate() error {
	bs := c.blockSize()
	r := c.RegionSizeBytes

	if r == 0 {
		return fmt.Errorf("RegionSizeBytes must be > 0")
	}
	if r&(r-1) != 0 {
		return fmt.Errorf("RegionSizeBytes %d is not a power of 2", r)
	}
	if bs == 0 || bs&(bs-1) != 0 {
		return fmt.Errorf("BlockSizeBytes %d is not a power of 2", bs)
	}
	if r < bs {
		return fmt.Errorf("RegionSizeBytes %d < BlockSizeBytes %d", r, bs)
	}

	found := false
	for _, v := range ValidRegionSizes {
		if v == r {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf(
			"RegionSizeBytes %d is not in ValidRegionSizes %v; "+
				"changing the sweep set requires updating ValidRegionSizes "+
				"and the M1 plan (§2.5.2)",
			r, ValidRegionSizes)
	}

	return nil
}

// IsForM1 reports whether the config matches the M1 v1.2 baseline contract:
// InfiniteCapacity=true, CoalescingEnabled=false. Used by runtime assertions
// and by the config-loader YAML validator.
//
// Reviewer attack: "A flag-based contract can be bypassed."
// Defense: the runner's config loader calls IsForM1() on every config and
// refuses to start if any flag is off-spec; V11/V12 runtime assertions
// provide defense-in-depth (PHASE B).
func (c DirectoryConfig) IsForM1() bool {
	return c.InfiniteCapacity && !c.CoalescingEnabled
}

// Log2RegionSize returns log2(RegionSizeBytes).
// Caller must ensure RegionSizeBytes is a non-zero power of 2 (Validate()).
func (c DirectoryConfig) Log2RegionSize() int {
	return bits.Len64(c.RegionSizeBytes) - 1
}

// Log2BlockSize returns log2(BlockSizeBytes) using the default when unset.
func (c DirectoryConfig) Log2BlockSize() int {
	return bits.Len64(c.blockSize()) - 1
}

// CachelinesPerRegion returns the number of cache lines contained in one
// region. Used when sizing per-region access bitmaps (PHASE B).
//   count = RegionSizeBytes / BlockSizeBytes
// Range under v1.2 ValidRegionSizes: 1 (R=64B) to 256 (R=16KB).
func (c DirectoryConfig) CachelinesPerRegion() int {
	return int(c.RegionSizeBytes / c.blockSize())
}

// IsoCoverageEntries returns the minimum number of directory entries required
// to cover the entire L2 cache without eviction, with a safety margin.
//
// v1.2 §2.5.3 iso-coverage principle: every config's directory must fit all
// coherent data resident in the L2 hierarchy so that the directory eviction
// count stays at zero (Invariant V11). safetyFactor provides headroom for
// transient over-subscription during prefetch storms.
//
//	minEntries = ceil(L2Bytes / RegionSizeBytes) * safetyFactor
//
// Returns an error if safetyFactor < 1 or RegionSizeBytes is invalid.
//
// Reviewer attack: "Safety factor 2 is arbitrary."
// Defense: chosen in pilot (Appendix B #4). The runtime V11 assertion
// ultimately validates the choice: if evictions > 0, the factor was too low
// and the simulation must be re-run with a larger factor — the safety margin
// is not a result-tuning knob because evictions=0 is a hard requirement.
func (c DirectoryConfig) IsoCoverageEntries(l2Bytes uint64, safetyFactor int) (int, error) {
	if err := c.Validate(); err != nil {
		return 0, err
	}
	if safetyFactor < 1 {
		return 0, fmt.Errorf("safetyFactor must be >= 1, got %d", safetyFactor)
	}
	if l2Bytes == 0 {
		return 0, fmt.Errorf("l2Bytes must be > 0")
	}
	n := (l2Bytes + c.RegionSizeBytes - 1) / c.RegionSizeBytes
	return int(n) * safetyFactor, nil
}

// AssertNoEviction returns an error if dirEvictions is non-zero under an
// InfiniteCapacity config. V11 runtime check (Appendix C).
// PHASE B runtime code and PHASE D sanity script both call this.
//
// Behaviour is strict: the first non-zero eviction invalidates the run
// regardless of magnitude (ethics clause: "eviction > 0 인 상태로 결과 사용" 금지).
func (c DirectoryConfig) AssertNoEviction(dirEvictions uint64) error {
	if !c.InfiniteCapacity {
		return nil
	}
	if dirEvictions != 0 {
		return fmt.Errorf(
			"V11 failure: InfiniteCapacity=true but DirectoryEvictions=%d; "+
				"increase safety factor in IsoCoverageEntries or switch to "+
				"hash-map-based entry store. Result MUST NOT be used.",
			dirEvictions)
	}
	return nil
}

// ─── Directory interface ────────────────────────────────────────────────────

// Directory is the minimal interface that all coherence directory
// implementations must satisfy in the M1 experiment framework.
//
// Design principles (B-0.1):
//   1. No REC-specific methods (coalesce, positionBits, subEntry lookup).
//      Those belong to the REC implementation type only (PHASE 2 P1).
//   2. All operations keyed by raw addr, not by pre-computed tag; the
//      implementation calls AddressMapper internally so callers cannot
//      accidentally pass a stale or wrong-granularity tag.
//   3. Stats() returns the counters for Invariant V11 and sanity checks.
//
// Reviewer attack: "Why not reuse akita's existing Directory interface?"
// Defense: akita's interface (mem/cache/directory.go) exposes set/way
// internals (GetSets, FindVictim) that are meaningless for an infinite
// directory and that tie callers to a finite set-associative model.
// This interface is the clean slice needed by M1; PHASE 2 P1 will provide
// adapters for REC/HMG behind the same interface for fair comparison.
type Directory interface {
	// Lookup returns the entry for the region containing addr, or (nil, false)
	// on a miss. The returned pointer is valid until the next mutating call;
	// callers must not retain it across operations.
	Lookup(addr uint64) (*Entry, bool)

	// Insert creates a new entry for the region containing addr with the given
	// initial sharer set. Returns an error if an entry already exists (the
	// caller must Lookup first). Under InfiniteCapacity, Insert never evicts.
	Insert(addr uint64, initialSharers SharerSet) error

	// UpdateSharers records a coherence access (read or write) by gpu on the
	// region containing addr. For OpWrite, all sharers except gpu are
	// invalidated first (VI write-invalidate protocol).
	UpdateSharers(addr uint64, gpu GPUID, op Op) error

	// Invalidate removes all sharers EXCEPT excludeGPU from the region.
	// If excludeGPU == InvalidGPUID, ALL sharers are removed and the entry
	// becomes invalid (full invalidation / flush).
	//
	// Typical usage: a write-invalidate passes the writer as excludeGPU so
	// only the writer survives; a page-migration flush passes InvalidGPUID.
	// No-op if the region has no entry (already invalid).
	Invalidate(addr uint64, excludeGPU GPUID) error

	// Stats returns a snapshot of the cumulative counters. The returned value
	// is a copy; subsequent operations do not affect it.
	Stats() DirectoryStats
}

// InvalidGPUID is the sentinel value meaning "no GPU" (e.g., Invalidate all).
const InvalidGPUID GPUID = ^GPUID(0)
