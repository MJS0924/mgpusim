package runner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sarchlab/akita/v4/datarecording"
	"github.com/sarchlab/akita/v4/mem/cache/optdirectory"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/simulation"
	"github.com/sarchlab/akita/v4/tracing"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cu"
	"github.com/sarchlab/mgpusim/v4/amd/timing/rdma"
)

const (
	tableName   = "mgpusim_metrics"
	cohDirTable = "cohDir_metrics"
)

type metric struct {
	Location string
	What     string
	Value    float64
	Unit     string
}

type kernelTimeTracer struct {
	tracer *tracing.BusyTimeTracer
	comp   tracing.NamedHookable
}

type instCountTracer struct {
	tracer *instTracer
	cu     tracing.NamedHookable
}

type cacheLatencyTracer struct {
	tracer *tracing.AverageTimeTracer
	cache  tracing.NamedHookable
}

type cacheHitRateTracer struct {
	tracer *tracing.StepCountTracer
	cache  tracing.NamedHookable
}

type cohDirTracer struct {
	tracer *tracing.StepCountTracer
	cohDir tracing.NamedHookable
}

type tlbHitRateTracer struct {
	tracer *tracing.StepCountTracer
	tlb    tracing.NamedHookable
}

type dramTransactionCountTracer struct {
	tracer *dramTracer
	dram   tracing.NamedHookable
}

type rdmaTransactionCountTracer struct {
	outgoingTracer *tracing.AverageTimeTracer
	incomingTracer *tracing.AverageTimeTracer
	rdmaEngine     *rdma.Comp
}

type simdBusyTimeTracer struct {
	tracer *tracing.BusyTimeTracer
	simd   tracing.NamedHookable
}

type cuCPIStackTracer struct {
	cu     tracing.NamedHookable
	tracer *cu.CPIStackTracer
}

type trafficTracer struct {
	rdma   tracing.NamedHookable
	tracer *tracing.StepCountTracer
}

type reporter struct {
	dataRecorder datarecording.DataRecorder

	kernelTimeTracer        *kernelTimeTracer
	perGPUKernelTimeTracers []*kernelTimeTracer
	instCountTracers        []*instCountTracer
	cacheLatencyTracers     []*cacheLatencyTracer
	cacheHitRateTracers     []*cacheHitRateTracer
	cohDirtracers           []*cohDirTracer
	tlbHitRateTracers       []*tlbHitRateTracer
	dramTracers             []*dramTransactionCountTracer
	rdmaTransactionCounters []*rdmaTransactionCountTracer
	simdBusyTimeTracers     []*simdBusyTimeTracer
	cuCPITraces             []*cuCPIStackTracer
	trafficTracer           []*trafficTracer

	ReportInstCount            bool
	ReportCacheLatency         bool
	ReportCacheHitRate         bool
	ReportTLBHitRate           bool
	ReportRDMATransactionCount bool
	ReportDRAMTransactionCount bool
	ReportSIMDBusyTime         bool
	ReportCPIStack             bool

	log2BlockSize uint64
}

func newReporter(s *simulation.Simulation) *reporter {
	r := &reporter{
		dataRecorder: s.GetDataRecorder(),
	}

	r.injectTracers(s)

	r.dataRecorder.CreateTable(tableName, metric{})
	r.dataRecorder.CreateTable(cohDirTable, metric{})

	return r
}

func (r *reporter) injectTracers(s *simulation.Simulation) {
	r.injectKernelTimeTracer(s)
	r.injectInstCountTracer(s)
	r.injectCUCPIHook(s)
	r.injectCacheLatencyTracer(s)
	r.injectCacheHitRateTracer(s)
	r.injectCohDirTracer(s)
	r.injectTLBHitRateTracer(s)
	r.injectRDMAEngineTracer(s)
	r.injectDRAMTracer(s)
	r.injectSIMDBusyTimeTracer(s)
	r.injectTrafficTracer(s)
	r.injectCoalescabilityHooks(s)
}

