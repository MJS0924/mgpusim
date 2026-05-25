#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_6

../../pagerank \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=6 \
    -node=80000 -sparsity=0.005 -iterations=3 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/pagerank/pagerank_CD_6_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/pagerank_CD_6.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/pagerank_CD_6.sqlite3 2>/dev/null

