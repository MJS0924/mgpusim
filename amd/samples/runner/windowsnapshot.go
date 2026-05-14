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

// dirUtilProvider is the per-window equivalent of report.go's
// entryUtilProvider. It returns LIVE directory entry utilization
// (averaged over currently-valid entries) at the moment of call,
// instead of cumulative-eviction-time utilization. Implemented by
// REC.Comp and superdirectory.Comp.
type dirUtilProvider interface {
	Name() string
	// avgUtil is the per-entry sub-entry fill ratio averaged across valid
	// entries; validEntries is how many parent entries have at least one
	// valid sub-entry; cacheLines is the directory's cache-line coverage
	// (sum of valid sub-entries — each tracks one cache line in both REC
	// and SD).
	CurrentValidEntryUtilization() (avgUtil float64, validEntries int, cacheLines int)
}

// sharerHeatmapProvider is implemented by optdirectory.Comp when the
// -coalescability-heatmap flag is enabled. The runner calls
// EmitWindowSharerHeatmap at every window boundary and EmitTopAccessCounts
// once at simulation end.
type sharerHeatmapProvider interface {
	Name() string
	SharerHeatmapEnabled() bool
	EmitWindowSharerHeatmap(windowID int, dir string) (int, error)
	EmitTopAccessCounts(dir string) error
	CloseSharerHeatmap()
}

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

	// per-GPU directory components for live utilization snapshot.
	// Empty for variants that don't expose CurrentValidEntryUtilization
	// (e.g. plain CD optdirectory). Filled by injectWindowSnapshotter.
	dirUtilProviders []dirUtilProvider

	// per-GPU optdirectory components that emit the sharer heatmap.
	// Empty unless -coalescability-heatmap is enabled.
	sharerHeatmapProviders []sharerHeatmapProvider
	sharerHeatmapDir       string

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
	_ = s.writer.Write(s.csvHeader())
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
	// Sharer heatmap finalisation: dump per-block cumulative access counts
	// (so post-processing can identify top-K hot blocks) and close per-GPU
	// CSVs. Errors are reported but never fatal.
	for _, p := range s.sharerHeatmapProviders {
		if err := p.EmitTopAccessCounts(s.sharerHeatmapDir); err != nil {
			fmt.Printf("[sharer-heatmap] %s top-access write: %v\n",
				p.Name(), err)
		}
		p.CloseSharerHeatmap()
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
	// Sum hit/miss counters from L2-tier components only. injectCacheHitRateTracer
	// attaches to every component containing "Cache" (incl. L1V/L1S/L1I), so we
	// must filter out L1 here. L2 components in mgpusim/r9nano builder are
	// named "<gpu>.L2Cache[i]" (writebackcoh data L2) or "<gpu>.{CohDir,
	// SuperDir,RECDir,HMGDir,MGDDir}" (directories that emit hit/miss for
	// some variants). Use stepCount(comp, tracer, name) so each component's
	// counts come from EventCounts() when implemented (incEvent path) and
	// fall back to the StepCountTracer otherwise (legacy AddTaskStep path).
	for _, ct := range s.rep.cacheHitRateTracers {
		if !isL2TierComponent(ct.cache.Name()) {
			continue
		}
		readHit         += stepCount(ct.cache, ct.tracer, "read-hit")
		readMiss        += stepCount(ct.cache, ct.tracer, "read-miss")
		writeHit        += stepCount(ct.cache, ct.tracer, "write-hit")
		writeMiss       += stepCount(ct.cache, ct.tracer, "write-miss")
		remoteReadHit   += stepCount(ct.cache, ct.tracer, "remote-read-hit")
		remoteReadMiss  += stepCount(ct.cache, ct.tracer, "remote-read-miss")
		evictValid      += stepCount(ct.cache, ct.tracer, "EvictValidBlock")
		evictInvalid    += stepCount(ct.cache, ct.tracer, "EvictInvalidBlock")
		invValidWrite   += stepCount(ct.cache, ct.tracer, "InvalidateValidBlock-Write")
		invInvalidWrite += stepCount(ct.cache, ct.tracer, "InvalidateInvalidBlock-Write")
		invValidEvict   += stepCount(ct.cache, ct.tracer, "InvalidateValidBlock-Evict")
		invInvalidEvict += stepCount(ct.cache, ct.tracer, "InvalidateInvalidBlock-Evict")
	}
	invValid   := invValidWrite + invValidEvict
	invInvalid := invInvalidWrite + invInvalidEvict

	// rdma.Comp now records traffic via in-memory eventCounts (see
	// mgpusim/amd/timing/rdma/comp.go:recordMsgSend). Prefer the
	// EventCounts() map; only fall back to the StepCountTracer for
	// providers that don't implement eventCountsProvider.
	var rdmaReadBytes, rdmaWriteBytes, rdmaInvBytes uint64
	for _, tt := range s.rep.trafficTracer {
		var counts map[string]uint64
		if p, ok := tt.rdma.(eventCountsProvider); ok {
			counts = p.EventCounts()
		} else {
			counts = make(map[string]uint64)
			for _, name := range tt.tracer.GetStepNames() {
				counts[name] = tt.tracer.GetStepCount(name)
			}
		}
		for name, count := range counts {
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

	// Per-provider live directory entry utilization + cache-line coverage.
	// Columns are written in the same order as s.dirUtilProviders (locked
	// in at open() time). Aggregate util is weighted by valid entry count;
	// aggregate coverage is the simple sum across providers.
	var aggSum float64
	var aggCount int
	var aggLines int
	for _, p := range s.dirUtilProviders {
		util, valid, lines := p.CurrentValidEntryUtilization()
		row = append(row,
			fmt.Sprintf("%.6f", util),
			fmt.Sprintf("%d", valid),
			fmt.Sprintf("%d", lines),
		)
		aggSum += util * float64(valid)
		aggCount += valid
		aggLines += lines
	}
	if len(s.dirUtilProviders) > 0 {
		var aggUtil float64
		if aggCount > 0 {
			aggUtil = aggSum / float64(aggCount)
		}
		row = append(row,
			fmt.Sprintf("%.6f", aggUtil),
			fmt.Sprintf("%d", aggCount),
			fmt.Sprintf("%d", aggLines),
		)
	}

	_ = s.writer.Write(row)
	s.writer.Flush()

	// Sharer heatmap dump for this window (RLE per-GPU).
	for _, p := range s.sharerHeatmapProviders {
		if _, err := p.EmitWindowSharerHeatmap(s.windowIdx, s.sharerHeatmapDir); err != nil {
			fmt.Printf("[sharer-heatmap] %s window=%d write: %v\n",
				p.Name(), s.windowIdx, err)
		}
	}

	s.windowIdx++
}

// csvHeader returns the CSV header. Per-provider directory utilization
// columns are appended in the same order as s.dirUtilProviders, which
// is finalized before open() is called.
func (s *windowSnapshotter) csvHeader() []string {
	h := windowCSVHeader()
	for _, p := range s.dirUtilProviders {
		short := shortDirName(p.Name())
		h = append(h,
			"cur_dir_util_"+short,
			"cur_dir_valid_"+short,
			"cur_dir_cachelines_"+short,
		)
	}
	if len(s.dirUtilProviders) > 0 {
		h = append(h,
			"cur_dir_util_agg",
			"cur_dir_valid_agg",
			"cur_dir_cachelines_agg",
		)
	}
	return h
}

// shortDirName extracts a stable short tag from a directory component
// name like "GPU[1].SuperDir" or "GPU[2].RECDir" so that CSV headers
// stay analyzable.
func shortDirName(name string) string {
	gpuTag := name
	if i := strings.Index(name, "GPU["); i >= 0 {
		if j := strings.Index(name[i:], "]"); j > 0 {
			gpuTag = "GPU" + name[i+4:i+j]
		}
	}
	short := strings.ReplaceAll(name, ".", "_")
	short = strings.ReplaceAll(short, "[", "")
	short = strings.ReplaceAll(short, "]", "")
	if gpuTag != name {
		// Prefer compact "GPUn_<rest>" form.
		rest := name
		if i := strings.LastIndex(name, "."); i >= 0 {
			rest = name[i+1:]
		}
		short = gpuTag + "_" + rest
	}
	return short
}

// isL2TierComponent reports whether comp name belongs to an L2 data cache
// (writebackcoh "<gpu>.L2Cache[i]") or to one of the coherence directory
// components that emit hit/miss counters at the L2 boundary
// ("<gpu>.{CohDir,SuperDir,RECDir,HMGDir,MGDDir}"). Excludes per-CU L1
// caches (L1V/L1S/L1I), which also match cacheHitRateTracers' "Cache"
// substring filter but live below the L2 boundary in r9nano builder.
func isL2TierComponent(name string) bool {
	if strings.Contains(name, ".L1") {
		return false
	}
	if strings.Contains(name, ".L2Cache") {
		return true
	}
	for _, suf := range []string{".CohDir", ".SuperDir", ".RECDir", ".HMGDir", ".MGDDir"} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
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