// kernelBoundaryTracer implements tracing.Tracer.
// It fires cohDir.OnKernelBoundary each time a LaunchKernelReq task
// completes on the attached CommandProcessor.
//
// Why a Tracer (not a raw sim.Hook):
//   tracing.EndTask creates a Task with only the ID field populated —
//   task.What and task.EndTime are both zero.  The only correct way to
//   match "Which task is ending?" is to remember the task ID from
//   StartTask and look it up in EndTask, exactly as BusyTimeTracer does.
type kernelBoundaryTracer struct {
	cohDir        *optdirectory.Comp
	engine        sim.Engine
	pendingKernels map[string]bool // task IDs of in-flight LaunchKernelReq
	kernelID       int
}

func (t *kernelBoundaryTracer) StartTask(task tracing.Task) {
	if task.What != "*protocol.LaunchKernelReq" {
		return
	}
	t.pendingKernels[task.ID] = true
}

func (t *kernelBoundaryTracer) StepTask(task tracing.Task) {}

func (t *kernelBoundaryTracer) EndTask(task tracing.Task) {
	if !t.pendingKernels[task.ID] {
		return
	}
	delete(t.pendingKernels, task.ID)
	t.cohDir.OnKernelBoundary(t.engine.CurrentTime(), t.kernelID)
	t.kernelID++
}

func (t *kernelBoundaryTracer) AddMilestone(m tracing.Milestone) {}

// injectCoalescabilityHooks pairs every optdirectory.Comp with its
// CommandProcessor (matched by the shared GPU-name prefix, e.g. "GPU[2]")
// and attaches a kernelBoundaryTracer via tracing.CollectTrace so that
// OnKernelBoundary is called automatically at each kernel completion.
func (r *reporter) injectCoalescabilityHooks(s *simulation.Simulation) {
	cohDirByPrefix := make(map[string]*optdirectory.Comp)
	for _, comp := range s.Components() {
		if cd, ok := comp.(*optdirectory.Comp); ok {
			cohDirByPrefix[gpuNamePrefix(cd.Name())] = cd
		}
	}
	if len(cohDirByPrefix) == 0 {
		return
	}

	for _, comp := range s.Components() {
		if !strings.Contains(comp.Name(), "CommandProcessor") {
			continue
		}
		cd, ok := cohDirByPrefix[gpuNamePrefix(comp.Name())]
		if !ok {
			continue
		}
		tracer := &kernelBoundaryTracer{
			cohDir:         cd,
			engine:         s.GetEngine(),
			pendingKernels: make(map[string]bool),
		}
		tracing.CollectTrace(comp.(tracing.NamedHookable), tracer)
	}
}

// gpuNamePrefix strips the last dot-separated segment from a component name.
// "GPU[2].CohDir" → "GPU[2]", "GPU[2].CommandProcessor" → "GPU[2]".
func gpuNamePrefix(name string) string {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return name
	}
	return name[:idx]
}

func (r *reporter) injectKernelTimeTracer(s *simulation.Simulation) {
	if *unifiedGPUFlag != "" {
		tracer := tracing.NewBusyTimeTracer(
			s.GetEngine(),
			func(task tracing.Task) bool {
				return task.What == "*driver.LaunchUnifiedMultiGPUKernelCommand"
			})
		tracing.CollectTrace(
			s.GetComponentByName("Driver").(tracing.NamedHookable),
			tracer)
		r.kernelTimeTracer = &kernelTimeTracer{
			tracer: tracer,
			comp:   s.GetComponentByName("Driver").(tracing.NamedHookable),
		}
	} else {
		tracer := tracing.NewBusyTimeTracer(
			s.GetEngine(),
			func(task tracing.Task) bool {
				return task.What == "*driver.LaunchKernelCommand"
			})
		tracing.CollectTrace(
			s.GetComponentByName("Driver").(tracing.NamedHookable),
			tracer)
		r.kernelTimeTracer = &kernelTimeTracer{
			tracer: tracer,
			comp:   s.GetComponentByName("Driver").(tracing.NamedHookable),
		}
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "CommandProcessor") {
			tracer := tracing.NewBusyTimeTracer(
				s.GetEngine(),
				func(task tracing.Task) bool {
					return task.What == "*protocol.LaunchKernelReq"
				})
			tracing.CollectTrace(
				comp.(tracing.NamedHookable),
				tracer)
			r.perGPUKernelTimeTracers = append(
				r.perGPUKernelTimeTracers,
				&kernelTimeTracer{
					tracer: tracer,
					comp:   comp.(tracing.NamedHookable),
				})
		}
	}
}

