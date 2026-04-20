# B-3 β design: isolation grep results (Task A3)

Timestamp: 2026-04-20
Branch: m1-phase-b3-beta-design
Commit: (see git log)

## Check A: instrument/*.go — no akita import

Command: grep -rE '"github.com/sarchlab/akita' instrument/phase_metrics*.go instrument/phase_clock*.go
Result: PASS (0 matches)

Files checked:
  instrument/phase_metrics.go
  instrument/phase_metrics_bench_test.go
  instrument/phase_metrics_test.go
  instrument/phase_clock.go
  instrument/phase_clock_test.go

## Check B: coherence/*.go — no akita import

Command: grep -rE '"github.com/sarchlab/akita' coherence/*.go
Result: PASS (0 matches)

Files checked: all *.go in coherence/

## Check C: instrument/adapter/*.go — akita imports present (expected)

Command: grep -rE '"github.com/sarchlab/akita' instrument/adapter/*.go (excl. _test.go)
Result:
  instrument/adapter/cu_adapter.go: "github.com/sarchlab/akita/v4/sim"
  instrument/adapter/l2_adapter.go: "github.com/sarchlab/akita/v4/mem/cache/writebackcoh"
  instrument/adapter/l2_adapter.go: "github.com/sarchlab/akita/v4/sim"

This is expected and correct: instrument/adapter/ is the sole akita import layer.
directory_adapter.go imports coherence + instrument only (no akita).
phase_lifecycle.go imports instrument only (no akita).
common.go imports instrument only (no akita).

## Conclusion

3-layer isolation holds after β design implementation:
  Layer 1 (instrument/): akita-free ✓
  Layer 2 (instrument/adapter/): akita imports present ✓ (expected)
  Layer 3 (coherence/): akita-free ✓
