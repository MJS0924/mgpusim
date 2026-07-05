#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/vgg16/CD/run_8

../../vgg16 \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -log2-page-size=12 \
    -coherence-unit-size=8 \
    -epoch=1 -max-batch-per-epoch=2 -batch-size=32 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/CD/rawdata/mem_path/vgg16_CD_8_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/vgg16_CD_8.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/vgg16_CD_8.sqlite3 2>/dev/null

