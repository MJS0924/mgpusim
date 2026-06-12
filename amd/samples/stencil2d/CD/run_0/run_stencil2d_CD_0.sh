#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/stencil2d/CD/run_0

../../stencil2d \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=0 \
    -row=4000 -col=4000 -iter=4 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/stencil2d/stencil2d_CD_0_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/stencil2d_CD_0.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/stencil2d_CD_0.sqlite3 2>/dev/null

