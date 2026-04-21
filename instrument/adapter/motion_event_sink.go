package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	parquet "github.com/parquet-go/parquet-go"
	"github.com/sarchlab/akita/v4/mem/cache/superdirectory"
)

// ParquetMotionEvent is the flat parquet schema for one promotion/demotion.
type ParquetMotionEvent struct {
	EventType   string  `parquet:"event_type"`
	TimeSec     float64 `parquet:"time_sec"`
	Address     uint64  `parquet:"address"`
	FromBank    int32   `parquet:"from_bank"`
	ToBank      int32   `parquet:"to_bank"`
	SharerCount int32   `parquet:"sharer_count"`
	ValidSubs   int32   `parquet:"valid_subs"`
	Utilization float64 `parquet:"utilization"`
}

// MotionEventSink writes superdirectory promotion/demotion events to parquet.
// It buffers autoFlushEvery rows before flushing.
type MotionEventSink struct {
	mu             sync.Mutex
	filepath       string
	autoFlushEvery int
	buf            []ParquetMotionEvent
	writer         *parquet.GenericWriter[ParquetMotionEvent]
	file           *os.File
	totalPromotion uint64
	totalDemotion  uint64
}

// NewMotionEventSink creates a sink writing to path.
// The directory is created when needed.
func NewMotionEventSink(path string) (*MotionEventSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("MotionEventSink: mkdir %q: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("MotionEventSink: create %q: %w", path, err)
	}
	w := parquet.NewGenericWriter[ParquetMotionEvent](f)
	return &MotionEventSink{
		filepath:       path,
		autoFlushEvery: 4096,
		buf:            make([]ParquetMotionEvent, 0, 4096),
		writer:         w,
		file:           f,
	}, nil
}

// FlushLoggers drains events from all provided EventLoggers and writes them
// to the parquet file. Call this after simulation completes.
func (s *MotionEventSink) FlushLoggers(loggers []*superdirectory.EventLogger) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, l := range loggers {
		for _, ev := range l.Events() {
			row := s.convert(ev)
			s.buf = append(s.buf, row)
			if ev.Type == superdirectory.MotionEventPromotion {
				s.totalPromotion++
			} else {
				s.totalDemotion++
			}
			if len(s.buf) >= s.autoFlushEvery {
				if err := s.flushLocked(); err != nil {
					return err
				}
			}
		}
	}
	return s.flushLocked()
}

// Close finalises the parquet file. Call after FlushLoggers.
func (s *MotionEventSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.flushLocked(); err != nil {
		return err
	}
	if err := s.writer.Close(); err != nil {
		return fmt.Errorf("MotionEventSink close writer: %w", err)
	}
	return s.file.Close()
}

// Counts returns (promotions, demotions) written so far.
func (s *MotionEventSink) Counts() (uint64, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalPromotion, s.totalDemotion
}

// Filepath returns the output path.
func (s *MotionEventSink) Filepath() string { return s.filepath }

func (s *MotionEventSink) convert(ev superdirectory.MotionEvent) ParquetMotionEvent {
	evType := "promote"
	if ev.Type == superdirectory.MotionEventDemotion {
		evType = "demote"
	}
	return ParquetMotionEvent{
		EventType:   evType,
		TimeSec:     ev.Time,
		Address:     ev.Address,
		FromBank:    ev.FromBank,
		ToBank:      ev.ToBank,
		SharerCount: ev.SharerCount,
		ValidSubs:   ev.ValidSubs,
		Utilization: ev.Utilization,
	}
}

func (s *MotionEventSink) flushLocked() error {
	if len(s.buf) == 0 {
		return nil
	}
	if _, err := s.writer.Write(s.buf); err != nil {
		return fmt.Errorf("MotionEventSink write: %w", err)
	}
	s.buf = s.buf[:0]
	return nil
}
