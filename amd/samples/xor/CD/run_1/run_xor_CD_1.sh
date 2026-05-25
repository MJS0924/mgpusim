#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/xor/CD/run_1

../../xor \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=1 \
     \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/xor_CD_1.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/xor_CD_1.sqlite3 2>/dev/null

