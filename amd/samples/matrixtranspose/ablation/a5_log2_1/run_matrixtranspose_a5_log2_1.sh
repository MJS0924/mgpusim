#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/matrixtranspose/ablation/a5_log2_1

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A5_log2sweep/log2_1/rawdata/events/matrixtranspose_a5_log2_1_events.parquet

../../matrixtranspose \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-num-banks=9 \
    -sd-log2-sub-entry=1 \
    -log2-page-size=12 \
    -width=4000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/matrixtranspose/matrixtranspose_a5_log2_1_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A5_log2sweep/log2_1/rawdata/text/matrixtranspose_a5_log2_1.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A5_log2sweep/log2_1/rawdata/sql/matrixtranspose_a5_log2_1.sqlite3 2>/dev/null

