#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/size_sweep/size1500/CD_0
/root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=CoherenceDirectory -equal-dir-cap=true \
    -coherence-unit-size=0 \
    -x=1500 \
    -y=1500 \
    -z=1500 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/CD/rawdata/mem_path/matrixmultiplication_size1500_CD_0_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/matrixmultiplication_size1500_CD_0.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/matrixmultiplication_size1500_CD_0.sqlite3 2>/dev/null
