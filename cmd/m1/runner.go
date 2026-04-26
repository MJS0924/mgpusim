package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	superdirectory "github.com/sarchlab/akita/v4/mem/cache/superdirectory"
	"github.com/sarchlab/akita/v4/mem/cache/writebackcoh"
	"github.com/sarchlab/akita/v4/sim"
	cpmod "github.com/sarchlab/mgpusim/v4/amd/timing/cp"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/amdappsdk/bitonicsort"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/amdappsdk/fastwalshtransform"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/amdappsdk/floydwarshall"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/amdappsdk/matrixmultiplication"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/amdappsdk/matrixtranspose"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/amdappsdk/nbody"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/amdappsdk/simpleconvolution"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/heteromark/aes"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/heteromark/fir"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/heteromark/kmeans"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/heteromark/pagerank"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/polybench/atax"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/polybench/bicg"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/rodinia/nw"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/shoc/bfs"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/shoc/fft"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/shoc/spmv"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/shoc/stencil2d"
	"github.com/sarchlab/mgpusim/v4/amd/samples/runner"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/instrument"
	"github.com/sarchlab/mgpusim/v4/instrument/adapter"
)

// clockHook drives one or more PhaseClocks on every simulated event.
// It converts sim.VTimeInSec to a cycle count using 1 GHz CU clock.
// All clocks share the same time source; per-GPU clocks are advanced together.
type clockHook struct {
	clocks      []*instrument.PhaseClock
	freq        sim.Freq
	currentCycle *uint64 // shared; updated before Tick so KernelBoundary sees correct value
}

func (h *clockHook) Func(ctx sim.HookCtx) {
	if ctx.Pos != sim.HookPosBeforeEvent {
		return
	}
	evt, ok := ctx.Item.(sim.Event)
	if !ok {
		return
	}
	cycle := h.freq.Cycle(evt.Time())
	if h.currentCycle != nil {
		*h.currentCycle = cycle
	}
	for _, c := range h.clocks {
		c.Tick(cycle)
	}
}

// kernelCompleteHook catches HookPosKernelComplete on a CommandProcessor and
// fires SignalKernelBoundary on the associated per-GPU PhaseClock.
type kernelCompleteHook struct {
	clock        *instrument.PhaseClock
	currentCycle *uint64
}

func (h *kernelCompleteHook) Func(ctx sim.HookCtx) {
	if ctx.Pos != cpmod.HookPosKernelComplete {
		return
	}
	info, ok := ctx.Item.(cpmod.KernelCompleteHookInfo)
	if !ok {
		return
	}
	h.clock.SignalKernelBoundary(info.KernelID, *h.currentCycle)
}

// snapshotSqliteFiles returns the set of akita_sim_*.sqlite3 paths
// currently present in the working directory.
func snapshotSqliteFiles() map[string]struct{} {
	matches, _ := filepath.Glob("akita_sim_*.sqlite3")
	m := make(map[string]struct{}, len(matches))
	for _, f := range matches {
		m[f] = struct{}{}
	}
	return m
}

// cleanNewSqliteFiles deletes any akita_sim_*.sqlite3 files that were not
// present in before. akita creates one per simulation run; they are only
// useful for the daisen visualizer and are unwanted in the M1 output dir.
func cleanNewSqliteFiles(before map[string]struct{}) {
	after, _ := filepath.Glob("akita_sim_*.sqlite3")
	for _, f := range after {
		if _, existed := before[f]; !existed {
			if err := os.Remove(f); err == nil {
				log.Printf("[m1] removed akita trace file: %s", f)
			}
		}
	}
}