func (r *reporter) injectInstCountTracer(s *simulation.Simulation) {
	if !*reportAll && !*instCountReportFlag {
		return
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "CU") {
			tracer := newInstTracer()
			r.instCountTracers = append(r.instCountTracers,
				&instCountTracer{
					tracer: tracer,
					cu:     comp.(tracing.NamedHookable),
				})
			tracing.CollectTrace(comp.(tracing.NamedHookable), tracer)
		}
	}
}

func (r *reporter) injectCUCPIHook(s *simulation.Simulation) {
	if !*reportAll && !*reportCPIStackFlag {
		return
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "CU") {
			tracer := cu.NewCPIStackInstHook(
				comp.(*cu.ComputeUnit), s.GetEngine())
			tracing.CollectTrace(comp.(tracing.NamedHookable), tracer)

			r.cuCPITraces = append(r.cuCPITraces,
				&cuCPIStackTracer{
					tracer: tracer,
					cu:     comp.(tracing.NamedHookable),
				})
		}
	}
}

func (r *reporter) injectCacheLatencyTracer(s *simulation.Simulation) {
	if !*reportAll && !*cacheLatencyReportFlag {
		return
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "Cache") || strings.Contains(comp.Name(), "Dir") {
			tracer := tracing.NewAverageTimeTracer(
				s.GetEngine(),
				func(task tracing.Task) bool {
					return task.Kind == "req_in"
				})
			r.cacheLatencyTracers = append(r.cacheLatencyTracers,
				&cacheLatencyTracer{
					tracer: tracer,
					cache:  comp.(tracing.NamedHookable),
				})
			tracing.CollectTrace(comp.(tracing.NamedHookable), tracer)
		}
	}
}

func (r *reporter) injectCacheHitRateTracer(s *simulation.Simulation) {
	if !*reportAll && !*cacheLatencyReportFlag {
		return
	}

	for _, comp := range s.Components() {
		// if strings.Contains(comp.Name(), "Cache") || strings.Contains(comp.Name(), "Coh") {
		if strings.Contains(comp.Name(), "Cache") {
			tracer := tracing.NewStepCountTracer(
				func(task tracing.Task) bool { return true })
			r.cacheHitRateTracers = append(r.cacheHitRateTracers,
				&cacheHitRateTracer{
					tracer: tracer,
					cache:  comp.(tracing.NamedHookable),
				})
			tracing.CollectTrace(comp.(tracing.NamedHookable), tracer)
		}
	}
}

func (r *reporter) injectCohDirTracer(s *simulation.Simulation) {
	if !*reportAll && !*cacheLatencyReportFlag {
		return
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "Coh") || strings.Contains(comp.Name(), "Dir") {
			tracer := tracing.NewStepCountTracer(
				func(task tracing.Task) bool { return true })
			r.cohDirtracers = append(r.cohDirtracers,
				&cohDirTracer{
					tracer: tracer,
					cohDir: comp.(tracing.NamedHookable),
				})
			tracing.CollectTrace(comp.(tracing.NamedHookable), tracer)
		}
	}
}

func (r *reporter) injectTLBHitRateTracer(s *simulation.Simulation) {
	if !*reportAll && !*tlbHitRateReportFlag {
		return
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "TLB") {
			tracer := tracing.NewStepCountTracer(
				func(task tracing.Task) bool { return true })
			r.tlbHitRateTracers = append(r.tlbHitRateTracers,
				&tlbHitRateTracer{
					tracer: tracer,
					tlb:    comp.(tracing.NamedHookable),
				})
			tracing.CollectTrace(comp.(tracing.NamedHookable), tracer)
		}
	}
}

