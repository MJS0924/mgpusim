"""Tier B+: Option 3 prototype — M1a decay slope classification.

Metric: M1a decay slope = log(M1a@R16384) / log(R16384/R64)
        = log(AR_16384 / AR_64) / log(256)

Hypothesis: phases with slow AR decay (poor spatial coalescing, scattered accesses)
prefer small R; phases with fast AR decay (strong coalescing) can tolerate large R.

Optimal R per phase:
  - If M1a@R16384 > slow_threshold (e.g., 0.05): phase is poorly coalescing → R=64
  - If M1a@R16384 < fast_threshold (e.g., 0.01): phase is highly coalescing → R=16384
  - Otherwise: R=1024 (middle ground)

This is threshold-based classification, not argmax optimisation.
"""

import sys, math
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'

SLOW_THRESHOLD = 0.05   # M1a@16384 > 0.05 → scatter-heavy → R=64
FAST_THRESHOLD = 0.01   # M1a@16384 < 0.01 → coalesce-heavy → R=16384


def compute_m1a_at_rmax(df: pd.DataFrame) -> pd.DataFrame:
    """Compute M1a at R=16384 per (workload, seed, phase_index)."""
    base = df[df['region_size'] == 64][
        ['workload', 'seed', 'phase_index', 'active_regions']
    ].rename(columns={'active_regions': 'ar_64'})
    top = df[df['region_size'] == 16384][
        ['workload', 'seed', 'phase_index', 'active_regions']
    ].rename(columns={'active_regions': 'ar_16384'})
    merged = base.merge(top, on=['workload', 'seed', 'phase_index'], how='inner')
    merged['m1a_max'] = merged['ar_16384'] / merged['ar_64'].where(merged['ar_64'] > 0)
    merged['log_decay'] = merged['m1a_max'].apply(
        lambda x: math.log(x) / math.log(16384 / 64) if x > 0 else float('-inf')
    )
    return merged


def classify_optimal_r(m1a_max: float, slow_thr: float, fast_thr: float) -> int:
    if m1a_max > slow_thr:
        return 64
    elif m1a_max < fast_thr:
        return 16384
    else:
        return 1024


def run_p1_prime(df: pd.DataFrame, slow_thr: float, fast_thr: float,
                 threshold: float = 0.5) -> dict:
    decay_df = compute_m1a_at_rmax(df)
    active = decay_df[decay_df['ar_64'] > 0].copy()
    active['optimal_r'] = active['m1a_max'].apply(
        lambda x: classify_optimal_r(x, slow_thr, fast_thr)
    )

    entropies = []
    for (wl, seed), grp in active.groupby(['workload', 'seed']):
        if len(grp) < 2:
            continue
        counts = grp['optimal_r'].value_counts()
        probs = counts / len(grp)
        h = -sum(p * math.log(p) for p in probs if p > 0)
        entropies.append(h)

    mean_h = sum(entropies) / len(entropies) if entropies else 0.0
    status = 'PASS' if mean_h >= threshold else 'FAIL'
    opt_dist = dict(active['optimal_r'].value_counts())
    return {'status': status, 'value': round(mean_h, 4),
            'detail': f'entropy={mean_h:.4f} over {len(entropies)} pairs',
            'opt_r_distribution': opt_dist}


def run_p3_prime(df: pd.DataFrame, slow_thr: float, fast_thr: float,
                 threshold: float = 0.60, min_wl: int = 3) -> dict:
    decay_df = compute_m1a_at_rmax(df)
    active = decay_df[decay_df['ar_64'] > 0].copy()
    active['optimal_r'] = active['m1a_max'].apply(
        lambda x: classify_optimal_r(x, slow_thr, fast_thr)
    )

    pass_wl = 0
    details = {}
    for wl, grp in active.groupby('workload'):
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
    decay_df = compute_m1a_at_rmax(df)
    active = decay_df[decay_df['ar_64'] > 0].copy()

    print("=== Option 3: M1a decay slope classification ===\n")
    print("M1a@R=16384 per (workload, seed, phase_index):")
    print(active[['workload', 'seed', 'phase_index', 'ar_64', 'ar_16384',
                   'm1a_max', 'log_decay']].to_string(index=False))
    print()

    print(f"Classification thresholds: slow>{SLOW_THRESHOLD} → R=64, "
          f"fast<{FAST_THRESHOLD} → R=16384, else R=1024")
    active['optimal_r'] = active['m1a_max'].apply(
        lambda x: classify_optimal_r(x, SLOW_THRESHOLD, FAST_THRESHOLD)
    )
    print(f"\nOptimal R per phase:")
    print(active[['workload', 'seed', 'phase_index', 'm1a_max', 'optimal_r']].to_string(index=False))

    p1 = run_p1_prime(df, SLOW_THRESHOLD, FAST_THRESHOLD)
    p3 = run_p3_prime(df, SLOW_THRESHOLD, FAST_THRESHOLD)

    print(f"\nP1' status: {p1['status']} | value={p1['value']} | {p1['detail']}")
    print(f"  opt_r distribution: {p1.get('opt_r_distribution', {})}")
    print()
    print(f"P3' status: {p3['status']} | value={p3['value']} | {p3['detail']}")
    for wl, d in p3.get('workload_details', {}).items():
        print(f"  {wl}: mode_r={d['mode_r']}, mode_frac={d['mode_frac']}, dist={d['distribution']}")

    # Threshold sweep
    print("\n=== Threshold sweep ===")
    for slow in [0.02, 0.05, 0.10, 0.15, 0.20]:
        for fast in [0.005, 0.01]:
            p1 = run_p1_prime(df, slow, fast)
            p3 = run_p3_prime(df, slow, fast)
            print(f"  slow={slow}, fast={fast}: "
                  f"P1'={p1['status']}({p1['value']}), "
                  f"P3'={p3['status']}({p3['value']}/6), "
                  f"dist={p1.get('opt_r_distribution', {})}")


if __name__ == '__main__':
    main()
