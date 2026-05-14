# Investigation B: Counter Audit — Pipeline Stage Verification

**Date:** 2026-04-24  
**Workload:** fir (R=64, seed=42)  
**Config:** `-gpus 2,3,4,5 -coherence-directory=SuperDirectory -timing`  
**Status:** COMPLETE — pipeline confirmed end-to-end

## Pipeline Stages Under Audit

```
RDMA arrives at superDir
    ↓ remoteAcceptCount
doWriteMiss called (from remote)
    ↓ doWriteMissRemote
writeToBank called → allocationCount++
    ↓ allocationCount (parquet: cum_allocations)
BankEntryCount() > 0
    ↓ bank{0-4}_count (parquet)
```

## Measured Values — Correct GPU IDs (`-gpus 2,3,4,5`)

| GPU | remoteAccept | doWriteMiss (total) | doWriteMissRemote | cum_alloc | bank4_last |
|-----|-------------|---------------------|-------------------|-----------|------------|
| 1   | 0           | 0                   | 0                 | 0         | 0          |
| 2   | 1           | 2302                | 1                 | 1         | 1          |
| 3   | 1           | 2022                | 1                 | 1         | 1          |
| 4   | 1           | 2138                | 1                 | 1         | 1          |
| 5   | 0           | 2121                | 0                 | 0         | 0          |

GPU1 is the staging dummy (never receives compute traffic — expected).  
GPU5's `doWriteMiss=2121` is all local (`doWriteMissRemote=0`); the fir workload did not route remote writes to GPU5 in this run.

## Comparison — Wrong GPU IDs (`-gpus 1,2`, mini-pilot)

| GPU | remoteAccept | doWriteMissRemote | cum_alloc |
|-----|-------------|-------------------|-----------|
| 1   | 0           | 0                 | 0         |
| 2   | 0           | 0                 | 0         |

All zeros: first-touch migration (mmu.go:304) converts all cross-GPU accesses to local.

## Key Architectural Facts Confirmed

1. **`doWriteMiss` is LOCAL-only for local requests** (`directorystage.go:~620`):
   ```go
   if trans.fromLocal {
       trans.action = Nothing  // skip writeToBank
       return true
   }
   ```
   → `doWriteMiss total` is large (~2000) but nearly all local; only the 1 remote request reaches `writeToBank`.

2. **Bypass path** (`topparser.go:117`): `fromLocal || !toLocal → BypassingDirectory`.  
   Local reads and writes to non-local addresses are bypassed before reaching `doWriteMiss`.
   → The 1 remote write that reaches `doWriteMiss` is a genuine remote-origin write miss.

3. **RDMA wiring** (`builder.go:949`): `rdmaLowModuleFinder.LowModules` contains `superDir.RDMA` port.
   Remote requests from RDMA engine → `topPort` with `fromLocal=false` set in `topparser.go:60`.

4. **`alloc=3`** in the log line refers to `bs.TotalRows()` (3 snapshot rows written), NOT `cum_allocations`.
   `cum_allocations` is read from parquet and equals 1 per active GPU.

## Pipeline Status

| Stage | Status |
|-------|--------|
| RDMA arrives at superdirectory | ✓ CONFIRMED (remoteAccept=1 for GPU2,3,4) |
| doWriteMiss fires from remote | ✓ CONFIRMED (doWriteMissRemote=1) |
| writeToBank called → allocationCount++ | ✓ CONFIRMED (cum_alloc=1 in parquet) |
| BankEntryCount > 0 | ✓ CONFIRMED (bank4_count=1 at kernel boundary) |

## Conclusion

The full pipeline is confirmed end-to-end with `-gpus 2,3,4,5`.  
The failure mode under wrong GPU IDs is confirmed: RDMA is never invoked due to first-touch migration.  
No code fix is needed. The production PHASE C main run should use `-gpus 2,3,4,5`.
