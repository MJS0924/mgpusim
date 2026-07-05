#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/ablation/a0

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A0_no_promote_at_evict/rawdata/events/matrixmultiplication_a0_events.parquet

../../matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-promote-at-evict=false \
    -log2-page-size=12 \
    -x=2500 -y=2500 -z=2500 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/matrixmultiplication/matrixmultiplication_a0_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results_ablation/mem_path/matrixmultiplication/matrixmultiplication_a0_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A0_no_promote_at_evict/rawdata/text/matrixmultiplication_a0.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A0_no_promote_at_evict/rawdata/sql/matrixmultiplication_a0.sqlite3 2>/dev/null

