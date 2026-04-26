package adapter_test

import (
	"os"
	"path/filepath"
	"testing"

	parquet "github.com/parquet-go/parquet-go"
	superdirectory "github.com/sarchlab/akita/v4/mem/cache/superdirectory"
	"github.com/sarchlab/mgpusim/v4/instrument"
	"github.com/sarchlab/mgpusim/v4/instrument/adapter"
)

// buildMinimalSuperDir creates a real superdirectory.Comp suitable for testing.
// Uses the smallest legal config: 1 bank, 64 B block, 1 sub-entry, 512 KB.
func buildMinimalSuperDir(t *testing.T) *superdirectory.Comp {
	t.Helper()
	dir := superdirectory.MakeBuilder().
		WithNumBanks(5).
		WithLog2BlockSize(6).
		WithLog2NumSubEntry(2).
		WithByteSize(512 * 1024).
		WithWayAssociativity(8).
		WithDisableCBF(true).
		WithDisableRSB(true).
		Build("Test.SuperDir")
	return dir
}

// readBankSnapshots opens the parquet file at path and reads all BankSnapshot rows.
func readBankSnapshots(t *testing.T, path string) []adapter.BankSnapshot {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("stat parquet: %v", err)
	}
	rows, err := parquet.Read[adapter.BankSnapshot](f, info.Size())
	if err != nil {
		t.Fatalf("read parquet: %v", err)
	}
	return rows
}

func TestBankSnapshotSink_WritesRowsAndFlushes(t *testing.T) {
	dir := buildMinimalSuperDir(t)
	path := filepath.Join(t.TempDir(), "snap_gpu0.parquet")

	sink, err := adapter.NewBankSnapshotSink(path, 0, dir)
	if err != nil {
		t.Fatalf("NewBankSnapshotSink: %v", err)
	}

	// Take 5 periodic + 2 kernel-boundary snapshots.
	for i := uint64(0); i < 5; i++ {
		sink.TakeSnapshot(i*1000, "", false)
	}
	sink.TakeSnapshot(5000, "kernel-A", true)
	sink.TakeSnapshot(6000, "kernel-B", true)

	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	rows := readBankSnapshots(t, path)

	if got, want := len(rows), 7; got != want {
		t.Errorf("row count = %d, want %d", got, want)
	}
	if sink.TotalRows() != 7 {
		t.Errorf("TotalRows = %d, want 7", sink.TotalRows())
	}

	// Check kernel-boundary flags.
	kbCount := 0
	for _, r := range rows {
		if r.IsKernelBoundary {
			kbCount++
		}
	}
	if kbCount != 2 {
		t.Errorf("IsKernelBoundary count = %d, want 2", kbCount)
	}
}

func TestBankSnapshotSink_CapacitiesPopulated(t *testing.T) {
	dir := buildMinimalSuperDir(t)
	path := filepath.Join(t.TempDir(), "snap.parquet")

	sink, err := adapter.NewBankSnapshotSink(path, 1, dir)
	if err != nil {
		t.Fatalf("NewBankSnapshotSink: %v", err)
	}
	sink.TakeSnapshot(0, "", false)
	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	rows := readBankSnapshots(t, path)
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	r := rows[0]

	// All capacities must be > 0 and consistent with the known formula.
	// numSet_base = 512*1024 / (8*64) = 1024; NumSets[i] = 1024>>5<<i
	wantCap := [5]int32{32 * 8, 64 * 8, 128 * 8, 256 * 8, 512 * 8}
	got := [5]int32{r.Bank0Capacity, r.Bank1Capacity, r.Bank2Capacity, r.Bank3Capacity, r.Bank4Capacity}
	if got != wantCap {
		t.Errorf("capacities = %v, want %v", got, wantCap)
	}

	// GPUID must match.
	if r.GPUID != 1 {
		t.Errorf("GPUID = %d, want 1", r.GPUID)
	}
}

