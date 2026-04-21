package coherence

import (
	"testing"
)

// makeFinitieCfg returns a DirectoryConfig for finite-capacity mode.
func makeFiniteCfg(maxEntries int) DirectoryConfig {
	return DirectoryConfig{
		RegionSizeBytes:  64,
		InfiniteCapacity: false,
		MaxEntries:       maxEntries,
	}
}

// makeInfiniteCfg returns a DirectoryConfig for infinite-capacity (M1 baseline).
func makeInfiniteCfg() DirectoryConfig {
	return DirectoryConfig{
		RegionSizeBytes:  64,
		InfiniteCapacity: true,
	}
}

// TestFiniteCapacity_LRUEviction verifies that:
//   - With MaxEntries=10, inserting 15 distinct regions causes exactly 5 evictions.
//   - The 5 evicted regions are the 5 inserted first (LRU order).
//   - EvictionCount in Stats() equals 5.
func TestFiniteCapacity_LRUEviction(t *testing.T) {
	cfg := makeFiniteCfg(10)
	d, err := NewPlainVIDirectory(cfg)
	if err != nil {
		t.Fatalf("NewPlainVIDirectory: %v", err)
	}

	var evictedTags []uint64
	d.AddCallback(func(e SharerEvent) {
		if e.Kind == SharerEventKindCapacityEvict {
			evictedTags = append(evictedTags, e.RegionTag)
		}
	})

	// Insert 15 regions at addresses 0*64, 1*64, ..., 14*64 (distinct region tags).
	for i := 0; i < 15; i++ {
		addr := uint64(i) * 64
		if err := d.Insert(addr, SharerSet(0)); err != nil {
			t.Fatalf("Insert(%d): %v", i, err)
		}
	}

	stats := d.Stats()
	if stats.Evictions != 5 {
		t.Errorf("expected 5 evictions, got %d", stats.Evictions)
	}
	if len(evictedTags) != 5 {
		t.Errorf("expected 5 evict callbacks, got %d", len(evictedTags))
	}
	// The first 5 inserted (tags 0..4) should be evicted (they are LRU).
	for i, tag := range evictedTags {
		if tag != uint64(i) {
			t.Errorf("evicted[%d]: expected tag %d, got %d", i, i, tag)
		}
	}
}

// TestFiniteCapacity_LRUOrder verifies that accessing a region keeps it alive.
// Insert 10 regions, touch region 0 (making it MRU), then insert 1 more.
// Region 1 (now LRU, not region 0) should be evicted.
func TestFiniteCapacity_LRUOrder(t *testing.T) {
	cfg := makeFiniteCfg(10)
	d, err := NewPlainVIDirectory(cfg)
	if err != nil {
		t.Fatalf("NewPlainVIDirectory: %v", err)
	}

	var evictedTags []uint64
	d.AddCallback(func(e SharerEvent) {
		if e.Kind == SharerEventKindCapacityEvict {
			evictedTags = append(evictedTags, e.RegionTag)
		}
	})

	// Insert regions 0..9.
	for i := 0; i < 10; i++ {
		if err := d.Insert(uint64(i)*64, SharerSet(0)); err != nil {
			t.Fatalf("Insert(%d): %v", i, err)
		}
	}

	// Touch region 0 via UpdateSharers (makes it MRU; region 1 becomes LRU).
	if err := d.UpdateSharers(0, GPUID(1), OpRead); err != nil {
		t.Fatalf("UpdateSharers: %v", err)
	}

	// Insert region 10 → should evict region 1 (LRU, not region 0).
	if err := d.Insert(uint64(10)*64, SharerSet(0)); err != nil {
		t.Fatalf("Insert(10): %v", err)
	}

	if len(evictedTags) != 1 {
		t.Fatalf("expected 1 eviction, got %d", len(evictedTags))
	}
	if evictedTags[0] != 1 {
		t.Errorf("expected region 1 evicted, got region %d", evictedTags[0])
	}
}

// TestFiniteCapacity_EvictionInvalidationCount verifies that
// SharerEventKindCapacityEvict carries the pre-eviction sharer set
// so callers can count invalidation messages.
func TestFiniteCapacity_EvictionInvalidationCount(t *testing.T) {
	cfg := makeFiniteCfg(1)
	d, err := NewPlainVIDirectory(cfg)
	if err != nil {
		t.Fatalf("NewPlainVIDirectory: %v", err)
	}

	var capturedEvent SharerEvent
	d.AddCallback(func(e SharerEvent) {
		if e.Kind == SharerEventKindCapacityEvict {
			capturedEvent = e
		}
	})

	// Insert region 0 with 2 sharers.
	d.UpdateSharers(0, GPUID(1), OpRead)
	d.UpdateSharers(0, GPUID(2), OpRead)

	// Insert region 64 → capacity=1 → region 0 evicted.
	if err := d.Insert(64, SharerSet(0)); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if capturedEvent.Sharers.Len() != 2 {
		t.Errorf("expected 2 sharers in evict event, got %d", capturedEvent.Sharers.Len())
	}
}

// TestFiniteCapacity_NoV11Panic verifies that finite-capacity mode does NOT
// panic on evictions (the V11 panic fires only in infinite-capacity mode).
func TestFiniteCapacity_NoV11Panic(t *testing.T) {
	cfg := makeFiniteCfg(2)
	d, err := NewPlainVIDirectory(cfg)
	if err != nil {
		t.Fatalf("NewPlainVIDirectory: %v", err)
	}
	// Insert 3 entries: should trigger 1 eviction without panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic in finite mode: %v", r)
		}
	}()
	for i := 0; i < 3; i++ {
		d.Insert(uint64(i)*64, SharerSet(0))
	}
}

// TestInfiniteCapacity_BackwardCompat verifies that the existing infinite-capacity
// mode still works after the DirectoryConfig change (MaxEntries=0, InfiniteCapacity=true).
func TestInfiniteCapacity_BackwardCompat(t *testing.T) {
	cfg := makeInfiniteCfg()
	d, err := NewPlainVIDirectory(cfg)
	if err != nil {
		t.Fatalf("NewPlainVIDirectory: %v", err)
	}
	// Insert many entries — no evictions should occur.
	for i := 0; i < 100; i++ {
		if err := d.Insert(uint64(i)*64, SharerSet(0)); err != nil {
			t.Fatalf("Insert(%d): %v", i, err)
		}
	}
	stats := d.Stats()
	if stats.Evictions != 0 {
		t.Errorf("expected 0 evictions in infinite mode, got %d", stats.Evictions)
	}
}

// TestFiniteCapacity_ValidateRejectsConflict verifies that Validate() rejects
// InfiniteCapacity=true with MaxEntries>0 and InfiniteCapacity=false with MaxEntries=0.
func TestFiniteCapacity_ValidateRejectsConflict(t *testing.T) {
	tests := []struct {
		name    string
		cfg     DirectoryConfig
		wantErr bool
	}{
		{
			name:    "infinite+no-max: valid",
			cfg:     DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: true, MaxEntries: 0},
			wantErr: false,
		},
		{
			name:    "finite+max: valid",
			cfg:     DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: false, MaxEntries: 8192},
			wantErr: false,
		},
		{
			name:    "infinite+max: invalid",
			cfg:     DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: true, MaxEntries: 8192},
			wantErr: true,
		},
		{
			name:    "finite+no-max: invalid",
			cfg:     DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: false, MaxEntries: 0},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
