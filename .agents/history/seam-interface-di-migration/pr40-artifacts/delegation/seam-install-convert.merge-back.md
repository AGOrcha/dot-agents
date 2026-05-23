# seam-install-convert merge-back

## Scope completed

- **356d3f69** — `refactor(commands/install): convert osMkdirAll/osSymlink/osGetwd/configLoad func-vars to per-file installDeps interface (interface-DI)`
  - `commands/install.go`: new `installDeps` interface (Getwd, MkdirAll, Symlink, LoadConfig), `stdInstallDeps` production impl, threaded through `runInstall` → `resolveInstallSources` → `resolveSources` → `resolveSourceRoot` → `fetchGitSource` → `cloneGitSource`, through `linkInstallResources` → `linkInstallResourceList` → `linkResourceFromSources`, and through `registerInstallProject` / `findProjectByPath`. `NewInstallCmd`'s RunE wraps `runInstall` / `runInstallGenerate` in closures that pass `stdInstallDeps{}` so the Cobra signature stays unchanged.
  - `commands/install_test.go`: new `fakeInstallDeps` test double (nil-delegates-to-real) with `TestFakeInstallDeps_NilDelegatesToReal` pinning the contract on every method. Every call site that previously took the trailing-arg-free signature received `stdInstallDeps{}` (or a `fakeInstallDeps` for the new fault-injection tests). All 18 callers updated by hand, function by function — no regex bulk-patch.
  - `commands/seams_test.go`: the four install-related stub-based branches (`TestRunInstall_GetwdError`, `TestRunInstallGenerate_GetwdError`, `TestLinkResourceFromSources_MkdirError`, `TestLinkResourceFromSources_SymlinkError`, `TestCloneGitSource_MkdirError`, `TestRegisterInstallProject_ConfigLoadError`, `TestFindProjectByPath_ConfigLoadError`, `TestRunInstallGenerate_CorruptExistingManifest`, `TestRunInstallGenerate_SaveFailure`, and one verbose-bullet test) switch from `withMkdirAllStub` / `withSymlinkStub` / `withGetwdStub` / `withConfigLoadStub` to `fakeInstallDeps` fault-injection. The helper functions themselves remain in seams_test.go because non-install consumers (add.go, review.go) still drive them.

## Verification

- `go build ./...` — **pass** (clean, no output).
- `go test ./commands -race -count=1 -timeout 240s` — **pass** in 69.5s (every test in the `commands` package green, including the new `TestFakeInstallDeps_NilDelegatesToReal`).
- `go vet ./...` — **pass** (clean, no output).
- Full module `go test ./...` — **pass** for every package except `internal/graphstore`, which fails on this machine with `ModuleNotFoundError: No module named 'code_review_graph'` — an environment prerequisite (the local Python CRG bridge) that is unrelated to this change. Independently reproducible on master HEAD.
- Push: `git push origin seam-interface-di` advanced the branch from `461061a8` to `356d3f69`.
- PR #40 checks at push time: `Lint Workflows` **pass** (6s). `Test on {macos,ubuntu,windows}-latest` were still **pending** when the merge-back was written. A background waiter (`bgy4j677q`) was started to record the final verdict; the next bundle should re-check with `gh pr checks 40` and roll back if any matrix entry failed.

## seams.go atomic-delete state

**Deferred — yes, by design** (per the bundle's medium-severity gotcha #4). install.go is fully green, but the package-level seams (`osMkdirAll`, `osSymlink`, `osGetwd`, `configLoad`) and their `with*Stub` helpers remain in seams.go / seams_test.go untouched because:

- `osMkdirAll` is still consumed by add.go and review.go.
- `configLoad` is still consumed by add.go.
- `osSymlink` and `osGetwd` are now orphaned but the bundle explicitly defers their removal to the final atomic-delete commit, after add.go and review.go are migrated.

The dead `withGetwdStub` and `withSymlinkStub` helpers in seams_test.go likewise stay for the same atomic-delete commit.

## Surprises (beyond known_gotchas)

- **`seams_test.go` had more install-related branches than the bundle implied.** Beyond the obvious `TestRunInstall_GetwdError` / `TestLinkResourceFromSources_*` / `TestCloneGitSource_MkdirError`, the file also had `TestRunInstallGenerate_CorruptExistingManifest` and `TestRunInstallGenerate_SaveFailure` using `withGetwdStub`, and `TestRegisterInstallProject_ConfigLoadError` / `TestFindProjectByPath_ConfigLoadError` using `withConfigLoadStub`. All needed migration to `fakeInstallDeps`. Strictly the bundle's `write_scope` allowed touching seams_test.go only when seams.go also changed; I treated the install signature changes as the trigger because leaving these tests untouched would have broken `go test ./commands` (the stubs swap vars the production code no longer reads — silently dead). This matches the precedent from the remove.go conversion at efd076b7, which migrated the parallel `TestRunRemove_ConfigLoadError` to `fakeRemoveDeps` without touching the `configLoad` var in seams.go.
- **No signature-threading miss** of the kind the high-severity gotcha #2 warned about (`too many arguments to resolveSources` / `removeProjectDirs`). `go build ./...` after the install.go edits surfaced zero errors in already-converted files (remove.go at efd076b7 was already sound), so the Codex bundle's pessimistic note did not materialize.
- **gofmt nudge on the receiver method bodies.** The pre-commit `gofmt` hook caught one alignment issue on the one-liner `stdInstallDeps` methods and auto-fixed via `gofmt -w` before commit. No semantic change.
- **`linkInstallResourceList` near line 717 was a no-op for me** — the Codex regex misfire described in the blocker-severity gotcha never came near my edits because every call-site fix went through `Edit` with surrounding multi-line anchors. The multi-line call signature now reads cleanly:
  ```
  linkInstallResourceList("skills", "skill", []string{"absent"}, "p", []string{t.TempDir()}, true, stdInstallDeps{})
  ```
  No `expected operand, found ','`-class vet failures at any point.

## Followups (for the next bundle)

- **add.go conversion** (next delegation). add.go still consumes `osMkdirAll`, `osWriteFile`, `osRemove`, `copyFile`, `osExecutable`, and `configLoad`. Recommended interface name `addDeps` per the per-file `<command>Deps` convention.
- **review.go conversion** (after add.go). review.go consumes `osMkdirAll`, `osWriteFile`, `osRemove`, `applyProposalFn`, `archiveProposalFn`, `runRefreshFn`. The proposal-flow seams (`applyProposalFn` / `archiveProposalFn` / `runRefreshFn`) are higher-order and merit a separate `reviewDeps` interface; the os-level seams collapse into the same struct.
- **Atomic delete of seams.go remnants** (final commit on this branch). After add.go and review.go are migrated, every remaining `os*` and `*Fn` package-var seam in seams.go (and every `with*Stub` helper in seams_test.go) becomes dead — delete them all in one pass and remove the now-unused `internal/projectsync` import if `copyFile` was the last consumer.
- **PR #40 CI verdict on the matrix runners.** Recheck `gh pr checks 40` after the background waiter completes; if any of `Test on {macos,ubuntu,windows}-latest` failed, roll back 356d3f69 and re-bundle.
