"""Tier B++: Hybrid candidates — combining top-2 options (O2 + O5).

O2 (AR-reduction efficiency) produces genuine non-monotone optimal R.
O5 (Composite cost model) provides principled weight-based trade-off.

Hybrid: normalise O2's efficiency and O5's score, then blend:
  hybrid_score(R) = alpha * efficiency_norm(R) + (1-alpha) * composite_score_norm(R)

Additionally computes pairwise correlations between:
  - optimal_r choices across all 5 options
  - raw metrics (active_regions, l2hr, util, M1a, M5a) vs optimal_r from O2/O5
"""

import sys, math
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
import numpy as np
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'


def compute_efficiency(df, ar_base):
    """Option 2: AR saved per wasted byte."""
    ar_saved = (ar_base - df['active_regions']).clip(lower=0)
    wasted = (df['region_fetched_bytes'] - ar_base['region_accessed_bytes_base']).clip(lower=1) \
        if 'region_accessed_bytes_base' in ar_base else None
    return ar_saved


def build_phase_frame(df: pd.DataFrame) -> pd.DataFrame:
    """Build per-(workload, seed, phase_index, region_size) frame with all metrics."""
    base = df[df['region_size'] == 64][
        ['workload', 'seed', 'phase_index', 'active_regions',
         'region_fetched_bytes', 'region_accessed_bytes']
    ].rename(columns={
        'active_regions': 'ar_64',
        'region_fetched_bytes': 'fetched_64',
        'region_accessed_bytes': 'accessed_base',
    })
    merged = df.merge(base, on=['workload', 'seed', 'phase_index'], how='left')

    # Core metrics
    total_l2 = merged['l2_hits'] + merged['l2_misses']
    merged['l2hr'] = merged['l2_hits'] / total_l2.where(total_l2 > 0)
    merged['util'] = merged['region_accessed_bytes'] / merged['region_fetched_bytes'].where(
        merged['region_fetched_bytes'] > 0
    )
    merged['m1a'] = merged['active_regions'] / merged['ar_64'].where(merged['ar_64'] > 0)
    merged['m5a'] = merged['retired_wavefronts'] / merged['active_regions'].where(
        merged['active_regions'] > 0
    )

    # O2: AR-reduction efficiency
    ar_saved = (merged['ar_64'] - merged['active_regions']).clip(lower=0)
    wasted = (merged['region_fetched_bytes'] - merged['accessed_base']).clip(lower=1)
    merged['o2_efficiency'] = ar_saved / wasted

    # O5: composite score (w1=0.5, w2=0.5)
    merged['fetch_loss'] = 1 - merged['util']
    merged['ar_overhead'] = merged['m1a']
    merged['o5_score'] = -(0.5 * merged['fetch_loss'] + 0.5 * merged['ar_overhead'])

    return merged


def normalise_within_phase(df: pd.DataFrame, col: str) -> pd.Series:
    """Min-max normalise col within each (workload, seed, phase_index) group."""
    key = ['workload', 'seed', 'phase_index']
    grp_min = df.groupby(key)[col].transform('min')
    grp_max = df.groupby(key)[col].transform('max')
    rng = (grp_max - grp_min).where((grp_max - grp_min) > 0)
    return (df[col] - grp_min) / rng


def optimal_r_for_col(df: pd.DataFrame, col: str) -> pd.DataFrame:
    """Return DataFrame with columns [workload, seed, phase_index, optimal_r_{col}]."""
    key = ['workload', 'seed', 'phase_index']
    active = df[df['ar_64'] > 0].copy()
    idx = active.groupby(key)[col].idxmax()
    result = active.loc[idx, key + ['region_size']].rename(
        columns={'region_size': f'optimal_r_{col}'}
    ).reset_index(drop=True)
    return result


