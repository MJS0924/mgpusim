#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RAW_DIR="${SCRIPT_DIR}/raw"
BIN="${SCRIPT_DIR}/b35_overhead"
GPUS="1"

cd "${REPO_ROOT}"

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
        grep "B35_WALL_CLOCK" "${logfile}" || true
        echo "  → log: ${logfile}"
    done
    echo ""
}

run_scenario s2
run_scenario s3
run_scenario s4

echo "=== S2-S4 measurement complete ==="
grep -h "B35_WALL_CLOCK" "${RAW_DIR}"/*.log 2>/dev/null || echo "(none)"
