#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/nbody/superdirectory

export EVENT_LOG_PATH=/root/mgpusim_home/results/superdirectory/rawdata/events/nbody_events.parquet

../nbody \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=SuperDirectory -sd-promote-at-evict=false \
    -log2-page-size=12 \
    -particles=2097152 -iter=4 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/nbody/nbody_SD_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/nbody/nbody_SD_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/superdirectory/rawdata/text/nbody_superdirectory.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/superdirectory/rawdata/sql/nbody_superdirectory.sqlite3 2>/dev/null

