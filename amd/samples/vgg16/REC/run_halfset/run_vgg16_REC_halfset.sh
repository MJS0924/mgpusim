#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/vgg16/REC/run_halfset

../../vgg16 \
    -timing \
    -unified-gpus=1,2,3,4,5 \
    -use-unified-memory \
    -coherence-directory=REC \
    -log2-page-size=12 \
    -epoch=1 -max-batch-per-epoch=2 -batch-size=32 \
    -rec-half-set \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/vgg16_REC_halfset.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/vgg16_REC_halfset.sqlite3 2>/dev/null

