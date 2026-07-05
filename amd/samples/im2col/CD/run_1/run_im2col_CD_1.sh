#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/im2col/CD/run_1

../../im2col \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -log2-page-size=12 \
    -coherence-unit-size=1 \
    -N=1 -C=3 -H=735 -W=735 -kernel-height=3 -kernel-width=3 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/im2col/im2col_CD_1_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/im2col/im2col_CD_1_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/im2col_CD_1.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/im2col_CD_1.sqlite3 2>/dev/null

