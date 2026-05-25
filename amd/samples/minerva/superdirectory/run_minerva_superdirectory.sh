#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/minerva/superdirectory

export EVENT_LOG_PATH=/root/mgpusim_home/results/superdirectory/rawdata/events/minerva_events.parquet

../minerva \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=SuperDirectory \
    -log2-page-size=12 \
    -epoch=1 -max-batch-per-epoch=2 -batch-size=512 \
    -report-all \
    > /root/mgpusim_home/results/superdirectory/rawdata/text/minerva_superdirectory.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/superdirectory/rawdata/sql/minerva_superdirectory.sqlite3 2>/dev/null

