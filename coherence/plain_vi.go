package coherence

import (
	"container/list"
	"fmt"
)

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
// SharerEventKind identifies the type of SharerEvent fired by PlainVIDirectory.
type SharerEventKind uint8

const (
	// SharerEventKindSharerUpdate fires when a region's sharer set is updated.
	SharerEventKindSharerUpdate SharerEventKind = iota
	// SharerEventKindWriteInvalidate fires on write-init invalidation (OpWrite).
	SharerEventKindWriteInvalidate
	// SharerEventKindEvictInvalidate fires on eviction-init invalidation (Invalidate call).
	// Under InfiniteCapacity (V11) this must never fire in practice.
	SharerEventKindEvictInvalidate
	// SharerEventKindCapacityEvict fires when an entry is displaced by the LRU
	// policy in finite-capacity mode. Unlike SharerEventKindEvictInvalidate
	// (which fires on explicit Invalidate calls), this kind fires only on
	// capacity-triggered evictions. Sharers field carries the pre-eviction
	// sharer set so listeners can count invalidation messages per eviction.
	SharerEventKindCapacityEvict
)

// SharerEvent is emitted by PlainVIDirectory on every sharer-set mutation.
// Registered callbacks receive this event; see AddCallback.
type SharerEvent struct {
	Kind            SharerEventKind
	RegionTag       uint64      // AddressMapper.EntryTag(addr)
	CachelineOffset uint32      // AddressMapper.SubOffset(addr) for Update events
	Sharers         SharerSet   // sharer set after the operation (Update events only)
}

type PlainVIDirectory struct {
	cfg       DirectoryConfig
	mapper    AddressMapper
	entries   map[uint64]*Entry // key: AddressMapper.EntryTag(addr)
	stats     DirectoryStats
	callbacks []func(SharerEvent)
	// LRU fields (finite-capacity mode only; nil when InfiniteCapacity=true).
	lruList  *list.List               // front = most-recently-used; back = eviction candidate
	lruIndex map[uint64]*list.Element // regionTag → list element for O(1) access
}

// AddCallback registers cb to be called on every SharerEvent fired by this
// directory. Callbacks are invoked synchronously in registration order.
// Safe to call only before simulation starts (no mutex; SerialEngine guarantee).
func (d *PlainVIDirectory) AddCallback(cb func(SharerEvent)) {
	d.callbacks = append(d.callbacks, cb)
}

func (d *PlainVIDirectory) fireCallbacks(e SharerEvent) {
	for _, cb := range d.callbacks {
		cb(e)
	}
}

// NewPlainVIDirectory creates a PlainVIDirectory from cfg.
//
// Supported modes:
//   - Infinite capacity (M1 baseline): InfiniteCapacity=true, MaxEntries=0.
//     V11 invariant: DirectoryEvictions must stay 0. Panics if violated.
//   - Finite capacity (B-7.0+ capacity test): InfiniteCapacity=false, MaxEntries>0.
//     LRU eviction fires SharerEventKindCapacityEvict when capacity is reached.
//     DirectoryEvictions > 0 is expected and valid in this mode.
//
// R_A1 defense: returns an error (not panic) on config violations.
// Error over panic makes this testable (Scenario 6 in B-0.4).
func NewPlainVIDirectory(cfg DirectoryConfig) (*PlainVIDirectory, error) {
	if cfg.CoalescingEnabled {
		return nil, fmt.Errorf(
			"PlainVIDirectory requires CoalescingEnabled=false (§6 REC-exclusion); "+
				"coalescing belongs to the REC implementation (PHASE 2 P1 only)",
		)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("PlainVIDirectory: invalid config: %w", err)
	}
	d := &PlainVIDirectory{
		cfg:     cfg,
		mapper:  NewAddressMapper(cfg),
		entries: make(map[uint64]*Entry),
	}
	if cfg.IsForFiniteMode() {
		d.lruList = list.New()
		d.lruIndex = make(map[uint64]*list.Element)
	}
	return d, nil
}

// ─── LRU helpers (finite-capacity mode only) ─────────────────────────────────

