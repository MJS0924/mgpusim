# M1 Simulation Modification Plan (v1.2)

> Adaptive Region-Size Directory — M1 Motivation Experiment
> Target venue: HPCA 2027

---

## §2.5.6 L2 block size 고정 원칙 (2026-04-19 결정)

### 원칙

제안 기법 (Adaptive Region-Size Directory) 은 coherence granularity 만
가변이며 **L2 cache block size 는 64B 고정**이다. Directory 는 여러
cacheline 을 하나의 region entry 로 묶어 tracking 하지만 L2 자체는
cacheline 단위로 동작한다.

M1 은 제안 기법의 intrinsic upper bound 를 측정하므로 L2 block size
역시 모든 config 에서 64B 고정이다. L2 block size 를 region size 와
함께 바꾸면 (largeBlockCache 사용) L2 eviction pattern 이 region size
에 따라 달라져 workload intrinsic 이 아닌 L2 artifact 가 metric 에
섞인다. 이는 M1 을 제안 기법의 upper bound 에서 이탈시킨다.

### Region utilization metric 구현 (β design)

`L2Adapter` 가 cacheline-level L2 이벤트를 받아 `AddressMapper.EntryTag`
로 region-level 로 재해석하고, phase 단위 dedup 으로 "한 region 이 한
phase 에 활성화된 횟수 = 1" 로 기록한다. 이는 제안 기법 directory 의
per-region entry tracking 을 정적으로 재현한 것이다.

구체적으로:
- `L2Adapter.currentPhaseRegions map[uint64]bool` — phase 내 활성화된 region tag 집합
- `L2Adapter.registerRegionIfNew(addr)` — `mapper.EntryTag(addr)` 로 tag 계산 →
  미등록이면 `metrics.AddRegionFetch(tag, regionSize)` 호출
- `L2Adapter.ResetPhase()` — phase boundary 시 dedup map 초기화
- `RegisterPhaseLifecycle(..., resetables ...PhaseResetable)` — boundary 콜백에서
  `l2Adapter.ResetPhase()` 자동 호출

### Reviewer 공격 방어

> "β 의 adapter dedup 은 실제 directory 가 하지 않는 일 아니냐"

제안 기법 directory 는 region 당 1 entry 유지하므로 β 의 dedup 과
동일 semantics. PHASE 2 P1 에서 실제 directory 구현 vs M1 adapter dedup
의 per-region 활성화 시퀀스 일치를 sanity check 로 검증 예정
(see `TODO_PHASE2.md`).

### 사용자 판단 근거

제안 기법의 architectural 선택과 M1 upper bound 의 architectural 선택이
일치해야 함. L2 block size 는 제안 기법의 core assumption 이므로 M1
에서도 동일하게 고정해야 측정이 의미를 갖는다.

---

## §2.5.1 "Intrinsic" 정의

임의 region size R 에 대해 최적의 디렉토리를 가정했을 때 얻을 수 있는
sharing/locality metric. 측정 플랫폼 아티팩트 (유한 디렉토리 용량, 코얼레싱,
eviction policy) 를 제거한 순수 workload 특성.

**Plain VI Directory** (B-0): 2-state VI, infinite capacity, no coalescing,
no REC. Region size × workload metric 변화가 모두 workload intrinsic 에서 비롯됨.

---

## §2.5.6 관련 구현 파일

| 파일 | 역할 |
|------|------|
| `instrument/adapter/l2_adapter.go` | β design: mapper injection + phase dedup |
| `instrument/adapter/cu_adapter.go` | mapper injection + V12 warning count |
| `instrument/adapter/phase_lifecycle.go` | PhaseResetable 호출 |
| `instrument/adapter/common.go` | SnapshotSink, InMemorySink, PhaseResetable |
| `coherence/address_mapper.go` | EntryTag, SubOffset 구현 |
| `coherence/plain_vi.go` | SharerEvent callback system |