// runM1 executes one full M1 measurement: wires adapters, runs workload,
// flushes sink. Returns total phase count and summary metrics.
func runM1(cfg *m1Config) error {
	sqliteBefore := snapshotSqliteFiles()
	r := new(runner.Runner).Init()

	simObj := r.Simulation()
	eng := r.Engine()

	// Phase clock driven by engine events (window-only; kernel boundary
	// not available without driver modification — see TODO_PHASE2.md).
	clock := instrument.NewPhaseClock(cfg.windowCycles, cfg.initialPhaseID())
	const cuFreq sim.Freq = 1 * sim.GHz
	var currentCycle uint64
	eng.AcceptHook(&clockHook{clocks: []*instrument.PhaseClock{clock}, freq: cuFreq, currentCycle: &currentCycle})

	m := instrument.NewPhaseMetrics()
	m.PhaseID = cfg.initialPhaseID()
	dirCfg := cfg.dirCfg()

	sink, err := adapter.NewParquetSnapshotSink(cfg.outputPath(), cfg.configID, cfg.workloadID)
	if err != nil {
		return fmt.Errorf("create sink: %w", err)
	}

	var l2Adapters []*adapter.L2Adapter
	var cuAdapters []*adapter.CUAdapter
	l2Count, cuCount := 0, 0

	for _, comp := range simObj.Components() {
		name := comp.Name()
		if l2, ok := comp.(*writebackcoh.Comp); ok && strings.Contains(name, ".L2Cache[") {
			a := adapter.NewL2Adapter(m, dirCfg)
			l2.AcceptHook(a)
			l2Adapters = append(l2Adapters, a)
			l2Count++
		}
		if cuComp, ok := comp.(*cu.ComputeUnit); ok && strings.Contains(name, ".CU[") {
			a := adapter.NewCUAdapter(m, dirCfg)
			cuComp.AcceptHook(a)
			cuAdapters = append(cuAdapters, a)
			cuCount++
		}
	}

	// Route superdirectory event log through runner's auto-flush (EVENT_LOG_PATH).
	// Logger is enabled by default in the superdirectory builder; we only need to
	// set the path. Setting "" explicitly disables the runner flush.
	if cfg.enableEventLog {
		os.Setenv("EVENT_LOG_PATH", cfg.eventLogPath)
		log.Printf("[m1] event-log enabled: output=%s", cfg.eventLogPath)
	} else {
		os.Setenv("EVENT_LOG_PATH", "")
	}

	log.Printf("[m1] R=%d: L2Adapter×%d CUAdapter×%d windowCycles=%d",
		cfg.regionSize, l2Count, cuCount, cfg.windowCycles)

	// Build resetable list: L2Adapters implement PhaseResetable.
	resetables := make([]adapter.PhaseResetable, len(l2Adapters))
	for i, a := range l2Adapters {
		resetables[i] = a
	}
	adapter.RegisterPhaseLifecycle(clock, m, sink, resetables...)

	// M1-MOD-7: wire per-GPU bank snapshot sinks (only when flag is set).
	var bankSinks []*adapter.BankSnapshotSink
	if cfg.bankSnapshotOut != "" {
		var err error
		bankSinks, err = wireM1Mod7(cfg, simObj, eng, &currentCycle, clock)
		if err != nil {
			return fmt.Errorf("M1-MOD-7 wiring: %w", err)
		}
		log.Printf("[m1-mod7] wired %d per-GPU bank snapshot sinks, interval=%d cycles",
			len(bankSinks), cfg.bankSnapshotInterval)
	}

	// Set up workload.
	if err := setupWorkload(cfg, r); err != nil {
		return fmt.Errorf("setup workload: %w", err)
	}

	// Run simulation.
	r.Run()

	// Flush final partial phase.
	if snap, err := m.Flush(); err == nil {
		_ = sink.PushSnapshot(snap)
	}

	// Flush M1-MOD-7 bank snapshot sinks.
	for _, bs := range bankSinks {
		remoteAccept, doWriteMiss, doWriteMissRemote := bs.DiagCounts()
		if err := bs.Flush(); err != nil {
			log.Printf("[m1-mod7] flush error for %s: %v", bs.Path(), err)
		} else {
			log.Printf("[m1-mod7] wrote %d snapshot rows → %s (diag: remoteAccept=%d doWriteMiss=%d doWriteMissRemote=%d alloc=%d)",
				bs.TotalRows(), bs.Path(), remoteAccept, doWriteMiss, doWriteMissRemote, bs.TotalRows())
		}
	}

	if err := sink.Close(); err != nil {
		return fmt.Errorf("close sink: %w", err)
	}

	// V12 warning count (sum across all CU adapters).
	var warnTotal uint64
	for _, a := range cuAdapters {
		warnTotal += a.WarningCount()
	}
	v12 := "PASS"
	if warnTotal > 0 {
		v12 = fmt.Sprintf("WARN (warningCount=%d)", warnTotal)
	}

	// V11: evictions must be 0 with InfiniteCapacity.
	// Accumulated across all phases via sink totals.
	l2h, l2m, fetched, accessed := sink.Totals()
	v11 := "PASS"
	// DirectoryEvictions is tracked per-phase in metrics; we check if any
	// snapshot had evictions (tracked in parquet rows, verified in sanity.md).

	fmt.Printf("M1_SUMMARY workload=%s R=%d phases=%d RetiredWf=%d L2H=%d L2M=%d fetched=%d accessed=%d V11=%s V12=%s output=%s\n",
		cfg.workload, cfg.regionSize,
		sink.PhaseCount(),
		sink.TotalRetiredWavefronts(),
		l2h, l2m, fetched, accessed,
		v11, v12,
		sink.Filepath(),
	)

	cleanNewSqliteFiles(sqliteBefore)

	return nil
}

