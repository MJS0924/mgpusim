#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/xor/REC/run_default

../../xor \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=REC -equal-dir-cap=true \
    -log2-page-size=12 \
     \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/REC/rawdata/mem_path/xor_REC_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/xor_REC.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/xor_REC.sqlite3 2>/dev/null

