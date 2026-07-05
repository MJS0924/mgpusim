#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/vgg16/REC/run_halfset

../../vgg16 \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=REC -equal-dir-cap=true \
    -log2-page-size=12 \
    -epoch=1 -max-batch-per-epoch=2 -batch-size=32 \
    -rec-half-set \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/REC/rawdata/mem_path/vgg16_REC_halfset_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/vgg16_REC_halfset.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/vgg16_REC_halfset.sqlite3 2>/dev/null

