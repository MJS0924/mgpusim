#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/bfs/REC/run_default

../../bfs \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=REC -equal-dir-cap=true \
    -log2-page-size=12 \
    -node=940000 -degree=32 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/bfs/bfs_REC_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/bfs/bfs_REC_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/bfs_REC.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/bfs_REC.sqlite3 2>/dev/null

