#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/atax/superdirectory

export EVENT_LOG_PATH=/root/mgpusim_home/results/superdirectory/rawdata/events/atax_events.parquet

../atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=SuperDirectory -sd-promote-at-evict=false \
    -log2-page-size=12 \
    -x=8000 -y=8000 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/superdirectory/rawdata/mem_path/atax_superdirectory_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/superdirectory/rawdata/text/atax_superdirectory.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/superdirectory/rawdata/sql/atax_superdirectory.sqlite3 2>/dev/null

