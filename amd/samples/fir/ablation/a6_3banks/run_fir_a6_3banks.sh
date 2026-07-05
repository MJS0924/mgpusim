#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/fir/ablation/a6_3banks

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A6_nbank/3banks/rawdata/events/fir_a6_3banks_events.parquet

../../fir \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory -sd-promote-at-evict=false \
    -sd-num-banks=3 \
    -sd-log2-sub-entry=2 \
    -log2-page-size=12 \
    -length=16000000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/fir/fir_a6_3banks_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results_ablation/mem_path/fir/fir_a6_3banks_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A6_nbank/3banks/rawdata/text/fir_a6_3banks.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A6_nbank/3banks/rawdata/sql/fir_a6_3banks.sqlite3 2>/dev/null

