#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/xor/coalescability

../xor \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -log2-page-size=12 \
     \
    -coalescability-heatmap \
    -coalescability-heatmap-dir=/root/mgpusim_home/results/coalescability/rawdata/heatmap/xor \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/coalescability/rawdata/per_window/xor_coalescability_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/coalescability/rawdata/mem_path/xor_coalescability_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/coalescability/rawdata/text/xor_coalescability.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/coalescability/rawdata/sql/xor_coalescability.sqlite3 2>/dev/null

