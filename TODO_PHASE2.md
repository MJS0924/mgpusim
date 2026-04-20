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

---

## Multi-GPU GPU-ID convention (from D.0 Path A, 2026-04-20)

**Context**: `akita/mem/cache/optdirectory/coherencedirectory.go:340`
contains a pre-existing bug `item |= 1 << (id - 2)` that panics when
any GPU ID is 1 (introduced in akita commit `429ce443`, 2026-04-19,
predates the B-phase M1 work).

**Workaround adopted for PHASE C/D**: use `-gpus=2,3,4,5` (4 GPUs,
IDs starting at 2). No mgpusim or M1-side code change required; M1
adapters attach cleanly to all 4 L2 caches with this convention.

**Phase 2 action**: apply the one-line akita fix (`id - minID`, or
`if id < 2 { return }`) to `recordAccessMask`, then restore the
canonical `-gpus=0,1,2,3` (or `1,2,3,4`) convention across all
scripts and documents. Fix complexity: LOW (~1-5 lines in
`coherencedirectory.go`; no REC/HMG dependency).

Details and reproduction logs: `results/m1/multigpu_feasibility.md`
and `results/m1/d0_probe/`.

---

## Per-instruction retirement count (from B-3.6)

**Context**: `PhaseMetrics.RetiredWavefronts` counts wavefronts reaching
`WfCompleted` state — one count per wavefront (= 64 threads). Sufficient for
wavefront-level utilization analysis in PHASE 1.

**If per-instruction granularity is needed**: the executed instruction count
is not available at the `HookPosWfRetired` call site without additional
bookkeeping. Options:

1. **Hook detail**: define `WfRetiredDetail{InstructionCount uint64}` and pass
   it as `ctx.Detail` from `invokeWfRetiredHook()`, reading from a per-wf
   executed-instruction counter maintained in the issue stage.
2. **Wavefront struct field**: add `ExecutedInstructions uint64` to
   `wavefront.Wavefront`, increment at issue, read at retirement.
3. **Approximation**: `RetiredWavefronts × avg_instructions_per_wavefront`
   (workload-specific, not general).

**Current decision**: wavefront count only. Revisit if the paper requires
instruction-level CPI/IPC metrics.
