# PHASE D.2 — Aggregate Summary

**Date:** 2026-04-20
**Branch:** m1-phase-d2-full-sweep
**Source:** results/m1/d2/raw/sweep_summary.tsv (90 runs, 3-seed averages)

All metrics are seed-averaged. For deterministic workloads (all except pagerank) seeds
produce identical values, so the average equals the per-seed value.

---

## 1. Seed-averaged metrics per (workload, R)

### simpleconvolution — home-node, phases=3, RetiredWf=4132, L2H=56909, L2M=33619

| R | avg_wall_s | fetched | accessed | util% | l2_hit% |
|---|---|---|---|---|---|
| 64 | 58 | 4213632 | 2105408 | 50.0 | 62.9 |
| 256 | 57 | 4216320 | 2105408 | 49.9 | 62.9 |
| 1024 | 61 | 4229120 | 2105408 | 49.8 | 62.9 |
| 4096 | 64 | 4284416 | 2105408 | 49.1 | 62.9 |
| 16384 | 66 | 4325376 | 2105408 | 48.7 | 62.9 |

### matrixtranspose — true multi-GPU, phases=2, RetiredWf=256, L2H=0, L2M=45154

| R | avg_wall_s | fetched | accessed | util% | l2_hit% |
|---|---|---|---|---|---|
| 64 | 11 | 4197888 | 2097152 | 49.9 | 0.0 |
| 256 | 11 | 4199424 | 2097152 | 49.9 | 0.0 |
| 1024 | 10 | 4206592 | 2097152 | 49.8 | 0.0 |
| 4096 | 11 | 4243456 | 2097152 | 49.4 | 0.0 |
| 16384 | 10 | 4325376 | 2097152 | 48.5 | 0.0 |

### matrixmultiplication — true multi-GPU, phases=4, RetiredWf=64, L2H=16976, L2M=22644

| R | avg_wall_s | fetched | accessed | util% | l2_hit% |
|---|---|---|---|---|---|
| 64 | 23 | 1261056 | 626944 | 49.7 | 42.8 |
| 256 | 24 | 1268736 | 626944 | 49.4 | 42.8 |
| 1024 | 24 | 1343488 | 626944 | 46.7 | 42.8 |
| 4096 | 23 | 1449984 | 626944 | 43.2 | 42.8 |
| 16384 | 23 | 1802240 | 626944 | 34.8 | 42.8 |

### pagerank — home-node, phases=3, RetiredWf=3072, L2H=13807†, L2M=750

| R | avg_wall_s | fetched† | accessed | util% | l2_hit% |
|---|---|---|---|---|---|
| 64 | 19 | 175317 | 90240 | 51.5 | 94.8 |
| 256 | 19 | 184917 | 90240 | 48.8 | 94.8 |
| 1024 | 20 | 195584 | 90240 | 46.1 | 94.8 |
| 4096 | 19 | 229376 | 90240 | 39.3 | 94.8 |
| 16384 | 20 | 311296 | 90240 | 29.0 | 94.8 |

†seed-averaged: seed42=13550 / seed43=14605 / seed44=13266; fetched varies at R=64,256.

### fir — true multi-GPU, phases=2, RetiredWf=1024, L2H=255, L2M=8224

| R | avg_wall_s | fetched | accessed | util% | l2_hit% |
|---|---|---|---|---|---|
| 64 | 17 | 1050496 | 524352 | 49.9 | 3.0 |
| 256 | 17 | 1053184 | 524352 | 49.8 | 3.0 |
| 1024 | 16 | 1067008 | 524352 | 49.1 | 3.0 |
| 4096 | 17 | 1122304 | 524352 | 46.7 | 3.0 |
| 16384 | 17 | 1228800 | 524352 | 42.7 | 3.0 |

### stencil2d — home-node, phases=3, RetiredWf=248, L2H=9437, L2M=31867

