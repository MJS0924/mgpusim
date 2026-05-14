# 인수인계: SD/REC 데드락 root cause 및 fix 미적용 상태

작성: 2026-05-10. 데드락 root cause 진단 완료, fix 미적용 상태에서 다른 대화로 이어가기 위한 문서.

---

## 1. 현재 데드락 사례

### 1.1. matrixtranspose CD_8 (이전, fix 적용 후 해결)
- **증상**: `[Warning] No More Event: 0 0` + `[Driver DEBUF 5] Engine stops running`
- **위치**: optdirectory (CD)의 inv 처리 path
- **적용된 fix**: 옵션 C
  - inv 전용 송신 큐 분리 (`invRemoteSendToBottomQue` for CD, `sendToRemoteBottomInvQue` for SD/REC)
  - `sendToBottom()`에서 inv queue 우선 drain
  - `maxInflightEviction` 128 → 1024 → 256 (조정 후)
- **결과**: 재실행 시 데드락 안 걸림 (matrixtranspose 11개 변종 모두 완주)

### 1.2. stencil2d SD (현재, 미해결 — 본 문서의 핵심)
- **증상**: SD 프로세스 alive 7시간 24분, CPU 33%로 라이브락. CSV 396 lines에서 5.9시간째 멈춤.
- **stdout 로그 패턴**:
  - `[doWrite] stall: finer MSHR entry overlaps coarser request`
  - `[doWriteHit] Cannot push to bottom sender buffer`
  - `[doWrite] Pipeline is full`
- **runtime state (사용자가 dlv로 확인)**:
  - `GPU[5].L2Cache[1].writeBuffer.inflightEviction = 128` (= maxInflightEviction CAP 도달)
  - `GPU[5].L2Cache[1].writeBuffer.pendingEvictions = 896` (큐에 쌓여있음)
  - `GPU[4].L2Cache[1].dirStage.activeString = "[doRead] [handleReadMiss] MSHR is full"`
  - localBuf (cap=2)에 read trans 2개 가득, 모두 MSHR full로 stall

---

## 2. 검증된 데드락 사이클

```
GPU[5].L2Cache[1] (writebackcoh)        GPU[4].L2Cache[1] (writebackcoh)
─────────────────────────────────       ─────────────────────────────────
inflightEviction = 128 (CAP)            MSHR is full (CAP)
↓                                        ↓
eviction 응답 대기              ←───→   read miss 처리 못 함
↓                                        ↓
새 write 처리 못 함                      새 fetch 응답 못 보냄
└──────────── circular wait ─────────────┘
```

**Root cause**: writebackcoh L2 cache의 `maxInflightEviction = 128` cap 도달 + 다른 L2의 MSHR full → 양 L2가 서로 응답 못 보내는 cross-GPU circular wait.

---

## 3. SD/REC stall counter (검증)

### SD (sim time 0.015s)
| Counter | 값 | 비율 (per sim sec) |
|---|---:|---:|
| stallBottomBufFull | 233M | 15.5B/s |
| stallInflightFetch | 78M | 5.25B/s |
| stallBottomPortBusy | 24M | 1.6B/s |
| stallInflightInv | 7M | 483M/s |

### REC (sim time 0.059s)
| Counter | 값 | 비율 |
|---|---:|---:|
| stallBottomBufFull | 345M | 5.8B/s |
| stallBottomPortBusy | 322M | 5.5B/s |
| stallInflightInv | **158M** | 2.7B/s |
| stallInflightFetch | 34M | 581M/s |
| silentResetCount | 1.28M | — |

**해석**:
- SD: **fetch path 중심 stall** (Inflight**Fetch** > Inflight**Inv**)
- REC: **inv path 중심 stall** (Inflight**Inv** > Inflight**Fetch**)
- 옵션 C(inv 분리)는 REC type 데드락엔 도움됐지만, **SD type 데드락엔 무관**

---

## 4. 코드 구조 (검증된 사실)

### 4.1. Buffer cap (모든 directory 변종)
- `numReqPerCycle = 16` (r9nano builder가 `WithNumReqPerCycle(16)` 명시)
- 모든 internal buffer cap = 16 (bottomSenderBuf, mshrStageBuf, dirStageBuf, etc.)

### 4.2. Directory MSHR 동작 (일반 cache와 다름)
- directory MSHR entry는 **fetch 응답을 기다리지 않음**
- bankstage가 trans 처리 → bottomSenderBuf + mshrStageBuf에 동시 push
- mshrStage가 mshrStageBuf 처리 → 즉시 `mshr.Remove`
- 응답 처리는 bottomSender의 `processDataReadyRsp`에서 별도 (MSHR과 무관)

### 4.3. Promotion/Demotion
- `mshrStage.go:160/196/320`에서 `mshr.Add` 호출하지만 **fetch 발사 안 함**
- bank 재할당만 수행 (entry 다른 bank로 이동)
- promotionQueue/demotionQueue로 들어가서 별도 처리

### 4.4. Inflight cap 검증
- `bottomSender.tooManyInflightRequest()`:
  - isLocal=true: limit = 96 (= 128 × 75%)
  - isLocal=false: limit = 32 (= 128 × 25%)
- cap 도달 → trans head가 bottomSenderBuf에서 못 빠짐 → 누적 stall

### 4.5. writebackcoh L2 측 cap
- `maxInflightEviction = 128` (writebackcoh.writeBufferStage)
- `maxInflightFetch = 128`
- `writeBufferCapacity = 1024`
- MSHR 사이즈는 별도 확인 필요

