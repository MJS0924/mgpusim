package coherence

// AddressMapper maps raw memory addresses to directory entry tags, sub-entry
// offsets, and region base addresses for a specific region-size configuration.
//
// The two key formulae:
//
//	EntryTag(addr)  = addr >> Log2RegionSize
//	SubOffset(addr) = (addr >> Log2BlockSize) % CachelinesPerRegion
//
// A-4 tag unification contract: every L1 → L2 → Directory hop that needs a
// coherence tag MUST route through AddressMapper.EntryTag(). No callsite may
// recompute the shift with its own constants. This single source of truth
// prevents the log2BlockSize subtraction being done differently in two
// places — a bug class that historically masked phase-ordering errors.
//
// A-6 coalescing-disabled contract: the mapper's output is regular (no
// tag collisions across regions, no overlap with sub-structure). The
// baseline VI directory simply looks up EntryTag() and, on access, marks
// the bit at SubOffset() — no coalescing, no promotion logic.
type AddressMapper struct {
	log2RegionSize int // log2(RegionSizeBytes)
	log2BlockSize  int // log2(BlockSizeBytes)
	numCachelines  int // 1 << (log2RegionSize - log2BlockSize)
}

// NewAddressMapper builds an AddressMapper from a validated DirectoryConfig.
// Panics on invalid config to surface misconfiguration at init time rather
// than silently producing wrong tags at runtime.
func NewAddressMapper(cfg DirectoryConfig) AddressMapper {
	if err := cfg.Validate(); err != nil {
		panic("coherence.NewAddressMapper: " + err.Error())
	}
	log2R := cfg.Log2RegionSize()
	log2B := cfg.Log2BlockSize()
	return AddressMapper{
		log2RegionSize: log2R,
		log2BlockSize:  log2B,
		numCachelines:  1 << (log2R - log2B),
	}
}

// EntryTag returns the directory entry tag for addr.
// All addresses within the same region share the same tag.
//
//	tag = addr >> Log2RegionSize
func (m AddressMapper) EntryTag(addr uint64) uint64 {
	return addr >> m.log2RegionSize
}

// SubOffset returns the cache-line index within the region containing addr.
// Range: [0, CachelinesPerRegion). At R=64B, always returns 0.
//
//	subOffset = (addr >> Log2BlockSize) & (CachelinesPerRegion - 1)
//
// Uses a bitmask AND because CachelinesPerRegion is a power of 2.
func (m AddressMapper) SubOffset(addr uint64) int {
	return int(addr>>m.log2BlockSize) & (m.numCachelines - 1)
}

// RegionBase returns the base address of the region containing addr,
// aligned to RegionSizeBytes.
func (m AddressMapper) RegionBase(addr uint64) uint64 {
	return (addr >> m.log2RegionSize) << m.log2RegionSize
}

// Log2RegionSize exposes the configured log2(RegionSizeBytes).
func (m AddressMapper) Log2RegionSize() int {
	return m.log2RegionSize
}

// Log2BlockSize exposes the configured log2(BlockSizeBytes).
func (m AddressMapper) Log2BlockSize() int {
	return m.log2BlockSize
}

// CachelinesPerRegion returns the number of cache lines per region.
func (m AddressMapper) CachelinesPerRegion() int {
	return m.numCachelines
}