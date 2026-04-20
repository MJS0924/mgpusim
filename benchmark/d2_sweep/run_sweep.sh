#!/bin/bash
# PHASE D.2 full sweep: 6 workloads × 5 region sizes × 3 seeds = 90 runs.
# Conventions: docs/phase_d1_conventions.md §1-§4.
# Usage: bash benchmark/d2_sweep/run_sweep.sh [from repo root]
set -o pipefail

WORKLOADS=(simpleconvolution matrixtranspose matrixmultiplication pagerank fir stencil2d)
REGIONS=(64 256 1024 4096 16384)
SEEDS=(42 43 44)

OUT=results/m1/d2/raw
LOG=${OUT}/sweep.log
SUMMARY=${OUT}/sweep_summary.tsv
mkdir -p "${OUT}"

echo -e "workload\tregion\tseed\tstatus\twall_clock_s\tphases\tretired_wf\tl2h\tl2m\tfetched\taccessed" > "${SUMMARY}"
> "${LOG}"

TOTAL=$(( ${#WORKLOADS[@]} * ${#REGIONS[@]} * ${#SEEDS[@]} ))
COUNT=0
PASS=0; FAIL=0; TIMEOUT_COUNT=0

echo "PHASE D.2 sweep: ${TOTAL} runs" | tee -a "${LOG}"
echo "Estimated: ~$((TOTAL * 24 / 60)) min sequential" | tee -a "${LOG}"
echo "" | tee -a "${LOG}"

for W in "${WORKLOADS[@]}"; do
  for R in "${REGIONS[@]}"; do
    for S in "${SEEDS[@]}"; do
      COUNT=$(( COUNT + 1 ))
      F="${OUT}/${W}_R${R}_seed${S}.parquet"

      # Skip already-completed runs (resume support).
      if [ -f "${F}" ] && [ -s "${F}" ]; then
        echo "[$COUNT/$TOTAL] SKIP ${W} R=${R} seed=${S} (exists)" | tee -a "${LOG}"
        # Extract metrics from existing summary line in log if available.
        EXISTING=$(grep "M1_SUMMARY workload=${W} R=${R}" "${LOG}" | \
                   grep "seed${S}.parquet" | tail -1)
        if [ -n "${EXISTING}" ]; then
          PHASES=$(echo "${EXISTING}" | sed 's/.*phases=\([0-9]*\).*/\1/')
          RWF=$(echo "${EXISTING}"   | sed 's/.*RetiredWf=\([0-9]*\).*/\1/')
          L2H=$(echo "${EXISTING}"   | sed 's/.*L2H=\([0-9]*\).*/\1/')
          L2M=$(echo "${EXISTING}"   | sed 's/.*L2M=\([0-9]*\).*/\1/')
          FET=$(echo "${EXISTING}"   | sed 's/.*fetched=\([0-9]*\).*/\1/')
          ACC=$(echo "${EXISTING}"   | sed 's/.*accessed=\([0-9]*\).*/\1/')
          echo -e "${W}\t${R}\t${S}\tPASS\t0\t${PHASES}\t${RWF}\t${L2H}\t${L2M}\t${FET}\t${ACC}" \
            >> "${SUMMARY}"
        else
          echo -e "${W}\t${R}\t${S}\tPASS\t0\t?\t?\t?\t?\t?\t?" >> "${SUMMARY}"
        fi
        PASS=$(( PASS + 1 ))
        continue
      fi

      echo -n "[$COUNT/$TOTAL] ${W} R=${R} seed=${S} ... " | tee -a "${LOG}"
      START=$(date +%s)

      timeout 900 go run ./cmd/m1 \
        -workload="${W}" \
        -region-size="${R}" \
        -seed="${S}" \
        -gpus=2,3,4,5 \
        -output-dir="${OUT}" \
        -timing -disable-rtm \
        >> "${LOG}" 2>&1

      STATUS=$?
      ELAPSED=$(( $(date +%s) - START ))

      if [ "${STATUS}" -eq 0 ] && [ -f "${F}" ] && [ -s "${F}" ]; then
        # Extract metrics from the M1_SUMMARY line.
        SUMMARY_LINE=$(grep "M1_SUMMARY workload=${W} R=${R}" "${LOG}" | \
                       grep "seed${S}.parquet" | tail -1)
        PHASES=$(echo "${SUMMARY_LINE}" | sed 's/.*phases=\([0-9]*\).*/\1/')
        RWF=$(echo "${SUMMARY_LINE}"    | sed 's/.*RetiredWf=\([0-9]*\).*/\1/')
        L2H=$(echo "${SUMMARY_LINE}"    | sed 's/.*L2H=\([0-9]*\).*/\1/')
        L2M=$(echo "${SUMMARY_LINE}"    | sed 's/.*L2M=\([0-9]*\).*/\1/')
        FET=$(echo "${SUMMARY_LINE}"    | sed 's/.*fetched=\([0-9]*\).*/\1/')
        ACC=$(echo "${SUMMARY_LINE}"    | sed 's/.*accessed=\([0-9]*\).*/\1/')
        echo "PASS (${ELAPSED}s)" | tee -a "${LOG}"
        echo -e "${W}\t${R}\t${S}\tPASS\t${ELAPSED}\t${PHASES}\t${RWF}\t${L2H}\t${L2M}\t${FET}\t${ACC}" \
          >> "${SUMMARY}"
        PASS=$(( PASS + 1 ))
      elif [ "${STATUS}" -eq 124 ]; then
        echo "TIMEOUT >900s" | tee -a "${LOG}"
        echo -e "${W}\t${R}\t${S}\tTIMEOUT\t${ELAPSED}\t-\t-\t-\t-\t-\t-" >> "${SUMMARY}"
        TIMEOUT_COUNT=$(( TIMEOUT_COUNT + 1 ))
      else
        echo "FAIL (status=${STATUS}, ${ELAPSED}s)" | tee -a "${LOG}"
        echo -e "${W}\t${R}\t${S}\tFAIL\t${ELAPSED}\t-\t-\t-\t-\t-\t-" >> "${SUMMARY}"
        FAIL=$(( FAIL + 1 ))
      fi

    done
  done
done

echo "" | tee -a "${LOG}"
echo "=== SWEEP COMPLETE ===" | tee -a "${LOG}"
echo "PASS=${PASS}  FAIL=${FAIL}  TIMEOUT=${TIMEOUT_COUNT}  TOTAL=${COUNT}" | tee -a "${LOG}"
PARQUET_COUNT=$(ls "${OUT}"/*.parquet 2>/dev/null | wc -l)
echo "Parquet files: ${PARQUET_COUNT}" | tee -a "${LOG}"
