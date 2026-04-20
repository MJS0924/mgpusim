#!/bin/bash
# Resume run: only workloads not yet completed in the first pass.
set -o pipefail

WORKLOADS=(
  fir
  aes
  kmeans
  pagerank
  atax
  bicg
  nw
  bfs
  fft
  spmv
  stencil2d
)

OUT=results/m1/d1/verify
LOG=${OUT}/run.log
mkdir -p "${OUT}"

PASS=0
FAIL=0
TIMEOUT_COUNT=0

for W in "${WORKLOADS[@]}"; do
  echo "=== ${W} ===" | tee -a "${LOG}"
  START=$(date +%s)

  timeout 600 go run ./cmd/m1 \
    -workload="${W}" \
    -region-size=1024 \
    -seed=42 \
    -gpus=2,3,4,5 \
    -output-dir="${OUT}" \
    -timing -disable-rtm \
    >> "${LOG}" 2>&1

  STATUS=$?
  ELAPSED=$(( $(date +%s) - START ))

  if [ "${STATUS}" -eq 0 ]; then
    echo "  PASS (${ELAPSED}s)" | tee -a "${LOG}"
    PASS=$(( PASS + 1 ))
  elif [ "${STATUS}" -eq 124 ]; then
    echo "  TIMEOUT >600s" | tee -a "${LOG}"
    TIMEOUT_COUNT=$(( TIMEOUT_COUNT + 1 ))
  else
    echo "  FAIL (status=${STATUS}, ${ELAPSED}s)" | tee -a "${LOG}"
    FAIL=$(( FAIL + 1 ))
  fi
done

echo "" | tee -a "${LOG}"
echo "=== RESUME SUMMARY ===" | tee -a "${LOG}"
echo "PASS=${PASS}  FAIL=${FAIL}  TIMEOUT=${TIMEOUT_COUNT}" | tee -a "${LOG}"
echo "Parquet outputs:" | tee -a "${LOG}"
ls -la "${OUT}"/*.parquet 2>/dev/null | tee -a "${LOG}" || echo "  (none)" | tee -a "${LOG}"
