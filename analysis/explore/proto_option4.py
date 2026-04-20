"""Tier B+: Option 4 prototype — M5a density threshold.

Metric: M5a(R) = retired_wavefronts / active_regions
        = compute work per active region (wavefronts per region)

Hypothesis: phases with high compute density per region are well-served by
large R (each region does more useful work). Phases with low compute density
prefer small R (each region covers less compute; large R wastes tracking
overhead with little compute benefit).

Optimal R per phase:
  optimal_r = smallest R where M5a(R) >= gamma threshold
  Rationale: we want the smallest R that achieves enough density per region.

Since M5a is monotone ↑ with R (constant wavefronts, decreasing AR), this
threshold method gives a per-phase optimal R based on total wavefronts/AR_64.
"""

import sys, math
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
from analysis.load_data import load_all_runs

DATA_DIR = Path(__file__).resolve().parents[2] / 'results/m1/d2/raw'
REGION_SIZES = [64, 256, 1024, 4096, 16384]
GAMMA = 5.0  # wavefronts per region threshold


def optimal_r_by_density(grp: pd.DataFrame, gamma: float) -> int:
    """Smallest R where M5a(R) >= gamma. Falls back to R=64 if none qualify."""
    grp_sorted = grp.sort_values('region_size')
    for _, row in grp_sorted.iterrows():
        if row['active_regions'] > 0:
            m5a = row['retired_wavefronts'] / row['active_regions']
            if m5a >= gamma:
                return int(row['region_size'])
    return 64  # fallback (even R=64 doesn't hit threshold)


def run_p1_prime(df: pd.DataFrame, gamma: float, threshold: float = 0.5) -> dict:
    key = ['workload', 'seed', 'phase_index']
    active = df[df['active_regions'] > 0].copy()

    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_density(grp, gamma)
        opt_rows.append({'workload': gkey[0], 'seed': gkey[1],
                         'phase_index': gkey[2], 'optimal_r': opt_r})

    if not opt_rows:
        return {'status': 'FAIL', 'value': 0.0, 'detail': 'no active phases'}

    opt_df = pd.DataFrame(opt_rows)

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


def run_p3_prime(df: pd.DataFrame, gamma: float,
                 threshold: float = 0.60, min_wl: int = 3) -> dict:
    key = ['workload', 'seed', 'phase_index']
    active = df[df['active_regions'] > 0].copy()

    opt_rows = []
    for gkey, grp in active.groupby(key):
        opt_r = optimal_r_by_density(grp, gamma)
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
    active = df[df['active_regions'] > 0].copy()
    active['m5a'] = active['retired_wavefronts'] / active['active_regions']

    print("=== Option 4: M5a density threshold ===\n")
    print("M5a per (workload, phase_index, region_size) — mean over seeds:")
    pivot = active.groupby(['workload', 'phase_index', 'region_size'])['m5a'].mean()
    for (wl, ph), grp in pivot.groupby(level=[0, 1]):
        print(f"  {wl} phase {ph}: " +
              ", ".join(f"R={r}: {v:.3f}" for r, v in grp.reset_index(level=[0,1], drop=True).items()))
    print()

    p1 = run_p1_prime(df, GAMMA)
    p3 = run_p3_prime(df, GAMMA)

    print(f"P1' (γ={GAMMA}): {p1['status']} | value={p1['value']} | {p1['detail']}")
    print(f"  opt_r distribution: {p1.get('opt_r_distribution', {})}")
    print()
    print(f"P3' (γ={GAMMA}): {p3['status']} | value={p3['value']} | {p3['detail']}")
    for wl, d in p3.get('workload_details', {}).items():
        print(f"  {wl}: mode_r={d['mode_r']}, mode_frac={d['mode_frac']}, dist={d['distribution']}")

    # Gamma sweep
    print("\n=== Gamma sweep ===")
    for gamma in [0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 20.0, 50.0, 100.0]:
        p1 = run_p1_prime(df, gamma)
        p3 = run_p3_prime(df, gamma)
        print(f"  γ={gamma:6.1f}: P1'={p1['status']}({p1['value']}), "
              f"P3'={p3['status']}({p3['value']}/6), "
              f"dist={p1.get('opt_r_distribution', {})}")


if __name__ == '__main__':
    main()
