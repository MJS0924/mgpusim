# M1 Proposition Verification (PHASE E.1)

## Data source
- **Date:** 2026-04-20
- **Branch:** m1-phase-e1-analysis-infra
- **Files:** 90 parquet files from `results/m1/d2/raw/`
- **Rows:** 255 phase rows (6 workloads × 5 region sizes × 3 seeds)
- **Load OK:** Yes

## Proposition results

| P# | Name | Status | Value | Threshold | Note |
|---|---|---|---|---|---|
| P1 | Window entropy | **FAIL** | 0.0000 | 0.5 | Mean entropy across 18 (workload, seed) pairs = 0.0000. Per-workload avg: fir=0.000, matrixmultiplication=0.000, matrixt... |
| P2 | 3-metric agreement | **FAIL** | 0.0000 | 0.7 | 0/27 (workload, seed, phase) triples agree (0.0%). Per-workload: fir=0.000, matrixmultiplication=0.000, matrixtranspose=... |
| P3 | Phase mode bound | **FAIL** | 0 | >= 3 workloads with mode_frac <= 0.6 | 0/6 workloads satisfy mode_frac ≤ 0.6. Mode fracs: fir=1.0, matrixmultiplication=1.0, matrixtranspose=1.0, pagerank=1.0,... |
| P4 | DS mode bound | **SKIP** | - | mode_frac <= 0.6 | DS (data-structure) axis not measured in PHASE D. Deferred to future phase. |
| P5 | Joint entropy reduction | **SKIP** | - | >= 0.15 reduction | Requires DS axis. Deferred — DS axis not measured in PHASE D. |
| P6 | Track A/B agreement | **SKIP** | - | >= 0.8 | Track B (replay) not yet implemented. Deferred. |

## Root cause analysis

### Why P1~P3 all fail

The "optimal R per phase" computation requires a metric that varies with R
and shows different optimal R in different phases. Analysis of the actual data
reveals that none of the three metrics meet this requirement:

| Metric | Varies with R? | R-independent reason |
|---|---|---|
| L2 hit rate | **No** (confirmed across all 90 runs) | L2 cache operates at 64B cache-line granularity, not M1 region granularity. L2H/L2M counts are unchanged regardless of region size. |
| Sharer consistency rate | **No** (SCR = 1.0 for all 255 phase rows) | All active regions are sharer-consistent in every run. Metric is saturated. |
| Region utilization | Yes, but **monotone** ↓ with R | accessed/fetched always decreases with R — R=64 is always optimal. No phase-specific variation. |

**Consequence:** When L2HR and SCR do not vary with R, the `idxmax()` optimal-R
selection is a tie-break artifact (returns R=1024, the first file loaded in
alphabetical order). Utilization always returns R=64. The three metrics never
agree (P2=0%) and entropy is 0 (P1) because L2HR is constant across R.

### active_regions shows phase-differentiated patterns (promising signal)

`active_regions` count DOES vary with R and shows phase-specific profiles:

  matrixmultiplication: active_regions phase-ratio at R=64 = 142.0x, at R=16384 = 4.7x
  pagerank: active_regions phase-ratio at R=64 = 1.1x, at R=16384 = 1.1x

The phase-specific `active_regions` footprint (number of distinct memory regions
accessed per phase) varies differently across phases at different R values.
This suggests a reformulated hypothesis based on region footprint diversity
rather than L2HR-based optimal R could be meaningful.

## PHASE E.2 Recommendation

**REDESIGN** — 3/3 implementable propositions FAIL. Root cause: current metrics (L2HR, SCR) are R-independent; optimal-R determination is not meaningful with current measurement design. Metric redesign required before Phase E.2.

### Required before Phase E.2

1. **Reformulate P1~P3** to use `active_regions` count or a composite metric
   that varies with R in a phase-specific way.
   - P1 (revised): entropy of the `active_regions` size distribution across phases
     at fixed R — does each phase have a distinct "preferred region granularity"?
   - P2 (revised): agreement between optimal-R from utilization and from
     active_regions footprint minimization.
   - P3 (revised): fraction of phases where the optimal R (minimizing fetched
     overhead per accessed byte) differs from the global optimum.

2. **Or add per-region L2 hit tracking** so that L2HR is measured at region
   granularity (hits within a region vs. fetches for that region). This would
   make L2HR genuinely R-dependent.

3. **SCR saturation**: all regions are sharer-consistent in every run.
   Either the workloads chosen have inherently consistent sharing, or the
   metric needs a different formulation to capture meaningful variation.

