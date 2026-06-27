#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/stencil2drelu/CD/run_4

../../stencil2drelu \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=4 \
     \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/mgpusim/amd/samples/stencil2drelu/CD/run_4/metrics_mem_path.csv \
    -report-all \
    > /dev/null

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/stencil2drelu_CD_4.sqlite3 2>/dev/null