def main():
    df = load_all_runs(str(DATA_DIR))
    frame = build_phase_frame(df)
    active = frame[frame['ar_64'] > 0].copy()

    print("=== Hybrid: O2 + O5 ===\n")

    # Normalise O2 and O5 within each phase
    active['o2_norm'] = normalise_within_phase(active, 'o2_efficiency')
    active['o5_norm'] = normalise_within_phase(active, 'o5_score')

    # Hybrid scores at different alpha values
    print("Hybrid optimal R distribution (alpha * O2 + (1-alpha) * O5):")
    key = ['workload', 'seed', 'phase_index']
    for alpha in [0.0, 0.25, 0.5, 0.75, 1.0]:
        active['hybrid'] = alpha * active['o2_norm'] + (1 - alpha) * active['o5_norm']
        idx = active.groupby(key)['hybrid'].idxmax()
        opt_r = active.loc[idx, 'region_size']
        dist = dict(opt_r.value_counts())
        # Entropy
        counts = opt_r.value_counts()
        total_n = len(opt_r)
        probs = counts / total_n
        h = -sum(p * math.log(p) for p in probs if p > 0)
        print(f"  alpha={alpha:.2f}: dist={dist}, entropy={h:.4f}")

    print()

    # Phase-level hybrid (alpha=0.5) scores
    active['hybrid_05'] = 0.5 * active['o2_norm'] + 0.5 * active['o5_norm']
    print("Hybrid (alpha=0.5) score per phase — showing optimal R:")
    for (wl, ph), grp in active.groupby(['workload', 'phase_index']):
        scores = grp.groupby('region_size')['hybrid_05'].mean()
        opt = scores.idxmax()
        score_strs = ", ".join(f"R={r}: {v:.4f}" for r, v in scores.items())
        print(f"  {wl} phase {ph}: [{score_strs}] → R={opt}")

    print()

    # ── Correlation analysis ──────────────────────────────────────────────────
    print("=== Optimal-R Correlation Between Options ===\n")

    opt_o2 = optimal_r_for_col(active, 'o2_efficiency')
    opt_o5 = optimal_r_for_col(active, 'o5_score')

    merged_opts = opt_o2.merge(opt_o5, on=key)
    agree = (merged_opts['optimal_r_o2_efficiency'] == merged_opts['optimal_r_o5_score']).mean()
    print(f"O2 vs O5 optimal_r agreement: {agree:.3f} ({int(agree * len(merged_opts))}/{len(merged_opts)})")
    print(f"O2 distribution: {dict(merged_opts['optimal_r_o2_efficiency'].value_counts())}")
    print(f"O5 distribution: {dict(merged_opts['optimal_r_o5_score'].value_counts())}")

    print()

    # Spearman rank correlation between raw metrics and O2 optimal_r
    print("=== Raw metric correlation with O2 efficiency ===")
    per_phase_agg = (
        active.groupby(key + ['region_size'])
              .agg(
                  o2_efficiency=('o2_efficiency', 'mean'),
                  active_regions=('active_regions', 'mean'),
                  util=('util', 'mean'),
                  m1a=('m1a', 'mean'),
                  m5a=('m5a', 'mean'),
                  l2hr=('l2hr', 'mean'),
              )
              .reset_index()
    )
    for m in ['active_regions', 'util', 'm1a', 'm5a', 'l2hr']:
        valid = per_phase_agg[['o2_efficiency', m]].dropna()
        corr = valid.corr(method='spearman').iloc[0, 1]
        print(f"  spearman(o2_efficiency, {m}) = {corr:.4f}")

    print()

    # Summary: which metric best predicts O2 optimal_r?
    print("=== Phase-pair analysis: which workloads show inter-phase optimal-R variation? ===")
    o2_opt = optimal_r_for_col(active, 'o2_efficiency')
    for wl, grp in o2_opt.groupby('workload'):
        phases = sorted(grp['phase_index'].unique())
        opt_rs = {int(row['phase_index']): int(row['optimal_r_o2_efficiency'])
                  for _, row in grp.iterrows()}
        unique_rs = len(set(opt_rs.values()))
        print(f"  {wl}: phases={phases}, optimal_r_by_phase={opt_rs}, unique_R={unique_rs}")


if __name__ == '__main__':
    main()
