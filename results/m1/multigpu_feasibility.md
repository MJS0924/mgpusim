# PHASE D.0 — Multi-GPU M1 Feasibility Assessment

**Date:** 2026-04-20
**Branch:** m1-phase-d0-multigpu-feasibility
**Test workload:** simpleconvolution 512×512 mask=3 (from PHASE C pilot)
**Test driver:** `cmd/m1` (unchanged from PHASE C)

## A — Coherence mode / GPU-count survey

`runner/flag.go:147` lists five coherence-directory modes; every mode
that uses the β-design region-tracking path passes through
`writebackcoh.Comp` L2 caches:

| `-coherence-directory` value | Internal code | Directory module | L2 module | Notes |
|---|---|---|---|---|
| `CoherenceDirectory` (default) | 0 | `optdirectory.Comp` | `writebackcoh.Comp` | Our PHASE C pilot used this |
| `LargeBlockCache` | 1 | `optdirectory.Comp` | `largeblkcache.Comp` | **no writebackcoh** |
| `SuperDirectory` | 2 | `superdirectory.Comp` | `writebackcoh.Comp` | M1 adapters compatible |
| `REC` | 3 | `optdirectory.Comp` (REC variant) | `writebackcoh.Comp` | **M1 excludes** |
| `HMG` | 4 | `optdirectory.Comp` (HMG variant) | `writebackcoh.Comp` | **M1 excludes** |

Multi-GPU is driven by `-gpus N1,N2,...`; the runner accepts any list.
The `r9nano` timing builder constructs one GPU per ID in the list.

## B — Path A: `writebackcoh` + default directory at 4 GPUs

**First attempt** (`-gpus 1,2,3,4`): panic during H2D memcopy before
kernel launch.

```
panic: runtime error: negative shift amount
  at optdirectory.(*Comp).recordAccessMask (coherencedirectory.go:340)
  from optdirectory.(*bankStage).InsertNewEntry (bankstage.go:269)
  from optdirectory.(*bankStage).finalizeTrans (bankstage.go:230)
```

Offending line (`coherencedirectory.go:340`):

```go
item |= 1 << (id - 2)
```

`id` comes from `srcToGPUID(srcPort.String())` which parses `GPU[N]`
from the source port name. When a request originates from `GPU[1]`,
`id - 2 = -1`, which is an invalid shift amount in Go and panics.

**Workaround attempt** (`-gpus 2,3,4,5`): run completes cleanly.

```
Simulation Terminate
M1_SUMMARY workload=simpleconvolution R=64 phases=3 RetiredWf=4132 \
  L2H=56909 L2M=33619 fetched=4213632 accessed=2105408 \
  V11=PASS V12=PASS output=.../simpleconvolution_R64_seed42.parquet
```

- 4 GPUs simulated end-to-end.
- V11 PASS (no directory evictions with `InfiniteCapacity=true`).
- V12 PASS (accessed=2,105,408 ≤ fetched=4,213,632, ≈ 50 % utilization,
  consistent with the single-GPU R=64 pilot).
- RetiredWf=4132 — close to the single-GPU 4129; the small delta comes
  from multi-GPU work distribution and will be investigated in D.1+,
  not in D.0.

**Conclusion — Path A is viable** as long as GPU IDs ≥ 2. The existing
M1 instrumentation (L2Adapter, CUAdapter, DirectoryAdapter,
PhaseClock, ParquetSnapshotSink) attaches correctly to all 4 GPUs
without code change.

## C — optdirectory panic: pre-existing / REC-HMG independence

**Pre-existing confirmation.** `git blame` on
`akita/mem/cache/optdirectory/coherencedirectory.go` lines 326-341
points to commit `429ce443` by MJS0924, dated 2026-04-19 13:07 UTC,
titled *"Add cache/vm/mem modifications for research"*. This predates
every B-phase M1 commit (the first akita-side M1 commit is `31bfdb3`
*[B-3.2-akita] writebackcoh: add HookPosL2Access + HookPosRegionFetch*).
The panic is therefore **pre-existing in akita** and was not introduced
by the M1 work — no M1 regression.

**REC / HMG independence.** `recordAccessMask` is called from
`bankstage.go:{269,290,308,328}` — four call sites on the shared
bank-stage path. This path runs in every `-coherence-directory` mode
that uses `optdirectory.Comp` (modes 0, 1, 3, 4). The function itself
contains no REC- or HMG-specific logic — it maintains a generic
`accessBitmask` keyed on block ID. `HMG` and `REC`-specific behavior
lives in separate code paths (`fetchSingleCacheLine`,
`emit*Metrics`). Conclusion: **the optdirectory panic is independent of
REC/HMG.** Fixing it does not require touching or depending on
REC/HMG code.

**Fix complexity (diagnosis only, not to be applied in D.0).** The
root cause is the hardcoded offset `id - 2`, which assumes GPU IDs
start at 2 (i.e., GPU[1] reserved for the CPU-side port). Two small
fixes are possible:

1. Change the shift to `id - minID` where `minID` is the smallest ID
   in `r.GPUIDs`. One-line change plus plumbing of `minID` into
   `optdirectory.Comp`.
2. Guard: `if id < 2 { return }` — drops the access-bitmask record for
   the CPU-origin path. Least-invasive but silently ignores CPU-side
   traffic in the debug-only `accessBitmask` map.

Either fix is ~1-5 lines in the akita repo (`coherencedirectory.go`
plus a builder field). No mgpusim changes required. **Complexity:
LOW.** Deferred out of D.0 scope per task instructions.

## D — Recommended path

**Path A (workaround-only) — recommended.**

- Use `-gpus 2,3,4,5` for the 4-GPU PHASE D runs. No code change.
- The M1 instrumentation works out of the box on 4 GPUs.
- Invariants V11, V12 hold; β dedup is expected to hold (to verify in
  D.1 with the R-sweep).

**Path B (optdirectory fix) — not required for D.1, but low-effort.**

- If a future D phase needs GPU[1] (e.g., to follow MGPUSim sample
  convention literally), the 1-line akita fix can be applied.
- Because the bug is pre-existing and REC/HMG-independent, the fix
  does not violate the M1 "no REC/HMG" principle.

**Path C (fallback to single-GPU) — not needed.**

- Path A already clears the blocker.

## Risk items carried into PHASE D.1

- **R-D0.1 (resolved).** 4-GPU simulation panics on `-gpus 1,2,3,4`.
  Mitigation: `-gpus 2,3,4,5` convention.
- **R-D0.2.** RetiredWavefronts delta (4132 vs 4129). Minor; flagged
  for investigation in D.1 if it grows with R.
- **R-D0.3.** Coalescability reports ("[PHASE 0 PASS]") appear in
  stdout per-GPU. These are from pre-existing akita printing paths and
  are informational only — the parquet sink output is unaffected.

## Estimated effort

- Path A (D.1 direct entry): 0 h blocker — proceed on user approval.
- Path B (if needed later): ~1-2 h for the akita-side fix + regression
  check.

## Verdict

**READY for PHASE D.1 under Path A.** Awaiting user approval before
proceeding.
