#!/bin/bash

cd /root/mgpusim_home/mgpusim/amd/samples/atax/CD/run_0_fifo

../../atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -coherence-directory=CoherenceDirectory \
    -log2-page-size=12 \
    -coherence-unit-size=0 \
    -cd-fifo-replacement \
    -x=8000 -y=8000 \
    -report-all \
    > /root/mgpusim_home/results/CD/rawdata/text/atax_CD_0_fifo.txt

mv akita_sim_*.sqlite3 /root/mgpusim_home/results/CD/rawdata/sql/atax_CD_0_fifo.sqlite3 2>/dev/null
