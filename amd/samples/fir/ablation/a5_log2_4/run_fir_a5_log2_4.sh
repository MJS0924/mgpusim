#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/fir/ablation/a5_log2_4

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/events/fir_a5_log2_4_events.parquet

../../fir \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-num-banks=3 \
    -sd-log2-sub-entry=4 \
    -log2-page-size=12 \
    -length=16000000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/fir/fir_a5_log2_4_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/text/fir_a5_log2_4.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/sql/fir_a5_log2_4.sqlite3 2>/dev/null

