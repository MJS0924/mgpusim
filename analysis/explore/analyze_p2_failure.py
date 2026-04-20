"""Tier A: P2 failure analysis — 3-metric agreement.

P2 checks that L2HR, utilization, and SCR agree on the same optimal-R in ≥ 70%
of phases.  It FAILed with 0.0% agreement.  Root causes:
  - L2HR is R-invariant → ties broken by idxmax (arbitrary)
  - SCR = 1.0 everywhere → always ties → arbitrary optimal_r
  - Utilization is monotone ↓ → always prefers R=64

This script quantifies each metric's behavior and writes p2_raw.csv.

Output columns per row (workload, R, seed, phase_index):
  l2hr, util, scr  (and their per-phase tie/invariance flags)
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'
OUT_CSV = Path(__file__).resolve().parents[2] / 'results/m1/e_explore/p2_raw.csv'


def main():
    df = load_all_runs(str(DATA_DIR))

    total_l2 = df['l2_hits'] + df['l2_misses']
    df['l2hr'] = df['l2_hits'] / total_l2.where(total_l2 > 0)
    df['util'] = df['region_accessed_bytes'] / df['region_fetched_bytes'].where(
        df['region_fetched_bytes'] > 0
    )
    df['scr'] = df['sharer_consistent_regions'] / df['active_regions'].where(
        df['active_regions'] > 0
    )

    raw = df[['workload', 'region_size', 'seed', 'phase_index',
              'l2hr', 'util', 'scr',
              'l2_hits', 'l2_misses',
              'region_fetched_bytes', 'region_accessed_bytes',
              'active_regions', 'sharer_consistent_regions']].copy()

    OUT_CSV.parent.mkdir(parents=True, exist_ok=True)
    raw.to_csv(OUT_CSV, index=False)
    print(f"Wrote {len(raw)} rows to {OUT_CSV}")

    print("\n=== P2 Failure Analysis: 3-Metric Agreement ===")

    # Per-phase optimal R for each metric
    METRICS = ['l2hr', 'util', 'scr']
    key = ['workload', 'seed', 'phase_index']
    active = df[(df['l2_hits'] + df['l2_misses'] + df['active_regions']) > 0].copy()

    for m in METRICS:
        # std across R per phase
        vstd = (
            active.groupby(key)[m]
                  .std()
                  .fillna(0)
        )
        print(f"\n{m} std across R (per phase):")
        print(f"  mean={vstd.mean():.6f}, max={vstd.max():.6f}, "
              f"fraction_zero={( vstd == 0).mean():.3f}")

    # SCR values
    print(f"\nSCR value distribution across all rows:")
    print(raw['scr'].describe().to_string())
    print(f"SCR == 1.0: {(raw['scr'] == 1.0).sum()} / {(~raw['scr'].isna()).sum()}")

    # Utilization monotonicity
    print("\nUtilization (util) mean per region_size (should decrease with R):")
    print(raw.groupby('region_size')['util'].mean().to_string())

    # L2HR per region_size
    print("\nL2HR mean per region_size (should be constant if R-invariant):")
    print(raw.groupby('region_size')['l2hr'].mean().to_string())

    # Agreement check (same logic as propositions.py P2)
    def optimal_r(grp, metric):
        return grp.loc[grp[metric].idxmax(), 'region_size']

    rows = []
    for gkey, grp in active.groupby(key):
        if len(grp) == 0:
            continue
        r_l2hr = grp.loc[grp['l2hr'].idxmax(), 'region_size']
        r_util = grp.loc[grp['util'].idxmax(), 'region_size']
        r_scr = grp.loc[grp['scr'].idxmax(), 'region_size']
        agree = (r_l2hr == r_util == r_scr)
        rows.append(dict(workload=gkey[0], seed=gkey[1], phase_index=gkey[2],
                         r_l2hr=r_l2hr, r_util=r_util, r_scr=r_scr, agree=agree))

    agree_df = pd.DataFrame(rows)
    if not agree_df.empty:
        print(f"\nAgreement rate: {agree_df['agree'].mean():.3f} ({agree_df['agree'].sum()}/{len(agree_df)})")
        print(f"\nOptimal R distribution per metric:")
        for m, col in [('l2hr', 'r_l2hr'), ('util', 'r_util'), ('scr', 'r_scr')]:
            print(f"  {m}: {dict(agree_df[col].value_counts())}")


if __name__ == '__main__':
    main()
