# Investigation A: Environment Diff — Mini-Pilot vs PHASE D

**Date:** 2026-04-24  
**Status:** COMPLETE

## Root Cause

The C.0 mini-pilot used `-gpus 1,2` (wrong). PHASE D used `-gpus 2,3,4,5` (correct).
This single flag difference explains all V1/V2 failures (bank counts = 0, cum_allocations = 0).

## MGPUSim GPU ID Convention

| GPU ID | Role |
|--------|------|
| GPU 1  | Staging/dummy GPU — exists for initialization, never computes |
| GPU 2–5 | Active compute GPUs |

Source: `results/m1/multigpu_feasibility.md` (D.0 finding), PHASE D sweep scripts.

## Why `-gpus 1,2` Breaks RDMA

When GPU 1 is in the active set, unified memory allocates pages with `DeviceID=1` on first touch.
`mmu.go:304` contains a hard-coded rule:

```go
if walking.page.DeviceID == 1 && walking.req.DeviceID != 1 {
    return true  // migrate to requesting GPU
}
```

**Effect:** Every cross-GPU access involving GPU1-resident pages triggers page migration instead of RDMA.
After migration, the page lives on the requesting GPU → subsequent accesses are local → RDMA path never fires.

## Why `-gpus 2,3,4,5` Works

No GPU 2–5 pages have `DeviceID=1`. Cross-GPU accesses (e.g. GPU3 accessing GPU2's data) go through RDMA:
1. L2 miss → `L2BottomMapper` routes to RDMA engine (address outside local range)
2. RDMA engine → PCIe → target GPU's RDMA engine
3. `rdmaLowModuleFinder` routes to `superDir.RDMA` port
4. `topParser` sets `fromLocal=false` → `remoteDirStageBuffer` → `doWriteMiss` → `writeToBank`

## Diff Table

| Parameter                  | Mini-Pilot (WRONG)      | PHASE D / Fix (CORRECT)    |
|---------------------------|-------------------------|----------------------------|
| `-gpus`                   | `1,2`                   | `2,3,4,5`                  |
| `-coherence-directory`    | `SuperDirectory`        | `SuperDirectory`           |
| GPU 1 in active set       | YES                     | NO                         |
| First-touch migration     | Fires for all X-GPU access | Never fires            |
| RDMA path taken           | NEVER                   | YES (confirmed by diag)    |
| `remoteAcceptCount`       | 0 for all GPUs          | 1–1 per GPU (GPU2–4)       |
| `cum_allocations` (last)  | 0 for all GPUs          | 1 for GPU2, GPU3, GPU4     |

## Parquet Evidence

```
WRONG (-gpus 1,2):
  fir_gpu1: cum_alloc=0  bank4_last=0
  fir_gpu2: cum_alloc=0  bank4_last=0

CORRECT (-gpus 2,3,4,5):
  fir_gpu1: cum_alloc=0  bank4_last=0  (staging dummy — expected)
  fir_gpu2: cum_alloc=1  bank4_last=1  ✓
  fir_gpu3: cum_alloc=1  bank4_last=1  ✓
  fir_gpu4: cum_alloc=1  bank4_last=1  ✓
  fir_gpu5: cum_alloc=0  bank4_last=0  (no remote traffic in fir/4-GPU run)
```

## Conclusion

Investigation A is **CLOSED**. The environment diff is exactly one flag: `-gpus 1,2` → `-gpus 2,3,4,5`.
All subsequent runs must use `-gpus 2,3,4,5`.
