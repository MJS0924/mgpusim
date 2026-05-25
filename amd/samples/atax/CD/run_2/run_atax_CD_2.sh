#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/atax/CD/run_2

../../atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=2 \
    -x=8000 -y=8000 \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/atax_CD_2.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/atax_CD_2.sqlite3 2>/dev/null

