#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/matrixtranspose/REC/run_default

../../matrixtranspose \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=REC \
    -log2-page-size=12 \
    -width=4000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/matrixtranspose/matrixtranspose_REC_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/matrixtranspose_REC.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/matrixtranspose_REC.sqlite3 2>/dev/null

