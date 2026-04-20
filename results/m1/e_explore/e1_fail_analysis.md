# E.1 Failure Analysis: P1 / P2 / P3

Generated: 2026-04-20 (Tier A)
Data: results/m1/d2/raw/ (90 parquet files, 255 phase rows)

---

## Summary

All three propositions (P1, P2, P3) failed for the same structural reason:
**the current metrics do not vary with region size R at the per-phase level.**
As a result, "optimal R per phase" is undefined — every phase produces a
three-way tie that gets broken arbitrarily by `idxmax()` (returns R=1024 by
DataFrame sort order for fir/stencil2d/etc.).

---

## P1 Failure: L2HR R-Invariance

**P1 goal**: Distribution of per-phase optimal-R (by L2HR) has Shannon entropy > 0.5 nats.
**Result**: entropy = 0.0 (all phases pick R=1024 as tie-break; no real variation).

**Root cause**: L2 cache uses 64-byte cache lines, independent of the M1 region
size parameter. `l2_hits` and `l2_misses` are identical across all 5 R values
for every phase row.

| Metric | Value |
|--------|-------|
| L2HR std across R (per phase), mean | 0.000000 |
| L2HR std across R (per phase), max | 0.000000 |
| Phases where L2HR range < 0.01 | 27 / 27 (100%) |
| L2HR mean per R=64 | 0.460993 |
| L2HR mean per R=16384 | 0.460993 |

**Workload-level L2HR** (varies across workloads but not across R):
| Workload | Mean L2HR |
|----------|-----------|
| pagerank | 0.964 |
| simpleconvolution | 0.629 |
| matrixmultiplication | 0.445 |
| stencil2d | 0.228 |
| fir | 0.030 |
| matrixtranspose | 0.000 |

**Data**: `results/m1/e_explore/p1_raw.csv` (255 rows; columns: workload, region_size, seed, phase_index, l2_hits, l2_misses, l2hr)

---

## P2 Failure: 3-Metric Agreement

**P2 goal**: L2HR, utilization, and SCR agree on optimal-R in ≥ 70% of active phases.
**Result**: agreement = 0 / 27 active phase-groups (0.0%).

**Root cause (three independent causes):**

1. **L2HR invariant**: std = 0.0 across R for every phase → tie-broken to R=1024.
2. **SCR saturated**: sharer_consistent_regions = active_regions for all 135 active rows → SCR = 1.0 everywhere → tie-broken to R=1024.
3. **Utilization monotone ↓**: region_accessed_bytes is constant across R (same physical bytes accessed); region_fetched_bytes increases with R → util monotonically decreases → always prefers R=64.

Because L2HR→R=1024 and util→R=64 always disagree, agreement rate is structurally 0%.

**SCR saturation evidence**:
| Stat | Value |
|------|-------|
| SCR count (active rows) | 135 |
| SCR mean | 1.0 |
| SCR std | 0.0 |
| SCR == 1.0 | 135 / 135 (100%) |

**Utilization per region size** (monotone decrease confirmed):
| R | util mean |
|---|-----------|
| 64 | 0.4715 |
| 256 | 0.4527 |
| 1024 | 0.4319 |
| 4096 | 0.4079 |
| 16384 | 0.3593 |

**Optimal R distribution per metric** (across all 27 active phase-groups):
| Metric | Always prefers |
|--------|---------------|
| L2HR | R=1024 (tie-break) |
| Utilization | R=64 (monotone minimum fetched overhead) |
| SCR | R=1024 (tie-break) |

**Data**: `results/m1/e_explore/p2_raw.csv` (255 rows; adds l2hr, util, scr columns)

---

## P3 Failure: Phase Mode Bound

**P3 goal**: In ≥ 3 workloads, the modal optimal-R accounts for ≤ 60% of phases.
**Result**: 0 / 6 workloads pass. Mode fraction = 1.0 for all 18 (workload, seed) pairs.

**Root cause**: Direct consequence of P1 failure. Since L2HR is R-invariant, every
phase's optimal-R is the same tie-break value (R=1024). The modal R = 1024 with
fraction = 1.0 = 100% for every workload.

**Mode fraction per (workload, seed)** — all = 1.0:
| Workload | Active phases (per seed) | Modal R | Mode fraction |
|----------|--------------------------|---------|---------------|
| fir | 1 | 1024 | 1.00 |
| matrixmultiplication | 3 | 1024 | 1.00 |
| matrixtranspose | 1 | 1024 | 1.00 |
| pagerank | 2 | 1024 | 1.00 |
| simpleconvolution | 1 | 1024 | 1.00 |
| stencil2d | 1 | 1024 | 1.00 |

Note: "Active phases" = phases where (l2_hits + l2_misses) > 0.
fir has 2 declared phases but only 1 active (phase 0 has zero L2 traffic).

**Data**: `results/m1/e_explore/p3_raw.csv` (255 rows; adds optimal_r column)

---

## Structural Diagnosis

The three failures share a single root cause tree:

```
L2 cache architecture (64B lines, independent of M1 region size R)
  └── L2HR is R-invariant (std=0 across R for every phase)
        ├── P1 FAIL: optimal-R entropy = 0 (all ties → one arbitrary R)
        ├── P2 FAIL: L2HR metric contributes nothing + SCR saturated
        │     └── SCR saturation: InfiniteCapacity=true → no evictions →
        │           sharer_consistent_regions = active_regions always
        └── P3 FAIL: mode_fraction = 1.0 everywhere (all phases → same R)

Utilization (region_accessed/region_fetched) is monotone ↓ with R
  └── P2 FAIL: util always prefers R=64, L2HR prefers R=1024 → zero agreement
```

**The M1 measurement design correctly captures region-level byte traffic, but
the L2 cache metric (L2HR) is structurally decoupled from the region abstraction.
Any replacement metric must vary with R at the per-phase granularity.**

---

## Promising Signal

`active_regions` (count of M1 regions touched per phase) DOES vary with R
and shows phase-differentiated profiles:
- Decreases with R (fewer larger regions cover the same working set)
- Varies across phases within a workload
- Ratio of max/min across R differs by phase (observed range: 4.7× to 142×)

This makes `active_regions` (or derived ratios) the primary candidate for
redesigned metrics. See `metric_exploration.md` for quantified analysis.