func (r *reporter) injectRDMAEngineTracer(s *simulation.Simulation) {
	if !*reportAll {
		return
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "RDMA") {
			t := &rdmaTransactionCountTracer{}
			t.rdmaEngine = comp.(*rdma.Comp)
			t.incomingTracer = tracing.NewAverageTimeTracer(
				s.GetEngine(),
				func(task tracing.Task) bool {
					if task.Kind != "req_in" {
						return false
					}

					isFromOutside := strings.Contains(
						string(task.Detail.(sim.Msg).Meta().Src), "RDMA")
					if !isFromOutside {
						return false
					}

					return true
				})
			t.outgoingTracer = tracing.NewAverageTimeTracer(
				s.GetEngine(),
				func(task tracing.Task) bool {
					if task.Kind != "req_in" {
						return false
					}

					isFromOutside := strings.Contains(
						string(task.Detail.(sim.Msg).Meta().Src), "RDMA")
					if isFromOutside {
						return false
					}

					return true
				})

			tracing.CollectTrace(t.rdmaEngine, t.incomingTracer)
			tracing.CollectTrace(t.rdmaEngine, t.outgoingTracer)

			r.rdmaTransactionCounters = append(r.rdmaTransactionCounters, t)
		}
	}
}

func (r *reporter) injectDRAMTracer(s *simulation.Simulation) {
	if !*reportAll && !*dramTransactionCountReportFlag {
		return
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "DRAM") {
			t := &dramTransactionCountTracer{}
			t.dram = comp.(tracing.NamedHookable)
			t.tracer = newDramTracer(s.GetEngine())

			tracing.CollectTrace(t.dram, t.tracer)

			r.dramTracers = append(r.dramTracers, t)
		}
	}
}

func (r *reporter) injectSIMDBusyTimeTracer(s *simulation.Simulation) {
	if !*reportAll && !*simdBusyTimeTracerFlag {
		return
	}

	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "SIMD") {
			perSIMDBusyTimeTracer := tracing.NewBusyTimeTracer(
				s.GetEngine(),
				func(task tracing.Task) bool {
					return task.Kind == "pipeline"
				})
			r.simdBusyTimeTracers = append(r.simdBusyTimeTracers,
				&simdBusyTimeTracer{
					tracer: perSIMDBusyTimeTracer,
					simd:   comp.(tracing.NamedHookable),
				})
			tracing.CollectTrace(comp.(tracing.NamedHookable), perSIMDBusyTimeTracer)
		}
	}
}

func (r *reporter) injectTrafficTracer(s *simulation.Simulation) {
	for _, comp := range s.Components() {
		if strings.Contains(comp.Name(), "RDMA") {
			tracer := tracing.NewStepCountTracer(
				func(task tracing.Task) bool { return true })
			r.trafficTracer = append(r.trafficTracer,
				&trafficTracer{
					tracer: tracer,
					rdma:   comp.(tracing.NamedHookable),
				})
			tracing.CollectTrace(comp.(tracing.NamedHookable), tracer)
		}
	}
}

func (r *reporter) report() {
	r.reportKernelTime()
	r.reportInstCount()
	r.reportCPIStack()
	r.reportSIMDBusyTime()
	r.reportCachelineUsage()
	r.reportCacheLatency()
	r.reportCacheHitRate()
	r.reportCacheEvictions()
	r.reportCohDir()
	r.reportTLBHitRate()
	r.reportRDMATransactionCount()
	r.reportDRAMTransactionCount()
	r.reportTraffic()
}

func (r *reporter) reportKernelTime() {
	kernelTime := float64(r.kernelTimeTracer.tracer.BusyTime())
	r.dataRecorder.InsertData(
		tableName,
		metric{
			Location: r.kernelTimeTracer.comp.Name(),
			What:     "kernel_time",
			Value:    kernelTime,
			Unit:     "second",
		},
	)

	for _, t := range r.perGPUKernelTimeTracers {
		kernelTime := float64(t.tracer.BusyTime())
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.comp.Name(),
				What:     "kernel_time",
				Value:    kernelTime,
				Unit:     "second",
			},
		)
	}
}

