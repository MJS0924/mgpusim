#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/atax/CD/run_1

../../atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -log2-page-size=12 \
    -coherence-unit-size=1 \
    -x=8000 -y=8000 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/CD/rawdata/mem_path/atax_CD_1_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/atax_CD_1.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/atax_CD_1.sqlite3 2>/dev/null

