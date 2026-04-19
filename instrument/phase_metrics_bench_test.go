package instrument

import (
	"testing"
)

// BenchmarkBaseline_Tick measures the cost of 1M Tick calls with no metric
// collection (pure clock overhead). This is the "off" baseline.
func BenchmarkBaseline_Tick(b *testing.B) {
	clock := NewPhaseClock(0, PhaseID{}) // window disabled → no listener overhead
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clock.Tick(uint64(i))
	}
}

// BenchmarkWithMetrics_AddRegionAccess measures 1M AddRegionAccess calls
// (hot path with all metric collection active).
// Pre-seeds one region to avoid measuring AddRegionFetch allocation cost.
func BenchmarkWithMetrics_AddRegionAccess(b *testing.B) {
	m := NewPhaseMetrics()
	m.AddRegionFetch(0x1000, 1024) // 16 cachelines pre-seeded
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.AddRegionAccess(0x1000, uint32(i%16))
	}
}

// BenchmarkWithMetrics_100K_AddRegionAccess_1M_Tick simulates the spec scenario:
// 1M Tick + 100K AddRegionAccess calls, measuring total cost of the
// metric-enabled path vs the baseline.
func BenchmarkWithMetrics_100K_AddRegionAccess_1M_Tick(b *testing.B) {
	for i := 0; i < b.N; i++ {
		clock := NewPhaseClock(0, PhaseID{}) // no window boundary overhead
		m := NewPhaseMetrics()
		m.AddRegionFetch(0x1000, 1024)

		// 1M Tick (baseline component)
		for t := uint64(0); t < 1_000_000; t++ {
			clock.Tick(t)
		}
		// 100K AddRegionAccess (metric component)
		for j := 0; j < 100_000; j++ {
			_ = m.AddRegionAccess(0x1000, uint32(j%16))
		}
	}
}

// BenchmarkBaseline_1M_Tick measures 1M Tick calls without any metric
// collection, providing the "off" comparison for the above scenario.
func BenchmarkBaseline_1M_Tick(b *testing.B) {
	for i := 0; i < b.N; i++ {
		clock := NewPhaseClock(0, PhaseID{})
		for t := uint64(0); t < 1_000_000; t++ {
			clock.Tick(t)
		}
	}
}

// BenchmarkWithMetrics_Flush measures the cost of Flush() over 100 active
// regions with sharer data — worst-case consistency computation.
func BenchmarkWithMetrics_Flush(b *testing.B) {
	for i := 0; i < b.N; i++ {
		m := NewPhaseMetrics()
		for r := uint64(0); r < 100; r++ {
			tag := r * 0x100
			m.AddRegionFetch(tag, 256)
			_ = m.AddRegionAccess(tag, 0)
			_ = m.AddRegionAccess(tag, 1)
		}
		_, _ = m.Flush()
	}
}