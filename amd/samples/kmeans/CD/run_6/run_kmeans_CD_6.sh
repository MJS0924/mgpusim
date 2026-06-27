#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/kmeans/CD/run_6

../../kmeans \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=6 \
    -points=500000 -features=32 -clusters=100 -max-iter=2 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/kmeans/kmeans_CD_6_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/kmeans/kmeans_CD_6_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/kmeans_CD_6.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/kmeans_CD_6.sqlite3 2>/dev/null

