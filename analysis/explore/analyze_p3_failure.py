"""Tier A: P3 failure analysis — phase mode bound.

P3 checks that in ≥ 3 workloads the modal optimal-R accounts for ≤ 60% of
phases (i.e., different phases prefer different R values).  It FAILed with
0/6 workloads passing.  Root cause: L2HR is R-invariant → all phases always
"prefer" the same R (tie-break artifact), so mode_fraction = 1.0 everywhere.

This script characterises the phase-level optimal-R distribution per workload
and writes p3_raw.csv.

Output columns (one row per workload × seed × phase_index × region_size):
  workload, seed, phase_index, region_size, l2hr, optimal_r_l2hr,
  mode_r, mode_fraction
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'
OUT_CSV = Path(__file__).resolve().parents[2] / 'results/m1/e_explore/p3_raw.csv'


def main():
    df = load_all_runs(str(DATA_DIR))

    total_l2 = df['l2_hits'] + df['l2_misses']
    df['l2hr'] = df['l2_hits'] / total_l2.where(total_l2 > 0)

    key = ['workload', 'seed', 'phase_index']
    active = df[(df['l2_hits'] + df['l2_misses']) > 0].copy()

    # Optimal R per phase
    idx = active.groupby(key)['l2hr'].idxmax()
    opt = active.loc[idx, key + ['region_size']].rename(columns={'region_size': 'optimal_r'})

    # mode R and fraction per (workload, seed)
    mode_rows = []
    for (wl, seed), grp in opt.groupby(['workload', 'seed']):
        mode_r = grp['optimal_r'].mode().iloc[0]
        mode_frac = (grp['optimal_r'] == mode_r).mean()
        mode_rows.append(dict(workload=wl, seed=seed, mode_r=mode_r,
                              mode_fraction=mode_frac, n_phases=len(grp)))

    mode_df = pd.DataFrame(mode_rows)

    # Merge optimal_r back onto full df for CSV
    raw = df[['workload', 'region_size', 'seed', 'phase_index', 'l2hr']].merge(
        opt, on=key, how='left'
    )

    OUT_CSV.parent.mkdir(parents=True, exist_ok=True)
    raw.to_csv(OUT_CSV, index=False)
    print(f"Wrote {len(raw)} rows to {OUT_CSV}")

    print("\n=== P3 Failure Analysis: Phase Mode Bound ===")
    print(f"\nMode fraction per (workload, seed):")
    print(mode_df.to_string(index=False))

    print(f"\nMode fraction summary:")
    print(mode_df['mode_fraction'].describe().to_string())

    print(f"\nWorkloads where mode_fraction ≤ 0.60 (P3 pass criterion):")
    passing = mode_df[mode_df['mode_fraction'] <= 0.60]
    print(passing[['workload', 'seed', 'mode_fraction']].to_string(index=False)
          if not passing.empty else "  None")

    print(f"\nOptimal-R distribution per workload (aggregated over seeds):")
    for wl, grp in opt.groupby('workload'):
        dist = grp['optimal_r'].value_counts().sort_index()
        total_n = len(grp)
        fracs = {r: f"{v/total_n:.2f}" for r, v in dist.items()}
        print(f"  {wl}: {fracs}")

    # Phase counts per workload (to understand context)
    print(f"\nPhase count per workload:")
    print(df.groupby('workload')['phase_index'].nunique().to_string())


if __name__ == '__main__':
    main()