func (r *reporter) reportInstCount() {
	kernelTime := float64(r.kernelTimeTracer.tracer.BusyTime())
	for _, t := range r.instCountTracers {
		cuFreq := float64(t.cu.(*cu.ComputeUnit).Freq)
		numCycle := kernelTime * cuFreq

		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.cu.Name(),
				What:     "cu_inst_count",
				Value:    float64(t.tracer.count),
				Unit:     "count",
			},
		)

		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.cu.Name(),
				What:     "cu_CPI",
				Value:    numCycle / float64(t.tracer.count),
				Unit:     "cycles/inst",
			},
		)

		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.cu.Name(),
				What:     "simd_inst_count",
				Value:    float64(t.tracer.simdCount),
				Unit:     "count",
			},
		)

		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.cu.Name(),
				What:     "simd_CPI",
				Value:    numCycle / float64(t.tracer.simdCount),
				Unit:     "cycles/inst",
			},
		)
	}
}

func (r *reporter) reportCPIStack() {
	for _, t := range r.cuCPITraces {
		cu := t.cu
		hook := t.tracer

		r.reportCPIStackEntries(hook, cu, false)
		r.reportCPIStackEntries(hook, cu, true)
	}
}

func (r *reporter) reportCPIStackEntries(
	hook *cu.CPIStackTracer,
	cu tracing.NamedHookable,
	simdStack bool,
) {
	cpiStack := hook.GetCPIStack()
	if simdStack {
		cpiStack = hook.GetSIMDCPIStack()
	}

	keys := make([]string, 0, len(cpiStack))
	for k := range cpiStack {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	stackTypeName := "CPIStack"
	if simdStack {
		stackTypeName = "SIMDCPIStack"
	}

	for _, name := range keys {
		value := cpiStack[name]
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: cu.Name(),
				What:     stackTypeName + "." + name,
				Value:    value,
				Unit:     "cycles/inst",
			},
		)
	}
}

func (r *reporter) reportSIMDBusyTime() {
	for _, t := range r.simdBusyTimeTracers {
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.simd.Name(),
				What:     "busy_time",
				Value:    float64(t.tracer.BusyTime()),
				Unit:     "second",
			},
		)
	}
}

func (r *reporter) reportCacheLatency() {
	for _, tracer := range r.cacheLatencyTracers {
		if tracer.tracer.AverageTime() == 0 {
			continue
		}

		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: tracer.cache.Name(),
				What:     "req_average_latency",
				Value:    float64(tracer.tracer.AverageTime()),
				Unit:     "second",
			},
		)
	}
}

func (r *reporter) reportCachelineUsage() {
	for _, tracer := range r.cacheHitRateTracers {
		n := 1 << (r.log2BlockSize - 6)
		for i := 0; i <= n; i++ {
			what := fmt.Sprintf("Usage: %d/%d", i, n)
			count := tracer.tracer.GetStepCount(what)

			if count == 0 {
				continue
			}

			r.dataRecorder.InsertData(tableName, metric{
				Location: tracer.cache.Name(),
				What:     what,
				Value:    float64(count),
				Unit:     "count",
			})
		}
	}

	for _, tracer := range r.cacheHitRateTracers {
		what := [4]string{
			"RW: true/true",
			"RW: false/true",
			"RW: true/false",
			"RW: false/false",
		}

		for _, s := range what {
			count := tracer.tracer.GetStepCount(s)
			if count == 0 {
				continue
			}

			r.dataRecorder.InsertData(tableName, metric{
				Location: tracer.cache.Name(),
				What:     s,
				Value:    float64(count),
				Unit:     "count",
			})
		}
	}
}

