#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/matrixtranspose/superdirectory

export EVENT_LOG_PATH=/root/mgpusim_home/results/superdirectory/rawdata/events/matrixtranspose_events.parquet

../matrixtranspose \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -log2-page-size=12 \
    -width=8000 -cd8-deadlock-fix=true \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/matrixtranspose/matrixtranspose_SD_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/matrixtranspose/matrixtranspose_SD_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/superdirectory/rawdata/text/matrixtranspose_superdirectory.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/superdirectory/rawdata/sql/matrixtranspose_superdirectory.sqlite3 2>/dev/null

