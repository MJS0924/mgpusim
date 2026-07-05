#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/minerva/CD/run_6

../../minerva \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -log2-page-size=12 \
    -coherence-unit-size=6 \
    -epoch=1 -max-batch-per-epoch=1 -batch-size=2048 -cd8-deadlock-fix=true -sd-peer-serve-reserve=true -l2-peer-evict-headroom=true \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/minerva/minerva_CD_6_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/minerva/minerva_CD_6_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/minerva_CD_6.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/minerva_CD_6.sqlite3 2>/dev/null

