#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/stencil2d/coalescability

../stencil2d \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -log2-page-size=12 \
    -row=2828 -col=2828 -iter=2 \
    -coalescability-heatmap \
    -coalescability-heatmap-dir=/root/mgpusim_home/results/coalescability/rawdata/heatmap/stencil2d \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/coalescability/rawdata/per_window/stencil2d_coalescability_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/coalescability/rawdata/text/stencil2d_coalescability.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/coalescability/rawdata/sql/stencil2d_coalescability.sqlite3 2>/dev/null

