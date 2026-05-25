#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/kmeans/superdirectory

export EVENT_LOG_PATH=/root/mgpusim_home/results/superdirectory/rawdata/events/kmeans_events.parquet

../kmeans \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=SuperDirectory \
    -log2-page-size=12 \
    -points=500000 -features=32 -clusters=100 -max-iter=2 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/kmeans/kmeans_SD_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/superdirectory/rawdata/text/kmeans_superdirectory.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/superdirectory/rawdata/sql/kmeans_superdirectory.sqlite3 2>/dev/null

