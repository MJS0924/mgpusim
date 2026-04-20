# PHASE D.2 — Sweep Sanity Check

**Date:** 2026-04-20
**Branch:** m1-phase-d2-full-sweep
**Config:** 6 workloads × 5 R × 3 seeds = 90 runs
**Source:** results/m1/d2/raw/sweep_summary.tsv

---

## 1. Run completion

| Metric | Value |
|---|---|
| Total runs | 90 |
| PASS | 90 |
| FAIL | 0 |
| TIMEOUT | 0 |

All 90 runs completed successfully within the 900s timeout.

---

## 2. V11 / V12 invariants

Both invariants were reported inline in M1_SUMMARY for every run.

| Workload | V11 (DirectoryEvictions=0) | V12 (accessed ≤ fetched) |
|---|---|---|
| simpleconvolution | PASS (15/15) | PASS (15/15) |
| matrixtranspose | PASS (15/15) | PASS (15/15) |
| matrixmultiplication | PASS (15/15) | PASS (15/15) |
| pagerank | PASS (15/15) | PASS (15/15) |
| fir | PASS (15/15) | PASS (15/15) |
| stencil2d | PASS (15/15) | PASS (15/15) |

**V11:** `InfiniteCapacity=true` holds across all region sizes and seeds. No directory evictions in any run.

**V12 spot-check** (highest R=16384, most conservative):

| Workload | accessed | fetched (R=16384) | margin |
|---|---|---|---|
| simpleconvolution | 2105408 | 4325376 | 2219968 |
| matrixtranspose | 2097152 | 4325376 | 2228224 |
| matrixmultiplication | 626944 | 1802240 | 1175296 |
| pagerank | 90240 | 311296 | 221056 |
| fir | 524352 | 1228800 | 704448 |
| stencil2d | 2034752 | 4259840 | 2225088 |

No violations. `accessed ≤ fetched` holds with comfortable margin.

---

## 3. RetiredWf consistency

D.1 reference values vs. D.2 observations across all 15 runs per workload.

| Workload | D.1 ref | D.2 observed (all 15 runs) | Consistent |
|---|---|---|---|
| simpleconvolution | 4132 | 4132 | ✓ |
| matrixtranspose | 256 | 256 | ✓ |
| matrixmultiplication | 64 | 64 | ✓ |
| pagerank | 3072 | 3072 | ✓ |
| fir | 1024 | 1024 | ✓ |
| stencil2d | 248 | 248 | ✓ |

RetiredWf is deterministic across all region sizes and seeds for all workloads. No anomalies.

---

## 4. Phase count consistency

| Workload | phases (all 15 runs) |
|---|---|
| simpleconvolution | 3 |
| matrixtranspose | 2 |
| matrixmultiplication | 4 |
| pagerank | 3 |
| fir | 2 |
| stencil2d | 3 |

Phase count is constant across R and seed for all workloads.

---

## 5. β dedup sensitivity (fetched vs. R)

`fetched` increases monotonically with region size for all workloads, confirming the β dedup mechanism: larger regions import more bytes per miss (coarser granularity → less spatial reuse).

| Workload | R=64 | R=256 | R=1024 | R=4096 | R=16384 | Ratio 16384/64 |
|---|---|---|---|---|---|---|
| simpleconvolution | 4213632 | 4216320 | 4229120 | 4284416 | 4325376 | 1.027 |
| matrixtranspose | 4197888 | 4199424 | 4206592 | 4243456 | 4325376 | 1.030 |
| matrixmultiplication | 1261056 | 1268736 | 1343488 | 1449984 | 1802240 | 1.429 |
| pagerank | 175317† | 184917† | 195584 | 229376 | 311296 | 1.776 |
| fir | 1050496 | 1053184 | 1067008 | 1122304 | 1228800 | 1.170 |
| stencil2d | 4073792 | 4201472 | 4204544 | 4218880 | 4259840 | 1.046 |

†seed-averaged (pagerank uses random graph, fetched varies slightly by seed at small R)

**Monotonicity:** All 6 workloads satisfy `fetched(R_i) ≤ fetched(R_{i+1})` across all 5 region sizes. No β reversal observed.

**Sensitivity range:** pagerank (×1.78) and matrixmultiplication (×1.43) show the highest sensitivity, indicating their working sets span many region boundaries. simpleconvolution (×1.03) and matrixtranspose (×1.03) are insensitive, consistent with large contiguous access patterns that fit well into any region size.

---

## 6. accessed constant across R

`accessed` (total bytes touched) is independent of region size for all workloads, as expected — the computation is deterministic.

| Workload | accessed (constant) |
|---|---|
| simpleconvolution | 2105408 |
| matrixtranspose | 2097152 |
| matrixmultiplication | 626944 |
| pagerank | 90240 |
| fir | 524352 |
| stencil2d | 2034752 |

---

## 7. Seed variation

| Workload | Metric with seed variation | Notes |
|---|---|---|
| pagerank | L2H varies: seed42=13550, seed43=14605, seed44=13266 | Random graph → different edge distributions |
| pagerank | fetched varies at R=64,256 | Boundary alignment varies with graph layout |
| all others | No seed variation in any metric | Deterministic workloads |

Pagerank seed variation is expected and not an anomaly: the graph topology is seeded, changing data locality and cache hit patterns. L2M=750 and accessed=90240 remain constant across seeds.

---

## 8. Anomaly summary

| Check | Result |
|---|---|
| V11 violations | 0 |
| V12 violations | 0 |
| RetiredWf mismatches vs D.1 | 0 |
| β reversal (fetched decreasing with R) | 0 |
| accessed varying with R | 0 |
| Phase count inconsistencies | 0 |
| Unexpected FAILs or TIMEOUTs | 0 |

**No anomalies detected. All sanity checks PASS.**

---

## 9. GO / NO-GO recommendation

**GO** — all 90 runs complete, all invariants hold, β sensitivity is monotonic and physically interpretable. The dataset is ready for Phase E analysis.
