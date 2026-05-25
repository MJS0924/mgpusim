#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/lenet/REC/run_default

../../lenet \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=REC \
    -log2-page-size=12 \
    -epoch=1 -max-batch-per-epoch=2 -batch-size=512 \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/lenet_REC.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/lenet_REC.sqlite3 2>/dev/null

