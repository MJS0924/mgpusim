#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/im2col/HMG

../im2col \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=HMG \
    -coherence-unit-size=2 \
    -log2-page-size=12 \
    -N=1 -C=3 -H=735 -W=735 -kernel-height=3 -kernel-width=3 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/im2col/im2col_HMG_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/HMG/rawdata/text/im2col_HMG.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/HMG/rawdata/sql/im2col_HMG.sqlite3 2>/dev/null