func (r *reporter) reportCacheEvictions() {
	for _, tracer := range r.cacheHitRateTracers {
		evb := tracer.tracer.GetStepCount("EvictValidBlock")
		eib := tracer.tracer.GetStepCount("EvictInvalidBlock")
		ivb := tracer.tracer.GetStepCount("InvalidateValidBlock")
		iib := tracer.tracer.GetStepCount("InvalidateInvalidBlock")
		prfSt := tracer.tracer.GetStepCount("PrefetchStart")
		prf := tracer.tracer.GetStepCount("Prefetch")
		evtPrf := tracer.tracer.GetStepCount("EvictAndPrefetch")
		prfDscHit := tracer.tracer.GetStepCount("PrefetchDiscard - Hit")
		prfDscBsy := tracer.tracer.GetStepCount("PrefetchDiscard - Busy")

		total := evb + eib + ivb + iib + prf + evtPrf + prfDscHit + prfDscBsy
		if total == 0 {
			continue
		}

		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "EvictValidBlock",
			Value:    float64(evb),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "EvictInvalidBlock",
			Value:    float64(eib),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "InvalidateValidBlock",
			Value:    float64(ivb),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "InvalidateInvalidBlock",
			Value:    float64(iib),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "PrefetchStart",
			Value:    float64(prfSt),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "Prefetch",
			Value:    float64(prf),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "EvictAndPrefetch",
			Value:    float64(evtPrf),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "PrefetchDiscard - Hit",
			Value:    float64(prfDscHit),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "PrefetchDiscard - Busy",
			Value:    float64(prfDscBsy),
			Unit:     "count",
		})
	}
}

func (r *reporter) reportCacheHitRate() {
	for _, tracer := range r.cacheHitRateTracers {
		readHit := tracer.tracer.GetStepCount("read-hit")
		readMiss := tracer.tracer.GetStepCount("read-miss")
		readMSHRHit := tracer.tracer.GetStepCount("read-mshr-hit")
		remoteReadHit := tracer.tracer.GetStepCount("remote-read-hit")
		remoteReadMiss := tracer.tracer.GetStepCount("remote-read-miss")
		remoteReadMSHRHit := tracer.tracer.GetStepCount("remote-read-mshr-hit")
		writeHit := tracer.tracer.GetStepCount("write-hit")
		writeMiss := tracer.tracer.GetStepCount("write-miss")
		writeMSHRHit := tracer.tracer.GetStepCount("write-mshr-hit")
		remoteWriteHit := tracer.tracer.GetStepCount("remote-write-hit")
		remoteWriteMiss := tracer.tracer.GetStepCount("remote-write-miss")
		remoteWriteMSHRHit := tracer.tracer.GetStepCount("remote-write-mshr-hit")
		ToLocal := tracer.tracer.GetStepCount("ToLocal")
		ToRemote := tracer.tracer.GetStepCount("ToRemote")

		totalTransaction := readHit + readMiss + remoteReadHit + remoteReadMiss + readMSHRHit + remoteReadMSHRHit +
			writeHit + writeMiss + remoteWriteHit + remoteWriteMiss + writeMSHRHit + remoteWriteMSHRHit +
			ToLocal + ToRemote

		if totalTransaction == 0 {
			continue
		}

		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "read-hit",
			Value:    float64(readHit),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "read-miss",
			Value:    float64(readMiss),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "read-mshr-hit",
			Value:    float64(readMSHRHit),
			Unit:     "count",
		})

		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "remote-read-hit",
			Value:    float64(remoteReadHit),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "remote-read-miss",
			Value:    float64(remoteReadMiss),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "remote-read-mshr-hit",
			Value:    float64(remoteReadMSHRHit),
			Unit:     "count",
		})

		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "write-hit",
			Value:    float64(writeHit),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "write-miss",
			Value:    float64(writeMiss),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "write-mshr-hit",
			Value:    float64(writeMSHRHit),
			Unit:     "count",
		})

		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "remote-write-hit",
			Value:    float64(remoteWriteHit),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "remote-write-miss",
			Value:    float64(remoteWriteMiss),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "remote-write-mshr-hit",
			Value:    float64(remoteWriteMSHRHit),
			Unit:     "count",
		})

		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "ToLocal",
			Value:    float64(ToLocal),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(tableName, metric{
			Location: tracer.cache.Name(),
			What:     "ToRemote",
			Value:    float64(ToRemote),
			Unit:     "count",
		})
	}
}

