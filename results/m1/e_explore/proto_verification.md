# Prototype Verification Results

Generated: 2026-04-20 (Tier B+)
Scripts: analysis/explore/proto_option1.py through proto_option5.py

---

## Overview

Five prototype metric options were evaluated against redesigned propositions
P1' (entropy of optimal-R distribution > 0.5) and P3' (modal optimal-R ≤ 60%
of phases in ≥ 3 workloads). P2' is implicitly evaluated by the quality of
optimal-R selection.

**Key finding**: P1' can be passed by Options 2, 3, 4, and 5.
P3' fails for all options due to a structural data limitation: 4 of 6
workloads have only 1 active phase, making phase diversity impossible for them.

---

## Option 1: Fetch-overhead Budget Threshold

**Method**: Optimal R = largest R where (fetched - accessed)/accessed ≤ α
**Parameter**: α (budget threshold)

| α | P1' | P3' | Distribution |
|---|-----|-----|-------------|
| 0.1–1.0 | FAIL | FAIL | {R=64: 27} |
| 2.0 | FAIL | FAIL | {R=16384: 18, R=4096: 6, R=64: 3} |
| 5.0 | FAIL | FAIL | {R=16384: 24, R=64: 3} |

**Verdict**: Fails P1' for all useful α. Root cause: even R=64 has
high fetch overhead (≥ 0.83×), so small α always falls back to R=64 (trivial).
Large α permits any R, leading to trivial argmax at R=16384 or R=4096.

**Not viable** for redesigned propositions.

---

## Option 2: AR-reduction Efficiency

**Method**: Optimal R = argmax efficiency(R) where
efficiency = (AR_64 - AR_R) / max(fetched_R - accessed, 1)

| Metric | Result |
|--------|--------|
| P1' | **PASS** (entropy = 0.549 > 0.5) |
| P3' | FAIL (1/6 workloads: matrixmultiplication only) |

**Optimal R distribution**: {R=1024: 12, R=4096: 12, R=256: 3}

**Phase-level results** (mean over seeds):
| Workload | Phase | Optimal R |
|----------|-------|-----------|
| fir | 1 | 1024 |
| matrixmultiplication | 0 | **256** |
| matrixmultiplication | 1 | **1024** |
| matrixmultiplication | 2 | **4096** |
| matrixtranspose | 1 | 4096 |
| pagerank | 1 | 1024 |
| pagerank | 2 | 1024 |
| simpleconvolution | 2 | 4096 |
| stencil2d | 1 | 4096 |

**Key insight**: Efficiency is genuinely non-monotone. The AR-saved numerator
decelerates as R grows (region count collapses quickly at first, then slowly),
while the wasted-bytes denominator accelerates. The crossover creates genuine
interior optima — different across phases and workloads.

**matrixmultiplication** achieves mode_frac = 0.333 ≤ 0.60 → passes P3' criterion,
but needs ≥ 3 workloads for P3' to PASS overall.

**Best Option for P1'.**

---

## Option 3: M1a Decay Slope Classification

**Method**: Classify phases by M1a at R=16384. 
  slow > threshold → R=64; fast < threshold → R=16384; else → R=1024

| Thresholds | P1' | P3' | Distribution |
|------------|-----|-----|-------------|
| slow=0.05, fast=0.01 | FAIL (0.318) | FAIL | {16384:24, 64:3} |
| slow=0.05, fast=0.005 | **PASS** (0.549) | FAIL (1/6) | {16384:15, 1024:9, 64:3} |
| slow=0.10, fast=0.005 | **PASS** (0.549) | FAIL (1/6) | same |

**Phase-level results** (best thresholds: slow=0.05, fast=0.005):
- matrixmultiplication phase 0 → R=64 (m1a_max=0.141 > 0.05, scatter-heavy)
- All others → R=16384 (m1a_max < 0.005) or R=1024 (0.005 ≤ m1a_max ≤ 0.05)

**Observation**: pagerank phases have m1a_max ≈ 0.006–0.008 → fall in middle zone
→ R=1024. This gives pagerank phase diversity relative to other workloads (not
between phases within pagerank — both phases give R=1024).

**Threshold-sensitive** — the PASS/FAIL boundary is narrow. Option 2 is more
robust (no hyperparameters, natural optimum).

---

## Option 4: M5a Density Threshold

**Method**: Optimal R = smallest R where M5a(R) = wavefronts/active_regions ≥ γ

