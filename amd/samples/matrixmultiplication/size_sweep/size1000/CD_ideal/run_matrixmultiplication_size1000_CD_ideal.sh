#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/size_sweep/size1000/CD_ideal
/root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -ideal-directory=true \
    -x=1000 \
    -y=1000 \
    -z=1000 \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/matrixmultiplication_size1000_CD_ideal.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/matrixmultiplication_size1000_CD_ideal.sqlite3 2>/dev/null
