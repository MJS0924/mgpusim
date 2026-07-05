#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/atax/size_sweep/size8000/CD_ideal
/root/mgpusim_home/mgpusim/amd/samples/atax/atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -coherence-unit-size=0 \
    -ideal-directory=true \
    -x=8000 \
    -y=8000 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/CD/rawdata/mem_path/atax_size8000_CD_ideal_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/atax_size8000_CD_ideal.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/atax_size8000_CD_ideal.sqlite3 2>/dev/null
