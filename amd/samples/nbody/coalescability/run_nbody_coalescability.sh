#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/nbody/coalescability

../nbody \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -log2-page-size=12 \
    -particles=1048576 -iter=4 \
    -coalescability-heatmap \
    -coalescability-heatmap-dir=/root/mgpusim_home/results/coalescability/rawdata/heatmap/nbody \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/coalescability/rawdata/per_window/nbody_coalescability_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/coalescability/rawdata/text/nbody_coalescability.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/coalescability/rawdata/sql/nbody_coalescability.sqlite3 2>/dev/null

