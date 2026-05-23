# seam-code-smell-cleanup — merge-back

- **Delegation:** `seam-code-smell-cleanup` (impl stage, bundle-only — no canonical plan)
- **Branch:** `seam-interface-di`
- **PR:** #40
- **Base SHA (in):** `09f1f12f`
- **Commit (out):** `af647378`
- **Commit message:** `chore(commands): address static-analysis findings flagged on the seam-DI cleanup`

## Summary

Single follow-up cleanup commit on PR #40 addressing **13 actionable
static-analysis findings** reported by the LSP on the
`seam-interface-di` branch. Zero behavior changes, zero signature
changes. All renames are positional `_` substitutions; argument types
and arities are unchanged so every caller (`grep`-verified) still
type-checks.

## Findings fixed (13 total, well past the 6-finding floor)

### Confirmed lint smells (3)

1. `commands/install_test.go:1365` — `slicescontains`: replaced the
   package-local `containsString(haystack, needle)` helper (deleted)
   with `slices.Contains` at the two call sites (lines 637, 640).
   Added `"slices"` to the import block. The identically-named
   helpers in `commands/agents/remove.go` and
   `internal/config/proposals.go` are untouched (different packages).
2. `commands/refresh.go:179` — `S1031`: removed the redundant
   `else if lines != nil` (ranging over a nil slice is a no-op);
   collapsed to `else { for _, line := range lines { ... } }`.
3. `commands/review_test.go:511` — replaced
   `interface{ Flags() interface{} }` with `interface{ Flags() any }`.

### Safe unused-parameter renames to `_` (10)

For each rename below, every caller in `commands/`, `internal/`, and
`cmd/` was `grep`-verified to use positional arguments. No method
satisfies an interface; no function is a cobra `(cmd, args)` handler.
Renaming a positional parameter to `_` preserves the signature.

4.  `commands/import_plugins.go:225` — `directPackagePluginRefsForManifest`: `sourceRoot` → `_`
5.  `commands/import_plugins.go:467` — `nameFromMarketplace`: `platformID` → `_`
6.  `commands/import_plugins.go:533` — `canonicalPackagePluginMarketplaceOutputs`: `manifestPath` → `_`
7.  `commands/doctor.go:625` — `collectBrokenUserLinks`: `agentsHome` → `_`
8.  `commands/doctor.go:740` — `printUserConfigStatus`: `agentsHome` → `_`
9.  `commands/status.go:872` — `printUserConfigSection`: `agentsHome` → `_`
10. `commands/status.go:1071` — `printClaudeAudit`: `name, agentsHome` → `_, _`
11. `commands/status.go:1085` — `printCodexAudit`: `name, agentsHome` → `_, _`
12. `commands/status.go:1169` — `printOpenCodeAudit`: `name, agentsHome` → `_, _`
13. `commands/status.go:1197` — `printCopilotAudit`: `name` → `_`

## Findings deliberately SKIPPED

- `commands/add.go:647` (`backupExistingConfigsList`, `projectPath`) —
  **SKIPPED**. The bundle's `forbidden_scope` rule (`commands/{init,
  install,remove,import,review,add}.go` open only for the explicit
  refresh/doctor/status set) forbids unused-param edits to `add.go`.
  In addition, after reading the function body (lines 624-687),
  `projectPath` IS used (line 657 calls
  `isManagedHardlinkToCanonicalSource(project, projectPath, f, agentsHome)`).
  The LSP report appears stale or misattributed; this candidate is
  not actionable regardless of scope.

No candidates were skipped because of cobra/interface protection in
this pass. The candidates explicitly flagged as unsafe in the bundle
(`init.go:55`, `doctor.go:64` cobra handlers) were not on my fix list
and were left untouched.

## Verification

```
cd .claude/worktrees/seam-di
go build ./...                                  # PASS
go vet ./...                                    # PASS (zero diagnostics)
go test ./commands -race -count=1 -timeout 240s # PASS (77.9s)
```

`go build ./... && go vet ./...` was also run after each individual
edit (per the bundle's decision lock), and each edit was committed
only after both passed.

## Push + CI

- `git push origin seam-interface-di` succeeded:
  `09f1f12f..af647378 seam-interface-di -> seam-interface-di`.
- `gh pr checks 40` immediately after push:
  Lint Workflows = pass; ubuntu/macos/windows test jobs queued
  (pending) in run `26202893077`.

## Follow-ups to record

- **For the `seam-interface-di-migration` spec follow-up list
  (master: `.agents/workflow/specs/seam-interface-di-migration/design.md`):**
  one candidate — `commands/add.go:647` (`projectPath` in
  `backupExistingConfigsList`) — was on the LSP-flagged list but was
  excluded by the cleanup bundle's `forbidden_scope` rule. On
  re-inspection the parameter is in fact *used* at line 657, so the
  diagnostic was stale. No further action needed beyond noting that
  the original LSP report had a false positive in `add.go`; do not
  schedule another cleanup pass for it.
- **No `add.go` / `init.go` / `install.go` / `remove.go` /
  `import.go` / `review.go` cleanups remaining from this candidate
  set** — the bundle's per-file behaviour-freeze means any further
  unused-param hygiene on those files needs a dedicated
  bundle/spec, not a follow-on commit on this PR.

## Notes / surprises

- **Surprise (minor, not in `known_gotchas`):** the bundle's
  candidate `commands/add.go:647 (projectPath)` is in fact a USED
  parameter (line 657 reference). LSP report was stale. Documented
  above; would have been a no-op edit anyway, but worth recording so
  the next cleaner does not re-chase it.
- **No staticcheck-CLI vs LSP version mismatch surfaced** — all the
  diagnostics in scope were straightforward and reproducible by
  inspection; `go vet ./...` reports zero issues post-fix.
- **Worktree discipline held:** every git operation used
  `git -C /Users/nikashp/Documents/dot-agents/.claude/worktrees/seam-di`;
  `git add` was always called with the explicit 6-file list rather
  than `.` or `-A`.

## Files changed

```
commands/doctor.go         |  4 ++--
commands/import_plugins.go |  6 +++---
commands/install_test.go   | 14 +++-----------
commands/refresh.go        |  2 +-
commands/review_test.go    |  2 +-
commands/status.go         | 10 +++++-----
6 files changed, 15 insertions(+), 23 deletions(-)
```
