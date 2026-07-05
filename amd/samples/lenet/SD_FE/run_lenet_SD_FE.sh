#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/lenet/SD_FE

../lenet \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory -sd-promote-at-evict=false \
    -sd-fe \
    -log2-page-size=12 \
    -epoch=1 -max-batch-per-epoch=1 -batch-size=2048 -l2-peer-evict-headroom=true \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/lenet/lenet_SD_FE_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/lenet/lenet_SD_FE_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/SD_FE/rawdata/text/lenet_SD_FE.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/SD_FE/rawdata/sql/lenet_SD_FE.sqlite3 2>/dev/null