func TestBankSnapshotSink_InvariantSumLeCapacity(t *testing.T) {
	dir := buildMinimalSuperDir(t)
	path := filepath.Join(t.TempDir(), "snap.parquet")

	sink, err := adapter.NewBankSnapshotSink(path, 2, dir)
	if err != nil {
		t.Fatalf("NewBankSnapshotSink: %v", err)
	}
	for i := 0; i < 20; i++ {
		sink.TakeSnapshot(uint64(i)*500, "", false)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	rows := readBankSnapshots(t, path)

	totalCap := int32(7936) // computed: sum of wantCap above
	for i, r := range rows {
		sum := r.Bank0Count + r.Bank1Count + r.Bank2Count + r.Bank3Count + r.Bank4Count
		if sum > totalCap {
			t.Errorf("row %d: sum(bank_counts)=%d > TOTAL_CAPACITY=%d", i, sum, totalCap)
		}
	}
}

func TestBankSnapshotSink_CumMetricsMonotonic(t *testing.T) {
	dir := buildMinimalSuperDir(t)
	path := filepath.Join(t.TempDir(), "snap.parquet")

	sink, err := adapter.NewBankSnapshotSink(path, 0, dir)
	if err != nil {
		t.Fatalf("NewBankSnapshotSink: %v", err)
	}
	for i := 0; i < 10; i++ {
		sink.TakeSnapshot(uint64(i)*1000, "", false)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	rows := readBankSnapshots(t, path)

	for i := 1; i < len(rows); i++ {
		if rows[i].CumEvictions < rows[i-1].CumEvictions {
			t.Errorf("cum_evictions decreased at row %d: %d < %d",
				i, rows[i].CumEvictions, rows[i-1].CumEvictions)
		}
	}
}

func TestBankSnapshotSink_PerGPUIndependence(t *testing.T) {
	dir0 := buildMinimalSuperDir(t)
	dir1 := buildMinimalSuperDir(t)
	tmp := t.TempDir()

	sink0, _ := adapter.NewBankSnapshotSink(filepath.Join(tmp, "gpu0.parquet"), 0, dir0)
	sink1, _ := adapter.NewBankSnapshotSink(filepath.Join(tmp, "gpu1.parquet"), 1, dir1)

	// GPU 0: 3 snapshots; GPU 1: 7 snapshots.
	for i := 0; i < 3; i++ {
		sink0.TakeSnapshot(uint64(i)*1000, "", false)
	}
	for i := 0; i < 7; i++ {
		sink1.TakeSnapshot(uint64(i)*1000, "", false)
	}

	sink0.Flush()
	sink1.Flush()

	if sink0.TotalRows() != 3 {
		t.Errorf("GPU0 rows = %d, want 3", sink0.TotalRows())
	}
	if sink1.TotalRows() != 7 {
		t.Errorf("GPU1 rows = %d, want 7", sink1.TotalRows())
	}
}

func TestBankSnapshotSink_WindowBoundaryFunc(t *testing.T) {
	dir := buildMinimalSuperDir(t)
	path := filepath.Join(t.TempDir(), "snap.parquet")

	sink, _ := adapter.NewBankSnapshotSink(path, 0, dir)

	cycle := uint64(42000)
	cycleGetter := func() uint64 { return cycle }

	cb := sink.WindowBoundaryFunc(cycleGetter)
	cb(instrument.PhaseID{}, instrument.PhaseID{Index: 1})

	cycle = 84000
	cb(instrument.PhaseID{Index: 1}, instrument.PhaseID{Index: 2})

	sink.Flush()

	rows := readBankSnapshots(t, path)

	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(rows))
	}
	if rows[0].SnapshotCycle != 42000 {
		t.Errorf("row0 cycle = %d, want 42000", rows[0].SnapshotCycle)
	}
	if rows[1].SnapshotCycle != 84000 {
		t.Errorf("row1 cycle = %d, want 84000", rows[1].SnapshotCycle)
	}
	if rows[0].IsKernelBoundary || rows[1].IsKernelBoundary {
		t.Error("window boundary rows should not have IsKernelBoundary=true")
	}
}

func TestBankSnapshotSink_KernelBoundaryFunc(t *testing.T) {
	dir := buildMinimalSuperDir(t)
	path := filepath.Join(t.TempDir(), "snap.parquet")

	sink, _ := adapter.NewBankSnapshotSink(path, 0, dir)
	cycle := uint64(99000)
	cb := sink.KernelBoundaryFunc(func() uint64 { return cycle })

	cb("kern-X", instrument.PhaseID{}, instrument.PhaseID{Index: 1})
	sink.Flush()

	rows := readBankSnapshots(t, path)

	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if !rows[0].IsKernelBoundary {
		t.Error("kernel boundary row should have IsKernelBoundary=true")
	}
	if rows[0].KernelID != "kern-X" {
		t.Errorf("KernelID = %q, want %q", rows[0].KernelID, "kern-X")
	}
	if rows[0].SnapshotCycle != 99000 {
		t.Errorf("SnapshotCycle = %d, want 99000", rows[0].SnapshotCycle)
	}
}
