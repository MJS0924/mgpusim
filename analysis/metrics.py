"""Derived metrics for M1 phase-level data."""

import numpy as np
import pandas as pd


# ──────────────────────────────────────────────────────────────────────────────
# Per-row (phase-level) scalar metrics
# ──────────────────────────────────────────────────────────────────────────────

def l2_hit_rate(df: pd.DataFrame) -> pd.Series:
    """L2H / (L2H + L2M). NaN when both are zero."""
    total = df['l2_hits'] + df['l2_misses']
    return df['l2_hits'].where(total > 0) / total.where(total > 0)


def region_utilization(df: pd.DataFrame) -> pd.Series:
    """RegionAccessedBytes / RegionFetchedBytes. NaN when fetched == 0."""
    f = df['region_fetched_bytes']
    return df['region_accessed_bytes'].where(f > 0) / f.where(f > 0)


def sharer_consistency_rate(df: pd.DataFrame) -> pd.Series:
    """SharerConsistentRegions / ActiveRegions. NaN when active == 0."""
    a = df['active_regions']
    return df['sharer_consistent_regions'].where(a > 0) / a.where(a > 0)


def add_derived_metrics(df: pd.DataFrame) -> pd.DataFrame:
    """Return a copy of df with l2hr, utilization, scr columns added."""
    out = df.copy()
    out['l2hr'] = l2_hit_rate(out)
    out['utilization'] = region_utilization(out)
    out['scr'] = sharer_consistency_rate(out)
    return out


def is_active_phase(df: pd.DataFrame) -> pd.Series:
    """True for phases with meaningful simulation activity."""
    return (
        (df['l2_hits'] + df['l2_misses'] + df['active_regions']) > 0
    )


# ──────────────────────────────────────────────────────────────────────────────
# Per-(workload, region_size, seed) aggregate
# ──────────────────────────────────────────────────────────────────────────────

def aggregate_run(df: pd.DataFrame) -> pd.DataFrame:
    """Sum L2/region metrics across phases; take max of RetiredWavefronts."""
    return (
        df.groupby(['workload', 'region_size', 'seed'])
        .agg(
            l2_hits=('l2_hits', 'sum'),
            l2_misses=('l2_misses', 'sum'),
            region_fetched_bytes=('region_fetched_bytes', 'sum'),
            region_accessed_bytes=('region_accessed_bytes', 'sum'),
            active_regions=('active_regions', 'sum'),
            sharer_consistent_regions=('sharer_consistent_regions', 'sum'),
            retired_wavefronts=('retired_wavefronts', 'max'),
            phase_count=('phase_index', 'count'),
        )
        .reset_index()
    )


def seed_average(agg_df: pd.DataFrame) -> pd.DataFrame:
    """Average numeric columns over the seed dimension."""
    numeric_cols = [
        'l2_hits', 'l2_misses', 'region_fetched_bytes', 'region_accessed_bytes',
        'active_regions', 'sharer_consistent_regions', 'retired_wavefronts',
        'phase_count',
    ]
    present = [c for c in numeric_cols if c in agg_df.columns]
    return (
        agg_df.groupby(['workload', 'region_size'])[present]
        .mean()
        .reset_index()
    )


# ──────────────────────────────────────────────────────────────────────────────
# Cross-R: optimal region size per (workload, seed, phase_index)
# ──────────────────────────────────────────────────────────────────────────────

def optimal_region_per_phase(df: pd.DataFrame) -> pd.DataFrame:
    """For each (workload, seed, phase_index), find the R maximising each metric.

    Returns a DataFrame with columns:
        workload, seed, phase_index,
        optimal_r_l2hr, optimal_r_util, optimal_r_scr
    Only includes rows where the phase has any activity.
    """
    df = add_derived_metrics(df)
    active = df[is_active_phase(df)].copy()

    if active.empty:
        cols = ['workload', 'seed', 'phase_index',
                'optimal_r_l2hr', 'optimal_r_util', 'optimal_r_scr']
        return pd.DataFrame(columns=cols)

    records = []
    for (workload, seed, phase_idx), grp in active.groupby(['workload', 'seed', 'phase_index']):
        # grp has one row per region_size that was run for this workload/seed/phase

        def best_r(metric_col: str) -> int:
            valid = grp.dropna(subset=[metric_col])
            if valid.empty:
                return int(grp['region_size'].min())
            idx = valid[metric_col].idxmax()
            return int(valid.loc[idx, 'region_size'])

        records.append({
            'workload': workload,
            'seed': seed,
            'phase_index': phase_idx,
            'optimal_r_l2hr': best_r('l2hr'),
            'optimal_r_util': best_r('utilization'),
            'optimal_r_scr': best_r('scr'),
        })

    return pd.DataFrame(records)
