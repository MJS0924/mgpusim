#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/spmv/REC/run_default

../../spmv \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=REC \
    -log2-page-size=12 \
    -dim=131072 -sparsity=0.000931 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/spmv/spmv_REC_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/spmv_REC.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/spmv_REC.sqlite3 2>/dev/null

