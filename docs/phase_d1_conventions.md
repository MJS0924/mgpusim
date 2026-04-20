# PHASE D.1 standing conventions

Captured 2026-04-20 from user-supplied edits (변경 1–5) before PHASE D.1
entry. All PHASE D.1 scripts, documents, and analyses should comply with
these conventions unless superseded by later user instruction.

## 1. Multi-GPU scope (supersedes the earlier single-GPU plan)

- M1 runs on **4 GPUs** (confirmed by D.0 Path A).
- Use `-gpus=2,3,4,5` throughout PHASE D.1. GPU IDs must be ≥ 2 to
  avoid the pre-existing akita `optdirectory` panic (see
  `TODO_PHASE2.md — Multi-GPU GPU-ID convention`).
- Phase 2 will revert to a canonical ID range after the akita fix.
- Relevance of each intrinsic metric to multi-GPU:
  - **L2 hit rate** — measurable at single-GPU too.
  - **Region utilization** — measurable at single-GPU too.
  - **Sharer consistency** — **only meaningful at 4 GPUs** (requires
    multi-sharer scenarios to exercise the metric).

## 2. Runner-invocation template

Every PHASE D.1 workload invocation uses:

```bash
timeout 600 go run ./cmd/m1 \
  -workload=${W} \
  -region-size=1024 \
  -seed=42 \
  -gpus=2,3,4,5 \
  # ... other flags per task spec
```

## 3. Sanity checks — additions beyond PHASE C

### C.4 Multi-GPU usage classification (new)

For every candidate workload, record how it actually uses the 4 GPUs:

1. Inspect the workload definition for `b.gpus` / `NumGPUs` references.
2. From the run log, extract per-GPU L2 access counts. Classify:
   - **true multi-GPU** — all four GPUs (IDs 2–5) show non-zero L2
     accesses (e.g., pagerank, matrixmultiplication candidates).
   - **data-parallel replicated** — all four show similar access
     patterns, but independent data (unverified category).
   - **home-node single-GPU** — only GPU[2] shows L2 access; the others
     are zero (simpleconvolution falls here based on D.0 observation).
3. Sharer-consistency measurement is only meaningful for *true
   multi-GPU* workloads, or for *home-node* workloads whose remote
   accesses actually traverse the directory.
4. Record the classification in the `Multi-GPU behavior` column of
   `selected_workloads.md`.

### C.5 RetiredWavefronts theoretical calibration (new)

- D.0 observed: simpleconvolution 512×512 mask=3 at 4-GPU reports
  `RetiredWf=4132`, vs single-GPU `4129`. Delta = **+3**.
- Working hypothesis: +3 comes from platform init/setup wavefronts
  contributed by the 4-GPU runtime. Exact source TBD during PHASE D.
- For each PHASE D.1 workload, compute
  `RetiredWf_theoretical_4GPU = RetiredWf_theoretical_1GPU + 3`.
- If the `+3` offset holds for every workload → record as "GPU init
  wavefront overhead = 3" and validate PHASE D.1 against the corrected
  theoretical.
- If the offset varies per workload → record the observed
  RetiredWavefronts as the workload's own "reference" value and
  validate PHASE D.1 by internal consistency (same workload, same
  number across region sizes) rather than an ab-initio theoretical.

## 4. `selected_workloads.md` table schema

Required columns:

| Workload | Pattern | Size | Baseline wall-clock | Phase count | Multi-GPU use | Sharer-relevant |

- `Multi-GPU use` ∈ { `true`, `home-node`, `replicated` } per C.4.
- `Sharer-relevant` ∈ { yes, no } — `true` and `home-node` are sharer-
  relevant (directory sees cross-GPU or remote traffic); `replicated`
  is not.

## 5. Out of scope for D.1

- Do **not** apply the akita `optdirectory` fix in D.1. It is tracked
  in `TODO_PHASE2.md` and will land in Phase 2.
- Do **not** change the `-gpus` convention away from `2,3,4,5`.