| R | avg_wall_s | fetched | accessed | util% | l2_hit% |
|---|---|---|---|---|---|
| 64 | 14 | 4073792 | 2034752 | 49.9 | 22.8 |
| 256 | 15 | 4201472 | 2034752 | 48.4 | 22.8 |
| 1024 | 15 | 4204544 | 2034752 | 48.4 | 22.8 |
| 4096 | 14 | 4218880 | 2034752 | 48.2 | 22.8 |
| 16384 | 15 | 4259840 | 2034752 | 47.7 | 22.8 |

---

## 2. L2 hit rate profile

| Workload | L2H | L2M | l2_hit% | Access pattern |
|---|---|---|---|---|
| pagerank | 13807 | 750 | **94.8** | Sparse graph, high node reuse |
| simpleconvolution | 56909 | 33619 | **62.9** | 2D stencil, overlap in mask window |
| matrixmultiplication | 16976 | 22644 | **42.8** | Dense GEMM, tiled — partial reuse |
| stencil2d | 9437 | 31867 | **22.8** | 2D stencil, streaming halo |
| fir | 255 | 8224 | **3.0** | Streaming filter, near-zero reuse |
| matrixtranspose | 0 | 45154 | **0.0** | Strided transpose, no cache reuse |

---

## 3. Region utilization (accessed/fetched) at baseline R=64

util% = accessed / fetched × 100. At R=64, all workloads are near 50% because the
region granularity is 64 bytes (one cache line) and symmetric alignment causes ~50%
of each fetch to be useful data.

| Workload | accessed | fetched (R=64) | util% |
|---|---|---|---|
| simpleconvolution | 2105408 | 4213632 | 50.0 |
| matrixtranspose | 2097152 | 4197888 | 49.9 |
| matrixmultiplication | 626944 | 1261056 | 49.7 |
| pagerank | 90240 | 175317 | 51.5 |
| fir | 524352 | 1050496 | 49.9 |
| stencil2d | 2034752 | 4073792 | 49.9 |

At R=64 the region = cache line, so `fetched ≈ 2 × accessed` for all workloads.
This is the expected floor: every access fetches the aligned region even if only
half of it is touched.

---

## 4. fetched overhead growth with R

`overhead%` = (fetched(R) - fetched(R=64)) / fetched(R=64) × 100.

| Workload | R=256 | R=1024 | R=4096 | R=16384 |
|---|---|---|---|---|
| simpleconvolution | +0.06% | +0.37% | +1.68% | +2.66% |
| matrixtranspose | +0.04% | +0.21% | +1.08% | +3.03% |
| matrixmultiplication | +0.61% | +6.54% | +15.0% | +42.9% |
| pagerank | +5.5% | +11.6% | +30.8% | +77.6% |
| fir | +0.25% | +1.57% | +6.8% | +17.0% |
| stencil2d | +3.1% | +3.1% | +3.6% | +4.6% |

**High overhead (R-sensitive):** pagerank (+77.6%) and matrixmultiplication (+42.9%)
show the most wasted bandwidth at large R, indicating their working sets have many
small, scattered active sub-regions.

**Low overhead (R-insensitive):** simpleconvolution (+2.7%) and matrixtranspose (+3.0%)
are nearly flat, indicating large contiguous access patterns where region padding is
amortized.

---

## 5. Wall-clock time

Wall-clock time shows minimal variation with R (no correlation), confirming the
simulation is compute-bound and not sensitive to region-size configuration at this
problem scale.

| Workload | min (s) | max (s) | range |
|---|---|---|---|
| simpleconvolution | 56 | 66 | 10s |
| matrixtranspose | 10 | 11 | 1s |
| matrixmultiplication | 23 | 24 | 1s |
| pagerank | 19 | 20 | 1s |
| fir | 16 | 18 | 2s |
| stencil2d | 14 | 15 | 1s |

Total sweep wall-clock: ~47 min sequential.
