import re
import warnings
from pathlib import Path

import pandas as pd
import pyarrow.parquet as pq

EXPECTED_WORKLOADS = frozenset({
    'simpleconvolution', 'matrixtranspose', 'matrixmultiplication',
    'pagerank', 'fir', 'stencil2d',
})
EXPECTED_REGIONS = frozenset({64, 256, 1024, 4096, 16384})
EXPECTED_SEEDS = frozenset({42, 43, 44})

_FNAME_RE = re.compile(
    r'^(?P<workload>[a-z][a-z0-9]*)_R(?P<region>\d+)_seed(?P<seed>\d+)\.parquet$'
)


def load_all_runs(data_dir: str) -> pd.DataFrame:
    """Load all parquet files in data_dir into a single DataFrame.

    Adds columns: workload (str), region_size (int), seed (int).
    Drops internal config_id / workload_id columns.
    """
    data_dir = Path(data_dir)
    frames = []
    for path in sorted(data_dir.glob('*.parquet')):
        m = _FNAME_RE.match(path.name)
        if not m:
            continue
        workload = m.group('workload')
        region_size = int(m.group('region'))
        seed = int(m.group('seed'))
        tbl = pq.read_table(path)
        df = tbl.to_pandas()
        df['workload'] = workload
        df['region_size'] = region_size
        df['seed'] = seed
        frames.append(df)
    if not frames:
        raise FileNotFoundError(f'No M1 parquet files found in {data_dir}')
    result = pd.concat(frames, ignore_index=True)
    result = result.drop(columns=['config_id', 'workload_id'], errors='ignore')
    # Ensure consistent dtypes for key columns
    result['region_size'] = result['region_size'].astype('int64')
    result['seed'] = result['seed'].astype('int64')
    return result


def get_workload_df(df: pd.DataFrame, workload: str) -> pd.DataFrame:
    return df[df['workload'] == workload].copy()


def get_config_df(df: pd.DataFrame, workload: str, region_size: int, seed: int) -> pd.DataFrame:
    mask = (
        (df['workload'] == workload) &
        (df['region_size'] == region_size) &
        (df['seed'] == seed)
    )
    return df[mask].copy()


def validate_load(df: pd.DataFrame) -> bool:
    """Check completeness and basic integrity of the loaded DataFrame.

    Prints warnings for any issues found. Returns True if all checks pass.
    """
    ok = True

    combos = df.groupby(['workload', 'region_size', 'seed']).size()
    expected_n = len(EXPECTED_WORKLOADS) * len(EXPECTED_REGIONS) * len(EXPECTED_SEEDS)
    if len(combos) != expected_n:
        warnings.warn(
            f'Expected {expected_n} (workload, R, seed) combinations, got {len(combos)}'
        )
        ok = False

    missing = []
    for w in EXPECTED_WORKLOADS:
        for r in EXPECTED_REGIONS:
            for s in EXPECTED_SEEDS:
                if (w, r, s) not in combos.index:
                    missing.append((w, r, s))
    if missing:
        warnings.warn(f'Missing runs: {missing}')
        ok = False

    rwf_max = df.groupby(['workload', 'region_size', 'seed'])['retired_wavefronts'].max()
    zero_rwf = rwf_max[rwf_max == 0]
    if not zero_rwf.empty:
        warnings.warn(f'Runs with RetiredWavefronts=0 in all phases: {list(zero_rwf.index)}')
        ok = False

    phase_counts = df.groupby(['workload', 'region_size', 'seed'])['phase_index'].count()
    zero_phase = phase_counts[phase_counts == 0]
    if not zero_phase.empty:
        warnings.warn(f'Runs with no phase rows: {list(zero_phase.index)}')
        ok = False

    return ok
