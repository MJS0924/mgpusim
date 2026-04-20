# M1 Phase-E Exploration Summary

Branch: m1-phase-e-exploration
Generated: 2026-04-20
Automation: fully automated (7h exploration, no manual intervention)

---

## TL;DR

**Why P1/P2/P3 failed**: The current metrics (L2HR, SCR, utilisation) do not vary
with region size R at the per-phase level. L2HR is structurally R-invariant (L2
cache uses 64B lines, independent of M1 region parameter). SCR is saturated at 1.0
everywhere (InfiniteCapacity=true, no evictions). Utilisation is monotone ↓ with R.

**What works instead**: The region-tracking layer (active_regions, fetched_bytes,
accessed_bytes) does vary with R. The AR-reduction efficiency metric (Option 2) and
composite cost model (Option 5), either alone or as a hybrid, produce genuine
non-monotone optimal R per phase — and pass a redesigned P1'.

**What is still limited**: P3 requires phase diversity within workloads. 4/6 workloads
have only 1 active phase in the current dataset, making P3 (≥3/6 workloads with
phase diversity) structurally unachievable without additional workloads or phase splits.

**User decision required**: Which redesigned metric direction to adopt for Phase F,
and whether to relax P3's min_workloads parameter or invest in a richer dataset.

---

## Document Reading Order

1. `e1_fail_analysis.md` — root cause analysis of P1/P2/P3 failure (Tier A)
2. `metric_exploration.md` — 5 candidate metrics computed from the D.2 data (Tier B)
3. `proto_verification.md` — prototype P1'/P3' results for all 5 options (Tier B+)
4. `option_pros_cons.md` — strengths, weaknesses, and requirements per option (Tier B+)
5. `hybrid_verification.md` — hybrid O2+O5 results and alpha sweep (Tier B++)
6. `option_correlation.md` — metric correlations and phase-level optimal-R summary (Tier B++)

Supporting data (CSVs):
- `p1_raw.csv`, `p2_raw.csv`, `p3_raw.csv` — per-row L2HR/util/SCR with optimal-R labels
- `candidate_metrics.csv` — M1a~M5a per (workload, R, seed, phase_index)
- `candidate_metrics_agg.csv` — same, averaged over seeds

Supporting scripts (analysis/explore/):
- `analyze_p1_failure.py`, `analyze_p2_failure.py`, `analyze_p3_failure.py`
- `candidate_metrics.py`
- `proto_option1.py` through `proto_option5.py`
- `hybrid_candidates.py`

---

## Key Findings

### Finding 1: L2HR and SCR are structurally R-invariant

```
L2HR std across R (per phase): mean = 0.0, max = 0.0 (100% invariant)
SCR = 1.0 for all 135 active phase rows (100% saturated)
```

These metrics cannot be fixed by parameter tuning. A metric redesign must
replace them entirely for proposition verification purposes.

### Finding 2: Utilisation is monotone ↓ with R

```
util mean: R=64→0.472, R=256→0.453, R=1024→0.432, R=4096→0.408, R=16384→0.359
```

Optimal R by utilisation = always R=64. Not usable as a non-trivial signal.

### Finding 3: Option 2 (AR efficiency) passes P1' without hyperparameters

```
P1' entropy (Option 2) = 0.549 > 0.5 threshold → PASS
Optimal R distribution: {R=1024: 12, R=4096: 12, R=256: 3}
```

The efficiency formula `(AR_64 - AR_R) / max(fetched_R - accessed, 1)` creates
a genuine non-monotone curve per phase because:
- Numerator (AR saved) saturates at large R (diminishing returns)
- Denominator (wasted bytes) grows with R
→ Interior peak between R=256 and R=4096, varying per phase

### Finding 4: matrixmultiplication phase 0 is genuinely different

```
M1a@R=16384: phase 0 = 0.141  vs  phases 1,2 = 0.005
O2 optimal R: phase 0 = R=256, phase 1 = R=1024, phase 2 = R=4096
```

Phase 0 accesses memory in a scattered pattern (likely matrix initialisation)
with weak spatial locality. Phases 1,2 have strong spatial locality with
fast AR decay. This is the only workload with genuine phase diversity.

### Finding 5: Hybrid O2+O5 achieves entropy = 1.31

