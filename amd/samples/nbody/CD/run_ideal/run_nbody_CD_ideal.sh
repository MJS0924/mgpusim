#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/nbody/CD/run_ideal

../../nbody \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=0 \
    -particles=2097152 -iter=4 \
    -ideal-directory=true \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/nbody/nbody_ideal_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/nbody/nbody_ideal_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/nbody_ideal.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/nbody_ideal.sqlite3 2>/dev/null

# motivation 경로에도 복사 (summarize.py 호환)
cp /root/mgpusim_home/results/CD/rawdata/sql/nbody_ideal.sqlite3 /root/mgpusim_home/results/motivation/rawdata/sql/nbody_motivation.sqlite3 2>/dev/null

# Coalescability CSV 수집
for csv_file in motivation_coalescability_GPU*.csv motivation_cumulative_GPU*.csv; do
    [ -f "$csv_file" ] && mv "$csv_file" "/root/mgpusim_home/results/motivation/rawdata/csv/nbody_$csv_file"
done

