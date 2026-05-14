#!/bin/bash

MAX_PARALLEL=4

trap 'echo "중단 중..."; kill 0; exit 1' INT TERM

run_bg() {
    local config_id=$1
    local script_path=$2
    echo "  [CD-pagerank][${config_id}] 실행 중..."
    bash "${script_path}" &
    while [ "$(jobs -rp | wc -l)" -ge "${MAX_PARALLEL}" ]; do
        wait -n 2>/dev/null || wait
    done
}

echo "=== [CD][pagerank] 시작 (병렬 최대 ${MAX_PARALLEL}) ==="
run_bg "0" "/root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_0/run_pagerank_CD_0.sh"
run_bg "1" "/root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_1/run_pagerank_CD_1.sh"
run_bg "2" "/root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_2/run_pagerank_CD_2.sh"
run_bg "4" "/root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_4/run_pagerank_CD_4.sh"
run_bg "6" "/root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_6/run_pagerank_CD_6.sh"
run_bg "8" "/root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_8/run_pagerank_CD_8.sh"
run_bg "ideal" "/root/mgpusim_home/mgpusim/amd/samples/pagerank/CD/run_ideal/run_pagerank_CD_ideal.sh"
wait
echo "=== [CD][pagerank] 완료 ==="