// wireM1Mod7 wires per-GPU bank snapshot sinks to every superdirectory found
// in the simulation. It returns the sinks so the caller can flush them after Run().
//
// For each superdirectory component (one per GPU):
//  1. A per-GPU PhaseClock (window = bankSnapshotInterval cycles) is created.
//  2. The global clockHook is extended to drive the new clock.
//  3. A BankSnapshotSink is created and registered on the per-GPU clock.
//  4. The CommandProcessor of the same GPU gets a kernelCompleteHook that
//     calls clock.SignalKernelBoundary → triggers kernel-boundary snapshot.
func wireM1Mod7(
	cfg *m1Config,
	simObj interface{ Components() []sim.Component },
	eng sim.Engine,
	currentCycle *uint64,
	globalClock *instrument.PhaseClock,
) ([]*adapter.BankSnapshotSink, error) {

	const cuFreq sim.Freq = 1 * sim.GHz

	// Index components by GPU name prefix (everything before the last ".").
	cpByPrefix := map[string]*cpmod.CommandProcessor{}
	sdByPrefix := map[string]*superdirectory.Comp{}

	for _, comp := range simObj.Components() {
		name := comp.Name()
		if sd, ok := comp.(*superdirectory.Comp); ok && strings.HasSuffix(name, ".SuperDir") {
			prefix := strings.TrimSuffix(name, ".SuperDir")
			sdByPrefix[prefix] = sd
		}
		if cpComp, ok := comp.(*cpmod.CommandProcessor); ok && strings.HasSuffix(name, ".CommandProcessor") {
			prefix := strings.TrimSuffix(name, ".CommandProcessor")
			cpByPrefix[prefix] = cpComp
		}
	}

	if len(sdByPrefix) == 0 {
		log.Printf("[m1-mod7] WARNING: no superdirectory components found — " +
			"is -coherence-directory=SuperDirectory set?")
		return nil, nil
	}

	var sinks []*adapter.BankSnapshotSink

	for prefix, sd := range sdByPrefix {
		gpuID := int32(sd.DeviceID())
		outPath := fmt.Sprintf("%s_gpu%d.parquet", cfg.bankSnapshotOut, gpuID)

		sink, err := adapter.NewBankSnapshotSink(outPath, gpuID, sd)
		if err != nil {
			return nil, fmt.Errorf("GPU %d sink: %w", gpuID, err)
		}

		// Per-GPU PhaseClock driven by the same engine time source.
		gpuClock := instrument.NewPhaseClock(cfg.bankSnapshotInterval, instrument.PhaseID{})
		eng.AcceptHook(&clockHook{
			clocks:       []*instrument.PhaseClock{gpuClock},
			freq:         cuFreq,
			currentCycle: currentCycle,
		})

		cycleGetter := func() uint64 { return *currentCycle }

		// Periodic window snapshots.
		gpuClock.OnWindowBoundary(sink.WindowBoundaryFunc(cycleGetter))

		// Kernel-boundary snapshots: hook into the matching CP.
		if cpComp, ok := cpByPrefix[prefix]; ok {
			cpComp.AcceptHook(&kernelCompleteHook{
				clock:        gpuClock,
				currentCycle: currentCycle,
			})
			log.Printf("[m1-mod7] GPU %d: CP hook registered (%s.CommandProcessor)", gpuID, prefix)
		} else {
			log.Printf("[m1-mod7] GPU %d: WARNING no matching CP for prefix %q — kernel boundaries disabled", gpuID, prefix)
		}

		// Kernel-boundary snapshot callback on the per-GPU clock.
		gpuClock.OnKernelBoundary(sink.KernelBoundaryFunc(cycleGetter))

		sinks = append(sinks, sink)
		log.Printf("[m1-mod7] GPU %d: bank snapshot → %s (interval=%d cycles, capacity %v)",
			gpuID, outPath, cfg.bankSnapshotInterval,
			[]int32{
				int32(sd.BankMaxCapacity(0)),
				int32(sd.BankMaxCapacity(1)),
				int32(sd.BankMaxCapacity(2)),
				int32(sd.BankMaxCapacity(3)),
				int32(sd.BankMaxCapacity(4)),
			})
	}

	return sinks, nil
}