| γ | P1' | P3' | Distribution |
|---|-----|-----|-------------|
| 0.5 | FAIL | FAIL | |
| 1.0 | **PASS** (0.665) | FAIL (1/6) | {64:15, 4096:3, 16384:3, 256:3, 1024:3} |
| 2.0 | FAIL | FAIL | |
| 5.0 | FAIL (0.347) | FAIL (1/6) | |
| 10.0 | FAIL | FAIL | |

**Best result**: γ=1.0 achieves highest P1' entropy (0.665), highest of all options.

**Phase-level results** (γ=1.0):
- pagerank phase 1: M5a@64=1.42 ≥ 1.0 → **R=64**
- pagerank phase 2: M5a@64=0.79 < 1.0, M5a@256=2.87 ≥ 1.0 → **R=256**
- fir, simpleconvolution: need R=1024 to hit γ=1.0
- matrixtranspose, stencil2d: even R=16384 barely hits 0.97 < 1.0 → **R=64**
- matmul phases 0,1: retired_wavefronts=0 → M5a=0 always → **R=64**

**Highest entropy of all options**, but γ is workload-sensitive. Low-wavefront
workloads (matrixtranspose, stencil2d) never hit threshold → forced to R=64.

**Pagerank P3' passes at γ=1.0 and γ=5.0** (mode_frac=0.5).

---

## Option 5: Composite Cost Model

**Method**: score(R) = -(w1 × fetch_loss + w2 × ar_overhead)
  fetch_loss = 1 - accessed/fetched (higher at large R, penalises large R)
  ar_overhead = AR_R / AR_64 (higher at small R, penalises small R)
  Optimal R = argmax score(R)

| w1 | w2 | P1' | P3' | Distribution |
|----|-----|-----|-----|-------------|
| 0.1 | 0.9 | **PASS** (0.665) | FAIL (1/6) | {16384:21, 4096:6} |
| 0.3 | 0.7 | FAIL (0.318) | FAIL | {4096:15, 16384:12} |
| 0.5 | 0.5 | **PASS** (0.549) | FAIL | {16384:12, 4096:11, 1024:4} |
| 0.7 | 0.3 | FAIL (0.318) | FAIL | |
| 0.9 | 0.1 | **PASS** (0.549) | FAIL | {1024:14, 4096:6, 256:4, 64:3} |

**Score curves** (w1=0.5, w2=0.5) show genuine interior optima:
- fir → R=4096 (plateau at R=4096–1024 balances both penalties)
- matmul phase 0 → R=16384 (ar_overhead dominates — high AR_64)
- matmul phases 1,2 → R=4096
- pagerank phase 1 → **R=1024** (fetch_loss rises faster than ar_overhead falls)
- pagerank phase 2 → **R=4096** (different phase balance)

**Most principled design** — explicit cost-benefit trade-off with interpretable
weights. Also achieves highest entropy at w1=0.1 (0.665), tied with Option 4.

---

## Comparative Summary

| Option | P1' Best | P3' Best | Phase diversity | Notes |
|--------|---------|---------|-----------------|-------|
| 1 (Budget) | FAIL | FAIL | None | Degenerate — all fall back to R=64 |
| **2 (Efficiency)** | **PASS 0.549** | FAIL 1/6 | Genuine interior optima | Best non-monotone behaviour |
| 3 (M1a slope) | PASS 0.549 | FAIL 1/6 | Threshold-dependent | 3 classes of R |
| **4 (M5a threshold)** | **PASS 0.665** | FAIL 1/6 | Rich R diversity | γ-sensitive; 0 wavefronts → stuck |
| **5 (Composite)** | **PASS 0.665** | FAIL 1/6 | Interior optima | Principled; w-sensitive |

---

## Why P3' is Structurally Unachievable

P3' (min_workloads=3) requires ≥ 3 workloads where phases disagree on optimal R.

Current dataset constraints:
- **4/6 workloads have 1 active phase** (fir, matrixtranspose, simpleconvolution, stencil2d)
  → mode_fraction = 1.0 trivially (no phase diversity possible)
- matrixmultiplication: 3 phases — can achieve mode_frac=0.333 (Options 2/3)
- pagerank: 2 phases — can achieve mode_frac=0.5 (Options 4/5 at certain γ/w)

Maximum achievable: 2/6 workloads pass. P3' requires 3/6.

**Resolution options for Phase F (user decision)**:
1. Lower min_workloads to 1 (passes with current data)
2. Reformulate as "fraction of workloads with ≥2 phases that pass" (eliminates trivially-1 workloads)
3. Add workloads with richer phase structure in a future D.3 sweep
4. Redesign phase detection to split currently-single-phase workloads further

See EXPLORATION_SUMMARY.md for prioritised recommendations.
