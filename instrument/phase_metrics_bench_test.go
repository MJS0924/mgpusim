package instrument

// Overhead status (B-2.6 / B-3.5 판정 기준, §B-4):
//
// B-2.6 micro-benchmark (Intel Xeon Gold 6246R @ 3.40GHz):
//   Baseline 1M Tick:                 2,370,374 ns/op  0 allocs
//   1M Tick + 100K AddRegionAccess:   4,632,762 ns/op
//   Overhead: Δ = 2,262,388 ns  ≈ 95.4% vs baseline
//
// CURRENT STATUS: 5% 기준 초과.
// 원인: micro-benchmark 에서 Tick 이 2.7 ns 로 저렴하기 때문에
//   AddRegionAccess(23 ns/op) 가 상대적으로 과장되어 보임.
// 실제 akita simulation 에서의 overhead 는 B-3.5 hookpoint 측정 시 판정.
// "akita Tick µs-scale → 실질 < 1%" 단정은 B-3.5 전까지 근거 없음 — 기재 금지.
// B-3.5 후 이 주석을 실측치로 교체할 것.

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