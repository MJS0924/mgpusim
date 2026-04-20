# PHASE C Pilot Re-run — V12 Fix Verification

**Workload:** simpleconvolution 512×512, mask=3
**Configs:** R ∈ {64, 256, 1024, 4096, 16384} bytes
**Seed:** 42
**Window:** 100,000 GPU cycles @ 1 GHz
**Date:** 2026-04-20
**Change under test:** β extended — CUAdapter auto-registers region on
access (commit b0b229fa); L2Adapter skips AddRegionFetch when the region
is already registered (shared dedup via `PhaseMetrics.IsRegionFetched`).

## Summary table (per config)

| R (B) | Phases | L2Hits | L2Misses | RegionFetchedBytes | RegionAccessedBytes | ActiveRegions (final) | Utilization | RetiredWf | V11 | V12 |
|------:|:------:|-------:|---------:|-------------------:|--------------------:|----------------------:|------------:|----------:|:---:|:---:|
|    64 |   3    | 51,062 |  32,908  |          5,336,832 |           2,671,232 |                46,254 |    50.05 %  |     4,129 | PASS | PASS |
|   256 |   3    | 51,062 |  32,908  |          5,585,408 |           2,671,232 |                11,960 |    47.82 %  |     4,129 | PASS | PASS |
| 1,024 |   3    | 51,062 |  32,908  |          6,660,096 |           2,671,232 |                 3,486 |    40.11 %  |     4,129 | PASS | PASS |
| 4,096 |   3    | 51,062 |  32,908  |          7,286,784 |           2,671,232 |                   939 |    36.66 %  |     4,129 | PASS | PASS |
|16,384 |   3    | 51,062 |  32,908  |          7,815,168 |           2,671,232 |                   251 |    34.18 %  |     4,129 | PASS | PASS |

`Utilization = RegionAccessedBytes / RegionFetchedBytes`, whole-run totals.

## V11 — DirectoryEvictions

`evict=0` in every row of every parquet file. **PASS** across all 5 configs.

## V12 — `RegionAccessedBytes ≤ RegionFetchedBytes`

All 5 `M1_SUMMARY` lines report `V12=PASS`; `warningCount=0` (not
surfaced because zero). `RegionAccessedBytes` is non-zero in every
active phase (phase 0 is the pre-kernel warm-up window, intentionally
empty). This resolves the `warningCount=197,632` / `accessed=0` failure
observed in the first Phase C pilot.

## β dedup sensitivity (D.2)

| R (B) | ActiveRegions (final phase) | Ratio vs R=64 |
|------:|----------------------------:|--------------:|
|    64 |                      46,254 |         1.00× |
|   256 |                      11,960 |         0.259× |
| 1,024 |                       3,486 |         0.0754× |
| 4,096 |                         939 |         0.0203× |
|16,384 |                         251 |         0.00543× |

Monotonic 184× reduction from R=64 to R=16,384. **β dedup PASS.**
(ActiveRegions numbers are higher than the first pilot because the CU
adapter now contributes its own access-driven regions; the ratio shape
is preserved.)

## Region-size utilization — the motivation signal

| R (B) | Utilization | Over-fetch factor |
|------:|------------:|------------------:|
|    64 |     50.05 % |            2.00×  |
|   256 |     47.82 % |            2.09×  |
| 1,024 |     40.11 % |            2.49×  |
| 4,096 |     36.66 % |            2.73×  |
|16,384 |     34.18 % |            2.93×  |

Utilization **decreases monotonically** as R grows — the exact
sparse-access pattern the β motivation experiment predicts. A small
fraction (~1/3) of each 16 KB region is actually touched, so coarse
regions pay a ~3× over-fetch cost. This is the signal the β design
needs to justify fine-grained region tracking.

## RetiredWavefronts total

4,129 per config — exact match with ceil(514²/64). Unchanged by the V12
fix, as expected.

## Wall-clock per config

| R (B) | Elapsed (ms) |
|------:|-------------:|
|    64 |       55,448 |
|   256 |       54,197 |
| 1,024 |       54,170 |
| 4,096 |       52,893 |
|16,384 |       53,275 |

Runtimes within 5 % of the original pilot — the auto-register branch
adds no meaningful overhead.

## Verdict

**PASS.**

- V11 (DirectoryEvictions=0): PASS across all 5 configs.
- V12 (accessed ≤ fetched, and >0): PASS; warningCount=0 everywhere.
- β dedup (ActiveRegions monotonically ↓ with R): PASS, 184× reduction preserved.
- RetiredWavefronts total = 4,129 per config (exact theoretical match).
- Utilization trend: monotonic 50 % → 34 %, matches β motivation intuition.

The pipeline now produces fully-populated phase snapshots. Recommend
proceeding to PHASE D after user review.