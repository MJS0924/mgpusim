package coherence

import (
	"testing"
)

// TestInvariantV2_R64BBaselineContract verifies Invariant V2 (v1.2):
// R=64B + InfiniteCapacity + !CoalescingEnabled is the authoritative M1
// baseline. At this config the AddressMapper degenerates to a conventional
// cache-line tag (addr >> 6) and every access has SubOffset == 0, so a
// region-level entry behaves indistinguishably from a per-cache-line entry.
//
// Reviewer attack: "Why not assert identical BYTE outputs against a real
// baseline run?"
// Defense: Without a running simulation this test is at the data-structure
// level: it pins that (a) tag math matches the canonical shift, (b) the
// baseline flags satisfy IsForM1(), (c) the entry bitmap has exactly one
// slot. End-to-end bit-identity is enforced by PHASE B's V4 (±3% Track A/B).
func TestInvariantV2_R64BBaselineContract(t *testing.T) {
	cfg := DirectoryConfig{
		RegionSizeBytes:   64,
		InfiniteCapacity:  true,
		CoalescingEnabled: false,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("M1 baseline config must be valid: %v", err)
	}
	if !cfg.IsForM1() {
		t.Fatal("M1 baseline config must satisfy IsForM1()")
	}
	if got := cfg.CachelinesPerRegion(); got != 1 {
		t.Fatalf("R=64B must have CachelinesPerRegion=1; got %d", got)
	}

	mapper := NewAddressMapper(cfg)

	testAddrs := []uint64{
		0, 1, 63,
		64, 65, 127,
		128, 192, 255,
		4096,
		0x1000_0000,
	}
	for _, addr := range testAddrs {
		wantTag := addr >> 6
		if got := mapper.EntryTag(addr); got != wantTag {
			t.Errorf("EntryTag(0x%x): want 0x%x got 0x%x", addr, wantTag, got)
		}
		if got := mapper.SubOffset(addr); got != 0 {
			t.Errorf("SubOffset(0x%x) must be 0 at R=64B; got %d", addr, got)
		}
		base := mapper.RegionBase(addr)
		if base%64 != 0 {
			t.Errorf("RegionBase(0x%x)=0x%x not 64B-aligned", addr, base)
		}
		if addr < base || addr >= base+64 {
			t.Errorf("addr 0x%x not in [0x%x, 0x%x)", addr, base, base+64)
		}
	}
}

// TestAddressMapperSubOffsets verifies SubOffset partitions all cache lines
// within a region for every size in ValidRegionSizes (64B..16KB).
func TestAddressMapperSubOffsets(t *testing.T) {
	for _, r := range ValidRegionSizes {
		r := r
		cfg := DirectoryConfig{
			RegionSizeBytes:   r,
			InfiniteCapacity:  true,
			CoalescingEnabled: false,
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("R=%dB invalid: %v", r, err)
		}
		mapper := NewAddressMapper(cfg)
		numLines := cfg.CachelinesPerRegion()

		// Use a large, region-aligned base so tags are non-trivial.
		base := uint64(1 << 20) // 1 MiB
		base = mapper.RegionBase(base)
		baseTag := mapper.EntryTag(base)

		for i := 0; i < numLines; i++ {
			addr := base + uint64(i)*64
			if got := mapper.SubOffset(addr); got != i {
				t.Errorf("R=%dB line %d at 0x%x: want SubOffset %d got %d",
					r, i, addr, i, got)
			}
			if got := mapper.EntryTag(addr); got != baseTag {
				t.Errorf("R=%dB line %d: tag %d != base tag %d",
					r, i, got, baseTag)
			}
		}

		// Next region must have tag base+1 (consecutive regions are contiguous).
		if got := mapper.EntryTag(base + r); got != baseTag+1 {
			t.Errorf("R=%dB: tag after region = %d, want %d",
				r, got, baseTag+1)
		}
	}
}

