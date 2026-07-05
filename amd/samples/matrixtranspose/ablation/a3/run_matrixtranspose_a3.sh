#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/matrixtranspose/ablation/a3

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/events/matrixtranspose_a3_events.parquet

../../matrixtranspose \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-promote-at-evict=false \
    -sd-disable-rsb=true \
    -sd-disable-cbf=true \
    -log2-page-size=12 \
    -width=8000 -cd8-deadlock-fix=true \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/matrixtranspose/matrixtranspose_a3_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results_ablation/mem_path/matrixtranspose/matrixtranspose_a3_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/text/matrixtranspose_a3.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/sql/matrixtranspose_a3.sqlite3 2>/dev/null

