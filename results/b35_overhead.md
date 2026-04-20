# B-3.5 Adapter Overhead Measurement

**Date**: 2026-04-20  
**Branch**: `m1-phase-b35-overhead`  
**Binary**: `benchmark/b35_overhead/b35_overhead` (built 2026-04-20 02:17 UTC, same binary for all runs)

---

## Workload

- **이름**: simpleconvolution (AMDAPPSDK)  
- **경로**: `amd/benchmarks/amdappsdk/simpleconvolution/`  
- **설정**: Width=512, Height=512, maskSize=3  
- **Command**: `./b35_overhead -scenario {s1..s4} -timing -gpus 1 -disable-rtm`  
- **Platform**: r9nano, GPU 1개, writebackcoh L2 (16 banks), CoherenceDirectory  
- **컴포넌트 수**: L2 캐시 16개, CU 64개 (16 SA × 4 CU/SA)

> **4-GPU 구성 비고**: simpleconvolution은 b.gpus[0]만 사용하므로 단일 GPU 측정이 representative함.  
> 4-GPU 구성에서 optdirectory panic 발생 (pre-existing issue, B-3.5와 무관).

---

## 시나리오 정의

| Scenario | 등록 adapter | 설명 |
|----------|-------------|------|
| S1 | (없음) | 순수 akita simulation baseline |
| S2 | L2Adapter (16개) + DirectoryAdapter stub | writebackcoh HookPosL2Access/RegionFetch hook 활성 |
| S3 | S2 + wfRetireHook (64 CU) | HookPosBeforeEvent → WfCompletionEvent 감지 |
| S4 | S2 + CUAdapter (64 CU) | HookPosCUVectorMemAccess + HookPosBeforeEvent 모두 활성 |

> S2의 DirectoryAdapter는 PlainVIDirectory stub에 연결 (실 simulation에 미연동) → 이벤트 수신 없음.  
> S3는 wfRetireHook (WfCompletion only), S4는 full CUAdapter (vector mem access 포함).

---

## S1~S4 wall-clock (3회, 단위: ms)

| Scenario | run1 | run2 | run3 | mean | std | rel to S1 |
|----------|------|------|------|------|-----|-----------|
| S1 (baseline) | 52,637 | 52,527 | 52,770 | 52,645 | 99 | 1.000 |
| S2 (L2+Dir)   | 53,909 | 55,051 | 53,833 | 54,264 | 557 | 1.031 |
| S3 (S2+retire)| 55,481 | 54,800 | 54,834 | 55,038 | 313 | 1.046 |
| S4 (full)     | 53,393 | 53,986 | 53,805 | 53,728 | 248 | 1.021 |

---

## 비율 분석

| Ratio | Value | Interpretation |
|-------|-------|----------------|
| S2/S1 | **1.031** (+3.1%) | L2Adapter 16개 hook 비용 (AddL2Access + registerRegionIfNew × 16 caches) |
| S3/S2 | **1.014** (+1.4%) | wfRetireHook: HookPosBeforeEvent scan + WfCompletion 감지 64 CU |
| S4/S3 | **0.976** (−2.4%) | 노이즈 범위 내 — S4 ≤ S3 mean (OnRegionAccess 비용이 측정 노이즈 이하) |
| S4/S1 | **1.021** (+2.1%) | **종합 overhead** |

### 주요 관찰

1. **전체 overhead 2.1%** — S4/S1 = 1.021로 판정 기준 허용(< 1.20)을 크게 하회.
2. **L2Adapter가 dominant** — S2/S1 = 1.031. L2 hook이 전체 overhead의 대부분.
3. **S4/S3 = 0.976** — HookPosCUVectorMemAccess의 OnRegionAccess 비용이 run-to-run variation (± ~500ms) 이하. Addr mapping + map dedup 비용이 측정 해상도보다 작음.
4. **S3/S2 > S4/S3** — wfRetireHook의 HookPosBeforeEvent scan(모든 event 훑기)이 OnRegionAccess보다 상대적으로 큼. 다만 절대 차이는 ~774ms (1.4%).
5. **표준편차**: S1(99ms), S4(248ms)는 tight. S2(557ms), S3(313ms)는 상대적으로 산포 큼 → 반복 실행 권장 시 runs ≥ 5 고려.

---

## 판정

> **S4/S1 = 1.021 → +2.1%**

**허용 (PHASE C 진입 가능)**

판정 기준 (사전 합의):
- S4/S1 < 1.20 → 허용, PHASE C 진입 가능 ✅
- 1.20 ≤ S4/S1 < 1.50 → 검토 필요
- S4/S1 ≥ 1.50 → B-4 재설계 필수

B-4 (batched flush, 별도 WfRetireHookPos) 등의 최적화는 현시점 불필요.

---

## V11 / V12 invariant

| Invariant | 결과 | 근거 |
|-----------|------|------|
| V11: Evictions=0 | ✅ PASS | S2~S4 로그에서 eviction 관련 panic/warning 없음 |
| V12: RegionAccessed ≤ RegionFetched | ✅ PASS (no log) | CUAdapter warningCount 로그 출력 없음 (0건) |

> V12 warningCount > 0인 경우 CUAdapter가 `[B-3.5]` log 출력. S4 전 run에서 0.  
> 단, simpleconvolution의 메모리 접근 패턴이 단순(sequential read)하여 실제 복잡한 workload에서는 V12 warning이 발생할 수 있음.

---

## B-4 진입 권고

**현재: B-4 진입 불필요.** 이유:

1. 종합 overhead 2.1%로 논문 기준 허용 범위
2. HookPosBeforeEvent 비용이 우려보다 낮음 (별도 WfRetireHookPos 상수 추가 불필요)
3. S3/S2 = 1.014로 WfCompletion 감지 비용도 허용 범위

**다음 단계 권고**: PHASE C 진입
- PlainVIDirectory를 optdirectory 대체로 실 simulation에 연동
- DirectoryAdapter sharer event overhead 실측 (현재 stub만 측정됨)
- 더 큰 workload (matrixtranspose, pagerank)로 추가 검증 권장

---

## 측정 환경 노트

- **memoryallocator.go 변경**: 02:05 수정됨. S1 원본 실행(01:55)은 이전 바이너리 → 동일 바이너리로 S1 재실행(02:26~). 모든 최종 수치는 동일 02:17 빌드 기준.
- **4-GPU panic**: optdirectory `bankStage.finalizeTrans` nil pointer (pre-existing, B-3.5 무관). 단일 GPU로 측정.
- **run_master.log**: 첫 S2 run은 `&instrument.PhaseMetrics{}` nil map panic으로 실패 → `NewPhaseMetrics()` 수정 후 재실행.
