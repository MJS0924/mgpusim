"""M1-P1~P6 proposition verification functions.

Each function returns a dict with keys:
  status   : 'PASS' | 'FAIL' | 'SKIP'
  value    : numeric result (or None for SKIP)
  threshold: the required threshold
  note     : human-readable explanation
"""

from typing import Any, Dict

import numpy as np
import pandas as pd

from .metrics import add_derived_metrics, is_active_phase, optimal_region_per_phase


def _shannon_entropy(values) -> float:
    """Shannon entropy (bits) of a discrete distribution."""
    counts = pd.Series(values).value_counts(normalize=True)
    probs = counts[counts > 0]
    return float(-(probs * np.log2(probs)).sum())


def p1_window_entropy(df: pd.DataFrame, threshold: float = 0.5) -> Dict[str, Any]:
    """P1: phase-variation entropy ≥ threshold.

    For each (workload, seed), compute the distribution of optimal-R choices
    (by L2 hit rate) across active phases. Average the Shannon entropy across
    all (workload, seed) pairs that have ≥ 2 active phases.
    """
    opt = optimal_region_per_phase(df)

    entropies = []
    details = {}
    for (workload, seed), grp in opt.groupby(['workload', 'seed']):
        phases = grp['optimal_r_l2hr'].values
        if len(phases) < 2:
            # Only 1 active phase — entropy is 0 by definition
            ent = 0.0
        else:
            ent = _shannon_entropy(phases)
        entropies.append(ent)
        details.setdefault(workload, []).append(ent)

    if not entropies:
        return dict(status='FAIL', value=None, threshold=threshold,
                    note='No (workload, seed) pairs with active phases found')

    mean_entropy = float(np.mean(entropies))
    per_workload = {w: float(np.mean(v)) for w, v in details.items()}
    status = 'PASS' if mean_entropy >= threshold else 'FAIL'
    return dict(
        status=status,
        value=round(mean_entropy, 4),
        threshold=threshold,
        note=(
            f'Mean entropy across {len(entropies)} (workload, seed) pairs = {mean_entropy:.4f}. '
            f'Per-workload avg: ' +
            ', '.join(f'{w}={v:.3f}' for w, v in sorted(per_workload.items()))
        ),
    )


def p2_three_metric_agreement(df: pd.DataFrame, threshold: float = 0.70) -> Dict[str, Any]:
    """P2: fraction of (workload, seed, phase) where all 3 optimal-R metrics agree ≥ threshold."""
    opt = optimal_region_per_phase(df)
    if opt.empty:
        return dict(status='FAIL', value=None, threshold=threshold,
                    note='No active phase data')

    opt['all_agree'] = (
        (opt['optimal_r_l2hr'] == opt['optimal_r_util']) &
        (opt['optimal_r_util'] == opt['optimal_r_scr'])
    )
    agree_rate = float(opt['all_agree'].mean())
    n_total = len(opt)
    n_agree = int(opt['all_agree'].sum())

    # Per-workload breakdown
    per_wl = opt.groupby('workload')['all_agree'].mean().round(3).to_dict()
    status = 'PASS' if agree_rate >= threshold else 'FAIL'
    return dict(
        status=status,
        value=round(agree_rate, 4),
        threshold=threshold,
        note=(
            f'{n_agree}/{n_total} (workload, seed, phase) triples agree ({agree_rate:.1%}). '
            f'Per-workload: ' +
            ', '.join(f'{w}={v:.3f}' for w, v in sorted(per_wl.items()))
        ),
    )


def p3_phase_mode_bound(df: pd.DataFrame, threshold: float = 0.60,
                        min_workloads: int = 3) -> Dict[str, Any]:
    """P3: for ≥ min_workloads, the modal optimal-R occupies ≤ threshold of active phases.

    Checks that no single region size dominates across phases (adaptive selection needed).
    """
    opt = optimal_region_per_phase(df)
    if opt.empty:
        return dict(status='FAIL', value=None, threshold=threshold,
                    note='No active phase data')

    wl_pass = []
    details = {}
    for workload, grp in opt.groupby('workload'):
        # Pool across all seeds: look at optimal_r_l2hr distribution
        mode_frac = float(grp['optimal_r_l2hr'].value_counts(normalize=True).iloc[0])
        passes = mode_frac <= threshold
        wl_pass.append(passes)
        details[workload] = round(mode_frac, 3)

    n_passing = sum(wl_pass)
    status = 'PASS' if n_passing >= min_workloads else 'FAIL'
    return dict(
        status=status,
        value=n_passing,
        threshold=f'>= {min_workloads} workloads with mode_frac <= {threshold}',
        note=(
            f'{n_passing}/{len(wl_pass)} workloads satisfy mode_frac ≤ {threshold}. '
            f'Mode fracs: ' +
            ', '.join(f'{w}={v}' for w, v in sorted(details.items()))
        ),
    )


def p4_ds_mode_bound(threshold: float = 0.60) -> Dict[str, Any]:
    """P4: DS mode bound — requires DS axis, not measured in PHASE D."""
    return dict(
        status='SKIP',
        value=None,
        threshold=f'mode_frac <= {threshold}',
        note='DS (data-structure) axis not measured in PHASE D. Deferred to future phase.',
    )


def p5_joint_entropy_reduction(threshold: float = 0.15) -> Dict[str, Any]:
    """P5: joint phase×DS conditional entropy reduction ≥ threshold. Requires DS axis."""
    return dict(
        status='SKIP',
        value=None,
        threshold=f'>= {threshold} reduction',
        note='Requires DS axis. Deferred — DS axis not measured in PHASE D.',
    )


def p6_track_ab_agreement(threshold: float = 0.80) -> Dict[str, Any]:
    """P6: Track A (accumulator) vs Track B (replay) agreement ≥ threshold."""
    return dict(
        status='SKIP',
        value=None,
        threshold=f'>= {threshold}',
        note='Track B (replay) not yet implemented. Deferred.',
    )


def verify_all(df: pd.DataFrame) -> Dict[str, Dict[str, Any]]:
    """Run all proposition checks and return results dict."""
    return {
        'P1': p1_window_entropy(df),
        'P2': p2_three_metric_agreement(df),
        'P3': p3_phase_mode_bound(df),
        'P4': p4_ds_mode_bound(),
        'P5': p5_joint_entropy_reduction(),
        'P6': p6_track_ab_agreement(),
    }
