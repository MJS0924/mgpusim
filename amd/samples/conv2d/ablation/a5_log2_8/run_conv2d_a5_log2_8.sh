#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/conv2d/ablation/a5_log2_8

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A5_log2sweep/log2_8/rawdata/events/conv2d_a5_log2_8_events.parquet

../../conv2d \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-num-banks=2 \
    -sd-log2-sub-entry=8 \
    -log2-page-size=12 \
    -N=1 -C=3 -H=333 -W=333 -output-channel=3 -kernel-height=7 -kernel-width=7 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/conv2d/conv2d_a5_log2_8_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A5_log2sweep/log2_8/rawdata/text/conv2d_a5_log2_8.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A5_log2sweep/log2_8/rawdata/sql/conv2d_a5_log2_8.sqlite3 2>/dev/null