func (r *reporter) reportCohDir() {
	for _, tracer := range r.cohDirtracers {
		UpdateEntry := tracer.tracer.GetStepCount("UpdateEntry")
		InvalidateByEviction := tracer.tracer.GetStepCount("InvalidateByEviction")
		InvalidateByWrite := tracer.tracer.GetStepCount("InvalidateByWrite")
		InvalidateByPromotion := tracer.tracer.GetStepCount("InvalidateByPromotion")
		InvalidateByDemotion := tracer.tracer.GetStepCount("InvalidateByDemotion")
		ToLocalData := tracer.tracer.GetStepCount("ToLocalData")
		ToRemoteData := tracer.tracer.GetStepCount("ToRemoteData")
		FromLocal := tracer.tracer.GetStepCount("FromLocal")
		FromRemote := tracer.tracer.GetStepCount("FromRemote")

		totalTransaction := UpdateEntry + InvalidateByEviction + InvalidateByWrite +
			ToLocalData + ToRemoteData + FromLocal + FromRemote
		// totalTransaction := ToLocalData + ToRemoteData

		if totalTransaction == 0 {
			continue
		}

		r.dataRecorder.InsertData(cohDirTable, metric{
			Location: tracer.cohDir.Name(),
			What:     "UpdateEntry",
			Value:    float64(UpdateEntry),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(cohDirTable, metric{
			Location: tracer.cohDir.Name(),
			What:     "InvalidateByEviction",
			Value:    float64(InvalidateByEviction),
			Unit:     "count",
		})
		r.dataRecorder.InsertData(cohDirTable, metric{
			Location: tracer.cohDir.Name(),
			What:     "InvalidateByWrite",
			Value:    float64(InvalidateByWrite),
			Unit:     "count",
		})

		if InvalidateByPromotion != 0 {
			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.cohDir.Name(),
				What:     "InvalidateByPromotion",
				Value:    float64(InvalidateByPromotion),
				Unit:     "count",
			})
		}
		if InvalidateByDemotion != 0 {
			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.cohDir.Name(),
				What:     "InvalidateByDemotion",
				Value:    float64(InvalidateByDemotion),
				Unit:     "count",
			})
		}

		r.dataRecorder.InsertData(cohDirTable, metric{
			Location: tracer.cohDir.Name(),
			What:     "ToLocalData",
			Value:    float64(ToLocalData),
			Unit:     "count",
		}) // directory에 들어온 요청 중, 주소가 local memory에 속하는 것
		r.dataRecorder.InsertData(cohDirTable, metric{
			Location: tracer.cohDir.Name(),
			What:     "ToRemoteData",
			Value:    float64(ToRemoteData),
			Unit:     "count",
		}) // directory에 들어온 요청 중, 주소가 remote memory에 속하는 것: 모두 From Remote임
		r.dataRecorder.InsertData(cohDirTable, metric{
			Location: tracer.cohDir.Name(),
			What:     "FromLocal",
			Value:    float64(FromLocal),
			Unit:     "count",
		}) // directory에 들어온 요청 중, local L1 cache에서 보낸 것
		r.dataRecorder.InsertData(cohDirTable, metric{
			Location: tracer.cohDir.Name(),
			What:     "FromRemote",
			Value:    float64(FromRemote),
			Unit:     "count",
		}) // directory에 들어온 요청 중, remote L2 cache에서 보낸 것: 모두 toLocalData임
	}

	for _, tracer := range r.cohDirtracers {
		what0 := "UpdateEntry"
		what1 := "InvalidateByEviction"
		what2 := "InvalidateByWrite"
		what3 := "InvalidateByPromotion"
		what4 := "InvalidateByDemotion"
		for bankID := 0; bankID < 5; bankID++ {
			UpdateEntry := tracer.tracer.GetStepCount(what0 + fmt.Sprintf(" - %d", bankID))
			InvalidateByEviction := tracer.tracer.GetStepCount(what1 + fmt.Sprintf(" - %d", bankID))
			InvalidateByWrite := tracer.tracer.GetStepCount(what2 + fmt.Sprintf(" - %d", bankID))
			InvalidateByPromotion := tracer.tracer.GetStepCount(what3 + fmt.Sprintf(" - %d", bankID))
			InvalidateByDemotion := tracer.tracer.GetStepCount(what4 + fmt.Sprintf(" - %d", bankID))

			totalTransaction := UpdateEntry + InvalidateByEviction + InvalidateByWrite

			if totalTransaction == 0 {
				continue
			}

			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.cohDir.Name(),
				What:     what0 + fmt.Sprintf(" - %d", bankID),
				Value:    float64(UpdateEntry),
				Unit:     "count",
			})
			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.cohDir.Name(),
				What:     what1 + fmt.Sprintf(" - %d", bankID),
				Value:    float64(InvalidateByEviction),
				Unit:     "count",
			})
			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.cohDir.Name(),
				What:     what2 + fmt.Sprintf(" - %d", bankID),
				Value:    float64(InvalidateByWrite),
				Unit:     "count",
			})
			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.cohDir.Name(),
				What:     what3 + fmt.Sprintf(" - %d", bankID),
				Value:    float64(InvalidateByPromotion),
				Unit:     "count",
			})
			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.cohDir.Name(),
				What:     what4 + fmt.Sprintf(" - %d", bankID),
				Value:    float64(InvalidateByDemotion),
				Unit:     "count",
			})
		}
	}

	// BankChecked - N: 요청 한 건을 처리하기 위해 N개의 bank를 확인한 횟수 (SuperDirectory 전용)
	for _, tracer := range r.cohDirtracers {
		for n := 1; n <= 10; n++ {
			what := fmt.Sprintf("BankChecked - %d", n)
			count := tracer.tracer.GetStepCount(what)
			if count == 0 {
				continue
			}
			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.cohDir.Name(),
				What:     what,
				Value:    float64(count),
				Unit:     "count",
			})
		}
	}
}

