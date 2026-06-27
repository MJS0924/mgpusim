#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/spmv/ablation/a5_log2_8

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A5_log2sweep/log2_8/rawdata/events/spmv_a5_log2_8_events.parquet

../../spmv \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-num-banks=2 \
    -sd-log2-sub-entry=8 \
    -log2-page-size=12 \
    -dim=131072 -sparsity=0.000931 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/spmv/spmv_a5_log2_8_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results_ablation/mem_path/spmv/spmv_a5_log2_8_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A5_log2sweep/log2_8/rawdata/text/spmv_a5_log2_8.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A5_log2sweep/log2_8/rawdata/sql/spmv_a5_log2_8.sqlite3 2>/dev/null

