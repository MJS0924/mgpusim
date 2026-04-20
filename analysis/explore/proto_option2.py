"""Tier B+: Option 2 prototype — AR reduction efficiency score.

Metric: efficiency(R) = (AR_64 - AR_R) / max(fetched_R - accessed, 1)
        = active regions saved per wasted byte

Hypothesis: numerator (AR saved) decelerates with R (concave curve),
while denominator (wasted bytes) accelerates with R (convex curve).
This creates a potential interior maximum → non-monotone optimal R.

Redesigned propositions:
  P1': entropy of optimal-R distribution > 0.5
  P3': mode_frac ≤ 0.60 in ≥ 3 workloads
"""

import sys, math
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
import numpy as np
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'


def compute_efficiency(df: pd.DataFrame) -> pd.DataFrame:
    """Compute efficiency score per (workload, seed, phase_index, region_size)."""
    # Get AR and fetched at R=64 as baseline
    base = df[df['region_size'] == 64][
        ['workload', 'seed', 'phase_index', 'active_regions',
         'region_fetched_bytes', 'region_accessed_bytes']
    ].rename(columns={
        'active_regions': 'ar_base',
        'region_fetched_bytes': 'fetched_base',
        'region_accessed_bytes': 'accessed_base',
    })
    merged = df.merge(base, on=['workload', 'seed', 'phase_index'], how='left')

    ar_saved = (merged['ar_base'] - merged['active_regions']).clip(lower=0)
    wasted_bytes = (merged['region_fetched_bytes'] - merged['accessed_base']).clip(lower=1)
    merged['efficiency'] = ar_saved / wasted_bytes
    return merged


def optimal_r_by_efficiency(grp: pd.DataFrame) -> int:
    """R that maximises efficiency(R)."""
    if grp.empty:
        return 64
    idx = grp['efficiency'].idxmax()
    return int(grp.loc[idx, 'region_size'])


def run_p1_prime(df: pd.DataFrame, threshold: float = 0.5) -> dict:
    eff_df = compute_efficiency(df)
    key = ['workload', 'seed', 'phase_index']
    active = eff_df[eff_df['ar_base'] > 0]

    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_efficiency(grp)
        opt_rows.append({'workload': gkey[0], 'seed': gkey[1],
                         'phase_index': gkey[2], 'optimal_r': opt_r})
    if not opt_rows:
        return {'status': 'FAIL', 'value': 0.0, 'detail': 'no active phases'}

    opt_df = pd.DataFrame(opt_rows)

    entropies = []
    for (wl, seed), grp in opt_df.groupby(['workload', 'seed']):
        if len(grp) < 2:
            continue
        counts = grp['optimal_r'].value_counts()
        probs = counts / len(grp)
        h = -sum(p * math.log(p) for p in probs if p > 0)
        entropies.append(h)

    mean_h = sum(entropies) / len(entropies) if entropies else 0.0
    status = 'PASS' if mean_h >= threshold else 'FAIL'
    return {'status': status, 'value': round(mean_h, 4),
            'threshold': threshold,
            'detail': f'mean entropy={mean_h:.4f} over {len(entropies)} pairs',
            'opt_r_distribution': dict(opt_df['optimal_r'].value_counts())}


def run_p3_prime(df: pd.DataFrame, threshold: float = 0.60, min_wl: int = 3) -> dict:
    eff_df = compute_efficiency(df)
    key = ['workload', 'seed', 'phase_index']
    active = eff_df[eff_df['ar_base'] > 0]

    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_efficiency(grp)
        opt_rows.append({'workload': gkey[0], 'seed': gkey[1],
                         'phase_index': gkey[2], 'optimal_r': opt_r})

    opt_df = pd.DataFrame(opt_rows) if opt_rows else pd.DataFrame(
        columns=['workload', 'seed', 'phase_index', 'optimal_r'])

    pass_wl = 0
    details = {}
    for wl, grp in opt_df.groupby('workload'):
        mode_r = grp['optimal_r'].mode().iloc[0]
        mode_frac = (grp['optimal_r'] == mode_r).mean()
        details[wl] = {'mode_r': int(mode_r), 'mode_frac': round(mode_frac, 3),
                        'distribution': dict(grp['optimal_r'].value_counts())}
        if mode_frac <= threshold:
            pass_wl += 1

    return {'status': 'PASS' if pass_wl >= min_wl else 'FAIL', 'value': pass_wl,
            'detail': f'{pass_wl}/{len(details)} workloads pass',
            'workload_details': details}


def main():
    df = load_all_runs(str(DATA_DIR))
    eff_df = compute_efficiency(df)

    print("=== Option 2: AR-reduction efficiency ===\n")

    # Show efficiency curves per workload/phase
    active = eff_df[eff_df['ar_base'] > 0].copy()
    print("Efficiency by (workload, phase_index, region_size) — averaged over seeds:")
    pivot = active.groupby(['workload', 'phase_index', 'region_size'])['efficiency'].mean()
    for (wl, ph), grp in pivot.groupby(level=[0, 1]):
        vals = grp.reset_index(level=[0, 1], drop=True)
        print(f"  {wl} phase {ph}:")
        for r, v in vals.items():
            print(f"    R={r}: efficiency={v:.6f}")
        # Find optimal R
        opt = vals.idxmax()
        print(f"    → optimal_r = {opt}")
    print()

    p1 = run_p1_prime(df)
    p3 = run_p3_prime(df)

    print(f"P1' status: {p1['status']} | value={p1['value']} | {p1['detail']}")
    print(f"  opt_r distribution: {p1.get('opt_r_distribution', {})}")
    print()
    print(f"P3' status: {p3['status']} | value={p3['value']} | {p3['detail']}")
    for wl, d in p3.get('workload_details', {}).items():
        print(f"  {wl}: mode_r={d['mode_r']}, mode_frac={d['mode_frac']}, dist={d['distribution']}")


if __name__ == '__main__':
    main()
