#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_0

../../pagerank \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -log2-page-size=12 \
    -coherence-unit-size=0 \
    -node=80000 -sparsity=0.005 -iterations=3 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/pagerank/pagerank_CD_0_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/pagerank/pagerank_CD_0_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/pagerank_CD_0.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/pagerank_CD_0.sqlite3 2>/dev/null

