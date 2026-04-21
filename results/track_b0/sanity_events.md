# Track B-0 Event Log — Sanity Measurement

Date: 2026-04-21

## Configuration

| Parameter       | Value                        |
|-----------------|------------------------------|
| Workload        | matrixmultiplication N=256   |
| Coherence mode  | SuperDirectory               |
| Region size     | R=64                         |
| GPUs            | 4                            |
| Window cycles   | 100 000                      |
| Seed            | 42                           |

## E.1 — Event Log ON Run

```
workload=matrixmultiplication R=64 phases=3 RetiredWf=64
L2H=64 L2M=8733 fetched=1116032 accessed=557120
V11=PASS  V12=PASS
```

| Metric                      | Value  |
|-----------------------------|--------|
| Superdirectory components   | 4      |
| Event log output path       | events.parquet |
| Event file size (bytes)     | 327    |
| Promotion events            | 0      |
| Demotion events             | 0      |
| Sample events               | n/a (0 events) |

### Finding: zero events is expected

Promotions require **all 4 sub-entries of an entry to be valid and to share the same
sharer set** before the entry can move to a coarser bank.  In the standard GPU benchmarks,
each wavefront/CU accesses a disjoint partition of data — sub-entries within one
256-byte-aligned region seldom all accumulate from the same L1 port before the entry
is evicted or superseded.

Demotions require an entry to already be at a bank coarser than the finest bank
(bank 4).  With no promotions occurring, every entry stays at bank 4, and
`InvalidateAndUpdateEntry` explicitly skips demotion at bank 4 (`bankID == numBanks-1`).

Observing non-zero counts requires a custom micro-benchmark with:
1. A 256-byte-aligned hot region read by exactly one L1 cache across ≥4 consecutive
   cache lines (triggers promotion).
2. A subsequent write to that region from a different GPU (triggers demotion).

This is by design: the unit tests in `event_log_test.go` inject transactions directly
into the promotion/demotion queues and verify the emission code is correct.

## E.2 — Overhead Measurement

| Run              | Wall time  |
|------------------|-----------|
| OFF (no log)     | 18.43 s   |
| ON  (log enabled)| 18.42 s   |
| Overhead         | **−0.1 %** (within measurement noise) |

Target: < 5 % overhead — **PASS**

## Infrastructure Verification

| Check                                              | Result |
|----------------------------------------------------|--------|
| Superdirectory components found by runner          | 4 ✓    |
| Event parquet file created                         | ✓      |
| File contains valid parquet header (empty dataset) | ✓      |
| No panics / errors during ON run                   | ✓      |
| V11 (no evictions with InfiniteCapacity)           | PASS ✓ |
| V12 (no CU warnings)                               | PASS ✓ |
| Unit tests (event_log_test.go, 3 cases)            | PASS ✓ |
