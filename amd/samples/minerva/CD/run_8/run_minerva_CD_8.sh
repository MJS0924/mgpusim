#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/minerva/CD/run_8

../../minerva \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=8 \
    -epoch=1 -max-batch-per-epoch=1 -batch-size=512 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/minerva/minerva_CD_8_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/minerva_CD_8.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/minerva_CD_8.sqlite3 2>/dev/null

