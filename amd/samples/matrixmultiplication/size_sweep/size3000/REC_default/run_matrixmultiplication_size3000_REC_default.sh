#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/size_sweep/size3000/REC_default
/root/mgpusim_home/mgpusim/amd/samples/matrixmultiplication/matrixmultiplication \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=REC \
    -x=3000 \
    -y=3000 \
    -z=3000 \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/matrixmultiplication_size3000_REC_default.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/matrixmultiplication_size3000_REC_default.sqlite3 2>/dev/null
