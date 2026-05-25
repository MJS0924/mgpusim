#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/atax/size_sweep/size1024/HMG
/root/mgpusim_home/mgpusim/amd/samples/atax/atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=HMG \
    -coherence-unit-size=2 \
    -x=1024 \
    -y=1024 \
    -report-all \
    > /root/mgpusim_home/results/HMG/rawdata/text/atax_size1024_HMG.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/HMG/rawdata/sql/atax_size1024_HMG.sqlite3 2>/dev/null
