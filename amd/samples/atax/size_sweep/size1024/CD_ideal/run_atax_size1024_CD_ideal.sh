#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/atax/size_sweep/size1024/CD_ideal
/root/mgpusim_home/mgpusim/amd/samples/atax/atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=CoherenceDirectory \
    -coherence-unit-size=0 \
    -ideal-directory=true \
    -x=1024 \
    -y=1024 \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/atax_size1024_CD_ideal.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/atax_size1024_CD_ideal.sqlite3 2>/dev/null