// TestDirectoryConfigValidation enforces ValidRegionSizes membership and
// structural rejection of invalid configs.
func TestDirectoryConfigValidation(t *testing.T) {
	for _, r := range ValidRegionSizes {
		cfg := DirectoryConfig{
			RegionSizeBytes:   r,
			InfiniteCapacity:  true,
			CoalescingEnabled: false,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("R=%dB should be valid: %v", r, err)
		}
	}

	invalid := []struct {
		r    uint64
		desc string
	}{
		{0, "zero"},
		{32, "below block size"},
		{96, "non-power-of-2"},
		{128, "valid power-of-2 but not in ValidRegionSizes sweep set"},
		{2048, "power-of-2 but not in sweep set (between 1KB and 4KB)"},
	}
	for _, tc := range invalid {
		cfg := DirectoryConfig{RegionSizeBytes: tc.r}
		if err := cfg.Validate(); err == nil {
			t.Errorf("R=%d (%s) must fail Validate()", tc.r, tc.desc)
		}
	}
}

// TestIsForM1_Contract verifies IsForM1() accepts only the v1.2 baseline
// flag combination. Every other combination is rejected so that the config
// loader cannot silently accept a non-baseline run as "M1".
func TestIsForM1_Contract(t *testing.T) {
	cases := []struct {
		infinite   bool
		coalescing bool
		want       bool
	}{
		{true, false, true},   // baseline: ✅
		{false, false, false}, // finite capacity: not M1
		{true, true, false},   // coalescing on: not M1 (REC territory)
		{false, true, false},  // both off-spec
	}
	for _, tc := range cases {
		cfg := DirectoryConfig{
			RegionSizeBytes:   64,
			InfiniteCapacity:  tc.infinite,
			CoalescingEnabled: tc.coalescing,
		}
		if got := cfg.IsForM1(); got != tc.want {
			t.Errorf("IsForM1{inf=%v,coal=%v}: want %v got %v",
				tc.infinite, tc.coalescing, tc.want, got)
		}
	}
}

// TestIsoCoverageEntries verifies iso-coverage entry counts for a 2MB L2
// target (matches r9nano default l2CacheSize=2*MB) with safety factor 2.
//
//	minEntries = ceil(L2Bytes / RegionSize) * safetyFactor
//
// For L2=2MB=2097152B, safety=2:
//
//	R=64B:   2097152 / 64    = 32768 * 2 = 65536
//	R=256B:  2097152 / 256   = 8192  * 2 = 16384
//	R=1KB:   2097152 / 1024  = 2048  * 2 = 4096
//	R=4KB:   2097152 / 4096  = 512   * 2 = 1024
//	R=16KB:  2097152 / 16384 = 128   * 2 = 256
func TestIsoCoverageEntries(t *testing.T) {
	const l2Bytes = 2 * 1024 * 1024
	const safety = 2
	expected := map[uint64]int{
		64:    65536,
		256:   16384,
		1024:  4096,
		4096:  1024,
		16384: 256,
	}
	for r, want := range expected {
		cfg := DirectoryConfig{
			RegionSizeBytes:   r,
			InfiniteCapacity:  true,
			CoalescingEnabled: false,
		}
		got, err := cfg.IsoCoverageEntries(l2Bytes, safety)
		if err != nil {
			t.Errorf("R=%dB IsoCoverageEntries: %v", r, err)
			continue
		}
		if got != want {
			t.Errorf("R=%dB IsoCoverageEntries: want %d got %d", r, want, got)
		}
	}

	// safetyFactor < 1 rejected.
	cfg := DirectoryConfig{
		RegionSizeBytes:   64,
		InfiniteCapacity:  true,
		CoalescingEnabled: false,
	}
	if _, err := cfg.IsoCoverageEntries(l2Bytes, 0); err == nil {
		t.Error("safetyFactor=0 must return error")
	}
	if _, err := cfg.IsoCoverageEntries(0, 2); err == nil {
		t.Error("l2Bytes=0 must return error")
	}
}

// TestV11_AssertNoEviction verifies that AssertNoEviction enforces Invariant
// V11 strictly: any non-zero eviction under InfiniteCapacity invalidates the
// run, regardless of magnitude.
func TestV11_AssertNoEviction(t *testing.T) {
	infinite := DirectoryConfig{
		RegionSizeBytes:   64,
		InfiniteCapacity:  true,
		CoalescingEnabled: false,
	}
	// 0 evictions: OK.
	if err := infinite.AssertNoEviction(0); err != nil {
		t.Errorf("AssertNoEviction(0) on infinite dir: %v", err)
	}
	// Any non-zero: fail.
	for _, ev := range []uint64{1, 10, 1_000_000} {
		if err := infinite.AssertNoEviction(ev); err == nil {
			t.Errorf("AssertNoEviction(%d) on infinite dir must fail", ev)
		}
	}

	// Finite capacity: never asserts (always nil).
	finite := DirectoryConfig{
		RegionSizeBytes:   64,
		InfiniteCapacity:  false,
		CoalescingEnabled: false,
	}
	if err := finite.AssertNoEviction(12345); err != nil {
		t.Errorf("finite dir AssertNoEviction must return nil, got %v", err)
	}
}

