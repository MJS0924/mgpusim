#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/conv2d/CD/run_8

../../conv2d \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=1800 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=8 \
    -N=1 -C=3 -H=333 -W=333 -output-channel=3 -kernel-height=7 -kernel-width=7 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/conv2d/conv2d_CD_8_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/conv2d_CD_8.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/conv2d_CD_8.sqlite3 2>/dev/null

