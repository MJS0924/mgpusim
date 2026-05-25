#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/pagerank/size_sweep/size20000/REC_default
/root/mgpusim_home/mgpusim/amd/samples/pagerank/pagerank \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=REC \
    -node=20000 \
    -sparsity=0.005 \
    -iterations=3 \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/pagerank_size20000_REC_default.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/pagerank_size20000_REC_default.sqlite3 2>/dev/null
