"""Tests for load_data module."""

import os
import warnings
from pathlib import Path

import pandas as pd
import pytest

from analysis.load_data import (
    EXPECTED_REGIONS,
    EXPECTED_SEEDS,
    EXPECTED_WORKLOADS,
    load_all_runs,
    validate_load,
    get_workload_df,
    get_config_df,
)

DATA_DIR = Path('results/m1/d2/raw')


@pytest.fixture(scope='module')
def df():
    return load_all_runs(str(DATA_DIR))


def test_load_returns_dataframe(df):
    assert isinstance(df, pd.DataFrame)


def test_90_runs_present(df):
    combos = df.groupby(['workload', 'region_size', 'seed']).size()
    expected = len(EXPECTED_WORKLOADS) * len(EXPECTED_REGIONS) * len(EXPECTED_SEEDS)
    assert len(combos) == expected, f'Expected {expected} runs, got {len(combos)}'


def test_total_row_count(df):
    # Phase counts from D.1: simpleconv=3, matrixtranspose=2, matrixmul=4, pagerank=3, fir=2, stencil2d=3
    # 15 runs per workload; sum = (3+2+4+3+2+3) * 15 = 17*15 = 255
    phase_sums = {'simpleconvolution': 3, 'matrixtranspose': 2, 'matrixmultiplication': 4,
                  'pagerank': 3, 'fir': 2, 'stencil2d': 3}
    expected_rows = sum(v * 15 for v in phase_sums.values())
    assert len(df) == expected_rows, f'Expected {expected_rows} rows, got {len(df)}'


def test_required_columns_present(df):
    required = [
        'workload', 'region_size', 'seed', 'phase_index',
        'start_cycle', 'end_cycle',
        'l2_hits', 'l2_misses',
        'region_fetched_bytes', 'region_accessed_bytes',
        'active_regions', 'sharer_consistent_regions',
        'retired_wavefronts', 'directory_evictions',
    ]
    for col in required:
        assert col in df.columns, f'Missing column: {col}'


def test_internal_ids_dropped(df):
    assert 'config_id' not in df.columns
    assert 'workload_id' not in df.columns


def test_column_types(df):
    assert df['workload'].dtype == object
    assert df['region_size'].dtype in ('int64', 'int32')
    assert df['seed'].dtype in ('int64', 'int32')
    assert df['l2_hits'].dtype in ('uint64', 'int64')
    assert df['retired_wavefronts'].dtype in ('uint64', 'int64')


def test_validate_load_passes(df):
    with warnings.catch_warnings():
        warnings.simplefilter('error')
        assert validate_load(df) is True


def test_get_workload_df(df):
    wdf = get_workload_df(df, 'pagerank')
    assert set(wdf['workload'].unique()) == {'pagerank'}
    assert len(wdf) > 0


def test_get_config_df(df):
    cdf = get_config_df(df, 'fir', 1024, 42)
    assert len(cdf) > 0
    assert (cdf['workload'] == 'fir').all()
    assert (cdf['region_size'] == 1024).all()
    assert (cdf['seed'] == 42).all()


def test_retired_wavefronts_nonzero_per_run(df):
    rwf_max = df.groupby(['workload', 'region_size', 'seed'])['retired_wavefronts'].max()
    assert (rwf_max > 0).all(), 'Some runs have RetiredWavefronts=0 in all phases'
