#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/stencil2d/ablation/a3

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/events/stencil2d_a3_events.parquet

../../stencil2d \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-disable-rsb=true \
    -sd-disable-cbf=true \
    -sd-promote-at-evict=false \
    -log2-page-size=12 \
    -row=4000 -col=4000 -iter=4 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/stencil2d/stencil2d_a3_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/text/stencil2d_a3.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A3_no_promote_at_evict/rawdata/sql/stencil2d_a3.sqlite3 2>/dev/null

