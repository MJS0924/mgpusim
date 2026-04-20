# PHASE D.1 — Workload Candidates

**Date:** 2026-04-20
**Branch:** m1-phase-d1-workload-verify

All benchmarks listed below were confirmed to exist and build successfully
(`go build ./amd/benchmarks/amdappsdk/... ./amd/benchmarks/heteromark/...
./amd/benchmarks/polybench/... ./amd/benchmarks/rodinia/...
./amd/benchmarks/shoc/...`). No build errors.

## Candidate table

| Workload | Path | Build | Access Pattern (expected) | Key Size Parameters |
|---|---|---|---|---|
| simpleconvolution | `amd/benchmarks/amdappsdk/simpleconvolution` | ✓ | Regular stencil (conv 2D) | `Width`, `Height`, `SetMaskSize(uint32)` |
| matrixtranspose | `amd/benchmarks/amdappsdk/matrixtranspose` | ✓ | Strided transpose | `Width int` |
| bitonicsort | `amd/benchmarks/amdappsdk/bitonicsort` | ✓ | Irregular sort passes | `Length int`, `OrderAscending bool` |
| matrixmultiplication | `amd/benchmarks/amdappsdk/matrixmultiplication` | ✓ | Dense GEMM | `X, Y, Z uint32` |
| nbody | `amd/benchmarks/amdappsdk/nbody` | ✓ | All-pairs dense compute | `NumParticles int32`, `NumIterations int32` |
| fastwalshtransform | `amd/benchmarks/amdappsdk/fastwalshtransform` | ✓ | Butterfly streaming | `Length uint32` |
| floydwarshall | `amd/benchmarks/amdappsdk/floydwarshall` | ✓ | Dense O(N³) | `NumNodes uint32`, `NumIterations uint32` |
| fir | `amd/benchmarks/heteromark/fir` | ✓ | Streaming filter | `Length int` (numTaps=16, fixed) |
| aes | `amd/benchmarks/heteromark/aes` | ✓ | Streaming cipher | `Length int` (bytes, must be multiple of 16) |
| kmeans | `amd/benchmarks/heteromark/kmeans` | ✓ | Iterative clustering | `NumClusters int`, `NumPoints int`, `NumFeatures int`, `MaxIter int` |
| pagerank | `amd/benchmarks/heteromark/pagerank` | ✓ | Irregular sparse graph | `NumNodes uint32`, `NumConnections uint32`, `MaxIterations uint32`, `RandSeed int64` |
| atax | `amd/benchmarks/polybench/atax` | ✓ | Matrix-vector (2 kernels) | `NX, NY int` |
| bicg | `amd/benchmarks/polybench/bicg` | ✓ | BiCG stencil (2 kernels) | `NX, NY int` |
| nw | `amd/benchmarks/rodinia/nw` | ✓ | Dynamic prog. (2 kernels) | `SetLength(int)`, `SetPenalty(int)` |
| bfs | `amd/benchmarks/shoc/bfs` | ✓ | Irregular graph traversal | `NumNode int`, `Degree int`, `MaxDepth int` (or `Path string` for file) |
| fft | `amd/benchmarks/shoc/fft` | ✓ | FFT streaming | `Bytes int32` (MB before ×1024²), `Passes int32` |
| spmv | `amd/benchmarks/shoc/spmv` | ✓ | Sparse matrix-vector | `Dim int32`, `Sparsity float64`, `RandSeed int64` |
| stencil2d | `amd/benchmarks/shoc/stencil2d` | ✓ | 2D stencil (halo) | `NumRows, NumCols int`, `NumIteration int` |

## Notes

- **dnn/** benchmarks excluded: require missing dataset packages (mnist, imagenet)
  and are out of M1 scope.
- All 18 candidates are from the four groups specified in the task spec.
- `fir.numTaps` is unexported and fixed to 16 in `Run()`; only `Length` is
  user-settable.
- `nw` uses `SetLength()` / `SetPenalty()` accessors, not exported struct fields.
- `fft.Bytes` is in MB before the benchmark multiplies by 1024×1024.
- `bfs` can use a synthetic graph (`NumNode + Degree`) or a file (`Path`);
  M1 runs will use synthetic.
- `floydwarshall`: O(N³) kernel — keep `NumNodes` small (≤ 256) to stay
  under 5-minute budget.
