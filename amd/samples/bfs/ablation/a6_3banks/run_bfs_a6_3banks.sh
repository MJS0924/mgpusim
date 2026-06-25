#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/bfs/ablation/a6_3banks

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A6_nbank/3banks/rawdata/events/bfs_a6_3banks_events.parquet

../../bfs \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-num-banks=3 \
    -sd-log2-sub-entry=2 \
    -log2-page-size=12 \
    -node=940000 -degree=32 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/bfs/bfs_a6_3banks_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A6_nbank/3banks/rawdata/text/bfs_a6_3banks.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A6_nbank/3banks/rawdata/sql/bfs_a6_3banks.sqlite3 2>/dev/null

