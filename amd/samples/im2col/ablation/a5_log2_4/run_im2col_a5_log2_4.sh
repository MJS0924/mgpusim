#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/im2col/ablation/a5_log2_4

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/events/im2col_a5_log2_4_events.parquet

../../im2col \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-num-banks=3 \
    -sd-log2-sub-entry=4 \
    -log2-page-size=12 \
    -N=1 -C=3 -H=735 -W=735 -kernel-height=3 -kernel-width=3 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/im2col/im2col_a5_log2_4_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/text/im2col_a5_log2_4.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A5_log2sweep/log2_4/rawdata/sql/im2col_a5_log2_4.sqlite3 2>/dev/null

