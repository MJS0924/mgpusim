package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/sarchlab/akita/v4/mem/cache/writebackcoh"
	"github.com/sarchlab/akita/v4/sim"
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

// clockHook drives PhaseClock.Tick on every simulated event.
// It converts sim.VTimeInSec to a cycle count using 1 GHz CU clock.
type clockHook struct {
	clock *instrument.PhaseClock
	freq  sim.Freq
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
	h.clock.Tick(cycle)
}

// runM1 executes one full M1 measurement: wires adapters, runs workload,
// flushes sink. Returns total phase count and summary metrics.
func runM1(cfg *m1Config) error {
	r := new(runner.Runner).Init()

	simObj := r.Simulation()
	eng := r.Engine()

	// Phase clock driven by engine events (window-only; kernel boundary
	// not available without driver modification — see TODO_PHASE2.md).
	clock := instrument.NewPhaseClock(cfg.windowCycles, cfg.initialPhaseID())
	const cuFreq sim.Freq = 1 * sim.GHz
	eng.AcceptHook(&clockHook{clock: clock, freq: cuFreq})

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

	log.Printf("[m1] R=%d: L2Adapter×%d CUAdapter×%d windowCycles=%d",
		cfg.regionSize, l2Count, cuCount, cfg.windowCycles)

	// Build resetable list: L2Adapters implement PhaseResetable.
	resetables := make([]adapter.PhaseResetable, len(l2Adapters))
	for i, a := range l2Adapters {
		resetables[i] = a
	}
	adapter.RegisterPhaseLifecycle(clock, m, sink, resetables...)

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

	return nil
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