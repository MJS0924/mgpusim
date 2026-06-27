#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/floydwarshall/CD/run_6

../../floydwarshall \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=6 \
    -node=4096 -iter=4 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/floydwarshall/floydwarshall_CD_6_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/floydwarshall/floydwarshall_CD_6_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/floydwarshall_CD_6.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/floydwarshall_CD_6.sqlite3 2>/dev/null

