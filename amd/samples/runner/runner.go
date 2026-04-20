// Package runner defines how default benchmark samples are executed.
package runner

import (
	"fmt"
	"log"

	// Enable profiling
	_ "net/http/pprof"
	"sync"

	"github.com/sarchlab/akita/v4/mem/cache/optdirectory"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/simulation"
	"github.com/sarchlab/akita/v4/tracing"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks"
	"github.com/sarchlab/mgpusim/v4/amd/driver"
	"github.com/sarchlab/mgpusim/v4/amd/samples/runner/emusystem"
	"github.com/sarchlab/mgpusim/v4/amd/samples/runner/timingconfig"
	"github.com/sarchlab/mgpusim/v4/amd/sampling"
)

type verificationPreEnablingBenchmark interface {
	benchmarks.Benchmark

	EnableVerification()
}

// Runner is a class that helps running the benchmarks in the official samples.
type Runner struct {
	simulation *simulation.Simulation
	platform   *sim.Domain
	reporter   *reporter

	Timing           bool
	Verify           bool
	Parallel         bool
	UseUnifiedMemory bool

	GPUIDs     []int
	benchmarks []benchmarks.Benchmark

	log2PageSize          uint64
	log2CacheBlockSize    uint64
	log2CoherenceUnitSize uint64
	pageMigrationPolicy   uint64
	coherenceDirectory    uint64
	idealDirectory        bool
}

// Init initializes the platform simulate
func (r *Runner) Init() *Runner {
	r.parseFlag()

	log.SetFlags(log.Llongfile | log.Ldate | log.Ltime)

	r.initSimulation()

	if r.Timing {
		r.buildTimingPlatform()
	} else {
		r.buildEmuPlatform()
	}

	r.createUnifiedGPUs()

	return r
}

func (r *Runner) initSimulation() {
	builder := simulation.MakeBuilder()

	if *parallelFlag {
		builder = builder.WithParallelEngine()
	}

	r.simulation = builder.Build()
}

func (r *Runner) buildEmuPlatform() {
	b := emusystem.MakeBuilder().
		WithSimulation(r.simulation).
		WithNumGPUs(r.GPUIDs[len(r.GPUIDs)-1])

	if *isaDebug {
		b = b.WithDebugISA()
	}

	r.platform = b.Build()
}

func (r *Runner) buildTimingPlatform() {
	fmt.Printf("Build Timing Platform\n")

	sampling.InitSampledEngine()

	b := timingconfig.MakeBuilder().
		WithSimulation(r.simulation).
		WithNumGPUs(r.GPUIDs[len(r.GPUIDs)-1]).
		WithLog2CacheBlockSize(r.log2CacheBlockSize).
		WithLog2PageSize(r.log2PageSize).
		WithLog2CoherenceUnitSize(r.log2CoherenceUnitSize).
		WithPageMigrationPolicy(r.pageMigrationPolicy).
		WithCoherenceDirectory(r.coherenceDirectory).
		WithIdealDirectory(r.idealDirectory)

	if *magicMemoryCopy {
		b = b.WithMagicMemoryCopy()
	}

	r.platform = b.Build()
	r.reporter = newReporter(r.simulation)
	r.configureVisTracing()
}

func (r *Runner) configureVisTracing() {
	if !*visTracing {
		return
	}

	visTracer := r.simulation.GetVisTracer()
	for _, comp := range r.simulation.Components() {
		tracing.CollectTrace(comp.(tracing.NamedHookable), visTracer)
	}
}

func (r *Runner) createUnifiedGPUs() {
	if *unifiedGPUFlag == "" {
		return
	}

	driver := r.simulation.GetComponentByName("Driver").(*driver.Driver)
	unifiedGPUID := driver.CreateUnifiedGPU(nil, r.GPUIDs)
	r.GPUIDs = []int{unifiedGPUID}
}

// AddBenchmark adds an benchmark that the driver runs
func (r *Runner) AddBenchmark(b benchmarks.Benchmark) {
	b.SelectGPU(r.GPUIDs)
	if r.UseUnifiedMemory {
		b.SetUnifiedMemory()
	}

	r.benchmarks = append(r.benchmarks, b)
}

// AddBenchmarkWithoutSettingGPUsToUse allows for user specified GPUs for
// the benchmark to run.
func (r *Runner) AddBenchmarkWithoutSettingGPUsToUse(b benchmarks.Benchmark) {
	if r.UseUnifiedMemory {
		b.SetUnifiedMemory()
	}

	r.benchmarks = append(r.benchmarks, b)
}

// Run runs the benchmark
func (r *Runner) Run() {
	r.Driver().Run()

	var wg sync.WaitGroup
	for _, b := range r.benchmarks {
		wg.Add(1)
		go func(b benchmarks.Benchmark, wg *sync.WaitGroup) {
			if r.Verify {
				if b, ok := b.(verificationPreEnablingBenchmark); ok {
					b.EnableVerification()
				}
			}

			b.Run()

			if r.Verify {
				b.Verify()
			}
			wg.Done()
		}(b, &wg)
	}
	wg.Wait()

	if r.reporter != nil {
		r.reporter.log2BlockSize = r.log2CacheBlockSize + r.log2CoherenceUnitSize
		r.reporter.report()
	}

	r.emitCoalescabilityReports()

	r.Driver().Terminate()

	fmt.Printf("Simulation Terminate\n")

	r.simulation.Terminate()
}

// Driver returns the GPU driver used by the current runner.
func (r *Runner) Driver() *driver.Driver {
	return r.simulation.GetComponentByName("Driver").(*driver.Driver)
}

// Engine returns the event-driven simulation engine used by the current runner.
func (r *Runner) Engine() sim.Engine {
	return r.simulation.GetEngine()
}

// Simulation returns the simulation object, allowing callers to iterate
// components and register hooks after Init() but before Run().
func (r *Runner) Simulation() *simulation.Simulation {
	return r.simulation
}

// emitCoalescabilityReports calls EmitCumulativeReport on every
// optdirectory.Comp registered in the simulation. This is the end-of-sim
// hook that writes motivation_cumulative_GPU{N}.csv and prints the PHASE 0 /
// R6 exit-criterion verdict to stdout.
func (r *Runner) emitCoalescabilityReports() {
	if r.simulation == nil {
		return
	}
	for _, comp := range r.simulation.Components() {
		if cd, ok := comp.(*optdirectory.Comp); ok {
			cd.EmitCumulativeReport()
		}
	}
}
