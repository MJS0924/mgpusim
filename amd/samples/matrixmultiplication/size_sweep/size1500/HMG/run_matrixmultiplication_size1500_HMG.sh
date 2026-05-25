#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/size_sweep/size1500/HMG
/root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=HMG \
    -coherence-unit-size=2 \
    -x=1500 \
    -y=1500 \
    -z=1500 \
    -report-all \
    > /root/mgpusim_home/results/HMG/rawdata/text/matrixmultiplication_size1500_HMG.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/HMG/rawdata/sql/matrixmultiplication_size1500_HMG.sqlite3 2>/dev/null
