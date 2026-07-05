#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/vgg16/CD/run_ideal

../../vgg16 \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -log2-page-size=12 \
    -coherence-unit-size=0 \
    -epoch=1 -max-batch-per-epoch=2 -batch-size=32 \
    -ideal-directory=true \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/CD/rawdata/mem_path/vgg16_ideal_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/vgg16_ideal.txt

# 결과 파일(SQLite) 이동 및 이름 변경
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/vgg16_ideal.sqlite3 2>/dev/null

# motivation 경로에도 복사 (summarize.py 호환)
cp /root/mgpusim_home/results/CD/rawdata/sql/vgg16_ideal.sqlite3 /root/mgpusim_home/results/motivation/rawdata/sql/vgg16_motivation.sqlite3 2>/dev/null

# Coalescability CSV 수집
for csv_file in motivation_coalescability_GPU*.csv motivation_cumulative_GPU*.csv; do
    [ -f "$csv_file" ] && mv "$csv_file" "/root/mgpusim_home/results/motivation/rawdata/csv/vgg16_$csv_file"
done