func (r *reporter) reportTLBHitRate() {
	for _, tracer := range r.tlbHitRateTracers {
		hit := tracer.tracer.GetStepCount("hit")
		miss := tracer.tracer.GetStepCount("miss")
		mshrHit := tracer.tracer.GetStepCount("mshr-hit")

		totalTransaction := hit + miss + mshrHit

		if totalTransaction == 0 {
			continue
		}

		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: tracer.tlb.Name(),
				What:     "hit",
				Value:    float64(hit),
				Unit:     "count",
			},
		)
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: tracer.tlb.Name(),
				What:     "miss",
				Value:    float64(miss),
				Unit:     "count",
			},
		)
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: tracer.tlb.Name(),
				What:     "mshr-hit",
				Value:    float64(mshrHit),
				Unit:     "count",
			},
		)
	}
}

func (r *reporter) reportRDMATransactionCount() {
	for _, t := range r.rdmaTransactionCounters {
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.rdmaEngine.Name(),
				What:     "outgoing_trans_count",
				Value:    float64(t.outgoingTracer.TotalCount()),
				Unit:     "count",
			},
		)
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.rdmaEngine.Name(),
				What:     "incoming_trans_count",
				Value:    float64(t.incomingTracer.TotalCount()),
				Unit:     "count",
			},
		)
	}
}

func (r *reporter) reportDRAMTransactionCount() {
	for _, t := range r.dramTracers {
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.dram.Name(),
				What:     "read_trans_count",
				Value:    float64(t.tracer.readCount),
				Unit:     "count",
			},
		)
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.dram.Name(),
				What:     "write_trans_count",
				Value:    float64(t.tracer.writeCount),
				Unit:     "count",
			},
		)
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.dram.Name(),
				What:     "read_avg_latency",
				Value:    float64(t.tracer.readAvgLatency),
				Unit:     "second",
			},
		)
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.dram.Name(),
				What:     "write_avg_latency",
				Value:    float64(t.tracer.writeAvgLatency),
				Unit:     "second",
			},
		)
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.dram.Name(),
				What:     "read_size",
				Value:    float64(t.tracer.readSize),
				Unit:     "bytes",
			},
		)
		r.dataRecorder.InsertData(
			tableName,
			metric{
				Location: t.dram.Name(),
				What:     "write_size",
				Value:    float64(t.tracer.writeSize),
				Unit:     "bytes",
			},
		)
	}
}

func (r *reporter) reportTraffic() {
	for _, tracer := range r.trafficTracer {
		stepNames := tracer.tracer.GetStepNames()

		for _, s := range stepNames {
			count := tracer.tracer.GetStepCount(s)

			r.dataRecorder.InsertData(cohDirTable, metric{
				Location: tracer.rdma.Name(),
				What:     s,
				Value:    float64(count),
				Unit:     "count",
			})
		}
	}
}
