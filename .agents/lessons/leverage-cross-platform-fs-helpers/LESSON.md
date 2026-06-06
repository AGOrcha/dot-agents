# Leverage cross-platform fs abstractions, not inline OS-specifics

## Pattern

When code (or a test) needs OS-divergent filesystem behavior, route it through the
project's existing cross-platform encapsulation instead of branching on
`runtime.GOOS` or hand-rolling an OS-specific trick:

- **Mutations** (mkdir / write / remove / remove-tree): `internal/fsops`
  (`MkdirAll`, `WriteFile`, `Remove`, `RemoveAll`) — OS-appropriate impls with
  Windows PowerShell fallbacks for cases the Go runtime refuses.
- **Forcing access/permission errors in tests**:
  `internal/testutil.MakeFileUnreadable` / `MakeDirUnreadable` — POSIX `chmod 0`
  vs Windows deny-ACE/byte-range-lock, with built-in **root/elevated and
  non-enforcing-FS skips** so the test never produces a false negative.
- **Fixtures / temp homes / git repos**: `internal/testutil` helpers
  (`NewTempProject`, `WriteAgentsRC`, `InitGitRepo`, …).

## Root cause

OS error semantics diverge in non-obvious ways. The trap that prompted this
lesson: a test made a fold-back parent path a regular file and asserted
`os.Stat` of a child returns a non-`IsNotExist` error. That holds on POSIX
(`ENOTDIR`) but **not** on Windows, where the same path maps to a NotExist-class
error — so the test green on macOS/Linux and **red on the Windows CI runner**.

The reflex "fix" — wrap the assertion in `if runtime.GOOS == "windows"` — is the
anti-pattern `testutil/perms.go` was built to kill: it silently lowers coverage
on the Windows runner and lets real Windows-only regressions slip through. The
mode-bits trick (`os.WriteFile(path, data, 0o000)` then read) is equally broken:
NTFS ignores POSIX mode bits, so the error path is never exercised on Windows.

## Rule

- Do **not** put `runtime.GOOS` branches or OS-divergent fs tricks in business
  logic or tests. Concentrate the platform policy in the shared helper.
- To force an fs access error portably, use `testutil.MakeDirUnreadable` /
  `MakeFileUnreadable` (they `t.Skip` where the deny can't be enforced).
- For fs mutations in production paths, prefer `fsops.*` over raw `os.*` so the
  Windows fallbacks and security hardening apply uniformly.
- Pure existence/`IsNotExist` checks are already portable — no helper needed; the
  divergence is in the *error-class* of non-NotExist failures.

## How to apply

When a test needs an fs failure branch:
1. Mutation should fail → drive it with the real input that fails everywhere
   (e.g. a missing parent) or assert via the `fsops` wrapper.
2. Need a permission/traverse failure → `testutil.MakeDirUnreadable(t, parent)`
   before the call under test; assert `err != nil` (no GOOS branch).
3. Tempted to write `if runtime.GOOS == ...` in a test → stop; the helper
   already owns that policy.

## Cross-references

- `internal/testutil/perms.go` / `perms_dir.go` — the canonical doc + impls.
- `internal/fsops/` — mutation abstraction (default vs windows build tags).
- [[match-ci-test-flags-locally]] — single-OS local green ≠ CI green; the merged
  multi-OS gate is the real check.
- [[build-tagged-test-import-cycle]] — when a low-level pkg can't import
  `testutil` (cycle), inline a documented copy of the helper (see
  `fsops_windows_test.go`), don't fall back to GOOS skips.
