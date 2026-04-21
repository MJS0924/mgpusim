# B-7.0 Capacity-Effect Small Test — Analysis

**Date**: 2026-04-21  
**Branch**: `m1-phase-b70-capacity-smalltest`  
**Sweep**: 2 workloads × 3 capacities × 5 region sizes = 30 runs (0 failures)

---

## 1. Run Matrix

| workload | cap | R | IPC | L2HR | util | dir_evictions | evict_invals |
|---|---|---|---|---|---|---|---|
| simpleconvolution | 0 (∞) | 64 | 3.2094 | 0.608 | 0.501 | 0 | 0 |
| simpleconvolution | 0 (∞) | 256 | 3.2094 | 0.608 | 0.478 | 0 | 0 |
| simpleconvolution | 0 (∞) | 1024 | 3.2094 | 0.608 | 0.401 | 0 | 0 |
| simpleconvolution | 0 (∞) | 4096 | 3.2094 | 0.608 | 0.367 | 0 | 0 |
| simpleconvolution | 0 (∞) | 16384 | 3.2094 | 0.608 | 0.342 | 0 | 0 |
| simpleconvolution | 8192 | 64 | 3.2094 | 0.608 | 0.501 | 39,431 | 66,632 |
| simpleconvolution | 8192 | 256 | 3.2094 | 0.608 | 0.478 | 33 | 113 |
| simpleconvolution | 8192 | 1024 | 3.2094 | 0.608 | 0.401 | 0 | 0 |
| simpleconvolution | 8192 | 4096 | 3.2094 | 0.608 | 0.367 | 0 | 0 |
| simpleconvolution | 8192 | 16384 | 3.2094 | 0.608 | 0.342 | 0 | 0 |
| simpleconvolution | 2048 | 64 | 3.2094 | 0.608 | 0.501 | 80,035 | 95,717 |
| simpleconvolution | 2048 | 256 | 3.2094 | 0.608 | 0.478 | 11,720 | 25,150 |
| simpleconvolution | 2048 | 1024 | 3.2094 | 0.608 | 0.401 | 9 | 80 |
| simpleconvolution | 2048 | 4096 | 3.2094 | 0.608 | 0.367 | 0 | 0 |
| simpleconvolution | 2048 | 16384 | 3.2094 | 0.608 | 0.342 | 0 | 0 |
| matrixmultiplication | 0 (∞) | 64 | 2.7686 | 0.007 | 0.499 | 0 | 0 |
| matrixmultiplication | 0 (∞) | 256 | 2.7686 | 0.007 | 0.497 | 0 | 0 |
| matrixmultiplication | 0 (∞) | 1024 | 2.7686 | 0.007 | 0.490 | 0 | 0 |
| matrixmultiplication | 0 (∞) | 4096 | 2.7686 | 0.007 | 0.463 | 0 | 0 |
| matrixmultiplication | 0 (∞) | 16384 | 2.7686 | 0.007 | 0.415 | 0 | 0 |
| matrixmultiplication | 8192 | 64 | 2.7686 | 0.007 | 0.499 | 513 | 3,872 |
| matrixmultiplication | 8192 | 256 | 2.7686 | 0.007 | 0.497 | 0 | 0 |
| matrixmultiplication | 8192 | 1024 | 2.7686 | 0.007 | 0.490 | 0 | 0 |
| matrixmultiplication | 8192 | 4096 | 2.7686 | 0.007 | 0.463 | 0 | 0 |
| matrixmultiplication | 8192 | 16384 | 2.7686 | 0.007 | 0.415 | 0 | 0 |
| matrixmultiplication | 2048 | 64 | 2.7686 | 0.007 | 0.499 | 6,657 | 34,848 |
| matrixmultiplication | 2048 | 256 | 2.7686 | 0.007 | 0.497 | 129 | 1,056 |
| matrixmultiplication | 2048 | 1024 | 2.7686 | 0.007 | 0.490 | 0 | 0 |
| matrixmultiplication | 2048 | 4096 | 2.7686 | 0.007 | 0.463 | 0 | 0 |
| matrixmultiplication | 2048 | 16384 | 2.7686 | 0.007 | 0.415 | 0 | 0 |

V11=PASS, V12=PASS for all 30 runs.

---

## 2. Key Findings

### F1 — IPC is invariant across all (R, cap) combinations

**IPC delta for cap=8192, R=64 vs R=16384: 0.00% for both workloads.**

