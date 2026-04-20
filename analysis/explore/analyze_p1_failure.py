"""Tier A: P1 failure analysis — window entropy.

P1 checks that the distribution of optimal-R choices (by L2HR) shows meaningful
entropy (> 0.5 nats) across phases.  It FAILed with value=0.0 because L2HR is
R-invariant (same for every region size within a run).  This script quantifies
the invariance precisely and writes p1_raw.csv.

Output columns:
  workload, region_size, seed, phase_index, l2_hits, l2_misses, l2hr
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'
OUT_CSV = Path(__file__).resolve().parents[2] / 'results/m1/e_explore/p1_raw.csv'


def main():
    df = load_all_runs(str(DATA_DIR))

    # Compute L2HR per row
    total = df['l2_hits'] + df['l2_misses']
    df['l2hr'] = df['l2_hits'] / total.where(total > 0)

    # For each (workload, seed, phase_index), compute L2HR std across R values
    # This directly quantifies R-invariance
    per_phase = (
        df.groupby(['workload', 'seed', 'phase_index'])
          .agg(
              l2hr_mean=('l2hr', 'mean'),
              l2hr_std=('l2hr', 'std'),
              l2hr_min=('l2hr', 'min'),
              l2hr_max=('l2hr', 'max'),
              l2hr_range=('l2hr', lambda x: x.max() - x.min()),
              n_regions=('region_size', 'count'),
          )
          .reset_index()
    )

    # Optimal R per phase (argmax L2HR); ties → smallest R
    active = df[(df['l2_hits'] + df['l2_misses']) > 0].copy()
    if not active.empty:
        idx = active.groupby(['workload', 'seed', 'phase_index'])['l2hr'].idxmax()
        optimal = active.loc[idx, ['workload', 'seed', 'phase_index', 'region_size']].rename(
            columns={'region_size': 'optimal_r'}
        )
        per_phase = per_phase.merge(optimal, on=['workload', 'seed', 'phase_index'], how='left')
    else:
        per_phase['optimal_r'] = pd.NA

    # Full raw: one row per (workload, R, seed, phase_index)
    raw = df[['workload', 'region_size', 'seed', 'phase_index',
              'l2_hits', 'l2_misses', 'l2hr']].copy()

    OUT_CSV.parent.mkdir(parents=True, exist_ok=True)
    raw.to_csv(OUT_CSV, index=False)
    print(f"Wrote {len(raw)} rows to {OUT_CSV}")

    # Summary stats
    print("\n=== P1 Failure Analysis: L2HR R-invariance ===")
    print(f"Total phase rows: {len(raw)}")
    print(f"Rows with L2HR > 0: {(raw['l2hr'] > 0).sum()}")
    print(f"\nPer-phase L2HR range across R values (should be ~0 if invariant):")
    print(per_phase['l2hr_range'].describe().to_string())
    print(f"\nPhases where l2hr_range < 0.01 (effectively invariant): "
          f"{(per_phase['l2hr_range'] < 0.01).sum()} / {len(per_phase)}")
    print(f"\nPer-workload mean L2HR:")
    print(df.groupby('workload')['l2hr'].mean().sort_values(ascending=False).to_string())

    # Entropy analysis
    print("\n=== Entropy of optimal_r distribution ===")
    if 'optimal_r' in per_phase.columns:
        for wl, grp in per_phase.groupby('workload'):
            counts = grp['optimal_r'].value_counts()
            total_n = len(grp)
            probs = counts / total_n
            import math
            entropy = -sum(p * math.log(p) for p in probs if p > 0)
            print(f"  {wl}: optimal_r distribution={dict(counts)}, entropy={entropy:.4f}")


if __name__ == '__main__':
    main()
