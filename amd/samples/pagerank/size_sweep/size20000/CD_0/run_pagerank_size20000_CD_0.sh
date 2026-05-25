#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/pagerank/size_sweep/size20000/CD_0
/root/mgpusim_home/mgpusim/amd/samples/pagerank/pagerank \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -node=20000 \
    -sparsity=0.005 \
    -iterations=3 \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/pagerank_size20000_CD_0.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/pagerank_size20000_CD_0.sqlite3 2>/dev/null
