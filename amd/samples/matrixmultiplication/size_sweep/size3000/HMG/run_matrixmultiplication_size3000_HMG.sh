#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/size_sweep/size3000/HMG
/root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=HMG -equal-dir-cap=true \
    -coherence-unit-size=2 \
    -x=3000 \
    -y=3000 \
    -z=3000 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/HMG/rawdata/mem_path/matrixmultiplication_size3000_HMG_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/HMG/rawdata/text/matrixmultiplication_size3000_HMG.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/HMG/rawdata/sql/matrixmultiplication_size3000_HMG.sqlite3 2>/dev/null
