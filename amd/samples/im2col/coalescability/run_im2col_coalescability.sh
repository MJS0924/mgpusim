#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/im2col/coalescability

../im2col \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -log2-page-size=12 \
    -N=1 -C=3 -H=520 -W=520 -kernel-height=3 -kernel-width=3 \
    -coalescability-heatmap \
    -coalescability-heatmap-dir=/root/mgpusim_home/results/coalescability/rawdata/heatmap/im2col \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/coalescability/rawdata/per_window/im2col_coalescability_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/coalescability/rawdata/text/im2col_coalescability.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/coalescability/rawdata/sql/im2col_coalescability.sqlite3 2>/dev/null

