#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/floydwarshall/REC/run_halfset

../../floydwarshall \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=REC -equal-dir-cap=true \
    -log2-page-size=12 \
    -node=4096 -iter=4 \
    -rec-half-set \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/floydwarshall/floydwarshall_REC_halfset_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/floydwarshall/floydwarshall_REC_halfset_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/floydwarshall_REC_halfset.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/floydwarshall_REC_halfset.sqlite3 2>/dev/null

