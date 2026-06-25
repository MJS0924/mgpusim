#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/minerva/HMG

../minerva \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=HMG \
    -coherence-unit-size=2 \
    -log2-page-size=12 \
    -epoch=1 -max-batch-per-epoch=1 -batch-size=512 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/minerva/minerva_HMG_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/HMG/rawdata/text/minerva_HMG.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/HMG/rawdata/sql/minerva_HMG.sqlite3 2>/dev/null

