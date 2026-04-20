"""Tier B+: Option 5 prototype — Composite cost model.

Metric: score(R) = -w1 * fetch_loss(R) - w2 * ar_overhead(R)
        fetch_loss(R) = 1 - region_accessed / region_fetched  (wasted fetch fraction)
        ar_overhead(R) = active_regions(R) / active_regions(R=64)  (= M1a, relative region count)

Both terms decrease the score. The optimal R maximises score by balancing:
  - low fetch_loss (prefer large R → less per-region overhead)

Wait: actually fetch_loss = 1 - util = 1 - accessed/fetched
  - As R increases, fetched increases, accessed stays constant → fetch_loss increases ↑
  - ar_overhead = M1a = AR_R / AR_64 → decreases ↓ with R

So:
  - w1 * fetch_loss: penalises large R
  - w2 * ar_overhead: penalises small R (high region count)
  → Natural trade-off that may produce interior optimum!

Default: w1=w2=0.5 (equal weight)
"""

import sys, math
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'
W1 = 0.5  # weight for fetch_loss
W2 = 0.5  # weight for ar_overhead


def compute_composite(df: pd.DataFrame, w1: float, w2: float) -> pd.DataFrame:
    base = df[df['region_size'] == 64][
        ['workload', 'seed', 'phase_index', 'active_regions']
    ].rename(columns={'active_regions': 'ar_64'})
    merged = df.merge(base, on=['workload', 'seed', 'phase_index'], how='left')

    util = merged['region_accessed_bytes'] / merged['region_fetched_bytes'].where(
        merged['region_fetched_bytes'] > 0
    )
    merged['fetch_loss'] = 1 - util
    merged['ar_overhead'] = merged['active_regions'] / merged['ar_64'].where(
        merged['ar_64'] > 0
    )
    merged['score'] = -(w1 * merged['fetch_loss'] + w2 * merged['ar_overhead'])
    return merged


def optimal_r_by_composite(grp: pd.DataFrame) -> int:
    if grp.empty:
        return 64
    idx = grp['score'].idxmax()
    return int(grp.loc[idx, 'region_size'])


def run_p1_prime(df, w1, w2, threshold=0.5):
    comp_df = compute_composite(df, w1, w2)
    key = ['workload', 'seed', 'phase_index']
    active = comp_df[comp_df['ar_64'] > 0]

    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_composite(grp)
        opt_rows.append({'workload': gkey[0], 'seed': gkey[1],
                         'phase_index': gkey[2], 'optimal_r': opt_r})

    opt_df = pd.DataFrame(opt_rows) if opt_rows else pd.DataFrame(
        columns=['workload', 'seed', 'phase_index', 'optimal_r'])

    entropies = []
    for (wl, seed), grp in opt_df.groupby(['workload', 'seed']):
        if len(grp) < 2:
            continue
        counts = grp['optimal_r'].value_counts()
        probs = counts / len(grp)
        h = -sum(p * math.log(p) for p in probs if p > 0)
        entropies.append(h)

    mean_h = sum(entropies) / len(entropies) if entropies else 0.0
    return {'status': 'PASS' if mean_h >= threshold else 'FAIL',
            'value': round(mean_h, 4),
            'detail': f'entropy={mean_h:.4f} over {len(entropies)} pairs',
            'opt_r_distribution': dict(opt_df['optimal_r'].value_counts())}


def run_p3_prime(df, w1, w2, threshold=0.60, min_wl=3):
    comp_df = compute_composite(df, w1, w2)
    key = ['workload', 'seed', 'phase_index']
    active = comp_df[comp_df['ar_64'] > 0]

    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_composite(grp)
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

    print("=== Option 5: Composite cost model ===\n")

    # Show score curves
    comp_df = compute_composite(df, W1, W2)
    active = comp_df[comp_df['ar_64'] > 0]
    print(f"Score curves (w1={W1}, w2={W2}) per (workload, phase_index, region_size):")
    for (wl, ph), grp in active.groupby(['workload', 'phase_index']):
        scores = grp.groupby('region_size')['score'].mean()
        opt = scores.idxmax()
        print(f"  {wl} phase {ph}: " +
              ", ".join(f"R={r}: {v:.4f}" for r, v in scores.items()) +
              f" → optimal_r={opt}")
    print()

    p1 = run_p1_prime(df, W1, W2)
    p3 = run_p3_prime(df, W1, W2)

    print(f"P1' (w1={W1}, w2={W2}): {p1['status']} | value={p1['value']} | {p1['detail']}")
    print(f"  opt_r distribution: {p1.get('opt_r_distribution', {})}")
    print()
    print(f"P3' (w1={W1}, w2={W2}): {p3['status']} | value={p3['value']} | {p3['detail']}")
    for wl, d in p3.get('workload_details', {}).items():
        print(f"  {wl}: mode_r={d['mode_r']}, mode_frac={d['mode_frac']}, dist={d['distribution']}")

    # Weight sweep
    print("\n=== Weight sweep ===")
    for w1 in [0.1, 0.3, 0.5, 0.7, 0.9]:
        w2 = 1.0 - w1
        p1 = run_p1_prime(df, w1, w2)
        p3 = run_p3_prime(df, w1, w2)
        print(f"  w1={w1:.1f}, w2={w2:.1f}: "
              f"P1'={p1['status']}({p1['value']}), "
              f"P3'={p3['status']}({p3['value']}/6), "
              f"dist={p1.get('opt_r_distribution', {})}")


if __name__ == '__main__':
    main()
