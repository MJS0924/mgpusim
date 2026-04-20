# TODO: PHASE 2 작업 목록

## PHASE 2 P1 sanity check (from B-3 β design decision)

**출처**: B-3 β design 결정 (2026-04-19)
**관련 문서**: `docs/M1_Simulation_Modification_Plan.md` §2.5.6

### 목표

동일 access trace 에 대해:
- M1 adapter dedup 이 기록한 per-region 활성화 시퀀스
- 실제 제안 기법 directory 가 기록한 per-region entry 활성화 시퀀스

가 일치해야 함.

### 방법

1. 소규모 workload (e.g., matrixtranspose 16×16) 에 대해 동시 실행
2. M1 adapter 의 `InMemorySink` 에 기록된 per-phase region tag 집합 추출
3. 실제 제안 기법 directory 의 per-phase entry 활성화 로그 추출
4. 집합 symmetric difference 가 0 인지 확인

### 불일치 시

β approximation 의 한계가 노출된 것이므로 discrepancy analysis 필요:
- V12 ordering 문제 (CU access → L2 fetch 순서) 가 count 에 영향?
- 동일 cacheline 에 대한 write-invalidate 후 re-fetch 가 double-count?

---

## PHASE 2 P1 기타 TODO

- (placeholder for future PHASE 2 tasks)