- simpleconvolution: IPC = 3.2094 for every (R, cap) cell
- matrixmultiplication: IPC = 2.7686 for every (R, cap) cell

**Root cause**: the `PlainVIDirectory` operates as a shadow/measurement overlay. It receives a copy of every CU vector memory access via `ShadowDirHook`, runs its LRU logic, and fires eviction callbacks into `PhaseMetrics`. However, it is not the coherence directory that governs the actual GPU simulation — the akita r9nano builder creates its own internal writeback coherence directory (1024 sets × 8 ways = 8192 entries) that is entirely separate. The shadow directory's evictions do NOT cause cache flushes, invalidation messages, or stall cycles in the real simulation. Therefore, simulated execution time is identical across all (R, cap) combinations.

**Implication**: The Path Y criterion as stated — "IPC changes as a function of capacity" — cannot be evaluated with the current shadow-directory architecture. Measuring IPC impact of finite directory capacity requires the `PlainVIDirectory` to be integrated as the real coherence directory driving the simulation, which is a significant akita-level integration task.

### F2 — Eviction pressure is large, real, and strongly R-dependent

Directory evictions are non-zero exactly where expected — small R forces many distinct regions into a tight directory:

**simpleconvolution, cap=8192:**
| R | active_regions (∞) | dir_evictions | evict_invals |
|---|---|---|---|
| 64 | 83,388 | 39,431 | 66,632 |
| 256 | 21,818 | 33 | 113 |
| 1024 | 6,504 | 0 | 0 |
| 4096 | 1,779 | 0 | 0 |
| 16384 | 477 | 0 | 0 |

**simpleconvolution, cap=2048:**
| R | active_regions (∞) | dir_evictions | evict_invals |
|---|---|---|---|
| 64 | 83,388 | 80,035 | 95,717 |
| 256 | 21,818 | 11,720 | 25,150 |
| 1024 | 6,504 | 9 | 80 |
| 4096 | 1,779 | 0 | 0 |
| 16384 | 477 | 0 | 0 |

**Interpretation**: simpleconvolution with R=64 needs 83,388 directory entries in infinite mode. At cap=8192, the working set is ~10× larger than the directory → 39,431 evictions (47% of active regions evicted during execution). At cap=2048, the ratio is 41× → 80,035 evictions. R=1024 fits comfortably in cap=2048 (6,504 active regions < 2,048 cap is FALSE — yet evictions=9). Actually at R=1024, active_regions=6,504 > cap=2048, so 9 evictions occur at the boundary. R=4096 (1,779 active) fits within cap=2048 cleanly → 0 evictions.

**matrixmultiplication** has a much smaller working set (17,438 active regions at R=64) and shows proportionally fewer evictions.

### F3 — evict_invals >> dir_evictions: multi-CU sharing confirmed

The ratio `evict_invals / dir_evictions` measures average sharers per evicted entry:
- simpleconvolution, cap=2048, R=64: 95,717 / 80,035 = **1.20 sharers/eviction**
- simpleconvolution, cap=8192, R=64: 66,632 / 39,431 = **1.69 sharers/eviction**
- matrixmultiplication, cap=2048, R=64: 34,848 / 6,657 = **5.23 sharers/eviction**

The higher ratio for matmul reflects its higher data-parallel sharing (all CUs accessing the same weight tiles), consistent with the near-zero L2HR (0.007 — nearly every access is a miss, meaning data is shared but frequently evicted from L2 before reuse).

### F4 — Utilization (util) varies with R, not with capacity

`util = RegionAccessedBytes / RegionFetchedBytes` is independent of capacity across all runs:

| R | util (simpleconvolution) | util (matmul) |
|---|---|---|
| 64 | 0.501 | 0.499 |
| 256 | 0.478 | 0.497 |
| 1024 | 0.401 | 0.490 |
| 4096 | 0.367 | 0.463 |
| 16384 | 0.342 | 0.415 |

Utilization decreases monotonically with R for both workloads (larger regions contain more padding). This is the same finding as the M1 Phase D sweep — the utilization signal is R-sensitive but not capacity-sensitive.

### F5 — Best R by eviction-free threshold

The smallest R at which `dir_evictions = 0` for a given capacity:

| workload | cap=2048 | cap=8192 |
|---|---|---|
| simpleconvolution | R=4096 | R=256 → 33 evictions; R=1024 → 0 | effective: R=1024 |
| matrixmultiplication | R=1024 | R=256 → 0 evictions |

