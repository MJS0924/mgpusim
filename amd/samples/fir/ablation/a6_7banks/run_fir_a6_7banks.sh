#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/fir/ablation/a6_7banks

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A6_nbank/7banks/rawdata/events/fir_a6_7banks_events.parquet

../../fir \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-num-banks=7 \
    -sd-log2-sub-entry=2 \
    -log2-page-size=12 \
    -length=16000000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/fir/fir_a6_7banks_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A6_nbank/7banks/rawdata/text/fir_a6_7banks.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A6_nbank/7banks/rawdata/sql/fir_a6_7banks.sqlite3 2>/dev/null

