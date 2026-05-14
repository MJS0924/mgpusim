# STEP0 Findings — M1-MOD-7 PHASE A

> Generated: 2026-04-23
> Investigator: Claude Code (M1-MOD-7 PHASE A)
> All claims accompanied by file:line evidence.

---

## Finding a — `PhaseClock.SignalKernelBoundary` 호출 지점

**결론: 현재 simulation 실행 중 `SignalKernelBoundary`가 호출되는 곳이 없다 — BLOCKING FINDING.**

- **정의 위치**: `instrument/phase_clock.go:110`
  ```go
  func (c *PhaseClock) SignalKernelBoundary(kernelID string, cycle uint64)
  ```
- **호출 위치**: `cmd/m1/main.go`와 `cmd/m1/runner.go` 전체에서 `SignalKernelBoundary` 호출 없음. Repo 전체 grep 결과 definition 및 comment 외에 호출부 0개.
- **명시적 TODO**: `cmd/m1/main.go:10` 주석:
  ```
  // Kernel boundary: not available without driver modification (see TODO_PHASE2.md).
  ```
  `cmd/m1/runner.go:105` 주석:
  ```
  // Phase clock driven by engine events (window-only; kernel boundary
  // not available without driver modification — see TODO_PHASE2.md).
  ```
- **커널 완료 시점의 코드 경로**:
  `amd/driver/driver.go:527` → `processLaunchKernelReturn()` → `amd/driver/driver.go:538` `logCmdComplete(cmd)` → `amd/driver/driver.go:380-382`:
  ```go
  func (d *Driver) logCmdComplete(cmd Command) {
      tracing.EndTask(cmd.GetID(), d)
  }
  ```
  `CommandHookInfo` 구조체(`amd/driver/commandhookinfo.go:9`)가 `IsStart bool` 필드를 가지나, `logCmdComplete`는 현재 `InvokeHook`을 호출하지 않음.

**PHASE B 계획**: `logCmdComplete` 내부에서 `*LaunchKernelCommand` 타입 체크 후 `d.InvokeHook(...)` 추가. `cmd/m1/runner.go`에 driver hook 등록하여 kernel 완료 시 `clock.SignalKernelBoundary(cmd.GetID(), cycle)` 호출. Kernel ID는 `LaunchKernelCommand.GetID()`(simulation-unique string) 사용 — FNV hash 불필요.

---

## Finding b — `motion_event_sink.go` Parquet schema와 추가할 필드

**기존 schema** (`instrument/adapter/motion_event_sink.go:18-27`):
```go
type ParquetMotionEvent struct {
    EventType   string  `parquet:"event_type"`    // "promote" / "demote"
    TimeSec     float64 `parquet:"time_sec"`       // sim.VTimeInSec cast
    Address     uint64  `parquet:"address"`
    FromBank    int32   `parquet:"from_bank"`
    ToBank      int32   `parquet:"to_bank"`
    SharerCount int32   `parquet:"sharer_count"`
    ValidSubs   int32   `parquet:"valid_subs"`
    Utilization float64 `parquet:"utilization"`
}
```

**M1-MOD-7가 필요로 하는 BankSnapshot schema** (별도 신규 Parquet 파일):
```
snapshot_cycle   uint64   // GPU cycle (PhaseClock 기준)
gpu_id           int32    // GPU device ID (superdirectory.Comp.deviceID)
kernel_id        string   // kernel boundary 이후 current kernel (빈 문자열 = 미시작)
bank0_count      int32    // 유효 entry 수 per bank (Bank 0 = 16KB/64KB coarsest)
bank1_count      int32
bank2_count      int32
bank3_count      int32
bank4_count      int32    // Bank 4 = 64B/256B finest
cum_promotions   int64    // EventLogger.buf 내 promote count (누적)
cum_demotions    int64    // EventLogger.buf 내 demote count (누적)
cum_evictions    uint64   // Comp.EvictCount() (누적)
```

**기존 motion_event_sink.go는 수정하지 않음** — 별도 `instrument/adapter/bank_snapshot_sink.go` 신규 작성. 기존 파일 참조: Parquet writer 패턴은 `motion_event_sink.go:34-42`(파일 생성), `:60-80`(flush 패턴)을 동일하게 따름.

---

## Finding c — per-bank entry count 획득 경로

**`SuperDirectory` interface** (`akita/mem/cache/superdirectory/internal/directory.go:131`):
```go
GetBanks() [][]CohSet
```
`CohSet`은 `CohEntries []*CohEntry` 필드를 가짐 (`directory.go:139`).
유효 entry 판단은 `CohEntry.IsValidEntry()` (`directory.go:57-66`).

