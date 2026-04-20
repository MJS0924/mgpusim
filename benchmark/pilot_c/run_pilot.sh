#!/usr/bin/env bash
# PHASE C pilot run: simpleconvolution × 5 region sizes.
# Usage: cd /root/mgpusim_home/mgpusim && bash benchmark/pilot_c/run_pilot.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
OUT="${REPO_ROOT}/results/m1/raw"
REGION_SIZES=(64 256 1024 4096 16384)

cd "${REPO_ROOT}"
mkdir -p "${OUT}"

echo "=== PHASE C Pilot Run ==="
echo "Workload : simpleconvolution 512x512 mask=3"
echo "Configs  : R=${REGION_SIZES[*]}"
echo "Output   : ${OUT}"
echo ""

for R in "${REGION_SIZES[@]}"; do
    echo "--- R=${R} ---"
    START=$(date +%s%N)
    go run ./cmd/m1 \
        -workload=simpleconvolution \
        -region-size="${R}" \
        -seed=42 \
        -window-cycles=100000 \
        -output-dir="${OUT}" \
        -config-id="${R}" \
        -timing \
        -gpus 1 \
        -disable-rtm \
        2>&1
    END=$(date +%s%N)
    ELAPSED=$(( (END - START) / 1000000 ))
    echo "PILOT_TIMING R=${R} elapsed_ms=${ELAPSED}"
    echo ""
done

echo "=== Pilot complete ==="
echo "Output files:"
ls -lh "${OUT}/"*.parquet 2>/dev/null || echo "(none found)"