// lruTouch moves tag to the front of the LRU list (mark as most recently used).
// No-op in infinite-capacity mode.
func (d *PlainVIDirectory) lruTouch(tag uint64) {
	if d.lruList == nil {
		return
	}
	if elem, ok := d.lruIndex[tag]; ok {
		d.lruList.MoveToFront(elem)
	} else {
		elem = d.lruList.PushFront(tag)
		d.lruIndex[tag] = elem
	}
}

// lruEvictIfFull evicts the least-recently-used entry when MaxEntries is
// reached, firing SharerEventKindCapacityEvict to notify listeners.
// No-op in infinite-capacity mode.
func (d *PlainVIDirectory) lruEvictIfFull() {
	if d.lruList == nil {
		return
	}
	for d.lruList.Len() > d.cfg.MaxEntries {
		back := d.lruList.Back()
		if back == nil {
			break
		}
		tag := back.Value.(uint64)
		d.lruList.Remove(back)
		delete(d.lruIndex, tag)

		e, ok := d.entries[tag]
		if !ok {
			continue
		}
		// Capture sharer set before invalidation so listeners can count messages.
		sharers := e.Sharers
		e.IsValid = false
		e.IsDirty = false
		e.Sharers = 0
		d.stats.Evictions++
		d.fireCallbacks(SharerEvent{
			Kind:      SharerEventKindCapacityEvict,
			RegionTag: tag,
			Sharers:   sharers, // pre-eviction sharer set
		})
	}
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
	// Touch LRU so a looked-up entry is not evicted before it is used.
	d.lruTouch(tag)
	return e, true
}

// Insert allocates a new entry for the region containing addr with
// initialSharers as the starting sharer set. Returns an error if the
// region already has a valid entry (caller must Lookup first to avoid
// double-insert, which would silently discard sharer state).
//
// Infinite-capacity mode: the map grows unbounded; Insert never evicts.
// If an eviction is somehow recorded, panic with "V11 violation".
//
// Finite-capacity mode: Insert may trigger LRU eviction via lruEvictIfFull
// before adding the new entry. Evictions are expected and valid.
func (d *PlainVIDirectory) Insert(addr uint64, initialSharers SharerSet) error {
	tag := d.mapper.EntryTag(addr)
	if e, ok := d.entries[tag]; ok && e.IsValid {
		return fmt.Errorf(
			"Insert: region tag 0x%x (addr 0x%x) already has a valid entry; "+
				"call Lookup before Insert",
			tag, addr,
		)
	}

	// In finite-capacity mode, evict LRU entry before inserting new one.
	// Touch first so the new entry is front-of-list after eviction.
	d.lruTouch(tag)
	d.lruEvictIfFull()

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

	// V11 guard: infinite-capacity directories must never evict.
	if d.cfg.InfiniteCapacity && d.stats.Evictions != 0 {
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
		// In finite-capacity mode, touch first then evict-if-full.
		d.lruTouch(tag)
		d.lruEvictIfFull()
		n := d.cfg.CachelinesPerRegion()
		e = &Entry{
			Tag:          tag,
			IsValid:      true,
			AccessBitmap: make([]bool, n),
		}
		d.entries[tag] = e
		d.stats.Inserts++
	} else {
		// Hit: mark as most-recently-used.
		d.lruTouch(tag)
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
			d.fireCallbacks(SharerEvent{
				Kind:      SharerEventKindWriteInvalidate,
				RegionTag: tag,
			})
		}
		e.Sharers = SharerSet(0).Add(gpu)
		e.IsDirty = true
	}

	d.fireCallbacks(SharerEvent{
		Kind:            SharerEventKindSharerUpdate,
		RegionTag:       tag,
		CachelineOffset: uint32(d.mapper.SubOffset(addr)),
		Sharers:         e.Sharers,
	})
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
		d.fireCallbacks(SharerEvent{
			Kind:      SharerEventKindEvictInvalidate,
			RegionTag: tag,
		})
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
	d.fireCallbacks(SharerEvent{
		Kind:      SharerEventKindEvictInvalidate,
		RegionTag: tag,
	})
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