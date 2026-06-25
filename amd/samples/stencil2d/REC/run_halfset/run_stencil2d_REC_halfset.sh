#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/stencil2d/REC/run_halfset

../../stencil2d \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=REC \
    -log2-page-size=12 \
    -row=4000 -col=4000 -iter=4 -cd8-deadlock-fix=true \
    -rec-half-set \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/stencil2d/stencil2d_REC_halfset_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/stencil2d_REC_halfset.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/stencil2d_REC_halfset.sqlite3 2>/dev/null