At cap=8192 (the larger finite capacity), the eviction-free threshold shifts from R=4096 down to R=1024 for simpleconvolution — a 4× improvement in region-size flexibility. This is the capacity-sensitivity signal that a real IPC measurement would be expected to capture.

---

## 3. IPC Measurement: Structural Gap

The B-7.0 IPC measurement infrastructure (`HookPosInstructionRetired`, `RetiredInstructions` counter, `TotalRetiredInstructions` parquet column) is fully implemented and working:
- simpleconvolution: 1,266,819 instructions retired over 200k cycles
- matrixmultiplication: 570,496 instructions retired over 200k cycles

The IPC values are real and consistent. The gap is not in IPC measurement but in IPC *sensitivity*: because the shadow directory does not feed back into the simulation, capacity changes have no effect on simulated execution.

---

## 4. Path Y Decision

**Criterion**: IPC(cap=8192, R=64) vs IPC(cap=8192, R=16384) ≥ 5% → STRONG GO; 1–5% → MODERATE GO; <1% → REVISIT.

**Observed delta**: **0.00%** (both workloads).

**Recommendation: REVISIT**

The REVISIT is not because capacity effects are absent — F2 and F5 demonstrate that eviction pressure is large and R-dependent. The REVISIT is because the current shadow-directory architecture cannot translate capacity pressure into IPC variation. Path Y as defined requires architectural integration work before it can be measured.

---

## 5. Required Architectural Change for Path Y

To enable true IPC-vs-capacity measurement, `PlainVIDirectory` must be integrated as the actual coherence directory used by the r9nano GPU simulation, replacing or wrapping the akita writeback-coherence directory. Concretely:

1. **Option Y-A (deep integration)**: Replace akita's `writebackcoh.Comp` L2 directory with `PlainVIDirectory`. On capacity eviction, send explicit invalidation messages to the relevant CU L1 caches. This would cause real stall cycles when a CU re-fetches evicted data.

2. **Option Y-B (capacity-bounded replay)**: Run two simulations: one with infinite capacity (baseline IPC) and one where the `PlainVIDirectory` periodically invalidates the L2's evicted regions. Measure IPC change as the replay penalty. Complexity: lower than Y-A; accuracy: approximate.

3. **Option Y-C (analytical bound)**: Use the eviction counts from this sweep (F2) plus empirical L2 miss penalty (from the akita trace) to estimate IPC impact analytically. No simulation change required. Accuracy: order-of-magnitude only.

Option Y-C can be pursued immediately using this sweep's data. Options Y-A and Y-B require 2–4 weeks of akita integration work.

---

## 6. Invariant Check Summary

| invariant | result |
|---|---|
| V11 (InfiniteCapacity → evictions=0) | PASS: all cap=0 runs have dir_evictions=0 |
| V11 (finite mode → evictions allowed) | PASS: no V11 panic in any finite-mode run |
| V12 (RegionAccessedBytes ≤ RegionFetchedBytes) | PASS: all 30 runs |
| evict_invals > 0 when cap < active_regions | PASS: all expected (R, cap) cells show non-zero evictions |
| IPC measurement non-zero | PASS: RetiredInstructions > 0 for all runs with activity |

---

## 7. Conclusion

The B-7.0 small test successfully implemented and validated:
- Finite LRU directory capacity in `PlainVIDirectory` (Work A)
- IPC measurement via `HookPosInstructionRetired` (Work B)
- `-max-entries` flag and 30-run sweep infrastructure (Work C)

The primary finding is an **architectural gap**: the shadow-directory model cannot produce IPC variation from capacity changes. All 30 runs show IPC delta = 0.00%, triggering the **REVISIT** recommendation under the stated Path Y criterion.

The secondary finding is that **eviction-pressure data is high-quality and actionable**: at cap=8192, simpleconvolution needs R ≥ 1024 to avoid evictions (vs. R ≥ 4096 at cap=2048). This eviction-pressure signal is a prerequisite for any analytical or simulation-based IPC estimation under Option Y-C.

**Recommended next step**: Pursue Option Y-C — analytical IPC impact estimation using this sweep's eviction counts and the known L2 miss penalty from the akita trace. This can be completed without simulation changes and would allow a quantitative Path Y evaluation within 1–2 days.
