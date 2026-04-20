"""Tests for propositions module — synthetic data boundary checks."""

import pandas as pd
import pytest

from analysis.propositions import (
    p1_window_entropy,
    p2_three_metric_agreement,
    p3_phase_mode_bound,
    p4_ds_mode_bound,
    p5_joint_entropy_reduction,
    p6_track_ab_agreement,
    verify_all,
)


def make_row(workload='w', region_size=64, seed=42, phase_index=1,
             l2_hits=0, l2_misses=0, region_fetched_bytes=0, region_accessed_bytes=0,
             active_regions=0, sharer_consistent_regions=0, retired_wavefronts=0,
             directory_evictions=0, start_cycle=100000, end_cycle=200000):
    return dict(
        workload=workload, region_size=region_size, seed=seed, phase_index=phase_index,
        start_cycle=start_cycle, end_cycle=end_cycle,
        l2_hits=l2_hits, l2_misses=l2_misses,
        region_fetched_bytes=region_fetched_bytes, region_accessed_bytes=region_accessed_bytes,
        active_regions=active_regions, sharer_consistent_regions=sharer_consistent_regions,
        retired_wavefronts=retired_wavefronts, directory_evictions=directory_evictions,
    )


def make_df(rows):
    return pd.DataFrame(rows)


# ── P1: window entropy ────────────────────────────────────────────────────────

def test_p1_fail_single_phase():
    # Only 1 active phase per (workload, seed) → entropy=0 → FAIL
    rows = [make_row(region_size=r, l2_hits=5, l2_misses=5,
                     region_fetched_bytes=200, region_accessed_bytes=100,
                     active_regions=3, sharer_consistent_regions=3)
            for r in [64, 256, 1024, 4096, 16384]]
    df = make_df(rows)
    result = p1_window_entropy(df, threshold=0.5)
    assert result['status'] == 'FAIL'
    assert result['value'] == 0.0


def test_p1_pass_varying_phases():
    # 2 active phases with different optimal R → entropy > 0
    rows = []
    # phase 1: high l2hr at R=64
    for r in [64, 256, 1024, 4096, 16384]:
        hits = 90 if r == 64 else 10
        rows.append(make_row(region_size=r, phase_index=1,
                              l2_hits=hits, l2_misses=100 - hits,
                              region_fetched_bytes=200, region_accessed_bytes=100,
                              active_regions=3, sharer_consistent_regions=3))
    # phase 2: high l2hr at R=4096
    for r in [64, 256, 1024, 4096, 16384]:
        hits = 10 if r != 4096 else 90
        rows.append(make_row(region_size=r, phase_index=2,
                              l2_hits=hits, l2_misses=100 - hits,
                              region_fetched_bytes=200, region_accessed_bytes=100,
                              active_regions=3, sharer_consistent_regions=3))
    df = make_df(rows)
    result = p1_window_entropy(df, threshold=0.5)
    assert result['status'] == 'PASS'
    assert result['value'] > 0.5


# ── P2: 3-metric agreement ────────────────────────────────────────────────────

def test_p2_pass_all_agree():
    # All 3 metrics maximised by R=256 (no ties for any metric)
    rows = [make_row(region_size=r, phase_index=1,
                     l2_hits=(90 if r == 256 else 10), l2_misses=10,
                     region_fetched_bytes=200,
                     region_accessed_bytes=(190 if r == 256 else 50),
                     active_regions=5,
                     sharer_consistent_regions=(5 if r == 256 else 3))
            for r in [64, 256, 1024, 4096, 16384]]
    df = make_df(rows)
    result = p2_three_metric_agreement(df, threshold=0.70)
    assert result['status'] == 'PASS'
    assert result['value'] == pytest.approx(1.0)


def test_p2_fail_disagree():
    # l2hr+util prefer R=64; scr prefers R=4096 (distinct best R for SCR)
    rows = [
        make_row(region_size=64,   phase_index=1, l2_hits=90, l2_misses=10,
                 region_fetched_bytes=100, region_accessed_bytes=90,
                 active_regions=10, sharer_consistent_regions=4),   # scr=0.4
        make_row(region_size=256,  phase_index=1, l2_hits=50, l2_misses=50,
                 region_fetched_bytes=200, region_accessed_bytes=100,
                 active_regions=10, sharer_consistent_regions=6),   # scr=0.6
        make_row(region_size=1024, phase_index=1, l2_hits=20, l2_misses=80,
                 region_fetched_bytes=500, region_accessed_bytes=100,
                 active_regions=10, sharer_consistent_regions=7),   # scr=0.7
        make_row(region_size=4096, phase_index=1, l2_hits=5, l2_misses=95,
                 region_fetched_bytes=1000, region_accessed_bytes=100,
                 active_regions=10, sharer_consistent_regions=10),  # scr=1.0
        make_row(region_size=16384, phase_index=1, l2_hits=1, l2_misses=99,
                 region_fetched_bytes=4000, region_accessed_bytes=100,
                 active_regions=10, sharer_consistent_regions=8),   # scr=0.8
    ]
    df = make_df(rows)
    result = p2_three_metric_agreement(df, threshold=0.70)
    # l2hr→R=64, util→R=64, scr→R=4096 → disagree
    assert result['status'] == 'FAIL'


# ── P3: phase mode bound ──────────────────────────────────────────────────────

def test_p3_fail_single_r_dominates():
    # All phases choose the same R → mode_frac = 1.0 → FAIL
    rows = [make_row(region_size=r, phase_index=1,
                     l2_hits=90 if r == 64 else 10, l2_misses=10,
                     region_fetched_bytes=200, region_accessed_bytes=100,
                     active_regions=3, sharer_consistent_regions=3)
            for r in [64, 256, 1024, 4096, 16384]]
    # Only 1 phase → mode is that phase's optimal R → fraction=1.0
    df = make_df(rows)
    result = p3_phase_mode_bound(df, threshold=0.60, min_workloads=1)
    assert result['status'] == 'FAIL'


def test_p3_pass_diverse_phases():
    # 5 phases each choosing a different R
    rows = []
    region_sizes = [64, 256, 1024, 4096, 16384]
    for phase_idx, best_r in enumerate(region_sizes, start=1):
        for r in region_sizes:
            hits = 90 if r == best_r else 10
            rows.append(make_row(region_size=r, phase_index=phase_idx,
                                  l2_hits=hits, l2_misses=100 - hits,
                                  region_fetched_bytes=200, region_accessed_bytes=100,
                                  active_regions=3, sharer_consistent_regions=3))
    df = make_df(rows)
    result = p3_phase_mode_bound(df, threshold=0.60, min_workloads=1)
    assert result['status'] == 'PASS'


# ── P4~P6 always SKIP ─────────────────────────────────────────────────────────

def test_p4_skip():
    assert p4_ds_mode_bound()['status'] == 'SKIP'


def test_p5_skip():
    assert p5_joint_entropy_reduction()['status'] == 'SKIP'


def test_p6_skip():
    assert p6_track_ab_agreement()['status'] == 'SKIP'


# ── verify_all ────────────────────────────────────────────────────────────────

def test_verify_all_keys():
    df = make_df([make_row()])
    result = verify_all(df)
    for key in ['P1', 'P2', 'P3', 'P4', 'P5', 'P6']:
        assert key in result
        assert 'status' in result[key]


def test_verify_all_p4_p5_p6_skip():
    df = make_df([make_row()])
    result = verify_all(df)
    assert result['P4']['status'] == 'SKIP'
    assert result['P5']['status'] == 'SKIP'
    assert result['P6']['status'] == 'SKIP'