---

## 5. 적용 고려된 fix 옵션 (미적용)

| 옵션 | 변경 | 효과 | 위험 |
|---|---|---|---|
| **A. writebackcoh maxInflightEviction 증가** | 128 → 512 또는 1024 | inflight slot 여유 ↑, 데드락 해소 | inv cost 약화 (이전 maxInflightEviction 1024 vs 256 논의) |
| **B. writebackcoh MSHR 증가** | 64 → 256 | read miss 처리 capacity ↑ | 자원 inflation |
| **C. eviction 응답 우선 drain path** | eviction 응답을 다른 응답보다 우선 | 회전율 ↑ | 복잡 |
| **D. cross-GPU eviction 우선순위** | 데드락 cycle 회피 | 정확한 fix | 매우 복잡 |
| **E. MSHR entry upgrade-and-merge (사용자 제안)** | finer entry를 coarser로 승격 후 merge | conflict 자체 제거 | 응답 routing 어려움 |

**추천**: A + B 조합이 가장 직접적. cap 증가는 inv cost 약화 우려가 적음 (eviction은 inv와 다른 path).

---

## 6. 적용된 코드 변경사항 (이전 sweep)

### 6.1. 옵션 C (Phase F equivalent)
**CD (optdirectory)**:
- [bottomSender.go:39-46](akita/mem/cache/optdirectory/bottomSender.go#L39): `invRemoteSendToBottomQue` 추가
- [bottomSender.go:767](akita/mem/cache/optdirectory/bottomSender.go#L767): `processInvalidationReq()`이 새 큐로 push
- [bottomSender.go:935-953](akita/mem/cache/optdirectory/bottomSender.go#L935): `sendToBottom()`에서 inv queue 우선 drain
- [builder.go:67](akita/mem/cache/optdirectory/builder.go#L67): `maxInflightEviction` 128 → 256

**SD (superdirectory)**:
- bottomSender.go: `sendToRemoteBottomInvQue` 추가, processInvalidationReq에서 사용, sendToBottom 우선 drain
- builder.go: `maxInflightEviction` 128 → 256

**REC**:
- 동일 패턴 적용

### 6.2. 기타 fix (앞선 작업)
- per-window snapshot의 RDMA bytes/L2 카운터 fix (writebackcoh의 incEvent migration 후 windowsnapshot 미업데이트 문제)
- SD cacheline coverage weighted formula (bank별 region size 가중)
- ideal 변종 per-window 플래그 추가 (2_make_shell.py + 9개 ideal script)
- optdirectory의 "Impossible GPU ID" stdout spam 제거

---

## 7. 현재 sweep 상태

- **Master sweep PID**: `636758` (2일 2시간 56분 진행)
- **현재 활성**:
  - matrixmultiplication 4개 (SD/REC/REC_halfset/HMG, 별도 process)
  - stencil2d 진행 중 (SD가 데드락, 다른 변종 진행)
- **완주된 워크로드**: fir, im2col, matrixtranspose, pagerank, spmv (모두 11개 변종)
- **남은 워크로드**: stencil2d (CD), relu, conv2d
- **`/root/mgpusim_home/script/run_all.sh`**: matrixtranspose 라인 활성, fir/im2col/matmul 주석 처리
- **`/root/mgpusim_home/script/run_all.sh.bak`**: 원본 백업

---

## 8. 데드락된 SD 프로세스 처리

stencil2d SD가 라이브락 상태로 7시간+ alive. fix 적용 전까지:
- 옵션 1: 그대로 두고 다른 변종이 sweep을 막지 않게 함 (현재 진행 가능)
- 옵션 2: kill하고 데이터 결손 처리

만약 fix 적용 후 재실행하려면:
1. stencil2d SD process kill
2. stencil2d SD CSV/sqlite 청소
3. writebackcoh fix 적용 후 rebuild
4. stencil2d SD만 별도 실행 또는 sweep 재시작

---

## 9. 핵심 파일

### 코드
- `/root/mgpusim_home/akita/mem/cache/writebackcoh/writebufferstage.go` — writeBuffer 구현
- `/root/mgpusim_home/akita/mem/cache/writebackcoh/builder.go` — maxInflightEviction 등 default 값
- `/root/mgpusim_home/akita/mem/cache/superdirectory/directorystage.go` — finer MSHR overlap 검사 (line 446-455)
- `/root/mgpusim_home/akita/mem/cache/superdirectory/bottomSender.go` — bottomSenderBuf 처리, inflight cap

### 분석 도구
- `/tmp/perf_check.py` — per-window 성능 비교 (5분 stale 필터)
- `/root/mgpusim_home/results/summary.csv` — 누적 metrics
- `/root/mgpusim_home/results/per_window/<workload>/<workload>_<variant>_per_window.csv` — per-window snapshots

### 활성 cron (옵션)
- `c2eddf4f`: 10분마다 활성 워크로드 perf 비교 (세션 종료 시 사라짐)

---

## 10. 다음 단계 권장

1. **옵션 A 시도**: writebackcoh의 `maxInflightEviction` 128 → 512로 증가
2. 빌드 후 stencil2d SD만 재실행 (다른 변종 데이터 유지)
3. stall counter 변화 확인 — `stallInflightFetch`/`stallBottomBufFull`이 줄어드는지
4. 데드락 재발 시 옵션 B (MSHR 증가) 추가
5. 그래도 안 되면 옵션 C/D/E 검토

진단은 verified, fix 결정은 사용자에게 위임된 상태.
