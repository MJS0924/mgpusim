package adapter

import (
	"os"
	"path/filepath"
	"testing"

	parquet "github.com/parquet-go/parquet-go"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

func TestParquetSnapshotSink_100Rows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.parquet")

	sink, err := NewParquetSnapshotSink(path, 1, 2)
	if err != nil {
		t.Fatalf("NewParquetSnapshotSink: %v", err)
	}

	// Push 100 snapshots.
	for i := 0; i < 100; i++ {
		snap := instrument.PhaseMetrics{
			PhaseID:             instrument.PhaseID{ConfigID: 1, WorkloadID: 2, Index: uint32(i)},
			StartCycle:          uint64(i * 1000),
			EndCycle:            uint64(i*1000 + 999),
			L2Hits:              uint64(i * 3),
			L2Misses:            uint64(i * 2),
			RegionFetchedBytes:  uint64(i * 64),
			RegionAccessedBytes: uint64(i * 32),
			ActiveRegions:       uint64(i),
			RetiredWavefronts:   uint64(i + 1),
		}
		if err := sink.PushSnapshot(snap); err != nil {
			t.Fatalf("PushSnapshot %d: %v", i, err)
		}
	}

	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify directory was auto-created.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("output file missing: %v", err)
	}

	// Read back and verify row count + spot-check fields.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer f.Close()

	fi, _ := f.Stat()
	reader := parquet.NewGenericReader[ParquetSnapshot](f, parquet.NewSchema("", parquet.SchemaOf(ParquetSnapshot{})))
	defer reader.Close()

	rows := make([]ParquetSnapshot, fi.Size()) // oversize buffer
	n, err := reader.Read(rows)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("reader.Read: %v", err)
	}
	rows = rows[:n]

	if n != 100 {
		t.Fatalf("want 100 rows; got %d", n)
	}

	// Spot-check row 0 and row 99.
	r0 := rows[0]
	if r0.ConfigID != 1 || r0.WorkloadID != 2 {
		t.Errorf("row0: want ConfigID=1 WorkloadID=2; got %d %d", r0.ConfigID, r0.WorkloadID)
	}
	if r0.PhaseIndex != 0 || r0.L2Hits != 0 || r0.RetiredWavefronts != 1 {
		t.Errorf("row0 fields mismatch: %+v", r0)
	}

	r99 := rows[99]
	if r99.PhaseIndex != 99 || r99.L2Hits != 99*3 || r99.RetiredWavefronts != 100 {
		t.Errorf("row99 fields mismatch: %+v", r99)
	}
}

func TestParquetSnapshotSink_SubdirAutoCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "out.parquet")

	sink, err := NewParquetSnapshotSink(path, 0, 0)
	if err != nil {
		t.Fatalf("NewParquetSnapshotSink: %v", err)
	}
	_ = sink.PushSnapshot(instrument.PhaseMetrics{})
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nested dir auto-create failed: %v", err)
	}
}

func TestParquetSnapshotSink_PhaseCount(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewParquetSnapshotSink(filepath.Join(dir, "t.parquet"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		_ = sink.PushSnapshot(instrument.PhaseMetrics{})
	}
	if sink.PhaseCount() != 7 {
		t.Errorf("want PhaseCount=7; got %d", sink.PhaseCount())
	}
	_ = sink.Close()
}