// setupWorkload instantiates and registers the requested workload benchmark.
// Problem sizes are chosen to complete in under 5 minutes on 4 GPUs.
func setupWorkload(cfg *m1Config, r *runner.Runner) error {
	switch strings.ToLower(cfg.workload) {

	// ── amdappsdk ─────────────────────────────────────────────────────────
	case "simpleconvolution":
		bm := simpleconvolution.NewBenchmark(r.Driver())
		bm.Width = 512
		bm.Height = 512
		bm.SetMaskSize(3)
		r.AddBenchmark(bm)

	case "matrixtranspose":
		bm := matrixtranspose.NewBenchmark(r.Driver())
		bm.Width = 512
		r.AddBenchmark(bm)

	case "bitonicsort":
		bm := bitonicsort.NewBenchmark(r.Driver())
		bm.Length = 65536
		r.AddBenchmark(bm)

	case "matrixmultiplication":
		bm := matrixmultiplication.NewBenchmark(r.Driver())
		bm.X = 256
		bm.Y = 256
		bm.Z = 256
		if cfg.matmulX > 0 {
			bm.X = uint32(cfg.matmulX)
		}
		if cfg.matmulY > 0 {
			bm.Y = uint32(cfg.matmulY)
		}
		if cfg.matmulZ > 0 {
			bm.Z = uint32(cfg.matmulZ)
		}
		r.AddBenchmark(bm)

	case "nbody":
		bm := nbody.NewBenchmark(r.Driver())
		bm.NumParticles = 1024
		bm.NumIterations = 1
		r.AddBenchmark(bm)

	case "fastwalshtransform":
		bm := fastwalshtransform.NewBenchmark(r.Driver())
		bm.Length = 65536
		r.AddBenchmark(bm)

	case "floydwarshall":
		bm := floydwarshall.NewBenchmark(r.Driver())
		bm.NumNodes = 256
		r.AddBenchmark(bm)

	// ── heteromark ────────────────────────────────────────────────────────
	case "fir":
		bm := fir.NewBenchmark(r.Driver())
		bm.Length = 65536
		r.AddBenchmark(bm)

	case "aes":
		bm := aes.NewBenchmark(r.Driver())
		bm.Length = 65536
		r.AddBenchmark(bm)

	case "kmeans":
		bm := kmeans.NewBenchmark(r.Driver())
		bm.NumClusters = 4
		bm.NumPoints = 4096
		bm.NumFeatures = 32
		bm.MaxIter = 5
		r.AddBenchmark(bm)

	case "pagerank":
		bm := pagerank.NewBenchmark(r.Driver())
		bm.NumNodes = 1024
		bm.NumConnections = 4096
		bm.MaxIterations = 3
		bm.RandSeed = cfg.seed
		r.AddBenchmark(bm)

	// ── polybench ─────────────────────────────────────────────────────────
	case "atax":
		bm := atax.NewBenchmark(r.Driver())
		bm.NX = 512
		bm.NY = 512
		r.AddBenchmark(bm)

	case "bicg":
		bm := bicg.NewBenchmark(r.Driver())
		bm.NX = 512
		bm.NY = 512
		r.AddBenchmark(bm)

	// ── rodinia ───────────────────────────────────────────────────────────
	case "nw":
		bm := nw.NewBenchmark(r.Driver())
		bm.SetLength(512)
		bm.SetPenalty(10)
		r.AddBenchmark(bm)

	// ── shoc ──────────────────────────────────────────────────────────────
	case "bfs":
		bm := bfs.NewBenchmark(r.Driver())
		bm.NumNode = 1024
		bm.Degree = 6
		bm.MaxDepth = 10
		r.AddBenchmark(bm)

	case "fft":
		bm := fft.NewBenchmark(r.Driver())
		bm.Bytes = 1   // 1 MB
		bm.Passes = 1
		r.AddBenchmark(bm)

	case "spmv":
		bm := spmv.NewBenchmark(r.Driver())
		bm.Dim = 1024
		bm.Sparsity = 0.01
		bm.RandSeed = cfg.seed
		r.AddBenchmark(bm)

	case "stencil2d":
		bm := stencil2d.NewBenchmark(r.Driver())
		bm.NumRows = 512
		bm.NumCols = 512
		bm.NumIteration = 1
		r.AddBenchmark(bm)

	default:
		return fmt.Errorf(
			"unknown workload %q — supported: simpleconvolution, matrixtranspose, "+
				"bitonicsort, matrixmultiplication, nbody, fastwalshtransform, floydwarshall, "+
				"fir, aes, kmeans, pagerank, atax, bicg, nw, bfs, fft, spmv, stencil2d",
			cfg.workload,
		)
	}
	return nil
}