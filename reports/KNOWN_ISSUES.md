# Known Issues — Residual Non-determinism

Last updated: 2026-04-18  
MGPUSim commit: `4277061dd690f72c633d5e7fc392bb7690e8ede0`

---

## RESOLVED

### [FIXED] PageRank graph generation non-determinism
- **File**: `amd/benchmarks/matrix/csr/matrixgenerator.go:16`
- **Root cause**: `rand.New(rand.NewSource(123))` return value was discarded; global `rand.*` (auto-seeded in Go 1.20+) was used for `rand.Float32()` / `rand.Int()`.
- **Fix**: Added `rng *rand.Rand` field; `MakeMatrixGenerator` now accepts `seed int64`; all rand calls use `g.rng.*`.
- **Verified**: Same seed → byte-identical log, identical kernel_time across 3 runs.

---

## OPEN

### [OPEN-1] Log output ordering non-determinism (cosmetic only)
- **File**: `amd/samples/runner/runner.go:163`
- **Description**: A goroutine is used for benchmark execution. The monitoring HTTP server startup message (`Monitoring simulation with http://localhost:<port>`) prints on a random port each run. Log lines from parallel goroutine initialization may appear in different order.
- **Impact on metrics**: **None.** MGPUSim uses a single-threaded event-driven simulation engine; the goroutine only wraps benchmark lifecycle, not simulation events. Cycle counts, cache metrics, and cohDir metrics are deterministic given the same seed.
- **Workaround**: When doing byte-level log comparison, filter the `Monitoring simulation` line: `grep -v "localhost\|Monitoring"`.

### [OPEN-2] spmv benchmark — vec[] initialization still uses global rand
- **File**: `amd/benchmarks/shoc/spmv/spmv.go:111`
- **Description**: The vector `b.vec[]` is initialized with `rand.Float32() * b.maxval` using the global rand, even after the matrix generator fix. `RandSeed` field was added to the struct but vec[] init was not changed.
- **Impact on pagerank**: **None** (pagerank does not use spmv).
- **Recommended fix**: Replace `rand.Float32()` in spmv.go's `initMem()` with a local seeded RNG using `b.RandSeed`.

### [OPEN-3] SQLite database filename contains random timestamp token
- **File**: MGPUSim akita reporter (upstream)
- **Description**: Output SQLite files are named `akita_sim_<random_token>.sqlite3`. The token changes every run, making scripted post-processing require glob patterns.
- **Impact on metrics**: **None.**
- **Workaround**: `mv akita_sim_*.sqlite3 <desired_name>.sqlite3` in run scripts (already done in REC/HMG/superdirectory run scripts).