```
alpha=0.5 hybrid entropy = 1.311 (vs O2=0.549, O5=0.549 individually)
```

Combining efficiency (O2) and composite cost (O5) after normalisation roughly
doubles the entropy. The distribution {R=1024:9, R=4096:9, R=16384:6, R=256:3}
shows spread across 4 region sizes. The hybrid does not change P3' outcome.

### Finding 6: m1a is the strongest predictor of O2 efficiency (ρ = −0.725)

m1a (AR_R / AR_64) is a direct proxy for O2's efficiency numerator.
Phases/R-values with high m1a (many active regions) have low efficiency —
meaning poor coalescing phases cannot benefit from large R.

---

## User Decision Items

### Decision A: Which metric redesign direction for Phase F?

Three viable options:

**Option 2 (AR-reduction efficiency) — Recommended**
- Pros: no hyperparameters, genuinely non-monotone, passes P1'
- Cons: unit mismatch (AR count / bytes), P3' still fails
- Action: adopt `efficiency(R) = (AR_64 - AR_R) / max(fetched_R - accessed, 1)` as
  the new P1'/P2' evaluation metric

**Option 5 (Composite cost model) — Alternative**
- Pros: principled, extensible, passes P1'
- Cons: weight parameter w1/w2 needs calibration, results vary with weights
- Action: adopt with w1=0.5, w2=0.5 as default (tunable with performance oracle)

**Hybrid O2+O5 — Highest entropy**
- Pros: highest diversity (entropy=1.31), uses strengths of both
- Cons: adds alpha parameter; two-step normalisation less interpretable
- Action: use if maximising phase discrimination is the priority

### Decision B: What to do about P3?

P3 requires ≥ 3/6 workloads to have inter-phase optimal-R diversity.
Current dataset: only 1/6 workloads achieves this (matrixmultiplication).

**Option B1: Lower min_workloads = 1**
- Passes with current data using any of O2/O3/O4/O5
- Risk: too weak a claim for a published metric proposition

**Option B2: Reformulate P3 as "fraction of multi-phase workloads"**
- "Mode fraction ≤ 0.60 in ≥ 2/3 of workloads with ≥2 active phases"
- matmul and pagerank are the relevant workloads; matmul passes with O2
- Requires workload qualification step; semantically more precise

**Option B3: Extend the dataset (D.3 sweep)**
- Add 3+ workloads known to have multiple distinct computation phases
- E.g.: BFS (traversal + relaxation), SpMV (scatter + gather), LSTM (cell + gate)
- Most defensible, but requires additional measurement work

**Option B4: Improve phase detection**
- Current phase windows may merge multiple computational phases
- Finer-grained phase detection could split single-phase workloads into 2–3
- Requires changes to the M1 measurement driver

### Decision C: How to handle L2HR and SCR?

These metrics should not be dropped from data collection — they characterise
workload behaviour (pagerank has 96% L2HR; matrixtranspose has 0%). But they
should not appear in redesigned P1~P3 verification formulas.

Possible roles:
- L2HR: workload characterisation only (not R-selection signal)
- SCR: flag when InfiniteCapacity=false is used in future experiments

---

## Automation Limits

This exploration was fully automated and stayed within the following bounds:
- No changes to `analysis/propositions.py` (as instructed)
- No changes to main branch
- No PR created or merged
- All work on `m1-phase-e-exploration`
- Evidence only — no metric redesign decisions made

The following could NOT be automated and require user judgement:
1. Which metric formula becomes the official redesign (Decision A)
2. P3 threshold adjustment (Decision B)
3. Whether to invest in D.3 sweep or Phase F proceeds with current dataset
4. How to handle the L2HR/SCR roles in future propositions (Decision C)

---

## Recommended Next Steps (Phase F)

If the user chooses Option 2 (Efficiency) + P3 reformulation (Option B2):

1. Modify `analysis/propositions.py`:
   - Replace L2HR with efficiency metric in P1 and P2
   - Drop SCR from P2's 3-metric agreement
   - Reformulate P3 to condition on workloads with ≥2 active phases

2. Re-run `analysis/verify_m1.py` on D.2 data with new propositions

3. Commit and create PR against main

Estimated effort: 1–2h coding + test updates.

The `analysis/explore/` scripts in this branch serve as working reference
implementations for all five options.
