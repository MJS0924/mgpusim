#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/bfs/CD/run_1

../../bfs \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=1 \
    -node=940000 -degree=32 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/bfs/bfs_CD_1_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/bfs_CD_1.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/bfs_CD_1.sqlite3 2>/dev/null

