"""Tier B+: Option 1 prototype — Fetch-overhead budget threshold.

Redesigned metrics:
  P1': Distribution of per-phase optimal-R (by fetch_overhead_rate) has
       Shannon entropy > 0.5 nats across phases per (workload, seed).
  P2': L2HR (dropped), util_budget, M1a_decay agree on optimal-R in ≥ 70% phases.
  P3': In ≥ 3 workloads, modal optimal-R ≤ 60% of phases.

Definition:
  fetch_overhead_rate(phase, R) = (fetched_R - accessed) / accessed
  optimal_R(phase) = largest R where fetch_overhead_rate ≤ α (default α=0.5)

Rationale:
  - accessed_bytes is constant across R (confirmed D.2)
  - fetched_bytes increases with R
  - For phases with small working sets (low accessed), fetched grows fast → prefers smaller R
  - For phases with large working sets (high accessed), overhead stays low → can use larger R
"""

import sys, math
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'
REGION_SIZES = [64, 256, 1024, 4096, 16384]
ALPHA = 0.5  # fetch overhead budget: 50%


def fetch_overhead_rate(fetched: float, accessed: float) -> float:
    if accessed <= 0:
        return float('inf')
    return (fetched - accessed) / accessed


def optimal_r_by_budget(grp: pd.DataFrame, alpha: float) -> int:
    """Largest R where fetch_overhead_rate ≤ alpha. Falls back to R=64."""
    grp_sorted = grp.sort_values('region_size', ascending=False)
    for _, row in grp_sorted.iterrows():
        rate = fetch_overhead_rate(row['region_fetched_bytes'], row['region_accessed_bytes'])
        if rate <= alpha:
            return int(row['region_size'])
    return 64  # fallback


def run_p1_prime(df: pd.DataFrame, alpha: float, threshold: float = 0.5) -> dict:
    """P1': entropy of optimal-R distribution per (workload, seed)."""
    key = ['workload', 'seed', 'phase_index']
    active = df[df['region_accessed_bytes'] > 0].copy()

    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_budget(grp, alpha)
        opt_rows.append({'workload': gkey[0], 'seed': gkey[1],
                         'phase_index': gkey[2], 'optimal_r': opt_r})
    if not opt_rows:
        return {'status': 'FAIL', 'value': 0.0, 'detail': 'no active phases'}

    opt_df = pd.DataFrame(opt_rows)

    entropies = []
    for (wl, seed), grp in opt_df.groupby(['workload', 'seed']):
        if len(grp) < 2:
            continue  # need ≥2 phases for meaningful entropy
        counts = grp['optimal_r'].value_counts()
        total_n = len(grp)
        probs = counts / total_n
        h = -sum(p * math.log(p) for p in probs if p > 0)
        entropies.append(h)

    mean_h = sum(entropies) / len(entropies) if entropies else 0.0
    status = 'PASS' if mean_h >= threshold else 'FAIL'
    return {
        'status': status, 'value': round(mean_h, 4),
        'threshold': threshold, 'n_pairs': len(entropies),
        'detail': f'mean entropy={mean_h:.4f} over {len(entropies)} (workload,seed) pairs',
        'per_pair': {f"{wl}:{s}": h for (wl, s), h in zip(
            [k for k, _ in opt_df.groupby(['workload', 'seed'])],
            entropies
        )},
        'opt_r_distribution': dict(opt_df['optimal_r'].value_counts()),
    }