**`Comp`에서의 접근 경로**: `Comp.directory`는 `internal.SuperDirectory` 인터페이스 (`superdirectory.go:85`). 현재 `Comp`에는 외부에서 호출 가능한 `NumValidEntries(bankID int) int` 메서드가 없음.

**추가 계획 (PHASE B)**: `superdirectory.go`에 아래를 추가:
```go
// BankEntryCount returns the number of valid entries in the given bank.
// Read-only; must not be called while the simulation Tick is in progress.
func (c *Comp) BankEntryCount(bankID int) int {
    banks := c.directory.GetBanks()
    if bankID < 0 || bankID >= len(banks) {
        return 0
    }
    count := 0
    for _, set := range banks[bankID] {
        for _, entry := range set.CohEntries {
            if entry.IsValidEntry() {
                count++
            }
        }
    }
    return count
}
```
Coherence-critical path 영향 없음: snapshot은 window/kernel boundary callback에서만 호출 → simulation tick 사이 idle 구간. Lock semantics: single-goroutine akita 모델로 concurrent access 없음 (akita SerialEngine 보장).

---

## Finding d — Superdirectory 초기 상태 (신규 entry 할당 bank)

**근거**: `akita/mem/cache/superdirectory/directorystage.go:doWriteMiss()`:
```go
// line ~278:
bankID := ds.cache.numBanks - 1
```
Miss 발생 시 항상 `numBanks - 1 = 4` (finest bank, Bank 4)에 entry를 할당.

**RSB 참조 경우**: `selectBank()` (`directorystage.go` 하단)에서 RSB hit 시 RSB에 기록된 이전 bank로 라우팅될 수 있으나, 최초 cold start에서는 RSB miss → 항상 Bank 4에서 시작. `doWriteMiss()` line의 RSB 참조 주석 블록은 주석 처리됨:
```go
// e := ds.cache.regionSizeBuffer.Search(...)
// if e.RegionID != -1 { bankID = e.RegionID ... }
```

**결론**: 초기 상태에서 모든 신규 entry는 Bank 4 (finest, s_k=64B)에서 시작. Promotion 발생 시 상위(coarser) bank로 이동. `STEP0_findings.md` 요구사항 충족.

---

## Finding e — Bank size 매핑 확인 (v2 spec과 코드 일치 여부)

### `regionLen` 배열 계산 (`akita/mem/cache/superdirectory/builder.go:282-285`):
```go
regionLen := []int{}
for i := b.numBanks - 1; i >= 0; i-- {
    regionLen = append(regionLen, int(b.log2BlockSize+uint64(i)*b.log2NumSubEntry))
    // regionLen = {14, 12, 10, 8, 6}
}
```

### 실제 값 (log2BlockSize=6, log2NumSubEntry=2, numBanks=5):

| Array index | `i` (loop) | regionLen[index] | s_k = 2^value | c_k = 4 × s_k |
|-------------|-----------|-----------------|---------------|----------------|
| 0 (Bank 0) | 4 | 14 | 16,384 B = **16 KB** | 65,536 B = **64 KB** |
| 1 (Bank 1) | 3 | 12 | 4,096 B = **4 KB** | 16,384 B = **16 KB** |
| 2 (Bank 2) | 2 | 10 | 1,024 B = **1 KB** | 4,096 B = **4 KB** |
| 3 (Bank 3) | 1 | 8 | 256 B | 1,024 B = **1 KB** |
| 4 (Bank 4) | 0 | 6 | 64 B | 256 B |

**M1-MOD-7 prompt v2 authoritative table과 완전 일치. 코드 수정 불필요.**

### Reviewer Red-Team Q1 대응:
- "과거 스펙(Bank 4 = 64B 단일 level)과 차이": 과거 스펙에서 `c_4 = s_4 = 64B`는 `log2NumSubEntry=0` (sub-entry 1개)일 때 발생. 현재 코드는 `log2NumSubEntry=2`로 `c_4 = 4×64B = 256B`. Design Invariant V8 (`c_k = 4×s_k`) 성립.
- `log2NumSubEntry=2`와 `log2BlockSize=6` 정합성: `s_4 = 2^6 = 64B = DefaultBlockSizeBytes` ✓

### 총 entry 수 실측:

r9nano builder (`amd/samples/runner/timingconfig/r9nano/builder.go:855`) 기준:
- `byteSize = 512 KB` (`builder.go:105`)
- `wayAssociativity = 8` (`builder.go:855` `WithWayAssociativity(8)`)
- `log2BlockSize = 6` → blockSize = 64B

```
numSet_base = 512×1024 / (8×64) = 1024 sets
NumSets[i] = 1024 >> 5 << i
```

| Bank | NumSets | Ways | Entries |
|------|---------|------|---------|
| 0 | 32 | 8 | 256 |
| 1 | 64 | 8 | 512 |
| 2 | 128 | 8 | 1,024 |
| 3 | 256 | 8 | 2,048 |
| 4 | 512 | 8 | 4,096 |
| **Total** | 992 | — | **7,936** |

**주의**: M1-MOD-7 prompt의 `TOTAL_CAPACITY=8192` 수치와 다름. 실제 코드 기준 총 entry 수는 **7,936**. Python 분석 스크립트에서 이 값을 사용할 것. Invariant check: `sum(bank_*_count) ≤ 7936`.

---

## Finding f — 빌드 가능한 workload 후보군

`cmd/m1/runner.go:setupWorkload()` (전체 switch-case)에서 확인된 목록:

| 계획서 workload | 실제 binary 이름 | 상태 |
|----------------|-----------------|------|
| PR (PageRank) | `pagerank` | ✓ 사용 가능 |
| GEMM | `matrixmultiplication` | ✓ 사용 가능 (가장 근접) |
| GEMV | — | **없음** — `bicg` (GEMV 2회 포함 polybench) 또는 `atax`로 대체 |
| ATAX | `atax` | ✓ 사용 가능 |
| FIR | `fir` | ✓ 사용 가능 |
| SC (Stencil) | `stencil2d` | ✓ 사용 가능 |

추가 사용 가능: `simpleconvolution`, `matrixtranspose`, `bitonicsort`, `nbody`, `fastwalshtransform`, `floydwarshall`, `aes`, `kmeans`, `bicg`, `nw`, `bfs`, `fft`, `spmv`.

**GEMV 대체 결정**: `bicg`는 ATAX와 유사 구조. 다양성을 위해 6번째 workload로 `spmv` (희소 행렬 × 벡터) 추천. 최종 목록:

| # | 이름 | binary | 특성 |
|---|------|--------|------|
| 1 | PageRank | `pagerank` | scattered access |
| 2 | MatMul | `matrixmultiplication` | coalesced, high locality |
| 3 | ATAX | `atax` | mixed |
| 4 | FIR | `fir` | low locality |
| 5 | Stencil2D | `stencil2d` | structured |
| 6 | SpMV | `spmv` | irregular, sparse |

**빌드 확인**: `go build ./cmd/m1/` 성공 (repo root `/root/mgpusim_home/mgpusim/` 기준).

---

## Reviewer Red-Team Self-Check (PHASE A)

**Q2: "kernel boundary가 어느 이벤트에서 trigger되는가?"**
→ 현재 trigger 없음. Finding (a) 참조. PHASE B에서 driver의 `logCmdComplete` (`driver.go:380`)에 `InvokeHook` 추가 → `cmd/m1/runner.go`의 driver hook이 `clock.SignalKernelBoundary(cmd.GetID(), cycle)` 호출.

**Q3: "snapshot 주기가 coarse해서 초반 dynamics를 놓치지 않는가?"**
→ kernel boundary snapshot을 별도로 추가하면 주기 무관하게 kernel 완료 시점을 정확히 기록. 주기적 snapshot은 1K cycle 기본값(PHASE B에서 overhead 측정 후 조정).

---

## BLOCKING FINDINGS 요약

| # | 항목 | 내용 | PHASE B 해결 방법 |
|---|------|------|------------------|
| 1 | `SignalKernelBoundary` 미구현 | 어디서도 호출 안 됨 (`cmd/m1/main.go:10` 명시) | `driver.go:logCmdComplete`에 `InvokeHook` 추가 → runner에서 clock 호출 |
| 2 | `BankEntryCount()` 미구현 | `Comp`에 per-bank entry count accessor 없음 | `superdirectory.go`에 read-only 메서드 추가 |

Non-blocking 발견:
- TOTAL_CAPACITY 실제값 = **7,936** (not 8,192) — Python invariant 수정 필요
- GEMV workload 없음 → `spmv` 대체

---

---

## §discrepancies — 코드와 문서 간 불일치 목록 (PHASE B.0 추가)

### D1 — wayAssociativity: 4 (prompt default) vs 8 (실제 r9nano)