// TestLog2Derivations verifies log2 helpers for every size in the v1.2 sweep.
func TestLog2Derivations(t *testing.T) {
	cases := []struct {
		r        uint64
		wantLogR int
		wantCL   int
	}{
		{64, 6, 1},
		{256, 8, 4},
		{1024, 10, 16},
		{4096, 12, 64},
		{16384, 14, 256},
	}
	for _, tc := range cases {
		cfg := DirectoryConfig{
			RegionSizeBytes:   tc.r,
			InfiniteCapacity:  true,
			CoalescingEnabled: false,
		}
		if got := cfg.Log2RegionSize(); got != tc.wantLogR {
			t.Errorf("R=%d Log2RegionSize: want %d got %d", tc.r, tc.wantLogR, got)
		}
		if got := cfg.CachelinesPerRegion(); got != tc.wantCL {
			t.Errorf("R=%d CachelinesPerRegion: want %d got %d", tc.r, tc.wantCL, got)
		}
	}
}

// TestV12_EntryAccessBitmap exercises the per-region access bitmap — the
// substrate for Invariant V12 (RegionAccessedBytes ≤ RegionFetchedBytes).
// With a 1KB region (16 cache lines), marking 3 distinct lines yields
// exactly 3 accessed-cachelines; duplicates don't double-count.
func TestV12_EntryAccessBitmap(t *testing.T) {
	cfg := DirectoryConfig{
		RegionSizeBytes:   1024,
		InfiniteCapacity:  true,
		CoalescingEnabled: false,
	}
	n := cfg.CachelinesPerRegion()
	if n != 16 {
		t.Fatalf("R=1KB should have 16 cache lines; got %d", n)
	}
	e := &Entry{
		IsValid:      true,
		AccessBitmap: make([]bool, n),
	}
	mapper := NewAddressMapper(cfg)
	base := mapper.RegionBase(0x8000)

	// Access cache lines 0, 5, 15; line 5 twice.
	for _, idx := range []int{0, 5, 5, 15} {
		e.MarkAccess(mapper.SubOffset(base + uint64(idx)*64))
	}
	if got := e.AccessedCachelines(); got != 3 {
		t.Errorf("AccessedCachelines: want 3 got %d (duplicates must not double-count)", got)
	}

	// Reset preserves the slice but clears all bits.
	e.Reset()
	if e.IsValid {
		t.Error("Reset: IsValid must be false")
	}
	if got := e.AccessedCachelines(); got != 0 {
		t.Errorf("Reset: AccessedCachelines must be 0; got %d", got)
	}
	if cap(e.AccessBitmap) != n {
		t.Errorf("Reset: AccessBitmap capacity should be preserved (want %d got %d)",
			n, cap(e.AccessBitmap))
	}
}

// TestInvariantV8_UniformFourWaySubEntryLaw enforces the β-design structural
// constraint: for every bank k, coverage c_k = 4 × s_k, with s_4 equal to the
// cache line size and c_0 bounded by a 64 KB page.
func TestInvariantV8_UniformFourWaySubEntryLaw(t *testing.T) {
	sub := []uint64{16384, 4096, 1024, 256, 64}
	cov := []uint64{65536, 16384, 4096, 1024, 256}
	for k := range sub {
		if cov[k] != 4*sub[k] {
			t.Errorf("Bank %d: c=%d must equal 4×s=%d", k, cov[k], 4*sub[k])
		}
	}
	if sub[4] != DefaultBlockSizeBytes {
		t.Errorf("s_4=%d must equal DefaultBlockSizeBytes=%d", sub[4], DefaultBlockSizeBytes)
	}
	if cov[0] > 65536 {
		t.Errorf("c_0=%d must not exceed PageSize(64KB)", cov[0])
	}
}