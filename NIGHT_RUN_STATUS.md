# Night run status (2026-04-19 ~ 2026-04-20)

Branch: `m1-phase-b3-beta-design`
Operator: Claude Sonnet 4.6 (automated, user sleeping)

---

## Completed

| Task | Status | Commit | Timestamp |
|------|--------|--------|-----------|
| B: pre-existing evidence | ✅ DONE | `176884dc` | 2026-04-20 |
| C: L2 fixation docs | ✅ DONE | `adb42f91` | 2026-04-20 |
| A1: adapter β design | ✅ DONE | `7974d1ab` | 2026-04-20 |
| A2: test updates | ✅ DONE | `7974d1ab` | 2026-04-20 |
| A3: grep + final | ✅ DONE | `(this commit)` | 2026-04-20 |

## Blocked
(없음 — 전 작업 정상 완료)

## Test summary

| Package | Tests | Result |
|---------|-------|--------|
| `coherence` | (B-0 suite) | ✅ PASS |
| `instrument` | T1~T7, M1~M10 + structural | ✅ PASS |
| `instrument/adapter` | I1~I11 + hook dispatch + lifecycle | ✅ PASS (18 tests) |

Pre-existing failures (unrelated to our work):
- `amd/timing/cu`, `cp`, `rdma`, `rob`, `driver`, `emu` — missing mock files
- See `results/b3_preexisting_evidence.md` for full analysis

## Isolation grep summary (A3)

| Check | Target | Result |
|-------|--------|--------|
| A | `instrument/*.go` — no akita import | ✅ PASS |
| B | `coherence/*.go` — no akita import | ✅ PASS |
| C | `instrument/adapter/*.go` — akita imports present | ✅ PASS (expected) |

See `results/b3_beta_grep.md` for full details.

## What was done

### Task B — Pre-existing evidence
- Tested at commit `59956873` (parent of B-3.1)
- Confirmed `amd/timing/cu` and related failures are pre-existing MockXxx missing files
- `coherence`, `instrument` green both before and after B-3 work

### Task C — L2 block size fixation documentation
- Created `docs/M1_Simulation_Modification_Plan.md` with §2.5.6:
  - L2 block size 64B 고정 이유 (largeBlockCache artifact 방지)
  - β design: `L2Adapter.registerRegionIfNew` + dedup 원리
  - Reviewer 방어: 제안 기법 directory 1-entry-per-region = β dedup
- Created `TODO_PHASE2.md`: PHASE 2 P1 sanity check (adapter dedup vs real directory)

### Task A1+A2 — β design implementation
- `common.go`: `PhaseResetable interface { ResetPhase() }`
- `phase_lifecycle.go`: `RegisterPhaseLifecycle(..., resetables ...PhaseResetable)`
- `l2_adapter.go`:
  - `NewL2Adapter(m, cfg DirectoryConfig)` — mapper injection
  - `registerRegionIfNew(addr)` — per-phase dedup via `EntryTag`
  - `OnL2Access(hit bool, addr uint64)` — hit/miss + region activation
  - `OnRegionFetch(addr, sizeBytes)` — delegates to `registerRegionIfNew`
  - `ResetPhase()` — clears dedup map
- `cu_adapter.go`:
  - `NewCUAdapter(m, cfg DirectoryConfig)` — mapper injection
  - `OnRegionAccess(addr)` — uses `EntryTag` + `SubOffset`
  - `WarningCount()` — V12 ordering warnings
- Tests I1~I7 updated (new constructor signatures)
- New tests I8~I11 + `TestRegisterPhaseLifecycle_ResetsL2AdapterOnBoundary`
- All 18 tests GREEN, go vet clean

## Morning checklist (사용자용)

1. `git log m1-phase-b3-beta-design` 으로 commit 순서 확인 (5 commits)
2. PR 생성: `https://github.com/MJS0924/mgpusim/pull/new/m1-phase-b3-beta-design`
3. **B-3.5 overhead 측정 진입 승인 여부** 결정 (현재 BLOCKED — 사용자 승인 필요)
4. `docs/M1_Simulation_Modification_Plan.md` §2.5.6 내용 확인
5. `TODO_PHASE2.md` PHASE 2 P1 sanity check 항목 검토

## Known issues

- `writebackcoh` go test: pre-existing `writebufferstage.go` format bug (unrelated)
- `amd/timing/cu` go test: missing mockgen-generated files (pre-existing, unrelated)
- B-3.5 (overhead measurement): 사용자 승인 없이 진입 금지 — 현재 대기 중
