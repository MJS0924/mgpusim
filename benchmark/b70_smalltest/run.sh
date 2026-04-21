#!/usr/bin/env bash
# B-7.0 small-test benchmark: 3 capacities × 5 region sizes × 2 workloads = 30 runs.
#
# Capacity values: 0 (infinite), 8192, 2048
# Region sizes (bytes): 64, 256, 1024, 4096, 16384
# Workloads: simpleconvolution, matrixmultiplication
#
# Outputs: results/m1/b70_smalltest/*.parquet  +  b70_summary.tsv
# Usage: cd <repo-root> && bash benchmark/b70_smalltest/run.sh [--dry-run]

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${REPO_ROOT}/results/m1/b70_smalltest"
SUMMARY_TSV="${OUT_DIR}/b70_summary.tsv"

CAPACITIES=(0 8192 2048)
REGION_SIZES=(64 256 1024 4096 16384)
WORKLOADS=(simpleconvolution matrixmultiplication)

SEED=42
WINDOW_CYCLES=100000

DRY_RUN=0
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=1
fi

mkdir -p "${OUT_DIR}"

# Build the binary once.
echo "[b70] building cmd/m1..."
go build -o "${OUT_DIR}/m1_bin" "${REPO_ROOT}/cmd/m1"
echo "[b70] build OK"

# Write TSV header.
printf 'workload\tregion_size\tmax_entries\tsummary_line\n' > "${SUMMARY_TSV}"

run_n=0
total=$(( ${#WORKLOADS[@]} * ${#REGION_SIZES[@]} * ${#CAPACITIES[@]} ))

for workload in "${WORKLOADS[@]}"; do
  wid=0
  [[ "${workload}" == "matrixmultiplication" ]] && wid=1

  cap_id=0
  for cap in "${CAPACITIES[@]}"; do
    for r in "${REGION_SIZES[@]}"; do
      run_n=$(( run_n + 1 ))
      cfg_id=$(( cap_id * ${#REGION_SIZES[@]} + (run_n - 1) % ${#REGION_SIZES[@]} ))

      echo "[b70] run ${run_n}/${total}: workload=${workload} R=${r} cap=${cap}"

      cmd=(
        "${OUT_DIR}/m1_bin"
        -workload="${workload}"
        -region-size="${r}"
        -max-entries="${cap}"
        -seed="${SEED}"
        -window-cycles="${WINDOW_CYCLES}"
        -output-dir="${OUT_DIR}"
        -config-id="${cfg_id}"
        -workload-id="${wid}"
        -timing
        -gpus 1
        -disable-rtm
      )

      if [[ "${DRY_RUN}" -eq 1 ]]; then
        echo "  DRY-RUN: ${cmd[*]}"
        continue
      fi

      summary_line=$("${cmd[@]}" 2>/tmp/b70_stderr.log | grep '^M1_SUMMARY' || true)
      if [[ -z "${summary_line}" ]]; then
        echo "  WARNING: no M1_SUMMARY line for run ${run_n}" >&2
        summary_line="ERROR"
      fi
      printf '%s\t%s\t%s\t%s\n' "${workload}" "${r}" "${cap}" "${summary_line}" >> "${SUMMARY_TSV}"
      echo "  ${summary_line}"
    done
    cap_id=$(( cap_id + 1 ))
  done
done

echo "[b70] done. summary → ${SUMMARY_TSV}"
