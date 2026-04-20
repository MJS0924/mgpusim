# Option Correlation and Metric Relationships

Generated: 2026-04-20 (Tier B++)
Script: analysis/explore/hybrid_candidates.py

---

## O2 vs O5 Optimal-R Agreement

| | O2 prefers R=256 | O2 prefers R=1024 | O2 prefers R=4096 |
|-|-----------------|------------------|------------------|
| O5 prefers R=1024 | 0 | 4 | 0 |
| O5 prefers R=4096 | 3 | 8 | 0 |
| O5 prefers R=16384 | 0 | 0 | 12 |

Agreement count: 7/27 (25.9%) — mainly where both agree on R=1024 (4 cases).

**Interpretation**: The two options are largely **complementary**, not redundant.
O2 avoids very large R (max is 4096); O5 often recommends R=16384.
Combining them at alpha=0.5 achieves higher entropy than either alone.

---

## Raw Metric Spearman Correlations with O2 Efficiency

Computed across all (workload, seed, phase_index, region_size) active rows:

| Metric | Spearman ρ with O2 efficiency | Interpretation |
|--------|-------------------------------|---------------|
| m1a (AR_R / AR_64) | **−0.7247** | Strong negative: high M1a → low efficiency |
| m5a (wavefronts/AR) | +0.3012 | Moderate positive: high compute density → higher efficiency |
| l2hr (L2 hit rate) | −0.2890 | Weak negative: likely workload confound |
| util (accessed/fetched) | +0.0120 | Near-zero: util doesn't drive efficiency |
| active_regions (raw) | −0.0184 | Near-zero: raw AR count not informative |

**m1a is the strongest predictor of O2 efficiency (ρ = −0.725)**.
This makes physical sense:
- High m1a at a given R means many regions still active (poor coalescing)
- High AR means AR_saved = AR_64 − AR_R is SMALL → numerator of efficiency is small
- High AR also means high tracked overhead (large denominator in wasted calculation)
- Together: high m1a → low efficiency → should NOT choose large R

**m5a has moderate positive correlation (ρ = 0.30)**:
- High compute density per region → O2 efficiency higher
- This is because phases with more computation per region benefit more from
  coalescing (each large region does meaningful work)

**l2hr has weak negative correlation (ρ = −0.29)**:
- Likely a workload confound: high-L2HR workloads (pagerank, simpleconvolution) tend
  to be home-node pattern with lower efficiency values at large R
- Not a causal relationship with R-selection

---

## Phase-Level Optimal-R Summary (O2, Best Option)

| Workload | # Active Phases | Optimal R per phase | Inter-phase diversity |
|----------|----------------|---------------------|-----------------------|
| fir | 1 | {1: R=1024} | None (1 phase) |
| matrixmultiplication | 3 | {0: R=256, 1: R=1024, 2: R=4096} | **3 different R values** |
| matrixtranspose | 1 | {1: R=4096} | None |
| pagerank | 2 | {1: R=1024, 2: R=1024} | None (both → R=1024) |
| simpleconvolution | 1 | {2: R=4096} | None |
| stencil2d | 1 | {1: R=4096} | None |

**Only matrixmultiplication demonstrates genuine inter-phase optimal-R diversity.**
Its three phases access memory at fundamentally different granularities:
- Phase 0: initialisation/setup — scattered accesses, small working set per region → R=256
- Phase 1: first computation pass — growing spatial locality → R=1024
- Phase 2: main computation — strong spatial locality, large working set → R=4096

This progression (R=256 → R=1024 → R=4096) aligns with the computation structure
of blocked matrix multiplication.

---

## Key Insight: Metric Decoupling

The current M1 design measures TWO orthogonal aspects:

1. **L2 cache behaviour** (L2HR, SCR): governed by 64B cache line, independent of R
2. **Region tracking** (active_regions, accessed/fetched): governed by M1 region size R

These aspects are structurally independent. The propositions (P1~P3) assumed they
would correlate through a shared "optimal R" concept, but they cannot — one metric
is R-invariant and the other monotone.

**The redesigned metrics (O2, O5, hybrid) operate entirely within the region-tracking
layer**. They do not attempt to combine L2HR with region metrics, which was the
original design flaw.

A viable M1 redesign should:
1. Drop L2HR and SCR as proposal-verification metrics (they remain useful as workload
   characterisation metrics, but not as R-selection signals).
2. Use region-tracking metrics (active_regions, accessed_bytes, fetched_bytes) as
   the primary signal layer.
3. Select a metric formula (O2 efficiency or O5 composite) that produces non-trivial
   optimal R per phase.
4. Verify with a richer dataset (more workloads with ≥2 active phases) before
   finalising P3 parameters.
