package runner

import (
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/sarchlab/akita/v4/mem/mempath"
	"github.com/sarchlab/akita/v4/sim"
)

// memPathClassStat aggregates the per-request latency statistics for one
// hit-location class (e.g. L1, L2_local, remote_L2, DRAM_local, DRAM_remote).
type memPathClassStat struct {
	count  uint64
	sumLat float64 // total latency, seconds
	minLat float64
	maxLat float64

	// Per-component segment contributions, keyed by "FROM->TO" component
	// labels (AT, L1, Dir, L2, DRAM, RDMA). Only non-coalesced probes
	// contribute here (their intermediate timestamps are their own).
	segSum    map[string]float64
	segCount  map[string]uint64
	fullPaths uint64 // number of non-coalesced probes contributing segments
}

// memPathCollector is the global sink for completed mem-latency probes. It is
// concurrency-safe because the parallel engine may complete requests on many
// goroutines; the probe itself is single-owner along its path, only the shared
// aggregation needs the lock. It performs pure observation and never affects
// simulated timing.
type memPathCollector struct {
	mu      sync.Mutex
	byClass map[string]*memPathClassStat
}

func newMemPathCollector() *memPathCollector {
	return &memPathCollector{byClass: map[string]*memPathClassStat{}}
}

// Add records one completed probe. Assigned to mempath.Collect by the runner.
func (c *memPathCollector) Add(p *mempath.Probe, total sim.VTimeInSec) {
	if p == nil {
		return
	}

	lat := float64(total)

	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.byClass[p.HitClass]
	if st == nil {
		st = &memPathClassStat{
			minLat:   lat,
			maxLat:   lat,
			segSum:   map[string]float64{},
			segCount: map[string]uint64{},
		}
		c.byClass[p.HitClass] = st
	}

	st.count++
	st.sumLat += lat
	if lat < st.minLat {
		st.minLat = lat
	}
	if lat > st.maxLat {
		st.maxLat = lat
	}

	// Per-component segment breakdown — only for probes that carry their own
	// timestamps end-to-end (coalesced secondaries share another request's
	// fill, so their intermediate deltas are not their own).
	if !p.Coalesced && len(p.Stamps) >= 2 {
		st.fullPaths++
		for i := 1; i < len(p.Stamps); i++ {
			from := memPathComponent(p.Stamps[i-1].Event)
			to := memPathComponent(p.Stamps[i].Event)
			// Same component on both ends = that component's own internal
			// latency (e.g. L2.in -> L2.hit). Different components = the
			// link/queueing latency between them (e.g. L1 -> L2).
			seg := from
			if from != to {
				seg = from + "->" + to
			}
			st.segSum[seg] += float64(p.Stamps[i].Time - p.Stamps[i-1].Time)
			st.segCount[seg]++
		}
	}
}

// memPathComponent normalizes a stamp event to its hardware component so the
// per-segment breakdown is keyed by component rather than the GPU-specific
// component name.
func memPathComponent(event string) string {
	switch {
	case len(event) >= 2 && event[:2] == "AT":
		return "AT"
	case len(event) >= 3 && event[:3] == "L1.":
		return "L1"
	case len(event) >= 4 && event[:4] == "Dir.":
		return "Dir"
	case len(event) >= 3 && event[:3] == "L2.":
		return "L2"
	case event == mempath.EvDRAM:
		return "DRAM"
	case len(event) >= 5 && event[:5] == "RDMA.":
		return "RDMA"
	default:
		return event
	}
}

// emitMemPathReport writes the path-wise latency summary CSV at the end of the
// run. No-op unless -mem-latency-trace was set. Latencies are reported in
// nanoseconds (simulated time is in seconds internally).
func (r *Runner) emitMemPathReport() {
	if !r.memLatencyTrace || r.memPathCollector == nil {
		return
	}

	out := r.memLatencyTraceOutput
	if out == "" {
		out = *filenameFlag + "_mem_path.csv"
	}

	f, err := os.Create(out)
	if err != nil {
		fmt.Printf("[mem-latency-trace] cannot create %s: %v\n", out, err)
		return
	}
	defer f.Close()

	c := r.memPathCollector
	c.mu.Lock()
	defer c.mu.Unlock()

	// Stable ordering of classes.
	classes := make([]string, 0, len(c.byClass))
	var totalCount uint64
	for k, st := range c.byClass {
		classes = append(classes, k)
		totalCount += st.count
	}
	sort.Strings(classes)

	// Section 1: per hit-location summary.
	fmt.Fprintln(f, "# section,hit_class,count,pct_requests,"+
		"avg_latency_ns,min_latency_ns,max_latency_ns")
	for _, k := range classes {
		st := c.byClass[k]
		pct := 0.0
		if totalCount > 0 {
			pct = 100 * float64(st.count) / float64(totalCount)
		}
		fmt.Fprintf(f, "summary,%s,%d,%.4f,%.4f,%.4f,%.4f\n",
			k, st.count, pct,
			1e9*st.sumLat/float64(st.count),
			1e9*st.minLat,
			1e9*st.maxLat,
		)
	}

	// Section 2: per hit-location, per-component average latency contribution.
	fmt.Fprintln(f, "# section,hit_class,segment,avg_ns,samples")
	for _, k := range classes {
		st := c.byClass[k]
		segs := make([]string, 0, len(st.segSum))
		for s := range st.segSum {
			segs = append(segs, s)
		}
		sort.Strings(segs)
		for _, s := range segs {
			n := st.segCount[s]
			if n == 0 {
				continue
			}
			fmt.Fprintf(f, "segment,%s,%s,%.4f,%d\n",
				k, s, 1e9*st.segSum[s]/float64(n), n)
		}
	}

	fmt.Printf("[mem-latency-trace] wrote %s (%d requests across %d classes)\n",
		out, totalCount, len(classes))
}
