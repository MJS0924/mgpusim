# Investigation C: Synthetic RDMA Micro-Test

**Date:** 2026-04-24  
**Status:** SUPERSEDED by Investigation B — see conclusion

## Original Plan

Write `samples/rdma_micro/main.go`:
- 2-GPU MGPUSim setup (GPU2, GPU3)
- Allocate data on GPU2, issue write kernel from GPU3
- After simulation: assert `BankSnapshotSink.TotalRows() > 0` and `cum_allocations > 0`

## Why Investigation B Supersedes This

Investigation B used the `fir` workload (minimal, 4KB FIR filter) as a functional equivalent.
The `fir` run with `-gpus 2,3,4,5` constitutes a real end-to-end RDMA micro-test because:

| Micro-Test Criterion | fir Run Result |
|----------------------|---------------|
| Data allocated on remote GPU | ✓ fir input array pages distributed across GPU2–5 |
| Write kernel executed by different GPU | ✓ each GPU executes its partition; cross-GPU writes occur |
| RDMA path fires | ✓ remoteAccept=1 for GPU2, GPU3, GPU4 |
| writeToBank called | ✓ doWriteMissRemote=1 per GPU |
| cum_allocations > 0 | ✓ cum_alloc=1 for GPU2, GPU3, GPU4 in parquet |
| Bank entry visible post-kernel | ✓ bank4_count=1 at is_kernel_boundary=true snapshot |

All criteria for a synthetic RDMA micro-test are met by the Investigation B run.

## What a Dedicated Micro-Test Would Add

A hand-crafted `rdma_micro` program with explicit 2-GPU RDMA would provide:
- **Isolation**: no multi-GPU workload complexity, pure RDMA path
- **Repeatability**: deterministic 1-write-per-GPU assertion
- **Regression**: single-file test runnable as unit test

This is recommended as future work for the HPCA 2027 supplemental material, but is not blocking for PHASE C main.

## Conclusion

Investigation C is **closed without a new binary**. The fir-based Investigation B run provides equivalent RDMA pipeline verification. A dedicated micro-test binary is deferred to post-PHASE-C cleanup.
