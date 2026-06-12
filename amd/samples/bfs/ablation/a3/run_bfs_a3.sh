#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/bfs/ablation/a3

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/events/bfs_a3_events.parquet

../../bfs \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-disable-rsb=true \
    -sd-disable-cbf=true \
    -sd-promote-at-evict=false \
    -log2-page-size=12 \
    -node=940000 -degree=32 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/bfs/bfs_a3_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/text/bfs_a3.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/sql/bfs_a3.sqlite3 2>/dev/null

