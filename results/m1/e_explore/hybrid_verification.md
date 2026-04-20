# Hybrid Candidate Verification

Generated: 2026-04-20 (Tier B++)
Script: analysis/explore/hybrid_candidates.py

---

## Hybrid Design

**Top-2 options** (Option 2 + Option 5) are combined after within-phase min-max normalisation:

```
hybrid_score(R) = α × efficiency_norm(R) + (1-α) × composite_score_norm(R)
```

Where:
- `efficiency_norm(R)` = (AR_R efficiency) min-max normalised within each phase
- `composite_score_norm(R)` = (O5 score) min-max normalised within each phase

---

## Alpha Sweep Results

| Alpha | O2 weight | O5 weight | Distribution | Entropy |
|-------|-----------|-----------|-------------|---------|
| 0.00 | 0% | 100% | {16384:12, 4096:11, 1024:4} | 1.009 |
| 0.25 | 25% | 75% | {4096:12, 1024:6, 16384:6, 256:3} | 1.273 |
| **0.50** | **50%** | **50%** | **{1024:9, 4096:9, 16384:6, 256:3}** | **1.311** |
| 0.75 | 75% | 25% | {4096:15, 1024:9, 256:3} | 0.937 |
| 1.00 | 100% | 0% | {1024:12, 4096:12, 256:3} | 0.965 |

**Peak entropy = 1.311 at alpha=0.50**. This is significantly higher than any single option
(Options 2, 5 each achieved ~0.549–0.665). The combination increases diversity.

---

## Phase-Level Results (alpha=0.5)

Score curves show clear interior peaks — no single R dominates:

| Workload | Phase | Optimal R | Score at Optimal | 2nd Best R | Score drop |
|----------|-------|-----------|-----------------|------------|-----------|
| fir | 1 | **1024** | 0.988 | 4096 (0.976) | −1.2% |
| matmul | 0 | **256** | 0.865 | 16384 (0.532) | −38% |
| matmul | 1 | **4096** | 0.999 | 1024 (0.981) | −1.8% |
| matmul | 2 | **4096** | 1.000 | 1024 (0.966) | −3.4% |
| matrixtranspose | 1 | **4096** | 0.999 | 16384 (0.987) | −1.2% |
| pagerank | 1 | **1024** | 1.000 | 256 (0.869) | −13.1% |
| pagerank | 2 | **1024** | 0.999 | 4096 (0.931) | −6.9% |
| simpleconvolution | 2 | **16384** | 0.997 | 4096 (0.996) | −0.1% |
| stencil2d | 1 | **16384** | 0.997 | 4096 (0.996) | −0.1% |

**matrixmultiplication** is the standout workload: phase 0 strongly prefers R=256
(38% score drop to next option), while phases 1,2 clearly prefer R=4096.
This reflects the genuine structural difference in phase 0 (scattered initialisation
access pattern) vs phases 1,2 (regular matrix computation).

---

## O2 vs O5 Disagreement Analysis

Agreement rate: 7/27 = 25.9% — the two options often recommend different R.

This low agreement is informative:
- O2 (efficiency) focuses on the AR-reduction benefit relative to fetch waste
- O5 (composite) balances fetch_loss against ar_overhead linearly

O2 tends to recommend smaller R (1024 most common) because efficiency peaks early
when AR reduction is large and wasted bytes are still moderate.

O5 tends to recommend larger R (16384, 4096) because the ar_overhead term in O5
penalises small R heavily (high AR count at small R), and fetch_loss grows slowly
for spatially-coherent workloads.

**Hybrid at alpha=0.5 splits the difference**, capturing both the efficiency peak
(O2) and the large-R bandwidth savings (O5).

---

## Proposition Results (Hybrid alpha=0.5)

**P1' redesigned** (entropy > 0.5 threshold):
- Hybrid entropy = **1.311** >> 0.5 → **PASS** (strongly)

**P3' redesigned** (mode_frac ≤ 0.60 in ≥ 3 workloads):
- matmul: mode_r=4096, mode_frac=6/9=0.667 → FAIL (marginally)
- pagerank: mode_r=1024, mode_frac=9/9=1.0 (both phases → R=1024) → FAIL
- All single-phase workloads: mode_frac=1.0 → FAIL
- Result: **0/6 workloads pass P3'** → FAIL

Note: P3' failure for hybrid is worse than O2 alone, because hybrid aligns
pagerank phases 1 and 2 to the same R=1024 (O2 alone also gave 1024 for both).
Matmul at 0.667 is close but still over threshold.

P3' remains structurally limited by dataset phase counts (see proto_verification.md).
