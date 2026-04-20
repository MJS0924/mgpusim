// Package main is the B-3.5 overhead measurement harness.
//
// It runs simpleconvolution under four instrumentation scenarios (S1–S4) and
// prints the wall-clock duration so run.sh can capture it.
//
// Usage:
//
//	./b35_overhead -scenario s1 -timing -gpus 1,2,3,4
//	./b35_overhead -scenario s2 -timing -gpus 1,2,3,4
//	./b35_overhead -scenario s3 -timing -gpus 1,2,3,4
//	./b35_overhead -scenario s4 -timing -gpus 1,2,3,4
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/sarchlab/akita/v4/mem/cache/writebackcoh"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/amdappsdk/simpleconvolution"
	"github.com/sarchlab/mgpusim/v4/amd/samples/runner"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/amd/timing/wavefront"
	"github.com/sarchlab/mgpusim/v4/coherence"
	"github.com/sarchlab/mgpusim/v4/instrument"
	"github.com/sarchlab/mgpusim/v4/instrument/adapter"
)

var scenarioFlag = flag.String("scenario", "s1",
	"Instrumentation scenario: s1 (baseline), s2 (L2+Dir), s3 (s2+CU retire), s4 (full)")

// wfRetireHook is a minimal sim.Hook for S3: counts only WfCompletionEvents.
// Used instead of the full CUAdapter so that OnRegionAccess is not called.
type wfRetireHook struct {
	metrics *instrument.PhaseMetrics
}

func (h *wfRetireHook) Func(ctx sim.HookCtx) {
	if ctx.Pos != sim.HookPosBeforeEvent {
		return
	}
	if _, ok := ctx.Item.(*wavefront.WfCompletionEvent); ok {
		h.metrics.AddRetiredWavefronts(1)
	}
}

func main() {
	rand.Seed(1)
	flag.Parse()

	r := new(runner.Runner).Init()

	scenario := strings.ToLower(*scenarioFlag)
	log.Printf("[B-3.5] scenario=%s", scenario)

	simObj := r.Simulation()

	// Shared PhaseMetrics for all adapters (serial engine — no mutex needed).
	m := instrument.NewPhaseMetrics()
	cfg := coherence.DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: true}

	switch scenario {
	case "s1":
		// No adapters.

	case "s2":
		// L2Adapter on every writebackcoh.Comp + DirectoryAdapter stub.
		for _, comp := range simObj.Components() {
			if l2, ok := comp.(*writebackcoh.Comp); ok &&
				strings.Contains(l2.Name(), ".L2Cache[") {
				l2.AcceptHook(adapter.NewL2Adapter(m, cfg))
			}
		}
		// DirectoryAdapter: PlainVIDirectory not yet wired into simulation;
		// instantiating it here measures the callback-registration path only.
		pvi, err := coherence.NewPlainVIDirectory(cfg)
		if err != nil {
			log.Fatalf("NewPlainVIDirectory: %v", err)
		}
		da := adapter.NewDirectoryAdapter(m)
		pvi.AddCallback(da.SharerEventCallback())
		log.Printf("[B-3.5] S2: registered L2Adapter on %d L2 caches (+DirectoryAdapter stub)",
			countL2(simObj))

	case "s3":
		// L2Adapter (same as S2) + wfRetireHook on every CU.
		for _, comp := range simObj.Components() {
			name := comp.Name()
			if l2, ok := comp.(*writebackcoh.Comp); ok &&
				strings.Contains(name, ".L2Cache[") {
				l2.AcceptHook(adapter.NewL2Adapter(m, cfg))
			}
			if cuComp, ok := comp.(*cu.ComputeUnit); ok &&
				strings.Contains(name, ".CU[") {
				cuComp.AcceptHook(&wfRetireHook{metrics: m})
			}
		}
		log.Printf("[B-3.5] S3: L2Adapter + wfRetireHook on %d CUs", countCU(simObj))

	case "s4":
		// Full: L2Adapter + full CUAdapter (handles both retire and vector-mem).
		for _, comp := range simObj.Components() {
			name := comp.Name()
			if l2, ok := comp.(*writebackcoh.Comp); ok &&
				strings.Contains(name, ".L2Cache[") {
				l2.AcceptHook(adapter.NewL2Adapter(m, cfg))
			}
			if cuComp, ok := comp.(*cu.ComputeUnit); ok &&
				strings.Contains(name, ".CU[") {
				cuComp.AcceptHook(adapter.NewCUAdapter(m, cfg))
			}
		}
		log.Printf("[B-3.5] S4: L2Adapter + full CUAdapter on %d CUs", countCU(simObj))

	default:
		log.Fatalf("unknown scenario %q — use s1/s2/s3/s4", scenario)
	}

	// Set up simpleconvolution benchmark (small: 512×512, mask=3).
	bm := simpleconvolution.NewBenchmark(r.Driver())
	bm.Width = 512
	bm.Height = 512
	bm.SetMaskSize(3)
	r.AddBenchmark(bm)

	start := time.Now()
	r.Run()
	elapsed := time.Since(start)

	fmt.Printf("B35_WALL_CLOCK scenario=%s elapsed_ms=%.0f\n",
		scenario, float64(elapsed.Milliseconds()))

	// Print accumulated PhaseMetrics so raw logs prove adapters were firing.
	fmt.Printf("B35_METRICS scenario=%s L2Hits=%d L2Misses=%d RegionFetchedBytes=%d RetiredWavefronts=%d\n",
		scenario,
		m.L2Hits,
		m.L2Misses,
		m.RegionFetchedBytes,
		m.RetiredWavefronts,
	)
}

func countL2(simObj interface {
	Components() []sim.Component
}) int {
	n := 0
	for _, c := range simObj.Components() {
		if _, ok := c.(*writebackcoh.Comp); ok &&
			strings.Contains(c.Name(), ".L2Cache[") {
			n++
		}
	}
	return n
}

func countCU(simObj interface {
	Components() []sim.Component
}) int {
	n := 0
	for _, c := range simObj.Components() {
		if _, ok := c.(*cu.ComputeUnit); ok &&
			strings.Contains(c.Name(), ".CU[") {
			n++
		}
	}
	return n
}
