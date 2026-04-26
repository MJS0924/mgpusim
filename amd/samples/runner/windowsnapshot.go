package runner

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/tracing"
)

// windowSnapshotter captures cumulative metrics at every N-instruction boundary.
// It implements tracing.Tracer so it can be attached directly to compute units.
// All snapshot state is guarded by mu for parallel-engine safety.
type windowSnapshotter struct {
	mu           sync.Mutex
	inflightInst map[string]struct{} // tracks in-flight "inst" tasks by ID
	totalInst    uint64
	windowInst   uint64
	windowIdx    int

	engine sim.Engine

	// back-references to metric sources (populated by the time simulation runs)
	rep *reporter

	outPath string
	file    *os.File
	writer  *csv.Writer
}

func newWindowSnapshotter(engine sim.Engine, windowInst uint64, outPath string, rep *reporter) *windowSnapshotter {
	return &windowSnapshotter{
		engine:       engine,
		windowInst:   windowInst,
		outPath:      outPath,
		rep:          rep,
		inflightInst: make(map[string]struct{}),
	}
}

func (s *windowSnapshotter) open() error {
	dir := filepath.Dir(s.outPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("per-window mkdir %s: %w", dir, err)
		}
	}
	f, err := os.Create(s.outPath)
	if err != nil {
		return fmt.Errorf("per-window create %s: %w", s.outPath, err)
	}
	s.file = f
	s.writer = csv.NewWriter(f)
	_ = s.writer.Write(windowCSVHeader())
	s.writer.Flush()
	return nil
}

func (s *windowSnapshotter) close() {
	if s.writer != nil {
		// Write a final partial-window row so the last instructions are captured.
		s.mu.Lock()
		if s.totalInst%s.windowInst != 0 {
			s.takeSnapshot()
		}
		s.mu.Unlock()
		s.writer.Flush()
	}
	if s.file != nil {
		_ = s.file.Close()
	}
}

// tracing.Tracer implementation — attached to every CU

func (s *windowSnapshotter) StartTask(task tracing.Task) {
	if task.Kind != "inst" {
		return
	}
	s.mu.Lock()
	s.inflightInst[task.ID] = struct{}{}
	s.mu.Unlock()
}

func (s *windowSnapshotter) StepTask(_ tracing.Task)          {}
func (s *windowSnapshotter) AddMilestone(_ tracing.Milestone) {}

func (s *windowSnapshotter) EndTask(task tracing.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.inflightInst[task.ID]; !found {
		return
	}
	delete(s.inflightInst, task.ID)
	s.totalInst++
	if s.totalInst%s.windowInst == 0 {
		s.takeSnapshot()
	}
}

// takeSnapshot must be called with s.mu held.
func (s *windowSnapshotter) takeSnapshot() {
	simTimeNs := float64(s.engine.CurrentTime()) * 1e9

	var (
		readHit, readMiss             uint64
		writeHit, writeMiss           uint64
		remoteReadHit, remoteReadMiss uint64
		evictValid, evictInvalid      uint64
		invValidWrite, invInvalidWrite uint64
		invValidEvict, invInvalidEvict uint64
	)
	for _, ct := range s.rep.cacheHitRateTracers {
		readHit        += ct.tracer.GetStepCount("read-hit")
		readMiss       += ct.tracer.GetStepCount("read-miss")
		writeHit       += ct.tracer.GetStepCount("write-hit")
		writeMiss      += ct.tracer.GetStepCount("write-miss")
		remoteReadHit  += ct.tracer.GetStepCount("remote-read-hit")
		remoteReadMiss += ct.tracer.GetStepCount("remote-read-miss")
		evictValid     += ct.tracer.GetStepCount("EvictValidBlock")
		evictInvalid   += ct.tracer.GetStepCount("EvictInvalidBlock")
		invValidWrite  += ct.tracer.GetStepCount("InvalidateValidBlock-Write")
		invInvalidWrite += ct.tracer.GetStepCount("InvalidateInvalidBlock-Write")
		invValidEvict  += ct.tracer.GetStepCount("InvalidateValidBlock-Evict")
		invInvalidEvict += ct.tracer.GetStepCount("InvalidateInvalidBlock-Evict")
	}
	invValid   := invValidWrite + invValidEvict
	invInvalid := invInvalidWrite + invInvalidEvict

	var rdmaReadBytes, rdmaWriteBytes, rdmaInvBytes uint64
	for _, tt := range s.rep.trafficTracer {
		for _, name := range tt.tracer.GetStepNames() {
			count := tt.tracer.GetStepCount(name)
			if count == 0 {
				continue
			}
			cat, size := parseRDMAStep(name)
			switch cat {
			case "read":
				rdmaReadBytes += count * size
			case "write":
				rdmaWriteBytes += count * size
			case "inv":
				rdmaInvBytes += count * size
			}
		}
	}

	row := []string{
		fmt.Sprintf("%d", s.windowIdx),
		fmt.Sprintf("%.2f", simTimeNs),
		fmt.Sprintf("%d", s.totalInst),
		fmt.Sprintf("%d", readHit),
		fmt.Sprintf("%d", readMiss),
		fmt.Sprintf("%d", writeHit),
		fmt.Sprintf("%d", writeMiss),
		fmt.Sprintf("%d", remoteReadHit),
		fmt.Sprintf("%d", remoteReadMiss),
		fmt.Sprintf("%d", evictValid),
		fmt.Sprintf("%d", evictInvalid),
		fmt.Sprintf("%d", invValid),
		fmt.Sprintf("%d", invInvalid),
		fmt.Sprintf("%d", invValidWrite),
		fmt.Sprintf("%d", invInvalidWrite),
		fmt.Sprintf("%d", invValidEvict),
		fmt.Sprintf("%d", invInvalidEvict),
		fmt.Sprintf("%d", rdmaReadBytes),
		fmt.Sprintf("%d", rdmaWriteBytes),
		fmt.Sprintf("%d", rdmaInvBytes),
	}
	_ = s.writer.Write(row)
	s.writer.Flush()
	s.windowIdx++
}

// parseRDMAStep parses an RDMA step name like "Read Req 64" into category and
// bytes-per-message. Returns ("", 0) for unrecognised names.
func parseRDMAStep(name string) (cat string, bytesPerMsg uint64) {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "", 0
	}
	n, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	if err != nil {
		return "", 0
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "read"):
		return "read", n
	case strings.Contains(lower, "write"):
		return "write", n
	case strings.Contains(lower, "inv"):
		return "inv", n
	}
	return "", 0
}

func windowCSVHeader() []string {
	return []string{
		"window_idx", "sim_time_ns", "cum_instructions",
		"cum_L2_read_hit", "cum_L2_read_miss",
		"cum_L2_write_hit", "cum_L2_write_miss",
		"cum_L2_remote_read_hit", "cum_L2_remote_read_miss",
		"cum_L2_EvictValid", "cum_L2_EvictInvalid",
		"cum_L2_InvalidateValid", "cum_L2_InvalidateInvalid",
		"cum_L2_InvalidateValid_Write", "cum_L2_InvalidateInvalid_Write",
		"cum_L2_InvalidateValid_Evict", "cum_L2_InvalidateInvalid_Evict",
		"cum_RDMA_read_bytes", "cum_RDMA_write_bytes", "cum_RDMA_inv_bytes",
	}
}
