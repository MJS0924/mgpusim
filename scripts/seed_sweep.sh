#!/usr/bin/env bash
# seed_sweep.sh — PageRank seed sensitivity analysis for HPCA reviewer response
#
# Usage:
#   ./scripts/seed_sweep.sh [binary] [protocol] [node] [sparsity] [iterations]
#
# Example:
#   ./scripts/seed_sweep.sh ./amd/samples/pagerank/pagerank REC 16384 0.005 4
#
# Output: mean ± std of kernel_time and key cohDir metrics across seeds.

set -euo pipefail

BINARY="${1:-./amd/samples/pagerank/pagerank}"
PROTO="${2:-REC}"
NODE="${3:-16384}"
SPARSITY="${4:-0.005}"
ITER="${5:-4}"
SEEDS=(1 7 42 100 2024)

TMPDIR_RUN=$(mktemp -d)
trap 'rm -rf "$TMPDIR_RUN"' EXIT

echo "=== Seed Sweep: protocol=$PROTO node=$NODE sparsity=$SPARSITY iterations=$ITER ==="
echo ""
printf "%-8s %-26s %-12s %-12s\n" "seed" "kernel_time(s)" "FromRemote" "ToRemoteData"
printf "%-8s %-26s %-12s %-12s\n" "--------" "-------------------------" "----------" "------------"

KT_VALUES=()

for seed in "${SEEDS[@]}"; do
    rundir="$TMPDIR_RUN/seed_${seed}"
    mkdir -p "$rundir"
    pushd "$rundir" > /dev/null

    "$OLDPWD/$BINARY" \
        -rand-seed="$seed" \
        -timing \
        -unified-gpus=1,2,3,4,5 \
        -use-unified-memory \
        -page-migration-policy=AccessCounter \
        -coherence-directory="$PROTO" \
        -log2-page-size=12 \
        -node="$NODE" \
        -sparsity="$SPARSITY" \
        -iterations="$ITER" \
        -report-all > /dev/null 2>&1

    db=$(ls akita_sim_*.sqlite3 2>/dev/null | head -1)
    kt=$(sqlite3 "$db" "SELECT SUM(value) FROM mgpusim_metrics WHERE what='kernel_time';" 2>/dev/null)
    remote=$(sqlite3 "$db" "SELECT SUM(value) FROM cohDir_metrics WHERE what='FromRemote';" 2>/dev/null)
    toremote=$(sqlite3 "$db" "SELECT SUM(value) FROM cohDir_metrics WHERE what='ToRemoteData';" 2>/dev/null)

    KT_VALUES+=("$kt")
    printf "%-8s %-26s %-12s %-12s\n" "$seed" "$kt" "$remote" "$toremote"
    popd > /dev/null
done

echo ""
echo "=== Statistics ==="
python3 - "${KT_VALUES[@]}" <<'EOF'
import sys, statistics
vals = [float(v) for v in sys.argv[1:]]
mean = statistics.mean(vals)
std  = statistics.stdev(vals) if len(vals) > 1 else 0.0
print(f"kernel_time  mean = {mean:.6e} s")
print(f"kernel_time  std  = {std:.6e} s  ({100*std/mean:.3f}%)")
print(f"kernel_time  min  = {min(vals):.6e} s")
print(f"kernel_time  max  = {max(vals):.6e} s")
EOF
