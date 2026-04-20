# PHASE C Pilot Sanity Report

**Workload:** simpleconvolution 512×512, mask=3
**Configs:** R ∈ {64, 256, 1024, 4096, 16384} bytes
**Seed:** 42
**Window:** 100,000 GPU cycles @ 1 GHz
**Date:** 2026-04-20

## Per-config summary

| R (B) | Phases | L2Hits | L2Misses | RegionFetchedBytes | ActiveRegions (final) | RetiredWf | V11 | V12 |
|------:|:------:|-------:|---------:|-------------------:|----------------------:|----------:|:---:|:---:|
|    64 |   3    | 51,062 |  32,908  |          2,665,600 |               23,090  |     4,129 | PASS | WARN |
|   256 |   3    | 51,062 |  32,908  |          5,414,912 |                5,974  |     4,129 | PASS | WARN |
| 1,024 |   3    | 51,062 |  32,908  |         21,659,648 |                1,741  |     4,129 | PASS | WARN |
| 4,096 |   3    | 51,062 |  32,908  |         49,139,712 |                  469  |     4,129 | PASS | WARN |
|16,384 |   3    | 51,062 |  32,908  |         57,294,848 |                  125  |     4,129 | PASS | WARN |

## Per-phase detail

All 5 configs produced the same 3-phase structure:

| Phase | Cycle Range       | L2H    | L2M    | RetiredWf |
|:-----:|:-----------------:|-------:|-------:|----------:|
|   0   | [0, 100000)       |      0 |      0 |         0 |
|   1   | [100000, 200000)  | 20,792 | 18,560 |     1,486 |
|   2   | [200000, 200000]  | 30,270 | 14,348 |     2,643 |

Phase 0 is kernel-launch overhead (no memory traffic yet). Phase 2 is the
final partial phase emitted by `m.Flush()` after `r.Run()` returns; its
cycle range is degenerate (start == end) because the engine stopped
mid-window. Totals per phase are identical across all R because cache
hits/misses and retired wavefronts are workload-invariant; only
`RegionFetchedBytes` and `ActiveRegions` vary with R (as expected).

## V11 — Directory evictions

**Invariant:** `DirectoryEvictions == 0` when `InfiniteCapacity=true`.

Verified: every row in all 5 parquet files has `evict=0`. **PASS**.

## V12 — `RegionAccessedBytes ≤ RegionFetchedBytes`

**Status:** WARN, `warningCount=197,632` per config, uniformly.

**Root cause:** The CU adapter observes `HookPosCUVectorMemAccess` when
the CU *issues* the memory request, which fires before the L2 fetch is
recorded. Under the current adapter ordering, the CU's region-access
update is rejected (warning) because the region has not yet been
"fetched" according to the L2 adapter. The consequence is
`RegionAccessedBytes = 0` in every phase row.

The technical invariant `0 ≤ fetched` still holds trivially, so V11/run
integrity is not compromised — but the `accessed_bytes` column carries
no signal in this pilot. Resolving this requires reordering the hook
callbacks or deferring CU updates until after L2 fetch completion;
tracked for Phase D follow-up.

## Region-size sensitivity (β dedup check)

`ActiveRegions` measures distinct regions touched. With larger R, more
addresses collapse into the same region, so `ActiveRegions` must
decrease monotonically as R grows — this is the β-dedup signal.

| R (B) | ActiveRegions (phase 2) | Ratio vs R=64 |
|------:|------------------------:|--------------:|
|    64 |                  23,090 |         1.00× |
|   256 |                   5,974 |         0.26× |
| 1,024 |                   1,741 |         0.075× |
| 4,096 |                     469 |         0.020× |
|16,384 |                     125 |         0.0054× |

Monotonic 184× reduction from R=64 to R=16384. **β dedup PASS.**

## Retired wavefront total

Theoretical (ceil(514×514 / 64)) = **4,129**.
Measured total across all phases, all 5 configs: **4,129** each.
Exact match — B-3.6 hook correctness confirmed in pipeline.

## Wall-clock per config

| R (B) | Elapsed (ms) |
|------:|-------------:|
|    64 |       55,419 |
|   256 |       54,580 |
| 1,024 |       54,723 |
| 4,096 |       53,424 |
|16,384 |       53,906 |

Variation < 4%; R does not materially affect simulation runtime at this
scale.

## Verdict

**CONDITIONAL PASS.**

- V11 (eviction=0): PASS across all configs.
- β dedup (ActiveRegions decreases with R): PASS, 184× reduction confirmed.
- RetiredWavefronts total: matches theoretical 4,129 exactly.
- V12 (accessed ≤ fetched): technical PASS, but `accessed_bytes` is zero
  due to CU/L2 hook ordering. This column is unusable in the current
  pilot output and must be fixed before Phase D modeling relies on it.

Pipeline is ready to scale to multi-workload Phase D *after* the V12
ordering issue is resolved.
