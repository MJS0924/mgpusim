# Candidate Metric Exploration

Generated: 2026-04-20 (Tier B)
Data: results/m1/d2/raw/ (90 parquets, 255 phase rows)
Scripts: analysis/explore/candidate_metrics.py

---

## Background

Current metrics fail propositions because:
- L2HR: completely R-invariant (cache uses 64B lines, not M1 regions)
- SCR: saturated at 1.0 for all rows (InfiniteCapacity=true)
- Utilization: monotone ↓ with R → always prefers R=64

Five candidate metrics were computed to identify signals that vary with R
at phase granularity.

---

## M1a: Active Regions Ratio (AR_R / AR_R64)

Definition: `active_regions(R) / active_regions(R=64)` for each phase.

| Region size | Mean M1a |
|-------------|----------|
| 64 | 1.0000 |
| 256 | 0.2749 |
| 1024 | 0.0968 |
| 4096 | 0.0492 |
| 16384 | 0.0202 |

**Monotone ↓ with R** — large R always produces fewer (larger) regions.
At R=16384, typically only 1–4% of R=64's region count remains.

**Key finding — M1a differs between phases (matrixmultiplication):**

| R | Phase 0 | Phase 1 | Phase 2 |
|---|---------|---------|---------|
| 64 | 1.000 | 1.000 | 1.000 |
| 256 | 0.435 | 0.252 | 0.250 |
| 1024 | 0.348 | 0.065 | 0.065 |
| 4096 | 0.304 | 0.017 | 0.017 |
| 16384 | **0.141** | **0.005** | **0.005** |

Phase 0 retains 14% of regions at R=16384 vs ~0.5% for phases 1 and 2.
This means phase 0 has poor spatial locality (scattered accesses) — AR does not
collapse with R. Phases 1 and 2 have strong spatial coherence.

Implication: M1a *slope* across R is a genuine phase differentiator.
Phase 0 has slow M1a decay → poor coalescing → prefers small R.
Phases 1,2 have fast M1a decay → good coalescing → can afford large R.

**Other workloads**: only 1 active phase each → no cross-phase comparison possible.

**M1a as optimal-R criterion**: since M1a is monotone ↓, argmax(M1a) = R=64 always.
M1a is useful as a *feature*, not as an optimisation objective.

---

## M2a: Fetched Bytes Standard Deviation Across R (per phase)

Definition: std(region_fetched_bytes over R values) per (workload, seed, phase_index).

| Stat | Value |
|------|-------|
| mean | 31,276 bytes |
| std | 32,913 bytes |
| min | 0 |
| max | 88,558 bytes |

This is a **phase-level scalar** — same value for all R within a phase.
High M2a means fetched bytes vary a lot across R (high R-sensitivity).
Not useful for choosing optimal R, but useful for characterising which phases
are most affected by R choice.

Phases with M2a=0: those where fetched_bytes is flat across R (inactive phases).

---

## M3a: Utilization Standard Deviation Across R (per phase)

Definition: std(region_accessed_bytes / region_fetched_bytes over R values) per phase.

| Stat | Value |
|------|-------|
| mean | 0.0245 |
| std | 0.0349 |
| min | 0.0 |
| max | 0.1089 |

Also a **phase-level scalar**. Highest M3a = 0.109 for the most R-sensitive
phases (those with large fetched-bytes overhead at large R).

---

## M4a: Consecutive-Phase Active Regions Overlap

Definition: min(AR_i, AR_{i+1}) / max(AR_i, AR_{i+1}) for adjacent phase pairs
at the same (workload, R, seed). A value near 1.0 means working sets barely
change between phases; near 0 means complete phase transition.

| Stat | Value |
|------|-------|
| count | 210 |
| mean | 0.399 |
| std | 0.459 |
| min | 0.000 |
| max | 1.000 |

High variance (std=0.459) indicates M4a is informative about phase transitions.
A high M4a(R) at large R indicates phases share the same coarse-grained working set
even if their fine-grained R=64 sets differ.

