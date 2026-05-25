#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/pagerank/size_sweep/size20000/HMG
/root/mgpusim_home/mgpusim/amd/samples/pagerank/pagerank \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=HMG \
    -coherence-unit-size=2 \
    -node=20000 \
    -sparsity=0.005 \
    -iterations=3 \
    -report-all \
    > /root/mgpusim_home/results/HMG/rawdata/text/pagerank_size20000_HMG.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/HMG/rawdata/sql/pagerank_size20000_HMG.sqlite3 2>/dev/null
