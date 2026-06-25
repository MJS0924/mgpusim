#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/pagerank/superdirectory

export EVENT_LOG_PATH=/root/mgpusim_home/results/superdirectory/rawdata/events/pagerank_events.parquet

../pagerank \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -log2-page-size=12 \
    -node=80000 -sparsity=0.005 -iterations=3 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/pagerank/pagerank_SD_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/superdirectory/rawdata/text/pagerank_superdirectory.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/superdirectory/rawdata/sql/pagerank_superdirectory.sqlite3 2>/dev/null

