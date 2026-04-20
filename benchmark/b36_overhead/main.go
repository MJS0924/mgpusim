// Package main is the B-3.6 overhead re-measurement harness.
//
// Identical to B-3.5 but uses the fixed HookPosWfRetired hook position so
// that RetiredInstructions is correctly counted in S3 and S4.
//
// Usage:
//
//	./b36_overhead -scenario s1 -timing -gpus 1 -disable-rtm
//	./b36_overhead -scenario s2 -timing -gpus 1 -disable-rtm
//	./b36_overhead -scenario s3 -timing -gpus 1 -disable-rtm
//	./b36_overhead -scenario s4 -timing -gpus 1 -disable-rtm
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

// wfRetireHook is a minimal sim.Hook for S3: listens on HookPosWfRetired
// (fired directly from evalSEndPgm, not via WfCompletionEvent).
type wfRetireHook struct {
	metrics *instrument.PhaseMetrics
}

func (h *wfRetireHook) Func(ctx sim.HookCtx) {
	if ctx.Pos != cu.HookPosWfRetired {
		return
	}
	_ = ctx.Item.(*wavefront.Wavefront) // type-assert to confirm item type
	h.metrics.AddRetiredInstructions(1)
}

func main() {
	rand.Seed(1)
	flag.Parse()

	r := new(runner.Runner).Init()

	scenario := strings.ToLower(*scenarioFlag)
	log.Printf("[B-3.6] scenario=%s", scenario)

	simObj := r.Simulation()

	m := instrument.NewPhaseMetrics()
	cfg := coherence.DirectoryConfig{RegionSizeBytes: 64, InfiniteCapacity: true}

	switch scenario {
	case "s1":
		// No adapters.

	case "s2":
		for _, comp := range simObj.Components() {
			if l2, ok := comp.(*writebackcoh.Comp); ok &&
				strings.Contains(l2.Name(), ".L2Cache[") {
				l2.AcceptHook(adapter.NewL2Adapter(m, cfg))
			}
		}
		pvi, err := coherence.NewPlainVIDirectory(cfg)
		if err != nil {
			log.Fatalf("NewPlainVIDirectory: %v", err)
		}
		da := adapter.NewDirectoryAdapter(m)
		pvi.AddCallback(da.SharerEventCallback())
		log.Printf("[B-3.6] S2: registered L2Adapter on %d L2 caches (+DirectoryAdapter stub)",
			countL2(simObj))

	case "s3":
		// L2Adapter + wfRetireHook (uses HookPosWfRetired — fires on evalSEndPgm path).
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
		log.Printf("[B-3.6] S3: L2Adapter + wfRetireHook on %d CUs", countCU(simObj))

	case "s4":
		// Full: L2Adapter + full CUAdapter (HookPosCUVectorMemAccess + HookPosWfRetired).
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
		log.Printf("[B-3.6] S4: L2Adapter + full CUAdapter on %d CUs", countCU(simObj))

	default:
		log.Fatalf("unknown scenario %q — use s1/s2/s3/s4", scenario)
	}

	bm := simpleconvolution.NewBenchmark(r.Driver())
	bm.Width = 512
	bm.Height = 512
	bm.SetMaskSize(3)
	r.AddBenchmark(bm)

	start := time.Now()
	r.Run()
	elapsed := time.Since(start)

	fmt.Printf("B36_WALL_CLOCK scenario=%s elapsed_ms=%.0f\n",
		scenario, float64(elapsed.Milliseconds()))

	fmt.Printf("B36_METRICS scenario=%s L2Hits=%d L2Misses=%d RegionFetchedBytes=%d RetiredInstructions=%d\n",
		scenario,
		m.L2Hits,
		m.L2Misses,
		m.RegionFetchedBytes,
		m.RetiredInstructions,
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