**Use case**: identifying whether phase-adjacent R recommendations should be
stable (smooth transitions preferred) or independent.

---

## M5a: Wavefront Density per Region (retired_wavefronts / active_regions)

Definition: `retired_wavefronts / active_regions` — compute work done per active region.

| Region size | Workload | Phase | M5a |
|-------------|----------|-------|-----|
| 64 | pagerank | 1 | 1.42 |
| 256 | pagerank | 1 | 5.60 |
| 1024 | pagerank | 1 | 20.90 |
| 4096 | pagerank | 1 | 68.27 |
| 16384 | pagerank | 1 | 227.56 |

**Monotone ↑ with R** (because wavefronts is constant, AR decreases).
At large R, each region encompasses more computation.

**Phase differentiation (pagerank, both phases):**

| R | Phase 1 M5a | Phase 2 M5a | Ratio 1/2 |
|---|-------------|-------------|-----------|
| 64 | 1.422 | 0.789 | 1.80 |
| 16384 | 227.6 | 102.4 | 2.22 |

The phase ratio is roughly preserved across R, but the absolute separation
at large R is dramatic (factor 2.2). This means M5a slope (log-log) encodes
a genuine per-phase characteristic (total wavefronts / total accesses at R64).

**M5a slope as optimal-R surrogate**: since M5a is monotone, argmax = R=16384.
But the absolute threshold M5a > γ (e.g., γ=10 wavefronts/region) at the smallest
R where it's satisfied gives a per-phase optimal R.

---

## Summary Table

| Metric | Varies with R? | Phase-differentiated? | Monotone? | Useful for optimal-R? |
|--------|---------------|----------------------|-----------|----------------------|
| L2HR | No (invariant) | No | N/A | No |
| SCR | No (saturated) | No | N/A | No |
| util | Yes | Weakly | ↓ (always R=64) | No (trivial) |
| **M1a** | Yes | **Yes** (matmul) | ↓ (always R=64) | Feature only |
| M2a | N/A (scalar) | Yes | N/A | Phase sensitivity |
| M3a | N/A (scalar) | Yes | N/A | Phase sensitivity |
| M4a | Yes | Yes | Non-monotone | Phase transitions |
| **M5a** | Yes | **Yes** (pagerank) | ↑ (always R=16384) | With threshold |

---

## Prototype Design Directions

No single metric is directly "optimal" — all are monotone or phase-level scalars.
But 5 prototype directions can be constructed:

1. **Option 1 (Fetch-overhead budget)**: Optimal R = largest R where
   `(fetched - accessed) / accessed < α`. Phase-specific because accessed varies.
   → Creates per-phase cutoffs; rich workloads (low fetched overhead) prefer larger R.

2. **Option 2 (AR-reduction efficiency)**: Metric = `(AR_64 - AR_R) / (fetched_R - accessed)`.
   AR saved per wasted byte. May be non-monotone (numerator decelerates, denominator
   accelerates with R) → could produce genuine per-phase optimal R.

3. **Option 3 (M1a decay slope)**: Classify phases by M1a curve shape (how fast AR
   collapses with R). Slow-decay phases → poor coalescing → prefer small R.
   Fast-decay phases → good coalescing → can tolerate large R.
   → A clustering/threshold approach over M1a@R=16384 value.

4. **Option 4 (M5a density threshold)**: Optimal R = smallest R where
   M5a(R) > γ (e.g., γ = 5). Since M5a ↑ with R, this gives a per-phase optimal R
   based on compute density requirements. Phases with high total wavefronts hit
   threshold at smaller R.

5. **Option 5 (Composite cost model)**: Weighted combination:
   `score(R) = w1 * (-util_loss(R)) + w2 * (-AR_overhead(R))` where
   util_loss = 1 - util(R) and AR_overhead = AR_R / AR_64.
   Find optimal R by argmax score(R). Weights w1, w2 are design parameters.

See `proto_option1.py` through `proto_option5.py` for prototype verifications.
