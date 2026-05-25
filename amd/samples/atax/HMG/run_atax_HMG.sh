#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/atax/HMG

../atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=HMG \
    -coherence-unit-size=2 \
    -log2-page-size=12 \
    -x=8000 -y=8000 \
    -report-all \
    > /root/mgpusim_home/results/HMG/rawdata/text/atax_HMG.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/HMG/rawdata/sql/atax_HMG.sqlite3 2>/dev/null

