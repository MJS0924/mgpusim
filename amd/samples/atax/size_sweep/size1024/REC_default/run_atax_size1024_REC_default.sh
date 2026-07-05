#!/bin/bash
cd /root/mgpusim_home/mgpusim/amd/samples/atax/size_sweep/size1024/REC_default
/root/mgpusim_home/mgpusim/amd/samples/atax/atax \
    -timing \
    -unified-gpus=1,2,3,4 \
    -use-unified-memory \
    -log2-page-size=12 \
    -coherence-directory=REC -equal-dir-cap=true \
    -x=1024 \
    -y=1024 \
    -mem-latency-trace \
    -mem-latency-trace-output=/root/mgpusim_home/results/REC/rawdata/mem_path/atax_size1024_REC_default_mem_path.csv \
    -report-all \
    > /root/mgpusim_home/results/REC/rawdata/text/atax_size1024_REC_default.txt
mv akita_sim_*.sqlite3 /root/mgpusim_home/results/REC/rawdata/sql/atax_size1024_REC_default.sqlite3 2>/dev/null
