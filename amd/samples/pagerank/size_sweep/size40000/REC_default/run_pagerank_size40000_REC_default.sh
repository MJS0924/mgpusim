#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/pagerank/size_sweep/size40000/REC_default
/root/mgpusim_home/mgpusim/amd/samples/pagerank/pagerank \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=REC -equal-dir-cap=true \
    -node=40000 \
    -sparsity=0.005 \
    -iterations=3 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/REC/rawdata/mem_path/pagerank_size40000_REC_default_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/pagerank_size40000_REC_default.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/pagerank_size40000_REC_default.sqlite3 2>/dev/null