def run_p3_prime(df: pd.DataFrame, alpha: float, threshold: float = 0.60,
                 min_workloads: int = 3) -> dict:
    """P3': per-workload mode fraction of optimal_r ≤ threshold."""
    key = ['workload', 'seed', 'phase_index']
    active = df[df['region_accessed_bytes'] > 0].copy()

    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_budget(grp, alpha)
        opt_rows.append({'workload': gkey[0], 'seed': gkey[1],
                         'phase_index': gkey[2], 'optimal_r': opt_r})
    if not opt_rows:
        return {'status': 'FAIL', 'value': 0, 'detail': 'no active phases'}

    opt_df = pd.DataFrame(opt_rows)

    pass_workloads = 0
    details = {}
    for wl, grp in opt_df.groupby('workload'):
        mode_r = grp['optimal_r'].mode().iloc[0]
        mode_frac = (grp['optimal_r'] == mode_r).mean()
        details[wl] = {'mode_r': int(mode_r), 'mode_frac': round(mode_frac, 3),
                        'n_phases': len(grp),
                        'distribution': dict(grp['optimal_r'].value_counts())}
        if mode_frac <= threshold:
            pass_workloads += 1

    status = 'PASS' if pass_workloads >= min_workloads else 'FAIL'
    return {
        'status': status, 'value': pass_workloads,
        'threshold': threshold, 'min_workloads': min_workloads,
        'detail': f'{pass_workloads}/{len(details)} workloads with mode_frac ≤ {threshold}',
        'workload_details': details,
    }


def main():
    df = load_all_runs(str(DATA_DIR))

    print(f"=== Option 1: Fetch-overhead budget (α={ALPHA}) ===\n")

    # Show fetch_overhead_rate by R for each workload/phase
    active = df[df['region_accessed_bytes'] > 0].copy()
    active['for_rate'] = (
        (active['region_fetched_bytes'] - active['region_accessed_bytes'])
        / active['region_accessed_bytes'].where(active['region_accessed_bytes'] > 0)
    )
    print("fetch_overhead_rate per region_size (mean over active phases):")
    print(active.groupby('region_size')['for_rate'].mean().to_string())
    print()
    print("fetch_overhead_rate per (workload, region_size) mean:")
    print(active.groupby(['workload', 'region_size'])['for_rate'].mean().unstack().round(3).to_string())
    print()

    # Optimal R per phase
    key = ['workload', 'seed', 'phase_index']
    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_budget(grp, ALPHA)
        for_r64 = fetch_overhead_rate(
            grp[grp['region_size'] == 64]['region_fetched_bytes'].values[0],
            grp[grp['region_size'] == 64]['region_accessed_bytes'].values[0],
        ) if len(grp[grp['region_size'] == 64]) > 0 else float('inf')
        opt_rows.append({'workload': gkey[0], 'seed': gkey[1],
                         'phase_index': gkey[2], 'optimal_r': opt_r,
                         'for_r64': round(for_r64, 4)})

    opt_df = pd.DataFrame(opt_rows)
    print("Optimal R per (workload, seed, phase_index) by budget method:")
    print(opt_df.to_string(index=False))
    print()

    p1_result = run_p1_prime(df, ALPHA)
    p3_result = run_p3_prime(df, ALPHA)

    print(f"P1' status: {p1_result['status']} | value={p1_result['value']} | {p1_result['detail']}")
    print(f"  opt_r distribution: {p1_result.get('opt_r_distribution', {})}")
    print()
    print(f"P3' status: {p3_result['status']} | value={p3_result['value']} | {p3_result['detail']}")
    for wl, d in p3_result.get('workload_details', {}).items():
        print(f"  {wl}: mode_r={d['mode_r']}, mode_frac={d['mode_frac']}, dist={d['distribution']}")

    # Try range of alpha values
    print("\n=== Alpha sweep (optimal_r distribution) ===")
    for alpha in [0.1, 0.2, 0.5, 1.0, 2.0, 5.0]:
        p3 = run_p3_prime(df, alpha)
        opt_rows_a = []
        for gkey, grp in active.groupby(key):
            opt_r = optimal_r_by_budget(grp, alpha)
            opt_rows_a.append(opt_r)
        dist = pd.Series(opt_rows_a).value_counts().to_dict()
        print(f"  α={alpha:.1f}: P3'={p3['status']}({p3['value']}/6), opt_r dist={dist}")


if __name__ == '__main__':
    main()
