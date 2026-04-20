# Option Pros and Cons

Generated: 2026-04-20 (Tier B+)

For each option this document lists: what the metric measures, its strengths,
weaknesses, and what additional measurement support it would require.

---

## Option 1: Fetch-overhead Budget Threshold

**Metric**: Largest R where (fetched_bytes - accessed_bytes)/accessed_bytes ≤ α

**Pros**:
- Intuitive: "how much fetch overhead can we tolerate?"
- No R-ordering assumption — just a budget constraint

**Cons**:
- Degenerate in current data: even R=64 has > 80% fetch overhead (fetched ≈ 2× accessed)
  for most workloads. No R satisfies tight budgets → always falls back to R=64.
- α must be workload-calibrated, not universal.
- Still monotone: for any fixed α, budget is either satisfied at all R ≥ R_min
  or at none. Interior optima require non-monotone cost function.
- Does not use active_regions information at all.

**Required**: Better utilisation of accessed bytes (or a different overhead denominator)
to make the budget meaningful. Current data makes this option impractical.

---

## Option 2: AR-reduction Efficiency

**Metric**: efficiency(R) = (AR_64 - AR_R) / max(fetched_R - accessed, 1)

**Pros**:
- **Genuinely non-monotone**: produces interior optima without threshold tuning.
- **Phase-differentiated**: matrixmultiplication phases get R=256, 1024, 4096 respectively.
- **No hyperparameters**: purely data-driven argmax.
- Uses both AR reduction (benefit) and wasted bytes (cost) in a single ratio.
- Meaningful units: "active regions saved per wasted byte".

**Cons**:
- Denominator unit mismatch: AR (count) vs bytes → the ratio has no clean physical unit.
- Numerator (AR saved) converges to AR_64 at large R → asymptotically constant.
  Denominator grows without bound → efficiency → 0 at very large R (may need clipping).
- Phases with AR_64=0 (inactive) are excluded from analysis.
- 4/6 workloads have 1 active phase → inter-phase diversity measured only for matmul/pagerank.

**Required**: Same measurement infrastructure as current D.2. No new metrics needed.
The formula only uses active_regions, region_fetched_bytes, region_accessed_bytes.

---

## Option 3: M1a Decay Slope Classification

**Metric**: M1a(16384) = AR_16384 / AR_64 as proxy for spatial coalescing efficiency

**Pros**:
- Interpretable: "what fraction of regions remain when using the coarsest granularity?"
- Clear physical meaning: fast decay = good spatial locality = can coalesce well.
- matrixmultiplication phase 0 genuinely classified differently (0.141) from phases 1,2 (0.005).

**Cons**:
- Binary/ternary classification: threshold boundaries are ad-hoc and narrow.
  Small threshold changes flip results dramatically.
- Uses only 2 out of 5 R measurements (R=64 and R=16384) — wastes intermediate data.
- Does not capture the cost side (fetch overhead) — purely locality-based.
- Same M1a@16384 for different phases could have different ideal R due to fetch overhead.
- Threshold PASS band (0.005–0.05) only captures matmul phases; all others fall into
  "fast" or "slow" category → creates only 2-3 distinct optimal R values.

**Required**: Same as current. Better with a continuous slope metric rather than threshold.

---

## Option 4: M5a Density Threshold

**Metric**: Smallest R where retired_wavefronts / active_regions ≥ γ

**Pros**:
- Highest entropy (0.665) among all options — richest R diversity.
- **Physical interpretation**: "smallest R that gives γ wavefronts per region"
  — ensures each region has sufficient compute to justify tracking overhead.
- pagerank both phases can be differentiated (phase 1 → R=64, phase 2 → R=256).
- Responds to total workload intensity (wavefronts is a proxy for computation done).

**Cons**:
- M5a is monotone ↑ — threshold is the only source of diversity, not natural curvature.
- Workloads with retired_wavefronts=0 (matmul phases 0,1) → M5a=0 → always R=64 (fallback).
  These are likely load/init phases where the computation unit is idle.
- γ is workload-scale sensitive: pagerank has M5a@64=1.4 while stencil2d has 0.004.
  A universal γ cannot discriminate between low-intensity workloads.
- matrixtranspose and stencil2d have M5a@16384 < 1.0 → never reach γ=1 → stuck at R=64.

**Required**: Non-zero retired_wavefronts in all phases (issue with matmul init phases).
May need separate γ per workload-class, reducing generalisability.

---

## Option 5: Composite Cost Model

**Metric**: score(R) = -(w1 × fetch_loss + w2 × ar_overhead)
  fetch_loss(R) = 1 - accessed/fetched (penalty for large R)
  ar_overhead(R) = AR_R / AR_64 (penalty for small R)

**Pros**:
- **Most principled design**: explicit cost-benefit trade-off with interpretable weights.
- Naturally produces interior optima: fetch_loss ↑ with R while ar_overhead ↓ with R.
- Tied for highest entropy (0.665) at w1=0.1, w2=0.9.
- Score curves show genuine peaks in the interior (R=1024 for pagerank phase 1, R=4096 for fir).
- **Extensible**: additional cost terms (e.g., tracking latency, coherence penalty) can be
  added as w_i × cost_i(R).
- Weights can be calibrated from application requirements (bandwidth-bound vs compute-bound).

**Cons**:
- Weight selection is a design parameter — different w1/w2 give different optima.
  No principled way to choose w1, w2 without a performance oracle.
- w1=0.3 and w1=0.7 give FAIL for P1' — the PASS region is narrow.
- fetch_loss and ar_overhead are both dimensionless fractions, but have different variance
  ranges → implicit scale sensitivity.
- Still only 2 workloads (matmul, pagerank) with multi-phase data.

**Required**: Same as current. The model structure is complete, but weight calibration
needs an external performance signal (e.g., measured DRAM bandwidth, execution time).

---

## Cross-option Comparison

| Criterion | O1 | O2 | O3 | O4 | O5 |
|-----------|----|----|----|----|-----|
| Non-monotone optimal R | No | **Yes** | Threshold | No (mono ↑) | **Yes** |
| No hyperparameters | No | **Yes** | No | No | No |
| P1' achievable | No | Yes | Yes | Yes | Yes |
| P3' achievable (current data) | No | 1/6 | 1/6 | 1/6 | 1/6 |
| Physical interpretability | Med | Med | **High** | Med | **High** |
| Extensibility | Low | Med | Low | Low | **High** |
| Robustness to inactive phases | Low | Med | Med | **Low** | Med |
| Uses all 5 R measurements | No | Yes | No | Yes | Yes |

**Recommendation order**: O2 (robustness) ≥ O5 (extensibility) > O4 (entropy) > O3 (interpretability) >> O1 (degenerate)

The hybrid O2+O5 (efficiency score as one cost term in composite model) may combine
the strengths of both. See hybrid_candidates.py.
