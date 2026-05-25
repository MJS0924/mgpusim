#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/relu/CD/run_0

../../relu \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=0 \
    -length=16000000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/relu/relu_CD_0_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/relu_CD_0.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/relu_CD_0.sqlite3 2>/dev/null

