#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/lenet/CD/run_6

../../lenet \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=6 \
    -epoch=1 -max-batch-per-epoch=2 -batch-size=512 \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/lenet_CD_6.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/lenet_CD_6.sqlite3 2>/dev/null

