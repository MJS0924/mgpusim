#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/vgg16/superdirectory

export EVENT_LOG_PATH=/root/mgpusim_home/results/superdirectory/rawdata/events/vgg16_events.parquet

../vgg16 \
    -timing \
    -unified-gpus=1,2,3,4,5 \
    -use-unified-memory \
    -coherence-directory=SuperDirectory \
    -log2-page-size=12 \
    -epoch=1 -max-batch-per-epoch=2 -batch-size=32 \
    -report-all \
    > /root/mgpusim_home/results/superdirectory/rawdata/text/vgg16_superdirectory.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/superdirectory/rawdata/sql/vgg16_superdirectory.sqlite3 2>/dev/null

