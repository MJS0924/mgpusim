# PHASE D Workload Final Selection

**Date:** 2026-04-20  
**Source:** PHASE D.1 verification (workload_verify.md)  
**Selection from:** 13 verified PASS workloads

## Final selection

| Workload | Pattern | Size | Baseline wall-clock | Phase count | Multi-GPU use | Sharer-relevant |
|---|---|---|---|---|---|---|
| simpleconvolution | regular stencil (2D conv) | 512×512 mask=3 | 63s | 3 | home-node | yes |
| matrixtranspose | strided transpose | Width=512 | 10s | 2 | true | yes |
| matrixmultiplication | dense GEMM | X=Y=Z=256 | 23s | 4 | true | yes |
| pagerank | sparse graph (irregular) | 1024 nodes, 4096 edges, 3 iter | 19s | 3 | home-node | yes |
| fir | streaming filter | Length=65536 (numTaps=16) | 15s | 2 | true | yes |
| stencil2d | 2D stencil with halo | 512×512, 1 iter | 13s | 3 | home-node | yes |

**Selected:** 6 workloads  
**Multi-GPU diversity:** 3 true / 3 home-node  
**All sharer-relevant:** yes (conventions §4: true and home-node both qualify)

## Excluded candidates

| Workload | Reason |
|---|---|
| fastwalshtransform | TIMEOUT >600s at Length=65536 |
| floydwarshall | excluded by user (O(N³) compute; expected timeout) |
| kmeans | TIMEOUT >600s at NumPoints=4096 |
| nw | FAIL — `panic: nw does not support multi-GPU mode` |
| bfs | FAIL — `panic: BFS does not support multi-GPU execution yet` |
| nbody | too small: RetiredWf=4, L2M=269 total; insufficient metric signal |
| aes | small workload (L2H+L2M=2272); pattern overlap with fir |
| bitonicsort | wall-clock=228s (3:48); tight for 15 configs × margin |
| atax | pattern similar to matrixmultiplication; home-node quota filled |
| bicg | near-identical to atax; home-node quota filled |
| fft | home-node streaming; pattern overlap with fir |
| spmv | home-node, very fast (3s), very sparse metric signal |

## Selection rationale

**Pattern diversity (6 distinct categories):**
- Regular stencil: simpleconvolution — covers convolutional memory access pattern
- Strided: matrixtranspose — covers stride-N access with no data reuse (L2H=0)
- Dense compute: matrixmultiplication — covers tiled GEMM with high L2 reuse
- Sparse graph: pagerank — covers irregular access (high L2 hit rate from working set)
- Streaming: fir — covers sequential streaming with near-zero L2H
- 2D stencil with halo: stencil2d — covers halo-exchange pattern (different from simpleconvolution: multi-iteration + padding)

**Multi-GPU use diversity:**
- true (3): matrixtranspose, matrixmultiplication, fir — all use `EnqueueLaunchKernel`
  with `numWI / numGPUs` per queue; directory sees inter-GPU traffic
- home-node (3): simpleconvolution, pagerank, stencil2d — kernel launches on
  context's selected GPU[2]; other GPUs may see directory traffic from
  remote pages (Distribute-allocated memory)

**Sharer-relevant subset:** all 6  
Per conventions §4: true and home-node workloads both generate directory traffic.
sharer-consistency metric is meaningful for all 6 (multi-sharer scenarios arise
from remote access in home-node or from distributed data in true multi-GPU).

## Estimated PHASE D.2 wall-clock

| Workload | Baseline (R=1024) | Per-run estimate |
|---|---|---|
| simpleconvolution | 63s | ~60s |
| matrixtranspose | 10s | ~10s |
| matrixmultiplication | 23s | ~23s |
| pagerank | 19s | ~19s |
| fir | 15s | ~15s |
| stencil2d | 13s | ~13s |

- Configs per workload: 5 region sizes (64, 256, 1024, 4096, 16384) × 3 seeds = 15
- Total runs: 6 × 15 = **90 runs**
- Average wall-clock: (63+10+23+19+15+13)/6 ≈ **24s/run**
- Sequential estimate: 90 × 24s = **2160s ≈ 36 min**
- Parallel 4-proc estimate: **≈ 9 min**

Region size has minimal effect on wall-clock (PHASE C showed <5% variation
across R=64..16384 for simpleconvolution). Estimate is conservative.

## RetiredWf reference values (for D.2 internal consistency)

| Workload | 4-GPU reference (D.1, R=1024, seed=42) |
|---|---|
| simpleconvolution | 4132 |
| matrixtranspose | 256 |
| matrixmultiplication | 64 |
| pagerank | 3072 |
| fir | 1024 |
| stencil2d | 248 |

PHASE D.2 validates each workload against its own reference: RetiredWf must be
identical across all 5 region sizes and 3 seeds for the same workload (region
size and seed do not affect the kernel dispatch count).
