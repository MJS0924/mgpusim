#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/spmv/coalescability

../spmv \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -log2-page-size=12 \
    -dim=92681 -sparsity=0.000931 \
    -coalescability-heatmap \
    -coalescability-heatmap-dir=/root/mgpusim_home/results/coalescability/rawdata/heatmap/spmv \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/coalescability/rawdata/per_window/spmv_coalescability_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/coalescability/rawdata/text/spmv_coalescability.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/coalescability/rawdata/sql/spmv_coalescability.sqlite3 2>/dev/null

