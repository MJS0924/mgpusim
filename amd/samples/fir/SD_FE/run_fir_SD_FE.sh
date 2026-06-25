#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/fir/SD_FE

../fir \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-fe \
    -log2-page-size=12 \
    -length=16000000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/fir/fir_SD_FE_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/SD_FE/rawdata/text/fir_SD_FE.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/SD_FE/rawdata/sql/fir_SD_FE.sqlite3 2>/dev/null

