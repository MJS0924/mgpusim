#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/fir/CD/run_2

../../fir \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -log2-page-size=12 \
    -coherence-unit-size=2 \
    -length=16000000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/fir/fir_CD_2_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/fir/fir_CD_2_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/fir_CD_2.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/fir_CD_2.sqlite3 2>/dev/null

