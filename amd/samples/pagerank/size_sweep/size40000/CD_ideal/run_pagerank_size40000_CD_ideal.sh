#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/pagerank/size_sweep/size40000/CD_ideal
/root/mgpusim_home/mgpusim/amd/samples/pagerank/pagerank \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -ideal-directory=true \
    -node=40000 \
    -sparsity=0.005 \
    -iterations=3 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/CD/rawdata/mem_path/pagerank_size40000_CD_ideal_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/pagerank_size40000_CD_ideal.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/pagerank_size40000_CD_ideal.sqlite3 2>/dev/null
