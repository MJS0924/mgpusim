#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/xor/HMG

../xor \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=HMG -equal-dir-cap=true \
    -coherence-unit-size=2 \
    -log2-page-size=12 \
     \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/HMG/rawdata/mem_path/xor_HMG_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/HMG/rawdata/text/xor_HMG.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/HMG/rawdata/sql/xor_HMG.sqlite3 2>/dev/null

