#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/CD/run_ideal

../../matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -inter-gpu-noc \
    -inter-gpu-noc-bw=300 \
    -use-unified-memory \
    -page-migration-policy=None \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -log2-page-size=12 \
    -coherence-unit-size=0 \
    -x=2500 -y=2500 -z=2500 \
    -ideal-directory=true \
    -per-window-snapshot \
    -window-instructions=50000 \
    -per-window-output=/root/mgpusim_home/results/per_window/matrixmultiplication/matrixmultiplication_ideal_per_window.csv \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/mem_path/matrixmultiplication/matrixmultiplication_ideal_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/matrixmultiplication_ideal.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/matrixmultiplication_ideal.sqlite3 2>/dev/null

# motivation 경로에도 복사 (summarize.py 호환)
cp /root/mgpusim_home/results/CD/rawdata/sql/matrixmultiplication_ideal.sqlite3 /root/mgpusim_home/results/motivation/rawdata/sql/matrixmultiplication_motivation.sqlite3 2>/dev/null

# Coalescability CSV 수집
for csv_file in motivation_coalescability_GPU*.csv motivation_cumulative_GPU*.csv; do
    [ -f "$csv_file" ] && mv "$csv_file" "/root/mgpusim_home/results/motivation/rawdata/csv/matrixmultiplication_$csv_file"
done

