#!/bin/bash

MAX_PARALLEL=4

trap 'echo "중단 중..."; kill 0; exit 1' INT TERM

run_bg() {
    local config_id=$1
    local script_path=$2
    echo "  [REC-stencil2d][${config_id}] 실행 중..."
    bash "${script_path}" &
    while [ "$(jobs -rp | wc -l)" -ge "${MAX_PARALLEL}" ]; do
        wait -n 2>/dev/null || wait
    done
}

echo "=== [REC][stencil2d] 시작 (병렬 최대 ${MAX_PARALLEL}) ==="
run_bg "default" "/root/mgpusim_home/mgpusim/amd/samples/stencil2d/REC/run_default/run_stencil2d_REC_default.sh"
wait
echo "=== [REC][stencil2d] 완료 ==="
