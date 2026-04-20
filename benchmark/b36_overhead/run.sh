#!/usr/bin/env bash
# B-3.6 overhead re-measurement script.
# Uses HookPosWfRetired so RetiredInstructions is correctly counted.
#
# Usage: cd /root/mgpusim_home/mgpusim && bash benchmark/b36_overhead/run.sh
# Output: benchmark/b36_overhead/raw/{scenario}_run{N}.log

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RAW_DIR="${SCRIPT_DIR}/raw"
BIN="${SCRIPT_DIR}/b36_overhead"

GPUS="1"

cd "${REPO_ROOT}"

echo "=== B-3.6 Overhead Re-Measurement ==="
echo "Workload   : simpleconvolution 512x512 mask=3"
echo "GPUs       : ${GPUS}"
echo "Scenarios  : S1 S2 S3 S4 (3 runs each)"
echo ""

echo "[build] go build ./benchmark/b36_overhead/ ..."
go build -o "${BIN}" ./benchmark/b36_overhead/
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
        { time -p "${BIN}" \
            -scenario "${scenario}" \
            -timing \
            -gpus "${GPUS}" \
            -disable-rtm \
            2>&1; } | tee "${logfile}"
        grep "B36_WALL_CLOCK\|B36_METRICS" "${logfile}" || true
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
echo "Quick summary:"
grep -h "B36_WALL_CLOCK\|B36_METRICS" "${RAW_DIR}"/*.log 2>/dev/null || echo "(none found)"
