package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	parquet "github.com/parquet-go/parquet-go"
	superdirectory "github.com/sarchlab/akita/v4/mem/cache/superdirectory"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// BankSnapshot is one Parquet row written by BankSnapshotSink.
type BankSnapshot struct {
	SnapshotCycle uint64 `parquet:"snapshot_cycle"`
	GPUID         int32  `parquet:"gpu_id"`
	KernelID      string `parquet:"kernel_id"`

	Bank0Count int32 `parquet:"bank0_count"`
	Bank1Count int32 `parquet:"bank1_count"`
	Bank2Count int32 `parquet:"bank2_count"`
	Bank3Count int32 `parquet:"bank3_count"`
	Bank4Count int32 `parquet:"bank4_count"`

	Bank0Capacity int32 `parquet:"bank0_capacity"`
	Bank1Capacity int32 `parquet:"bank1_capacity"`
	Bank2Capacity int32 `parquet:"bank2_capacity"`
	Bank3Capacity int32 `parquet:"bank3_capacity"`
	Bank4Capacity int32 `parquet:"bank4_capacity"`

	CumPromotions   int64  `parquet:"cum_promotions"`
	CumDemotions    int64  `parquet:"cum_demotions"`
	CumEvictions    uint64 `parquet:"cum_evictions"`
	CumAllocations  uint64 `parquet:"cum_allocations"`

	IsKernelBoundary bool `parquet:"is_kernel_boundary"`
}

// BankSnapshotSink writes per-GPU superdirectory bank state snapshots to Parquet.
//
// Wire-up (per GPU):
//
//	sink := NewBankSnapshotSink(path, gpuID, superDir)
//	clock.OnWindowBoundary(func(_, _ instrument.PhaseID) { sink.TakeSnapshot(clock, "") })
//	cp.AcceptHook(&kernelCompleteSinkHook{sink: sink, clock: clock})
type BankSnapshotSink struct {
	mu    sync.Mutex
	path  string
	gpuID int32
	dir   *superdirectory.Comp

	buf    []BankSnapshot
	writer *parquet.GenericWriter[BankSnapshot]
	file   *os.File

	cumPromotions int64
	cumDemotions  int64
	totalRows     uint64

	// capacities are read once at init (static for lifetime of simulation).
	capacities [5]int32
}

// NewBankSnapshotSink creates a sink writing to path for the given GPU and superdirectory.
// The directory must not be nil. path's parent directory is created if missing.
func NewBankSnapshotSink(path string, gpuID int32, dir *superdirectory.Comp) (*BankSnapshotSink, error) {
	if dir == nil {
		return nil, fmt.Errorf("BankSnapshotSink: superdirectory must not be nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("BankSnapshotSink: mkdir %q: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("BankSnapshotSink: create %q: %w", path, err)
	}
	w := parquet.NewGenericWriter[BankSnapshot](f)

	s := &BankSnapshotSink{
		path:  path,
		gpuID: gpuID,
		dir:   dir,
		buf:   make([]BankSnapshot, 0, 256),
		writer: w,
		file:  f,
	}
	for b := 0; b < 5; b++ {
		s.capacities[b] = int32(dir.BankMaxCapacity(b))
	}
	return s, nil
}

// TakeSnapshot captures the current bank state and enqueues it.
// cycle is the current simulation cycle; kernelID is non-empty on kernel boundaries.
// isKernelBoundary should be true when called from a kernel-complete hook.
func (s *BankSnapshotSink) TakeSnapshot(cycle uint64, kernelID string, isKernelBoundary bool) {
	el := s.dir.EventLogger()
	var promos, demotos int64
	if el != nil {
		for _, ev := range el.Events() {
			if ev.Type == 0 { // MotionEventPromotion
				promos++
			} else {
				demotos++
			}
		}
	}
	s.cumPromotions = promos
	s.cumDemotions = demotos

	row := BankSnapshot{
		SnapshotCycle: cycle,
		GPUID:         s.gpuID,
		KernelID:      kernelID,

		Bank0Count: int32(s.dir.BankEntryCount(0)),
		Bank1Count: int32(s.dir.BankEntryCount(1)),
		Bank2Count: int32(s.dir.BankEntryCount(2)),
		Bank3Count: int32(s.dir.BankEntryCount(3)),
		Bank4Count: int32(s.dir.BankEntryCount(4)),

		Bank0Capacity: s.capacities[0],
		Bank1Capacity: s.capacities[1],
		Bank2Capacity: s.capacities[2],
		Bank3Capacity: s.capacities[3],
		Bank4Capacity: s.capacities[4],

		CumPromotions:  s.cumPromotions,
		CumDemotions:   s.cumDemotions,
		CumEvictions:   s.dir.EvictCount(),
		CumAllocations: s.dir.AllocationCount(),

		IsKernelBoundary: isKernelBoundary,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, row)
	s.totalRows++
	if len(s.buf) >= 256 {
		_ = s.flushLocked()
	}
}

// Flush writes all buffered rows and closes the Parquet file.
func (s *BankSnapshotSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.flushLocked(); err != nil {
		return err
	}
	if err := s.writer.Close(); err != nil {
		return fmt.Errorf("BankSnapshotSink close writer: %w", err)
	}
	return s.file.Close()
}

// TotalRows returns the total number of snapshot rows written (including buffered).
func (s *BankSnapshotSink) TotalRows() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalRows
}

// Path returns the output file path.
func (s *BankSnapshotSink) Path() string { return s.path }

// DiagCounts exposes the superdirectory's diagnostic counters for investigation.
func (s *BankSnapshotSink) DiagCounts() (remoteAccept, doWriteMiss, doWriteMissRemote uint64) {
	return s.dir.DiagCounts()
}

func (s *BankSnapshotSink) flushLocked() error {
	if len(s.buf) == 0 {
		return nil
	}
	if _, err := s.writer.Write(s.buf); err != nil {
		return fmt.Errorf("BankSnapshotSink write: %w", err)
	}
	s.buf = s.buf[:0]
	return nil
}

// WindowBoundaryFunc returns a PhaseClock.OnWindowBoundary-compatible callback
// that takes a periodic snapshot of this GPU's bank state.
func (s *BankSnapshotSink) WindowBoundaryFunc(cycleGetter func() uint64) func(_, _ instrument.PhaseID) {
	return func(_, _ instrument.PhaseID) {
		s.TakeSnapshot(cycleGetter(), "", false)
	}
}

// KernelBoundaryFunc returns a PhaseClock.OnKernelBoundary-compatible callback
// that takes a kernel-boundary snapshot of this GPU's bank state.
func (s *BankSnapshotSink) KernelBoundaryFunc(cycleGetter func() uint64) func(string, instrument.PhaseID, instrument.PhaseID) {
	return func(kernelID string, _, _ instrument.PhaseID) {
		s.TakeSnapshot(cycleGetter(), kernelID, true)
	}
}
