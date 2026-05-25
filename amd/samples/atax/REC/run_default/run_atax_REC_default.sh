#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/atax/REC/run_default

../../atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=REC \
    -log2-page-size=12 \
    -x=8000 -y=8000 \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/atax_REC.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/atax_REC.sqlite3 2>/dev/null

