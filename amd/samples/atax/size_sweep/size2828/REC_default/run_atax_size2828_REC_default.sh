#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/atax/size_sweep/size2828/REC_default
/root/mgpusim_home/mgpusim/amd/samples/atax/atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=REC \
    -x=2828 \
    -y=2828 \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/atax_size2828_REC_default.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/atax_size2828_REC_default.sqlite3 2>/dev/null
