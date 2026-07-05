#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/ablation/a5_log2_4

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/events/matrixmultiplication_a5_log2_4_events.parquet

../../matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory -sd-promote-at-evict=false \
    -sd-num-banks=3 \
    -sd-log2-sub-entry=4 \
    -log2-page-size=12 \
    -x=2500 -y=2500 -z=2500 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/matrixmultiplication/matrixmultiplication_a5_log2_4_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results_ablation/mem_path/matrixmultiplication/matrixmultiplication_a5_log2_4_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/text/matrixmultiplication_a5_log2_4.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/sql/matrixmultiplication_a5_log2_4.sqlite3 2>/dev/null

