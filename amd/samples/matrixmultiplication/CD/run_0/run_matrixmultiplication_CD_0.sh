#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/CD/run_0

../../matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=0 \
    -x=2500 -y=2500 -z=2500 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/matrixmultiplication/matrixmultiplication_CD_0_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/matrixmultiplication_CD_0.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/matrixmultiplication_CD_0.sqlite3 2>/dev/null

