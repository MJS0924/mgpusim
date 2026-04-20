#!/usr/bin/env bash
# B-3.5 overhead measurement script.
# Runs simpleconvolution under S1–S4 instrumentation scenarios, 3 runs each.
#
# Usage: cd /root/mgpusim_home/mgpusim && bash benchmark/b35_overhead/run.sh
# Output: benchmark/b35_overhead/raw/{scenario}_run{N}.log

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RAW_DIR="${SCRIPT_DIR}/raw"
BIN="${SCRIPT_DIR}/b35_overhead"

# GPU IDs to use.
# NOTE: simpleconvolution uses only b.gpus[0]; multi-GPU causes optdirectory
# panic with 4 GPUs (pre-existing issue unrelated to our adapters).
# Using GPU 1 single: platform builds 1 GPU, workload runs on it.
GPUS="1"

cd "${REPO_ROOT}"

echo "=== B-3.5 Overhead Measurement ==="
echo "Workload   : simpleconvolution 512x512 mask=3"
echo "GPUs       : ${GPUS}"
echo "Scenarios  : S1 S2 S3 S4 (3 runs each)"
echo ""

# Build once
echo "[build] go build ./benchmark/b35_overhead/ ..."
go build -o "${BIN}" ./benchmark/b35_overhead/
echo "[build] OK — binary: ${BIN}"
echo ""

mkdir -p "${RAW_DIR}"

flush_caches() {
    if [ -w /proc/sys/vm/drop_caches ]; then
        sync
        echo 3 > /proc/sys/vm/drop_caches
    fi
}

run_scenario() {
    local scenario="$1"
    echo "--- Scenario ${scenario^^} ---"
    for run in 1 2 3; do
        local logfile="${RAW_DIR}/${scenario}_run${run}.log"
        echo -n "  run ${run}: "
        flush_caches
        # time -p writes to stderr; redirect stderr→stdout so tee captures it
        { time -p "${BIN}" \
            -scenario "${scenario}" \
            -timing \
            -gpus "${GPUS}" \
            -disable-rtm \
            2>&1; } | tee "${logfile}"
        # Extract B35_WALL_CLOCK line for quick summary
        grep "B35_WALL_CLOCK" "${logfile}" || true
        echo "  → log: ${logfile}"
    done
    echo ""
}

run_scenario s1
run_scenario s2
run_scenario s3
run_scenario s4

echo "=== Measurement complete ==="
echo "Raw logs in: ${RAW_DIR}/"
echo ""
echo "Quick summary (B35_WALL_CLOCK lines):"
grep -h "B35_WALL_CLOCK" "${RAW_DIR}"/*.log 2>/dev/null || echo "(none found)"
