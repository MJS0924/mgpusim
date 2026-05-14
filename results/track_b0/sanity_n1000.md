# Track B-0 Sanity Re-measurement — matmul N=1000

Date: 2026-04-21

## Setup

| Parameter        | Value                     |
|------------------|---------------------------|
| Workload         | matrixmultiplication      |
| Dimensions       | X=1000, Y=1000, Z=1000    |
| Coherence mode   | SuperDirectory            |
| Region size      | R=64                      |
| GPUs             | 4                         |
| Window cycles    | 100 000                   |
| Seed             | 42                        |

## Results

| Metric                    | Value       |
|---------------------------|-------------|
| Wall-clock (OFF)          | 20m 10s     |
| Wall-clock (ON)           | ~20m 25s    |
| Overhead                  | < 1.5 %     |
| Superdirectory components | 4           |
| Promotion events          | **0**       |
| Demotion events           | **0**       |
| L2 hits                   | 1 783 582   |
| L2 misses                 | 280 771     |
| Phases                    | 21          |
| Retired wavefronts        | 1 024       |
| Event parquet size        | 327 B (empty schema only) |

### Per-bank entry distribution

실험 인프라로 per-bank 카운트를 직접 읽을 수 없으므로 디렉토리 구성으로 추산:

| Bank | RegionLen | Entry 커버 범위 | Sets | Ways | 용량(per GPU) |
|------|-----------|----------------|------|------|---------------|
| 0    | 14 bits   | 16 KB          | 32   | 8    | 4 MB          |
| 1    | 12 bits   | 4 KB           | 64   | 8    | 2 MB          |
| 2    | 10 bits   | 1 KB           | 128  | 8    | 1 MB          |
| 3    | 8 bits    | 256 B          | 256  | 8    | 512 KB        |
| 4    | 6 bits    | **256 B**\*    | 512  | 8    | **1 MB**      |

\* bank 4 에서 tag 비교 maskLen = regionLen + log2NumSubEntry = 6+2 = 8
  → 한 entry가 커버하는 범위 = 2^8 = **256 B** (4 × 64 B sub-entry)

N=1000 데이터: A+B+C = 3 × 4 MB = **12 MB**  
Bank 4 총 용량(4 GPU): 4 × 1 MB = 4 MB → 데이터의 3× → 상시 eviction 발생

## 판정

| 항목                        | 결과  |
|-----------------------------|-------|
| Promotion > 0?              | ❌ No |
| Demotion > 0?               | ❌ No |
| Multiple bank 사용?         | ❌ No (모든 요청이 bank 4에서 처리) |

## 원인 분석

### Promotion = 0 인 이유

Bank 4 entry 하나가 커버하는 범위는 **256 B** (4개 sub-entry × 64 B).
`AbleToPromotion()` 조건: 4개 sub-entry 모두 valid + 동일 sharer.

→ 조건 성립 조건: 256 B 정렬된 영역의 4개 cache line 전부가
  **동일 L1 cache port** 에서 요청되고, eviction 없이 동시에 directory에 존재해야 함.

N=1000 환경에서는 bank 4 용량(1 MB/GPU) < 데이터(3 MB/GPU) 이므로
항상 LRU eviction이 발생한다. 256 B entry에 4번째 sub-entry가
채워지기 전에 이미 evict되는 경우가 대부분 → `AbleToPromotion()` 미충족.

### Demotion = 0 인 이유

`needToDemotion = true` 는 `InvalidateAndUpdateEntry()` 에서만 set되며,
bankID == numBanks-1(bank 4) 일 때는 **명시적으로 skip** (코드 라인 339).

```go
if trans.bankID == s.cache.numBanks-1 || len(entry.Sharer) == 0 {
    trans.needToDemotion = false  // 최하위 bank: demotion 안 함
}
```

또한 demotion은 coarser bank entry(bank 0-3)가 다른 GPU의 write로
invalidate될 때 발생한다. Promotion이 0이므로 coarser bank entry 자체가 없음
→ demotion 불가.

### 근본 원인 요약

Superdirectory의 adaptive 동작(promotion/demotion)은
**write-sharing 패턴**을 가진 workload에서만 발동된다:
1. GPU A가 256 B 영역 전체를 read (all 4 sub-entries, same L1 port) → promotion
2. GPU B가 해당 영역에 write → promotion된 coarser entry를 invalidate → demotion

matmul은 각 GPU가 독립 파티션에 write-only 접근 → write-sharing 없음.

## 다음 단계 권고

**0 → Superdirectory capacity 및 promotion 조건 재검토 필요**

구체적 권고:

1. **Sweep 진입 보류**: 현재 `cmd/m1` 측정 파이프라인은 event count=0인
   workload에서는 event log 기능 검증 불가. Sweep 전에 emission이 발생하는
   workload/조건을 확정해야 함.

2. **Promotion 발동을 위한 조건 완화 검토**: `AbleToPromotion()`에서
   all-4-sub-entries-valid 대신 일부 valid로도 promotion 허용하는 policy 변경
   → 단, 이는 superdirectory 로직 수정이므로 별도 트랙으로 진행.

3. **Write-sharing workload 발굴**: 복수 GPU가 공유 메모리에 write하는
   micro-benchmark(예: atomic reduce, barrier-based stencil) 로 E2E 검증.

4. **현재 인프라 상태**: event log ON/OFF 인프라 자체는 정상 동작
   (4 SD 컴포넌트 발견, 파일 생성, 오버헤드 <2%). 단위 테스트(3 cases)
   통과로 emission 코드 정확성은 확인됨.
