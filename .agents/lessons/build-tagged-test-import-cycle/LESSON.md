# Build-tagged test files hide OS-specific import cycles

## Pattern

A `go vet` import-cycle failure that only fires on **one OS matrix leg** (e.g. windows-latest)
while macos/ubuntu are green. The cause is a `//go:build <os>` **test** file that imports a
high-level helper, closing a cycle the other legs never compile.

Concrete instance (PR #200 / p1b):

```
internal/fsops  (fsops_windows_test.go, //go:build windows)
  → internal/testutil  (testutil.go)
    → internal/config  (fetcher.go — NEW edge added by p1b)
      → internal/fsops   ← cycle closes
```

`fsops_windows_test.go` is the **only** importer of `testutil` in the fsops test package, and it
compiles only under `//go:build windows`. So on macos/ubuntu the fsops test package never builds,
`go vet` never sees the cycle, and CI is green on 2 of 3 legs. The new `config → fsops` edge
(correct and desired — config's lock writer sits atop fsops) was the trigger, but the **latent**
defect is a low-layer package's test depending on a high-layer helper.

## Why it bites

- **Invisible locally** unless you cross-compile: `go vet ./...` on macOS skips windows-tagged files.
- **Misdiagnosed as a runtime/path bug** because it's "Windows-only" — it is a *compile-time test
  import* cycle, not a path/permission issue.
- This repo is high-risk: many `//go:build <os>` test files, and `internal/testutil` imports
  `internal/config` — so any new `config → <low-level-pkg>` edge can close a cycle through a
  platform-tagged test of that low-level package.

## How to apply

1. **Catch it before push, on any host:** `GOOS=windows go vet ./...` (and `GOOS=linux` if on mac)
   builds the tagged test packages and surfaces the cycle without needing the CI matrix.
2. **Fix at the layering smell, not the new edge.** The healthy edge (`config → fsops`) is a DAG
   edge; keep it. Break the edge where a **low-level package's test pulls a high-level helper**:
   decouple `fsops_windows_test.go` from `testutil` (inline the one helper it used —
   `MakeDirUnreadable`). Do NOT remove `config`'s import of `fsops`, and do NOT reach for DI/func
   injection just to dodge an import — that's indirection for its own sake.
3. **Treat `internal/testutil` importing `internal/config` as load-bearing risk.** Any low-level
   package whose platform-tagged test imports testutil is one new `config → lowpkg` edge away from
   a one-leg-only cycle. When adding such an edge, vet all GOOS.

Sibling: [[refresh-import-before-relink]], [[sonarcloud-gate-mechanics]].
