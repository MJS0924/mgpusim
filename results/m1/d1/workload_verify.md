# PHASE D.1 — Workload Verification

**Date:** 2026-04-20
**Branch:** m1-phase-d1-workload-verify
**Config:** R=1024, seed=42, -gpus=2,3,4,5, -timing -disable-rtm
**Timeout:** 600s per workload

## Verification table

| Workload | Build | Run | Wall-clock | Phases | Total L2 H/M | RetiredWf | V11 | V12 | Multi-GPU | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| simpleconvolution | ✓ | ✓ | 63s | 3 | 56909 / 33619 | 4132 | PASS | PASS | home-node | baseline (D.0 confirmed) |
| matrixtranspose | ✓ | ✓ | 10s | 2 | 0 / 45154 | 256 | PASS | PASS | true | L2H=0: purely streaming (no reuse) |
| bitonicsort | ✓ | ✓ | 228s | 72 | 853852 / 14931 | 69632 | PASS | PASS | true | high L2H (reuse); 3:48 wall-clock |
| matrixmultiplication | ✓ | ✓ | 23s | 4 | 16976 / 22644 | 64 | PASS | PASS | true | small WF count (256³ tiled) |
| nbody | ✓ | ✓ | 2s | 2 | 0 / 269 | 4 | PASS | PASS | home-node | negligible L2 traffic; tiny problem |
| fir | ✓ | ✓ | 15s | 2 | 255 / 8224 | 1024 | PASS | PASS | true | streaming filter, near-zero L2H |
| aes | ✓ | ✓ | 9s | 3 | 1088 / 1184 | 64 | PASS | PASS | true | small; per-GPU slices of gInput |
| pagerank | ✓ | ✓ | 19s | 3 | 13550 / 750 | 3072 | PASS | PASS | home-node | sparse graph; high L2H ratio |
| atax | ✓ | ✓ | 49s | 20 | 299004 / 16492 | 16 | PASS | PASS | home-node | 2-kernel; many phases; high L2H |
| bicg | ✓ | ✓ | 48s | 20 | 298945 / 16524 | 16 | PASS | PASS | home-node | similar to atax (BiCG pattern) |
| fft | ✓ | ✓ | 15s | 2 | 16439 / 16635 | 256 | PASS | PASS | home-node | balanced L2H/L2M (50% hit rate) |
| spmv | ✓ | ✓ | 3s | 2 | 3664 / 1514 | 16 | PASS | PASS | home-node | sparse; fast |
| stencil2d | ✓ | ✓ | 13s | 3 | 9437 / 31867 | 248 | PASS | PASS | home-node | 2D stencil; low L2H (streaming) |
| fastwalshtransform | ✓ | TIMEOUT | >600s | — | — | — | — | — | — | Length=65536 too large |
| floydwarshall | ✓ | excluded | — | — | — | — | — | — | — | excluded by user (O(N³) too slow) |
| kmeans | ✓ | TIMEOUT | >600s | — | — | — | — | — | — | iterative; 4×4096×32 too slow |
| nw | ✓ | FAIL | 2s | — | — | — | — | — | — | `panic: nw does not support multi-GPU mode` |
| bfs | ✓ | FAIL | 1s | — | — | — | — | — | — | `panic: BFS does not support multi-GPU execution yet.` |

**Verified (all sanity PASS):** 13 workloads
**Failed:** 2 (nw, bfs — explicit multi-GPU exclusion in SelectGPU)
**Timeout/Excluded:** 3 (fastwalshtransform, floydwarshall, kmeans)

## Per-GPU classification method

**Method used:** Indirect aggregate comparison + code-level dispatch analysis.

Direct per-GPU L2 access breakdown is not available in the current
`PhaseMetrics` infrastructure (metrics are aggregated across all 4 GPU
adapters). Two proxies were used instead:

1. **Code analysis** — does the workload call `EnqueueLaunchKernel(queue_i, ...,
   numWI/numGPUs, ...)` (true multi-GPU) or `LaunchKernel(context, ...)` without
   per-GPU work splitting (home-node)?

2. **Indirect aggregate comparison** — for simpleconvolution (the one workload
   with a single-GPU baseline from PHASE C): 4-GPU total L2H+L2M = 90528 vs
   single-GPU 83970 → ratio 1.08×. At true multi-GPU the ratio would be ≈1.0×
   (same total computation split across GPUs) or slightly above (coherence
   overhead). This is consistent with both home-node and true multi-GPU;
   D.0's explicit "home-node" label for simpleconvolution is retained.

**Classification rules:**
- `true`: `EnqueueLaunchKernel` on per-GPU queues with `numWI / numGPUs` work items
  → matrixtranspose, bitonicsort, matrixmultiplication, fir, aes
- `home-node`: `LaunchKernel(context, ...)` without per-GPU splitting (uses
  context's selected GPU[2]) → nbody, pagerank, atax, bicg, fft, spmv, stencil2d
- `home-node` (by D.0 observation + indirect metric): simpleconvolution

**Limitation:** Without per-GPU L2 access counts, "true" workloads whose data is
distributed via `driver.Distribute()` cannot be distinguished from those with
fully independent per-GPU data. The sharer-consistency metric in PHASE D.2 will
provide the empirical evidence.

## RetiredWavefronts calibration (conventions §3.5 C.5)

| Workload | Single-GPU theoretical | 4-GPU observed | Offset |
|---|---|---|---|
| simpleconvolution | 4129 (`ceil(514²/64)`) | 4132 | +3 |
| matrixtranspose | 256 (`512²/(4²×64)`) | 256 | **0** |
| bitonicsort | not pre-calculated | 69632 | reference |
| matrixmultiplication | not pre-calculated | 64 | reference |
| nbody | not pre-calculated | 4 | reference |
| fir | not pre-calculated | 1024 | reference |
| aes | not pre-calculated | 64 | reference |
| pagerank | not pre-calculated | 3072 | reference |
| atax | not pre-calculated | 16 | reference |
| bicg | not pre-calculated | 16 | reference |
| fft | not pre-calculated | 256 | reference |
| spmv | not pre-calculated | 16 | reference |
| stencil2d | not pre-calculated | 248 | reference |

**Finding:** The +3 offset does NOT hold universally. matrixtranspose (true
multi-GPU, even distribution) shows offset=0. simpleconvolution (home-node)
shows +3. Per conventions §3.5: "offset varies per workload → use per-workload
observed value as reference." PHASE D.2 will validate each workload against its
own D.1 reference RetiredWf value.

## Sanity checks summary

All 13 verified workloads satisfy:
- V11 (DirectoryEvictions=0): PASS — `InfiniteCapacity=true` holds
- V12 (accessed ≤ fetched): PASS — no violations
- warningCount (β extended): 0 for all (auto-register branch active since PR #4)
