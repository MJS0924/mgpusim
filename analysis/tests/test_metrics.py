"""Tests for metrics module."""

import numpy as np
import pandas as pd
import pytest

from analysis.metrics import (
    add_derived_metrics,
    aggregate_run,
    is_active_phase,
    l2_hit_rate,
    optimal_region_per_phase,
    region_utilization,
    seed_average,
    sharer_consistency_rate,
)


def make_phase_row(**kwargs):
    defaults = dict(
        workload='test', region_size=64, seed=42, phase_index=0,
        start_cycle=0, end_cycle=100000,
        l2_hits=0, l2_misses=0,
        region_fetched_bytes=0, region_accessed_bytes=0,
        active_regions=0, sharer_consistent_regions=0,
        retired_wavefronts=0, directory_evictions=0,
    )
    defaults.update(kwargs)
    return defaults


def make_df(rows):
    return pd.DataFrame(rows)


# ── l2_hit_rate ──────────────────────────────────────────────────────────────

def test_l2hr_basic():
    df = make_df([make_phase_row(l2_hits=3, l2_misses=1)])
    assert l2_hit_rate(df).iloc[0] == pytest.approx(0.75)


def test_l2hr_zero_total_is_nan():
    df = make_df([make_phase_row(l2_hits=0, l2_misses=0)])
    assert np.isnan(l2_hit_rate(df).iloc[0])


def test_l2hr_all_hits():
    df = make_df([make_phase_row(l2_hits=100, l2_misses=0)])
    assert l2_hit_rate(df).iloc[0] == pytest.approx(1.0)


# ── region_utilization ───────────────────────────────────────────────────────

def test_util_basic():
    df = make_df([make_phase_row(region_fetched_bytes=200, region_accessed_bytes=100)])
    assert region_utilization(df).iloc[0] == pytest.approx(0.5)


def test_util_zero_fetched_is_nan():
    df = make_df([make_phase_row(region_fetched_bytes=0, region_accessed_bytes=0)])
    assert np.isnan(region_utilization(df).iloc[0])


def test_util_bounds():
    # utilization should be ≤ 1.0 for valid data
    df = make_df([
        make_phase_row(region_fetched_bytes=1000, region_accessed_bytes=500),
        make_phase_row(region_fetched_bytes=1000, region_accessed_bytes=1000),
    ])
    vals = region_utilization(df).dropna()
    assert (vals <= 1.0).all()
    assert (vals >= 0.0).all()


# ── sharer_consistency_rate ───────────────────────────────────────────────────

def test_scr_basic():
    df = make_df([make_phase_row(active_regions=10, sharer_consistent_regions=8)])
    assert sharer_consistency_rate(df).iloc[0] == pytest.approx(0.8)


def test_scr_zero_active_is_nan():
    df = make_df([make_phase_row(active_regions=0, sharer_consistent_regions=0)])
    assert np.isnan(sharer_consistency_rate(df).iloc[0])


# ── add_derived_metrics ───────────────────────────────────────────────────────

def test_add_derived_metrics_columns():
    df = make_df([make_phase_row(l2_hits=5, l2_misses=5,
                                  region_fetched_bytes=200, region_accessed_bytes=100,
                                  active_regions=4, sharer_consistent_regions=4)])
    out = add_derived_metrics(df)
    assert 'l2hr' in out.columns
    assert 'utilization' in out.columns
    assert 'scr' in out.columns
    assert out['l2hr'].iloc[0] == pytest.approx(0.5)
    assert out['utilization'].iloc[0] == pytest.approx(0.5)
    assert out['scr'].iloc[0] == pytest.approx(1.0)


# ── is_active_phase ───────────────────────────────────────────────────────────

def test_active_phase_detects_activity():
    df = make_df([
        make_phase_row(l2_hits=0, l2_misses=0, active_regions=0),
        make_phase_row(l2_hits=10, l2_misses=5, active_regions=0),
        make_phase_row(l2_hits=0, l2_misses=0, active_regions=3),
    ])
    active = is_active_phase(df)
    assert active.tolist() == [False, True, True]


# ── aggregate_run ─────────────────────────────────────────────────────────────

def test_aggregate_run_shape():
    rows = [
        make_phase_row(workload='a', region_size=64, seed=42, phase_index=0, l2_hits=10),
        make_phase_row(workload='a', region_size=64, seed=42, phase_index=1, l2_hits=20),
        make_phase_row(workload='a', region_size=64, seed=43, phase_index=0, l2_hits=15),
    ]
    df = make_df(rows)
    agg = aggregate_run(df)
    assert len(agg) == 2  # 2 (workload, R, seed) combos
    row_a42 = agg[(agg['workload'] == 'a') & (agg['seed'] == 42)]
    assert row_a42['l2_hits'].iloc[0] == 30  # sum of 10 + 20


def test_seed_average_shape():
    rows = [
        make_phase_row(workload='a', region_size=64, seed=42, l2_hits=10),
        make_phase_row(workload='a', region_size=64, seed=43, l2_hits=20),
        make_phase_row(workload='a', region_size=64, seed=44, l2_hits=30),
    ]
    df = make_df(rows)
    agg = aggregate_run(df)
    avg = seed_average(agg)
    assert len(avg) == 1
    assert avg['l2_hits'].iloc[0] == pytest.approx(20.0)


# ── optimal_region_per_phase ──────────────────────────────────────────────────

def test_optimal_region_per_phase_picks_max_l2hr():
    # phase_index=1 at R=64: l2hr=0.5; at R=256: l2hr=0.8 → optimal = 256
    rows = [
        make_phase_row(workload='w', region_size=64,  seed=42, phase_index=0),
        make_phase_row(workload='w', region_size=64,  seed=42, phase_index=1,
                       l2_hits=5, l2_misses=5, region_fetched_bytes=200, region_accessed_bytes=100,
                       active_regions=5, sharer_consistent_regions=5),
        make_phase_row(workload='w', region_size=256, seed=42, phase_index=0),
        make_phase_row(workload='w', region_size=256, seed=42, phase_index=1,
                       l2_hits=8, l2_misses=2, region_fetched_bytes=200, region_accessed_bytes=100,
                       active_regions=3, sharer_consistent_regions=3),
    ]
    df = make_df(rows)
    opt = optimal_region_per_phase(df)
    row = opt[(opt['workload'] == 'w') & (opt['seed'] == 42) & (opt['phase_index'] == 1)]
    assert len(row) == 1
    assert row['optimal_r_l2hr'].iloc[0] == 256
