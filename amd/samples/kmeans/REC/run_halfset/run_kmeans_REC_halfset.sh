#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/kmeans/REC/run_halfset

../../kmeans \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=REC \
    -log2-page-size=12 \
    -points=500000 -features=32 -clusters=100 -max-iter=2 \
    -rec-half-set \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/kmeans/kmeans_REC_halfset_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/kmeans_REC_halfset.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/kmeans_REC_halfset.sqlite3 2>/dev/null

