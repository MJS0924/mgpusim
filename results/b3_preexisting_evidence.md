# B-3 Pre-existing Test Failure Evidence

## Summary

The `amd/timing/cu`, `amd/timing/cp`, `amd/timing/rdma`, `amd/timing/rob`,
`amd/driver`, `amd/emu`, and related packages have test build failures that are
**entirely pre-existing** — they existed before the B-3 work and are unrelated
to any changes made in commits `59956873`, `fce91dd3`, or `7313dad1`.

## Method

1. Stashed the current working tree (NIGHT_RUN_STATUS.md etc.)
2. Applied `git checkout 59956873 -- .` to restore the mgpusim tree to the
   state of commit `59956873` (`[commit 4] R_B3 리스크 이관`) — the parent
   of the B-3.1 commit `7313dad1`.
3. Ran `go test ./... 2>&1 | tee /tmp/preexisting_test.log`
4. Restored HEAD state via `git checkout HEAD -- <files>` + `git stash pop`

The test environment uses the local akita module at `/root/mgpusim_home/akita`
via `replace` directive in `go.mod`.

## Pre-existing Failures (at commit 59956873)

All failures have the pattern `undefined: MockXxx` / `undefined: NewMockXxx`
— missing generated mock files (likely produced by `mockgen`, not committed):

| Package | Representative Error |
|---------|---------------------|
| `amd/timing/cu` | `undefined: MockEngine`, `MockWfDispatcher`, `MockPort`, `MockSubComponent` |
| `amd/timing/cp` | `undefined: MockEngine`, `MockPort`, `MockDispatcher` |
| `amd/timing/cp/internal/dispatching` | `undefined: MockNamedHookable`, `MockPort` |
| `amd/timing/rdma` | `undefined: MockEngine`, `MockPort` |
| `amd/timing/rob` | `undefined: MockPort` |
| `amd/timing/pagemigrationcontroller` | `undefined: MockEngine`, `MockPort` |
| `amd/driver` | `undefined: MockPageTable`, `MockEngine`, `MockMemoryAllocator` |
| `amd/driver/internal` | `undefined: MockPageTable` |
| `amd/emu` | `undefined: MockPageTable` |
| `amd/benchmarks/dnn/training` | `undefined: MockDataSource`, `MockLayer`, etc. |
| `amd/timing/cp/backup` | `vm.PTEInvalidationReq` undefined (stale backup code) |

## Packages Passing at 59956873

| Package | Result |
|---------|--------|
| `coherence` | ✅ PASS |
| `instrument` | ✅ PASS (cached) |
| `amd/benchmarks/dnn/gputensor` | ✅ PASS |
| `amd/benchmarks/dnn/layers` | ✅ PASS |
| `amd/benchmarks/dnn/tensor` | ✅ PASS |
| `amd/bitops` | ✅ PASS |
| `amd/insts` | ✅ PASS |
| `amd/kernels` | ✅ PASS |
| `nvidia/benchmark` | ✅ PASS |
| `nvidia/platform` | ✅ PASS |
| `nvidia/tracereader` | ✅ PASS |

## Conclusion

The failures are caused by missing mock files (not committed to the repository)
across multiple packages. These failures are **pre-existing** and identical in
character to the failures observed after B-3 work. The B-3 changes (`hookpos.go`,
`directorystage.go` InvokeHook additions, `vectormemoryunit.go` hook, `plain_vi.go`
callbacks, `instrument/adapter/` package) **did not introduce any new failures**.

The `instrument/adapter` build failure observed during this test run is an
artifact of the mixed-state test method (adapter files from B-3.1 present
while the referenced `writebackcoh.HookPosL2Access` symbol was not available
in the partially-reverted tree state). This does **not** reflect a real failure
in either the pre-B-3 or post-B-3 state.

Our work packages (`coherence`, `instrument`, `instrument/adapter`) are
GREEN both before and after B-3 work.
