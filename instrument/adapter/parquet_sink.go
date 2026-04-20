package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	parquet "github.com/parquet-go/parquet-go"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// ParquetSnapshot is the flat schema written to disk.
// DSAccesses and AddrBucketAccesses are excluded for Phase C;
// they will be added in Phase D if needed.
type ParquetSnapshot struct {
	ConfigID                uint32 `parquet:"config_id"`
	WorkloadID              uint32 `parquet:"workload_id"`
	PhaseIndex              uint64 `parquet:"phase_index"`
	StartCycle              uint64 `parquet:"start_cycle"`
	EndCycle                uint64 `parquet:"end_cycle"`
	L2Hits                  uint64 `parquet:"l2_hits"`
	L2Misses                uint64 `parquet:"l2_misses"`
	RegionFetchedBytes      uint64 `parquet:"region_fetched_bytes"`
	RegionAccessedBytes     uint64 `parquet:"region_accessed_bytes"`
	ActiveRegions           uint64 `parquet:"active_regions"`
	SharerConsistentRegions uint64 `parquet:"sharer_consistent_regions"`
	WriteInitInvalidations  uint64 `parquet:"write_init_invalidations"`
	EvictInitInvalidations  uint64 `parquet:"evict_init_invalidations"`
	DirectoryEvictions      uint64 `parquet:"directory_evictions"`
	RetiredWavefronts       uint64 `parquet:"retired_wavefronts"`
}

// ParquetSnapshotSink writes phase snapshots to a parquet file.
// It buffers autoFlushEvery snapshots before flushing to disk.
type ParquetSnapshotSink struct {
	filepath       string
	configID       uint16
	workloadID     uint16
	autoFlushEvery int

	mu                   sync.Mutex
	buf                  []ParquetSnapshot
	writer               *parquet.GenericWriter[ParquetSnapshot]
	file                 *os.File
	phaseN               uint64
	totalRetiredWf       uint64
	totalL2Hits          uint64
	totalL2Misses        uint64
	totalRegionFetched   uint64
	totalRegionAccessed  uint64
}

// NewParquetSnapshotSink creates a ParquetSnapshotSink writing to path.
// The directory is created if it does not exist.
// configID and workloadID are embedded in every row (matching PhaseID types).
func NewParquetSnapshotSink(
	path string,
	configID, workloadID uint16,
) (*ParquetSnapshotSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("ParquetSnapshotSink: mkdir %q: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("ParquetSnapshotSink: create %q: %w", path, err)
	}
	w := parquet.NewGenericWriter[ParquetSnapshot](f)
	return &ParquetSnapshotSink{
		filepath:       path,
		configID:       configID,
		workloadID:     workloadID,
		autoFlushEvery: 100,
		buf:            make([]ParquetSnapshot, 0, 100),
		writer:         w,
		file:           f,
	}, nil
}

// PushSnapshot implements instrument.SnapshotSink.
// It converts the PhaseMetrics snapshot to ParquetSnapshot and buffers it.
func (s *ParquetSnapshotSink) PushSnapshot(snap instrument.PhaseMetrics) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := ParquetSnapshot{
		ConfigID:                uint32(s.configID),
		WorkloadID:              uint32(s.workloadID),
		PhaseIndex:              uint64(snap.PhaseID.Index),
		StartCycle:              snap.StartCycle,
		EndCycle:                snap.EndCycle,
		L2Hits:                  snap.L2Hits,
		L2Misses:                snap.L2Misses,
		RegionFetchedBytes:      snap.RegionFetchedBytes,
		RegionAccessedBytes:     snap.RegionAccessedBytes,
		ActiveRegions:           snap.ActiveRegions,
		SharerConsistentRegions: snap.SharerConsistentRegions,
		WriteInitInvalidations:  snap.WriteInitInvalidations,
		EvictInitInvalidations:  snap.EvictInitInvalidations,
		DirectoryEvictions:      snap.DirectoryEvictions,
		RetiredWavefronts:       snap.RetiredWavefronts,
	}
	s.buf = append(s.buf, row)
	s.phaseN++
	s.totalRetiredWf += snap.RetiredWavefronts
	s.totalL2Hits += snap.L2Hits
	s.totalL2Misses += snap.L2Misses
	s.totalRegionFetched += snap.RegionFetchedBytes
	s.totalRegionAccessed += snap.RegionAccessedBytes

	if len(s.buf) >= s.autoFlushEvery {
		return s.flushLocked()
	}
	return nil
}

// Close flushes remaining buffered rows and closes the file.
func (s *ParquetSnapshotSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.flushLocked(); err != nil {
		return err
	}
	if err := s.writer.Close(); err != nil {
		return fmt.Errorf("ParquetSnapshotSink close writer: %w", err)
	}
	return s.file.Close()
}

// PhaseCount returns how many snapshots have been pushed so far.
func (s *ParquetSnapshotSink) PhaseCount() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phaseN
}

// TotalRetiredWavefronts returns the sum of RetiredWavefronts across all phases.
func (s *ParquetSnapshotSink) TotalRetiredWavefronts() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalRetiredWf
}

// Totals returns (L2Hits, L2Misses, RegionFetchedBytes, RegionAccessedBytes)
// accumulated across all phases.
func (s *ParquetSnapshotSink) Totals() (l2h, l2m, fetched, accessed uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalL2Hits, s.totalL2Misses, s.totalRegionFetched, s.totalRegionAccessed
}

// Filepath returns the output file path.
func (s *ParquetSnapshotSink) Filepath() string { return s.filepath }

func (s *ParquetSnapshotSink) flushLocked() error {
	if len(s.buf) == 0 {
		return nil
	}
	if _, err := s.writer.Write(s.buf); err != nil {
		return fmt.Errorf("ParquetSnapshotSink write: %w", err)
	}
	s.buf = s.buf[:0]
	return nil
}