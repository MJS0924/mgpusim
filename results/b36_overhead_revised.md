# B-3.6 Adapter Overhead Re-Measurement (Revised)

**Date**: 2026-04-20  
**Branch**: `m1-phase-b36-wfretired-hook`  
**Binary**: `benchmark/b36_overhead/b36_overhead` (built 2026-04-20, same binary for all runs)  
**Supersedes**: `results/b35_overhead.md` (RetiredWavefronts was 0 due to WfCompletionEvent non-firing)

---

## 변경 사항 (B-3.5 대비)

| 항목 | B-3.5 | B-3.6 |
|------|-------|-------|
| Wf retire hook position | `HookPosBeforeEvent` (WfCompletionEvent 감지) | `HookPosWfRetired` (evalSEndPgm 직접 발화) |
| CUAdapter retire path | `*wavefront.WfCompletionEvent` type-assert | `cu.HookPosWfRetired` switch case |
| S3 retire path | `wfRetireHook` → HookPosBeforeEvent | `wfRetireHook` → HookPosWfRetired |
| RetiredWavefronts (S3/S4) | **0** (버그) | **4129** (정상) |

---

## Workload

- **이름**: simpleconvolution (AMDAPPSDK)
- **설정**: Width=512, Height=512, maskSize=3
- **Command**: `./b36_overhead -scenario {s1..s4} -timing -gpus 1 -disable-rtm`
- **Platform**: r9nano, GPU 1개, writebackcoh L2 (16 banks)
- **컴포넌트 수**: L2 캐시 16개, CU 64개 (16 SA × 4 CU/SA)

---

## 시나리오 정의

| Scenario | 등록 adapter | 설명 |
|----------|-------------|------|
| S1 | (없음) | 순수 akita simulation baseline |
| S2 | L2Adapter (16개) + DirectoryAdapter stub | writebackcoh HookPosL2Access/RegionFetch hook 활성 |
| S3 | S2 + wfRetireHook (64 CU, HookPosWfRetired) | evalSEndPgm 직접 발화 경로 |
| S4 | S2 + CUAdapter (64 CU, HookPosWfRetired + VectorMem) | 전체 CU instrumentation |

---

## S1~S4 wall-clock (3회, 단위: ms)

| Scenario | run1 | run2 | run3 | mean | std | rel to S1 |
|----------|------|------|------|------|-----|-----------|
| S1 (baseline) | 50,376 | 50,071 | 49,767 | 50,071 | 249 | 1.000 |
| S2 (L2+Dir)   | 49,575 | 50,287 | 49,755 | 49,872 | 302 | 0.996 |
| S3 (S2+retire)| 51,594 | 50,705 | 51,100 | 51,133 | 364 | 1.021 |
| S4 (full)     | 51,197 | 51,866 | 51,400 | 51,488 | 280 | 1.028 |

---

## B36_METRICS (canary — 모든 run에서 동일)

| Scenario | L2Hits | L2Misses | RegionFetchedBytes | RetiredWavefronts |
|----------|--------|----------|--------------------|---------------------|
| S1 | 0 | 0 | 0 | 0 |
| S2 | 51,062 | 32,908 | 2,106,112 | 0 |
| S3 | 51,062 | 32,908 | 2,106,112 | **4,129** ✅ |
| S4 | 51,062 | 32,908 | 2,106,112 | **4,129** ✅ |

> **Canary 통과**: S3/S4 `RetiredWavefronts=4129 > 0` — HookPosWfRetired 발화 확인.  
> S1/S2 metrics=0 은 정상 (adapter 없음 / CU adapter 없음).

---

## 비율 분석

| Ratio | Value | Interpretation |
|-------|-------|----------------|
| S2/S1 | **0.996** (−0.4%) | 노이즈 범위 내 — L2Adapter 16개 hook 비용 ≈ 0 |
| S3/S2 | **1.025** (+2.5%) | wfRetireHook: HookPosWfRetired dispatch 64 CU × N wavefronts |
| S4/S3 | **1.007** (+0.7%) | HookPosCUVectorMemAccess OnRegionAccess 추가 비용 |
| S4/S1 | **1.028** (+2.8%) | **종합 overhead** |

### 주요 관찰

1. **전체 overhead 2.8%** — S4/S1 = 1.028. 판정 기준(< 1.20) 크게 하회. ✅
2. **S2/S1 = 0.996** — B-3.5 대비 S2 절대값이 낮음. 동일 workload이나 OS 스케줄링 변동 (~200ms 노이즈 범위 내).
3. **S3/S2 = 1.025** — HookPosWfRetired dispatch가 가장 큰 비용 요인. evalSEndPgm에서 64 CU × 4129 wf retire × InvokeHook 호출.
4. **S4/S3 = 1.007** — VectorMem access(OnRegionAccess: addr mapping + map update)가 +0.7%. 노이즈와 구분되나 작음.
5. **표준편차**: 모든 시나리오 249~364ms. 3회 실행 기준 신뢰도 충분. (더 tight한 측정을 원한다면 runs≥5 권장)

---

## 판정

> **S4/S1 = 1.028 → +2.8%**

**허용 (PHASE C 진입 가능)** ✅

판정 기준 (사전 합의):
- S4/S1 < 1.20 → 허용, PHASE C 진입 가능 ✅
- 1.20 ≤ S4/S1 < 1.50 → 검토 필요
- S4/S1 ≥ 1.50 → B-4 재설계 필수

B-4 (batched flush, 별도 최적화) 현시점 불필요.

---

## V11 / V12 invariant

| Invariant | 결과 | 근거 |
|-----------|------|------|
| V11: Evictions=0 | ✅ PASS | S2~S4 로그에서 eviction panic/warning 없음 |
| V12: RegionAccessed ≤ RegionFetched | ✅ PASS | CUAdapter warningCount 로그 출력 없음 (0건) |

---

## B-4 진입 권고

**현재: B-4 진입 불필요.** 이유:

1. 종합 overhead 2.8%로 논문 기준 허용 범위
2. HookPosWfRetired dispatch 비용(+2.5%)이 허용 범위 이내
3. VectorMem access 비용(+0.7%)도 허용 범위 이내
4. NumHooks() guard로 adapter 미등록 시 dispatch 비용 없음

**다음 단계 권고**: PHASE C 진입
- PlainVIDirectory를 optdirectory 대체로 실 simulation에 연동
- DirectoryAdapter sharer event overhead 실측 (현재 stub만 측정됨)
- 더 큰 workload (matrixtranspose, pagerank)로 추가 검증 권장