- **출처**: `amd/samples/runner/timingconfig/r9nano/builder.go:855`
  ```go
  WithWayAssociativity(8)
  ```
- **superdirectory builder 기본값** (`akita/mem/cache/superdirectory/builder.go:62`): `wayAssociativity: 4`
- **실제 적용값**: M1-MOD-7은 r9nano builder를 통해 실행되므로 `wayAssociativity=8` 사용.
- **영향**: TOTAL_CAPACITY 계산값 차이 (8192 → 7936). Python saturation script의 invariant 수정 필요.

### D2 — per-bank set count 비균등 (실측값)

`SuperDirectoryImpl.NumSets[i] = numSet >> bank << i` (`internal/directory.go:185`):

| Bank | NumSets | Ways | Max Entries (capacity) |
|------|---------|------|------------------------|
| 0 (coarsest, s_k=16KB) | 32 | 8 | 256 |
| 1 (s_k=4KB) | 64 | 8 | 512 |
| 2 (s_k=1KB) | 128 | 8 | 1,024 |
| 3 (s_k=256B) | 256 | 8 | 2,048 |
| 4 (finest, s_k=64B) | 512 | 8 | 4,096 |
| **Total** | 992 | — | **7,936** |

Sets가 Bank 0→4로 2× 증가(비균등). **Bank 4가 전체 capacity의 51.6% 독점**.
이는 초기 cold-start에서 Bank 4 급증 현상을 구조적으로 강화함.
`BankMaxCapacity(bankID)` accessor로 occupancy_ratio = `bank_k_count / BankMaxCapacity(k)` 계산 가능.

### D3 — GEMV workload 부재

- **검색 범위**: `amd/benchmarks/` 전체, `cmd/m1/runner.go:setupWorkload()` 전체.
- **결론**: `gemv`, `GEMV`, `matvec`, `mvt` 키워드 매치 0개. GEMV 없음.
- **대체**: `spmv` (SpMV, sparse matrix-vector multiply, `amd/benchmarks/shoc/spmv/`)
  - 이유: scatter-gather access pattern이 GEMV의 irregular access와 유사
  - `cmd/m1/runner.go:setupWorkload()` switch-case에 이미 구현됨.

### D4 — driver hook vs CP hook 사용 방침

- **기존 driver hook** (`amd/driver/driver.go:380` `logCmdComplete`): all-GPU kernel boundary. 모든 GPU의 LaunchKernelRsp가 driver로 수렴한 후 발화. **cross-check용으로만 유지** — 4-GPU 설정에서 단일 `all_kernels_done` 신호로 사용, per-GPU PhaseClock에는 사용하지 않음.
- **CP hook (PHASE B.1에서 신규 추가)**: per-GPU kernel boundary. 각 GPU의 dispatcher가 `completeKernel()` 시점에 해당 GPU PhaseClock을 독립적으로 trigger.

---

## §workload-coverage

**M1-MOD-7 실행 대상 6개 workload (확정):**

| # | 이름 | binary | access pattern |
|---|------|--------|----------------|
| 1 | PageRank | `pagerank` | scattered, irregular |
| 2 | MatMul | `matrixmultiplication` | coalesced, high locality |
| 3 | ATAX | `atax` | mixed (GEMV × 2) |
| 4 | FIR | `fir` | streaming, low locality |
| 5 | Stencil2D | `stencil2d` | structured, neighbor |
| 6 | SpMV | `spmv` | sparse, irregular |

GEMV 부재로 ATAX(내부적으로 GEMV × 2 포함)가 GEMV 특성 일부 커버.

---

## EXIT CRITERIA 점검 (PHASE A)

- [x] Finding a: `SignalKernelBoundary` 호출부 확인 + kernelID 출처 — **없음** (driver.go:380이 hook point)
- [x] Finding b: `motion_event_sink.go` schema 확인 + 추가 필드 정의
- [x] Finding c: per-bank entry count 경로 — `GetBanks()+IsValidEntry()`, accessor 신규 필요
- [x] Finding d: 초기 상태 — Bank 4에서 시작 (`directorystage.go:doWriteMiss`)
- [x] Finding e: Bank size 매핑 — v2 table 코드 일치 확인, TOTAL_CAPACITY=7936
- [x] Finding f: 실행 가능 workload 6개 확정

**BLOCKING FINDING 존재**: Finding a (kernel boundary 미구현), Finding c (accessor 미구현).
PHASE B 착수 전 user 승인 필요.
