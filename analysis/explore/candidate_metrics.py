"""Tier B: 5 candidate metrics for M1 redesign exploration.

Each metric is designed to vary with R at per-phase granularity,
unlike the current L2HR (R-invariant) and SCR (saturated=1.0).

Candidate metrics:
  M1a: ActiveRegions ratio  — active_regions_R / active_regions_R64
       Captures how region count scales with R (working set density).
  M2a: Fetched bytes variance across R within a phase
       High variance → phase is sensitive to R choice.
  M3a: Utilization variance across R within a phase
       (util = accessed/fetched; same motivation as M2a).
  M4a: Cross-phase active_regions overlap approximation
       Jaccard-like: min(AR_i, AR_j) / max(AR_i, AR_j) for consecutive phases.
  M5a: Per-region access density
       retired_wavefronts / active_regions — proxy for compute intensity per region.

Output:
  results/m1/e_explore/candidate_metrics.csv — per row (workload, R, seed, phase_index)
  results/m1/e_explore/candidate_metrics_agg.csv — aggregated over seeds
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import numpy as np
import pandas as pd
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'
OUT_RAW = Path(__file__).resolve().parents[2] / 'results/m1/e_explore/candidate_metrics.csv'
OUT_AGG = Path(__file__).resolve().parents[2] / 'results/m1/e_explore/candidate_metrics_agg.csv'


def compute_m1a(df: pd.DataFrame) -> pd.DataFrame:
    """M1a: active_regions_R / active_regions_R64 per (workload, seed, phase_index)."""
    base = df[df['region_size'] == 64][
        ['workload', 'seed', 'phase_index', 'active_regions']
    ].rename(columns={'active_regions': 'ar_base'})
    merged = df.merge(base, on=['workload', 'seed', 'phase_index'], how='left')
    merged['m1a'] = merged['active_regions'] / merged['ar_base'].where(merged['ar_base'] > 0)
    return merged.set_index(merged.index)['m1a']


def compute_m2a(df: pd.DataFrame) -> pd.Series:
    """M2a: std of region_fetched_bytes across R per (workload, seed, phase_index).
    Assigned to each row (same value for all R within a group)."""
    key = ['workload', 'seed', 'phase_index']
    std_ser = df.groupby(key)['region_fetched_bytes'].transform('std').fillna(0)
    return std_ser


def compute_m3a(df: pd.DataFrame) -> pd.Series:
    """M3a: std of utilization across R per (workload, seed, phase_index)."""
    util = df['region_accessed_bytes'] / df['region_fetched_bytes'].where(
        df['region_fetched_bytes'] > 0
    )
    df2 = df.copy()
    df2['_util'] = util
    key = ['workload', 'seed', 'phase_index']
    std_ser = df2.groupby(key)['_util'].transform('std').fillna(0)
    return std_ser


def compute_m4a(df: pd.DataFrame) -> pd.Series:
    """M4a: overlap between consecutive phases at same R.
    Approximated as min(AR_i, AR_{i+1}) / max(AR_i, AR_{i+1}) for adjacent phase_index.
    A value close to 1 means working set barely changes between phases (low phase contrast).
    """
    df2 = df.sort_values(['workload', 'region_size', 'seed', 'phase_index']).copy()
    key = ['workload', 'region_size', 'seed']
    ar = df2['active_regions']
    ar_next = df2.groupby(key)['active_regions'].shift(-1)
    numerator = pd.concat([ar, ar_next], axis=1).min(axis=1)
    denominator = pd.concat([ar, ar_next], axis=1).max(axis=1).where(
        pd.concat([ar, ar_next], axis=1).max(axis=1) > 0
    )
    m4a = numerator / denominator
    # Re-align to original index
    m4a_aligned = pd.Series(m4a.values, index=df2.index).reindex(df.index)
    return m4a_aligned


def compute_m5a(df: pd.DataFrame) -> pd.Series:
    """M5a: retired_wavefronts / active_regions — compute density per region."""
    m5a = df['retired_wavefronts'] / df['active_regions'].where(df['active_regions'] > 0)
    return m5a


def main():
    df = load_all_runs(str(DATA_DIR))

    # Base metrics
    total_l2 = df['l2_hits'] + df['l2_misses']
    df['l2hr'] = df['l2_hits'] / total_l2.where(total_l2 > 0)
    df['util'] = df['region_accessed_bytes'] / df['region_fetched_bytes'].where(
        df['region_fetched_bytes'] > 0
    )

    # Candidate metrics
    df['m1a'] = compute_m1a(df)
    df['m2a'] = compute_m2a(df)
    df['m3a'] = compute_m3a(df)
    df['m4a'] = compute_m4a(df)
    df['m5a'] = compute_m5a(df)

    raw_cols = [
        'workload', 'region_size', 'seed', 'phase_index',
        'active_regions', 'retired_wavefronts',
        'region_fetched_bytes', 'region_accessed_bytes',
        'l2hr', 'util',
        'm1a', 'm2a', 'm3a', 'm4a', 'm5a',
    ]
    raw = df[raw_cols].copy()

    OUT_RAW.parent.mkdir(parents=True, exist_ok=True)
    raw.to_csv(OUT_RAW, index=False)
    print(f"Wrote {len(raw)} rows to {OUT_RAW}")

    # Aggregated over seeds: mean per (workload, region_size, phase_index)
    agg = (
        raw.groupby(['workload', 'region_size', 'phase_index'])
           .agg(
               active_regions_mean=('active_regions', 'mean'),
               util_mean=('util', 'mean'),
               m1a_mean=('m1a', 'mean'),
               m2a_mean=('m2a', 'mean'),
               m3a_mean=('m3a', 'mean'),
               m4a_mean=('m4a', 'mean'),
               m5a_mean=('m5a', 'mean'),
           )
           .reset_index()
    )
    agg.to_csv(OUT_AGG, index=False)
    print(f"Wrote {len(agg)} rows to {OUT_AGG}")

    # ── Print analysis ─────────────────────────────────────────────────────────
    print("\n=== Candidate Metrics Analysis ===\n")

    metrics = ['m1a', 'm2a', 'm3a', 'm4a', 'm5a']
    labels = {
        'm1a': 'M1a: AR ratio (R/R64)',
        'm2a': 'M2a: fetched_bytes std across R (per phase)',
        'm3a': 'M3a: util std across R (per phase)',
        'm4a': 'M4a: consecutive-phase AR overlap',
        'm5a': 'M5a: wavefronts/active_regions density',
    }

    for m in metrics:
        valid = raw[m].dropna()
        print(f"--- {labels[m]} ---")
        print(f"  count={len(valid)}, mean={valid.mean():.4f}, std={valid.std():.4f}, "
              f"min={valid.min():.4f}, max={valid.max():.4f}")
        # Variation across R per (workload, seed, phase_index)?
        if m in ('m1a', 'm5a'):
            per_phase_std = raw.groupby(['workload', 'seed', 'phase_index'])[m].std().fillna(0)
            print(f"  std across R per phase: mean={per_phase_std.mean():.4f}, "
                  f"max={per_phase_std.max():.4f}, "
                  f"fraction_nonzero={(per_phase_std > 0.001).mean():.3f}")
        if m == 'm1a':
            print(f"  M1a per region_size (mean):")
            print(raw.groupby('region_size')['m1a'].mean().to_string())
            print(f"  M1a per (workload) at R=16384:")
            sub = raw[raw['region_size'] == 16384].groupby('workload')['m1a'].mean()
            print(sub.to_string())
        print()

    # Phase differentiation: does M1a differ between phases?
    print("=== M1a phase differentiation ===")
    for wl, grp in raw.groupby('workload'):
        by_phase = grp.groupby(['phase_index', 'region_size'])['m1a'].mean()
        phases = sorted(grp['phase_index'].unique())
        if len(phases) >= 2:
            print(f"\n{wl} (phases {phases}):")
            pivot = grp.pivot_table(
                values='m1a', index='region_size', columns='phase_index', aggfunc='mean'
            )
            print(pivot.to_string())

    # M5a phase differentiation
    print("\n=== M5a phase differentiation (wavefronts/active_regions) ===")
    for wl, grp in raw.groupby('workload'):
        phases = sorted(grp['phase_index'].unique())
        if len(phases) >= 2:
            print(f"\n{wl}:")
            pivot = grp.pivot_table(
                values='m5a', index='region_size', columns='phase_index', aggfunc='mean'
            )
            print(pivot.to_string())


if __name__ == '__main__':
    main()
