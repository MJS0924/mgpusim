#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/im2col/ablation/a3

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/events/im2col_a3_events.parquet

../../im2col \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-promote-at-evict=false \
    -sd-disable-rsb=true \
    -sd-disable-cbf=true \
    -log2-page-size=12 \
    -N=1 -C=3 -H=735 -W=735 -kernel-height=3 -kernel-width=3 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/im2col/im2col_a3_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/text/im2col_a3.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/sql/im2col_a3.sqlite3 2>/dev/null

