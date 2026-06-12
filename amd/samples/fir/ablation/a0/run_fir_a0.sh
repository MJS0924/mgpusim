#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/fir/ablation/a0

export EVENT_LOG_PATH=/root/mgpusim_home/results_ablation/A0_no_rsb_cbf/rawdata/events/fir_a0_events.parquet

../../fir \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=SuperDirectory \
    -sd-disable-rsb=true \
    -sd-disable-cbf=true \
    -log2-page-size=12 \
    -length=16000000 \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results_ablation/per_window/fir/fir_a0_per_window.csv \
    -report-all \
    > /root/mgpusim_home/results_ablation/A0_no_rsb_cbf/rawdata/text/fir_a0.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results_ablation/A0_no_rsb_cbf/rawdata/sql/fir_a0.sqlite3 2>/dev/null

