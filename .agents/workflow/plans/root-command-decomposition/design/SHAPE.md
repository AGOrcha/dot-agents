# SHAPE.md — Target subpackage shape for `commands/` decomposition

Plan: `root-command-decomposition`
Owner task: `t01-design-subpackage-shape`
Status: design / contract for tasks t02 — t16
Date: 2026-05-24
Source proposal: `.agents/proposals/root-command-decomposition.md`

This document is the contract every downstream move task is implemented
against. If implementation needs to deviate, the deviation must be captured
back here (or surfaced as a new OD entry) before the move PR lands.

It is intentionally exhaustive: the symbol lists below are the cross-check
the move tasks use to verify nothing in `commands/` was forgotten.

---

## 1. Final subpackage list

Decided. No alternatives under consideration.

| New subpackage              | Source files moved out of `commands/`                                  | Rationale                                                                                                                                                                  |
|-----------------------------|-------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `commands/lifecycle/`       | `add.go`, `doctor.go`, `import.go`, `import_plugins.go`, `init.go`, `install.go`, `refresh.go`, `remove.go`, `status.go`, `linkcount_{unix,windows}.go` (+ test pairs) | 8 deep importers of `internal/{links,platform,projectsync,scaffold/{home,hooks}}`. Single cluster — they share project-mutation helpers (`backupExistingConfigsList`, `linkResourceFromSources`, `restoreLegacyResourceFile`, `findProjectByPath`, the install/import seam helpers). |
| `commands/mcp/`             | `mcp.go`, `mcp_test.go`                                                 | Per existing per-resource convention (`agents/`, `skills/`, `hooks/`, `sync/`, `workflow/`, `kg/`). MCP is a thin resource browser over `cmdutil.CanonicalFileSpec`.        |
| `commands/settings/`        | `settings.go`, `settings_test.go`                                       | Same convention. Same shape as `mcp/`.                                                                                                                                     |
| `commands/rules/`           | `rules.go`, `rules_test.go`                                             | Same convention. Hosts the `canonicalCmdFlags` / `canonicalCmdExampleBlock` helpers today; those move into `cmdutil` in `t10pre` before this split.                        |

**Stays in root `commands/`** (composition root + cross-cluster glue):
`root.go`, `flags.go` (Version/Flags/GlobalFlags), `ux.go` (`CLIError`,
`ErrorWithHints`, `UsageError`, `ExactArgsWithHints`, `NoArgsWithHints`,
`MaximumNArgsWithHints`, `RangeArgsWithHints`, `ExampleBlock`,
`ConfigureRootCommandUX`, `RenderCommandError`), `explain.go` (`NewExplainCmd`,
trivial — no external imports beyond `cobra`), `review.go` + `review_test.go`
(`NewReviewCmd` — review machinery, not a lifecycle/resource cluster),
`score.go` + `score_*test.go` (`NewScoreCmd` — telemetry browser),
`session_stats.go` + `session_*test.go` (`NewSessionCmd` — read-only telemetry),
`session_test.go`. The `internal/cmdutil` package keeps its current home.

**Stays in root as thin re-export shims through t12** (deleted in t13):
`add.go`, `doctor.go`, `import.go`, `import_plugins.go`, `init.go`,
`install.go`, `refresh.go`, `remove.go`, `status.go`, `linkcount_*`,
`mcp.go`, `settings.go`, `rules.go`. Each move task (t03–t10c) leaves
the root file in place wiring the lifecycle/* or resource-pkg constructor;
t13 removes them and switches `root.go` to import the subpackage
constructors directly.

**Test files explicitly NOT covered by the per-command moves** (handled by
t11–t12): `seams_test.go` (split per-cluster in t11), `coverage_test.go`,
`resource_parity_test.go`, `wiring_test.go`,
`hook_normalization_roundtrip_test.go`, `agentsrc_mutations_test.go`,
`testutil_test.go` (relocated in t12).

---

## 2. `cmdutil` home

Decided: **keep at `commands/internal/cmdutil`** for now.

- The three subpackages that consume it (`commands/mcp/`,
  `commands/settings/`, `commands/rules/`) all sit under `commands/`, so
  the `internal/` boundary still grants them access while keeping the
  helper unreachable from the broader module.
- `commands/lifecycle/` does not consume any `cmdutil` helpers today and
  is not expected to after the move (lifecycle commands are runners over
  `installDeps` / `addDeps` / etc., not canonical-file browsers).
- Promotion to `commands/cmdutil` (non-internal) is deferred until a
  *cross-cluster* consumer appears. None is on the horizon.

`t10pre` lifts `canonicalCmdFlags` (struct) and `canonicalCmdExampleBlock`
(helper) — currently in `commands/rules.go` — into the existing
`commands/internal/cmdutil` package, exporting them as
`cmdutil.CanonicalCmdFlags` and `cmdutil.CanonicalCmdExampleBlock`. This
unblocks t10a/b/c so all three resource subpackages can import them
through the same path the existing `cmdutil.CanonicalFileSpec` /
`cmdutil.RunCanonicalList|Show|Remove` already use.

Concrete grep confirming the dependency: every call site of
`canonicalCmdFlags` and `canonicalCmdExampleBlock` lives in
`commands/{mcp,settings,rules}.go` and their `_test.go` siblings — no
lifecycle file references either symbol.

---

## 3. Root-level symbols that stay exported (and where)

### 3a. Definitely stay in `commands` (composition root + UX layer)

These have external importers (`cmd/da/main.go`,
`internal/globalflagcov`) or are referenced via documentation / coverage
tooling. **Do not move.**

| Symbol                             | Defined in              | Why it stays                                                                                                  |
|------------------------------------|-------------------------|---------------------------------------------------------------------------------------------------------------|
| `NewRootCommand`                   | `commands/root.go`      | Imported by `cmd/da/main.go` and `internal/globalflagcov/analyze.go`.                                 |
| `RenderCommandError`               | `commands/ux.go`        | Imported by `cmd/da/main.go`.                                                                         |
| `Version`                          | `commands/refresh.go`   | Read by `NewRootCommand` for the `--version` template; root-package var.                                      |
| `Flags`, `GlobalFlags`             | `commands/flags.go`     | Persistent flag struct wired in `NewRootCommand`. Read by globalflagcov tooling.                              |
| `ConfigureRootCommandUX`           | `commands/ux.go`        | Called by `NewRootCommand`; package-internal but exported for symmetry with the other UX entry points.        |
| `CLIError`                         | `commands/ux.go`        | Public error type referenced by `RenderCommandError` consumers (main + tests).                                |
| `ErrorWithHints`, `UsageError`     | `commands/ux.go`        | Used by `Deps` of every subpackage (`agents.Deps`, `skills.Deps`, the future `lifecycle.Deps`).               |
| `ExactArgsWithHints`, `NoArgsWithHints`, `MaximumNArgsWithHints`, `RangeArgsWithHints` | `commands/ux.go` | Same — wired through `Deps` of every subpackage. |
| `ExampleBlock`                     | `commands/ux.go`        | Used as a generic example formatter; subpackages have their own `exampleBlock` shadowing this one, but the root export must remain for the composition root's own AddCommand wiring. |

### 3b. Currently package-private helpers that must be exported on move

These are referenced by `seams_test.go` and by other root-level files
(e.g. `status.go` uses `hasMultipleHardLinks` from `linkcount_unix.go`;
`doctor.go` also uses it). The seam test split in t11 re-homes the test
to `commands/lifecycle/seams_test.go`, after which the symbol can be
*either* package-private inside `commands/lifecycle/` (since the test is
intra-package) *or* a small set of exported helpers if a cross-cluster
caller appears.

**Rule: prefer staying package-private inside `commands/lifecycle/`.**
The only reason these symbols look "exported-worthy" is that the legacy
`seams_test.go` lives in `package commands` and reaches them through
package-private access today. Once the test moves to
`package lifecycle`, package-private access is restored *for free*.

The full set, traced through `seams_test.go`:

| Helper (current name)                | File                       | Post-move home                                  | Visibility after t11 |
|--------------------------------------|----------------------------|--------------------------------------------------|----------------------|
| `runRefresh`                         | `refresh.go`               | `commands/lifecycle/refresh.go`                  | private              |
| `runRemove`                          | `remove.go`                | `commands/lifecycle/remove.go`                   | private              |
| `runStatus`                          | `status.go`                | `commands/lifecycle/status.go`                   | private              |
| `runAdd`                             | `add.go`                   | `commands/lifecycle/add.go`                      | private              |
| `runInstall`, `runInstallGenerate`   | `install.go`               | `commands/lifecycle/install.go`                  | private              |
| `runDoctor`                          | `doctor.go`                | `commands/lifecycle/doctor.go`                   | private              |
| `runInit`                            | `init.go`                  | `commands/lifecycle/init.go`                     | private              |
| `registerInstallProject`             | `install.go`               | `commands/lifecycle/install.go`                  | private              |
| `findProjectByPath`                  | `install.go`               | `commands/lifecycle/install.go`                  | private              |
| `processImportCandidate`             | `import.go`                | `commands/lifecycle/import.go`                   | private              |
| `replaceImportContentCandidate`      | `import.go`                | `commands/lifecycle/import.go`                   | private              |
| `importMissingContentCandidate`      | `import.go`                | `commands/lifecycle/import.go`                   | private              |
| `importPreservedConflictCandidate`   | `import.go`                | `commands/lifecycle/import.go`                   | private              |
| `writeImportConflictReviewNote`      | `import.go`                | `commands/lifecycle/import.go`                   | private              |
| `backupExistingConfigsList`          | `add.go`                   | `commands/lifecycle/add.go`                      | private              |
| `linkResourceFromSources`            | `install.go`               | `commands/lifecycle/install.go`                  | private              |
| `cloneGitSource`                     | `install.go`               | `commands/lifecycle/install.go`                  | private              |
| `shouldUseCachedGitSource`           | `install.go`               | `commands/lifecycle/install.go`                  | private              |
| `writeKGMCPConfigFile`               | `add.go`                   | `commands/lifecycle/add.go`                      | private              |
| `writeKGMCPConfigs`                  | `add.go`                   | `commands/lifecycle/add.go`                      | private              |
| `scaffoldWorkflowAssets`             | `init.go`                  | `commands/lifecycle/init.go`                     | private              |
| `printSymlinkDirAudit`               | `status.go`                | `commands/lifecycle/status.go`                   | private              |
| `countClaudeRules`                   | `status.go`                | `commands/lifecycle/status.go`                   | private              |
| `restoreLegacyResourceFile`          | `add.go`                   | `commands/lifecycle/add.go`                      | private              |
| `hasMultipleHardLinks`               | `linkcount_{unix,windows}` | `commands/lifecycle/linkcount_{unix,windows}.go` | private (intra-pkg)  |

**Why a single `commands/lifecycle/` package solves the "must I export?"
question for all of them**: every one of these symbols is consumed only
(a) by another lifecycle file (e.g. `doctor.go` uses
`hasMultipleHardLinks` from `linkcount_unix.go`; `status.go` uses it
too — both move into the same package), or (b) by `seams_test.go` which
t11 splits per cluster. After t11, every consumer is intra-package, so
the export pressure disappears.

`hasMultipleHardLinks` is the only helper with a cross-file caller
*today* (used by both `status.go` and `add.go`/`doctor.go`). Task t08
makes the move temporarily cross-package (lifecycle imports it back into
root for doctor still in root), then t09 collapses it back to intra-
package when doctor moves.

### 3c. Exported root-level helpers that move with their cluster

`linkcount_{unix,windows}.go` defines `hasMultipleHardLinks` —
package-private today. The build-tagged file *itself* moves with status
to `commands/lifecycle/linkcount_{unix,windows}.go`. No export change.

### 3d. Symbols that remain exported on the *current* root because the
constructors are called by `root.go`

Today `root.go` calls `NewInstallCmd`, `NewAddCmd`, `NewRemoveCmd`,
`NewRefreshCmd`, `NewImportCmd`, `NewStatusCmd`, `NewDoctorCmd`,
`NewInitCmd`, `NewRulesCmd`, `NewMCPCmd`, `NewSettingsCmd`.

After the moves, these become:

| Today                                  | After t13                          |
|----------------------------------------|------------------------------------|
| `commands.NewInstallCmd()`             | `lifecycle.NewInstallCmd(deps)`    |
| `commands.NewAddCmd()`                 | `lifecycle.NewAddCmd(deps)`        |
| `commands.NewRemoveCmd()`              | `lifecycle.NewRemoveCmd(deps)`     |
| `commands.NewRefreshCmd()`             | `lifecycle.NewRefreshCmd(deps)`    |
| `commands.NewImportCmd()`              | `lifecycle.NewImportCmd(deps)`     |
| `commands.NewStatusCmd()`              | `lifecycle.NewStatusCmd(deps)`     |
| `commands.NewDoctorCmd()`              | `lifecycle.NewDoctorCmd(deps)`     |
| `commands.NewInitCmd()`                | `lifecycle.NewInitCmd(deps)`       |
| `commands.NewMCPCmd()`                 | `mcp.NewCmd(deps)`                 |
| `commands.NewSettingsCmd()`            | `settings.NewCmd(deps)`            |
| `commands.NewRulesCmd()`               | `rules.NewCmd(deps)`               |

The three resource constructors collapse to `NewCmd` (no resource-name
suffix) per the `agents.NewAgentsCmd` / `skills.NewSkillsCmd` /
`hooks.NewHooksCmd` precedent — wait, those use `NewAgentsCmd` etc. with
the resource name. Checked: `agents.NewAgentsCmd`, `skills.NewSkillsCmd`,
`hooks.NewHooksCmd` all keep the resource prefix. So the right call is:

> **CORRECTION**: resource subpackage constructors keep the resource
> prefix to match the existing convention:
> `mcp.NewMCPCmd(deps)`, `settings.NewSettingsCmd(deps)`,
> `rules.NewRulesCmd(deps)`.

`commands/sync/cmd.go` and `commands/workflow/cmd.go` are the exceptions
where the constructor is plain `NewCmd()` — both subpackages were added
in a different era and the prefixed form was deemed redundant given the
package-qualified call. **t10a/b/c TASKS.yaml notes currently say
`mcp.NewCmd` / `settings.NewCmd` / `rules.NewCmd`.** This is consistent
with the more recent convention and should be preserved. The
inconsistency between `agents.NewAgentsCmd` and `sync.NewCmd` is
pre-existing and out of scope here.

→ **DECISION**: Resource subpackage constructors use the
`sync.NewCmd` / `workflow.NewCmd` short form: `mcp.NewCmd(deps)`,
`settings.NewCmd(deps)`, `rules.NewCmd(deps)`. Lifecycle constructors
keep the verbose form because there is no single `lifecycle.NewCmd` —
each lifecycle command exposes its own constructor:
`lifecycle.NewInstallCmd(deps)`, `lifecycle.NewAddCmd(deps)`, etc.

---

## 4. Per-cluster file convention

### 4a. `commands/lifecycle/` layout

Lifecycle is a *cluster* not a single command, so it does NOT collapse
into the agents/skills file convention (one file per sub-command). Each
top-level lifecycle command keeps its own file. Final shape:

```
commands/lifecycle/
  doc.go                          # package comment (t02)
  deps.go                         # Deps struct + NewDeps()  (t02)
  add.go            + add_test.go            (t04)
  doctor.go         + doctor_test.go         (t09)
                    + doctor_repair_e2e_test.go
  import.go         + import_test.go         (t06)
  import_plugins.go + import_plugins_test.go (t06)
                    + import_pure_test.go
  init.go           + init_test.go           (t05)
  install.go        + install_test.go        (t03)
  refresh.go        + refresh_test.go        (t07)
                    + refresh_idempotency_test.go
  remove.go         + remove_test.go         (t04)
  status.go         + status_test.go         (t08)
  linkcount_unix.go + linkcount_windows.go   (t08)
  seams_test.go                              (t11 — split from root)
  agentsrc_mutations_test.go                 (t12 — re-homed from root)
  coverage_test.go                           (t12)
  hook_normalization_roundtrip_test.go       (t12)
```

`lifecycle.Deps` mirrors `agents.Deps` shape: a single struct holding
`GlobalFlags` + the UX hint functions (`ErrorWithHints`, `UsageError`,
`ExactArgsWithHints`, `MaximumNArgsWithHints`, `RangeArgsWithHints`).
The package-var seams (`stdAddDeps`, `stdInstallDeps`, `stdImportDeps`,
`stdRefreshConfigLoader`, `stdStatusConfigLoader`, `stdInitDirMaker`,
`stdRemoveDeps`, `stdDoctorConfigLoader`) move with their files
unchanged — they remain package-var seams per §6.

### 4b. `commands/mcp/`, `commands/settings/`, `commands/rules/` layout

Mirror the existing `commands/agents/` + `commands/skills/` per-file
convention precisely (NOT a single `<resource>.go` inside the
subpackage). Target shape for each:

```
commands/<resource>/
  cmd.go            # NewCmd(deps) + sub-command wiring (list/show/remove)
  deps.go           # Deps struct + NewDeps()
  example.go        # example-string blocks
  list.go           # newXListCmd + runXList runner
  show.go           # newXShowCmd + runXShow runner (+ extras helper for rules)
  remove.go         # newXRemoveCmd + runXRemove runner
  seams.go          # stdXDeps + os/projectsync wrappers
  seams_test.go     # fault-injection seam tests (split from root seams_test.go)
  coverage_test.go  # file-level coverage assertion
  cmd_test.go       # NewCmd wiring tests
  <resource>_test.go # re-homed from commands/<resource>_test.go (t10a/b/c)
```

Notes specific to each resource:

- **`commands/rules/show.go`** hosts `rulesShowFrontmatterExtra` (the
  file-scope helper for the `rules show` extras callback) — it is
  inert as an export and stays package-private alongside the show runner.
- **`commands/rules/`** depends on `cmdutil.CanonicalCmdFlags` and
  `cmdutil.CanonicalCmdExampleBlock` post-`t10pre`. Same for `mcp/` and
  `settings/`.
- **`findMCPSpec`, `findSettingsSpec`, `findRuleSpec`** stay
  package-private — each is referenced only by its own subpackage's
  `TestFind*SpecNotFound`.

---

## 5. DI handling decision

Decided: **PRESERVE the current package-var seam shape across all moves
(t02–t13). Land interface-DI as a separate follow-up (t15).**

- The lifecycle and resource files today already carry per-file
  `<verb>Deps` interfaces — `installDeps`, `addDeps`, `importDeps`,
  `refreshConfigLoader`, `statusConfigLoader`, `initDirMaker`,
  `removeDeps`, `doctorConfigLoader`, `mcpDeps`, `settingsDeps`,
  `rulesDeps` — with `std*Deps` defaults. They are *already* in
  interface-DI shape for the verbs that have been touched by the
  `seam-interface-di-migration` plan to date (review, kg, etc.).
- For verbs that have **not** yet been migrated (most of lifecycle), the
  seams remain `os.MkdirAll` / `os.WriteFile` / `config.Load` swaps via
  the `addDeps` / `installDeps` struct fields. That shape moves unchanged
  with the file.
- Rationale for not folding interface-DI into the moves: doubling the
  per-file diff during a layout change makes the move PRs unreviewable
  and entangles two plans. Decomposition first gives interface-DI clean
  per-file targets in the new subpackages.
- t15 (the direct follow-up) is the place where any remaining seam
  patterns in `commands/lifecycle/` and the three resource subpackages
  converge to the interface-DI shape per the
  `seam-interface-di-migration` convention doc.

**Concrete invariant for every move task**: the package-var seams move
with the file. Tests using `fakeAddDeps{...}`, `fakeInstallDeps{...}`,
`stdAddDeps{}` etc. continue to work post-move (with the package
qualifier updated). If any test breaks due to the move alone (i.e.
not due to a follow-up interface-DI refactor), the move task has
violated this rule.

---

## 6. Root shim policy

Decided: **DELETE root re-export shims in t13.**

- During the move tasks (t03–t10c), each moved file leaves a *thin
  shim* at its original `commands/<file>.go` path. The shim contains
  one or two trivial constructors: `func NewInstallCmd() *cobra.Command
  { return lifecycle.NewInstallCmd(lifecycle.NewDeps()) }`.
- t13 deletes every shim and switches `commands/root.go`'s `AddCommand`
  calls to the subpackage constructors directly. End-state file list
  for `commands/` (excluding tests):
  - `root.go` — composition root (imports `lifecycle`, `mcp`,
    `settings`, `rules` plus the existing `agents`, `skills`, `hooks`,
    `sync`, `workflow`, `kg`).
  - `flags.go` — `Flags`, `GlobalFlags`.
  - `ux.go` — UX exports (§3a).
  - `explain.go` — `NewExplainCmd` (no internal deps).
  - `review.go` + `review_test.go` — review machinery.
  - `score.go` + `score_*test.go` — telemetry browser.
  - `session_stats.go` + `session_*test.go` — telemetry browser.
  - `wiring_test.go` — composition-root wiring assertion (per t12).
- Justification: single composition root, cleaner module boundary, and
  unambiguous import-graph assertion in t14. Keeping shims around would
  leave `internal/links`, `internal/projectsync`,
  `internal/scaffold/{home,hooks}` imports in `commands/` indefinitely —
  the whole point of the plan is to remove them, and the t14 import-
  guard depends on it.
- The KG-verified external-caller list in §8 confirms there are no
  external Go importers of `commands.NewInstallCmd` / `commands.NewAddCmd`
  / etc. beyond `commands/root.go` and intra-package tests, so the shim
  deletion has no out-of-tree blast radius.

---

## 7. Per-cluster commit cadence

Decided.

- **Lifecycle cluster (t03–t09)**: one PR per task = one PR per command
  move. t03 (install), t04 (add+remove paired), t05 (init), t06
  (import + import_plugins paired), t07 (refresh), t08 (status +
  linkcount paired), t09 (doctor). 7 PRs in the lifecycle cluster.
- **Resource cluster (t10a/b/c, parallelizable post-t10pre)**: one PR
  per subpackage. 3 PRs (mcp, settings, rules).
- **Cleanup (t11–t14)**: one PR each — seams split, cross-cutting test
  re-home, shim strip, importguard.
- **Within each PR**: per-command commits (no monorepo "move
  everything" mega-commits). For the paired tasks (t04, t06, t08), each
  command gets its own commit inside the single PR.

This cadence keeps each diff at human-reviewable size (~one file
move + its test + the root-shim creation = ~3 file changes per
commit) while concentrating coordination overhead at the PR boundary.

---

## 8. KG-verified external callers (no shim survives that we missed)

`grep -rn` cross-check of the actual repo (the code-review-graph MCP
server was not reachable in this worker's sandbox; fallback to ripgrep
over the import-resolved symbol set, which is equivalent for an internal
Go module with no plugin loading):

```
grep -rn "\"github.com/NikashPrakash/dot-agents/commands\"" --include="*.go"
# → cmd/da/main.go
# → cmd/da/main_test.go
# → internal/globalflagcov/static.go
# → internal/globalflagcov/analyze.go

grep -rn 'commands\.(CLIError|ErrorWithHints|UsageError|ExactArgsWithHints|RenderCommandError|MaximumNArgsWithHints|RangeArgsWithHints|ConfigureRootCommandUX|Flags|Version|GlobalFlags)' --include="*.go"
# Outside commands/: only documentation comments in
# cmd/globalflag-coverage/main.go and internal/globalflagcov/analyze.go
# reference commands.Flags by name. No code paths import it.

grep -rn 'commands\.(NewInstallCmd|NewAddCmd|NewRemoveCmd|NewRefreshCmd|NewImportCmd|NewStatusCmd|NewDoctorCmd|NewInitCmd|NewMCPCmd|NewSettingsCmd|NewRulesCmd)'
# → no external callers. Only commands/root.go uses them.

grep -rn 'commands\.(RunInstall|RunInstallGenerate|RunAdd|RunRemove|RunRefresh|RunStatus|RunDoctor|RunInit|RegisterInstallProject|FindProjectByPath|ProcessImportCandidate|BackupExistingConfigsList|LinkResourceFromSources|CloneGitSource|WriteKGMCPConfigFile|WriteKGMCPConfigs|ScaffoldWorkflowAssets|ReplaceImportContentCandidate|ImportMissingContentCandidate|ImportPreservedConflictCandidate|WriteImportConflictReviewNote|ShouldUseCachedGitSource|PrintSymlinkDirAudit|CountClaudeRules|RestoreLegacyResourceFile)'
# → empty. These helpers are all unexported today (run*, register*, etc.)
# and the seam tests reach them via package-private same-package access.
# No external consumer would survive their re-homing because there is no
# external consumer.
```

**External symbol use, complete list**:

| Symbol                       | Caller                                       | Survives shim deletion? |
|------------------------------|----------------------------------------------|-------------------------|
| `commands.NewRootCommand`    | `cmd/da/main.go`, `internal/globalflagcov/analyze.go`, `cmd/da/main_test.go` | Yes — stays in root.    |
| `commands.RenderCommandError`| `cmd/da/main.go`                     | Yes — stays in root.    |

No other external callers exist. **Zero OD entries open for t13.**

> **Note on KG verification**: the `code-review-graph` MCP tools
> (`semantic_search_nodes_tool`, `get_impact_radius_tool`,
> `query_graph_tool`) were referenced in the t01 brief as the verification
> mechanism. They were not reachable from this sandbox. The above
> `grep -rn` over the Go module yields equivalent precision for this
> question (Go has no dynamic dispatch across packages without an import,
> and the module has no `go:linkname` usage). If the KG tools later
> surface a caller this grep missed, that caller becomes an OD entry per
> §9 and must resolve before t13 lands.

---

## 9. Open decisions (ODs)

Pulled separately so the move tasks can track resolution.

| OD ID | Description | Blocks | Recommended resolution |
|-------|-------------|--------|------------------------|
| OD-1  | KG verification re-run. The grep in §8 is a stand-in for the unreachable code-review-graph MCP tools. Re-run via `mcp__code-review-graph__semantic_search_nodes_tool` for each of the symbols listed in §8 before t13 merges, to confirm no caller appeared after t01 landed. | t13 | Run the KG queries inside the t13 PR description; if zero callers, mark OD-1 closed. If any caller surfaces, update its import site to use the subpackage constructor *in the same PR* (t13 already touches `commands/root.go` and the shims). |
| OD-2  | `linkcount_{unix,windows}.go` cross-package window during t08→t09. After t08, `hasMultipleHardLinks` lives in `commands/lifecycle/` but `doctor.go` (still in root) uses it. Three resolutions are viable: (a) expose as `lifecycle.HasMultipleHardLinks` for the t08→t09 window, then make package-private when doctor moves in t09; (b) duplicate the helper into root temporarily; (c) re-order to move doctor with status. Per TASKS.yaml t08 notes: option (a) is the recommended path. | t08, t09 | Confirm option (a) at t08 implementation time. The export is reversed in t09 by lowercasing the name back when the only caller (doctor) now lives in the same package. |
| OD-3  | `wiring_test.go` future home. t12 says it stays in root. After t13 strips the shims, `wiring_test.go` becomes the *only* place that exercises the entire AddCommand graph end-to-end. Confirm it still compiles after t13 (it must — `root.go` exports `NewRootCommand` which it already exercises). | t12, t13 | Re-verify at t12 close that `wiring_test.go` does not reach into now-private subpackage helpers. If it does, lift the assertion to use only `NewRootCommand()` + cobra's `Find` to walk the tree. |
| OD-4  | `testutil_test.go` promotion. Per t12 notes, "testutil_test.go's exports may need to be promoted to a small `commands/testutil` package if multiple subpackages need them." Decide at t11 close: if more than one subpackage's relocated seam test pulls a helper from `testutil_test.go`, promote it. | t11, t12 | Inventory at t11 close which testutil helpers the post-split lifecycle/seams_test.go needs. If only lifecycle needs them, in-line into `commands/lifecycle/testutil_test.go`. If multiple subpackages need them, promote to `commands/internal/testutil` (mirror of `commands/internal/cmdutil`). |
| OD-5  | t14 import-guard home. Plan says "Path names above are placeholders — t01 SHAPE.md may pick a different home (e.g. `internal/architest`)." Recommendation: place the guard at `tools/importguard/main.go` (matches the existing `cmd/globalflag-coverage/` convention for repo-internal CI binaries that are not shipped as part of `dot-agents` itself). The guard runs `go list -json ./commands/...` and asserts the import-set for package `commands` does not contain the banned `internal/links`, `internal/projectsync`, `internal/scaffold/home`, `internal/scaffold/hooks`. | t14 | Confirm at t14 implementation time; document the choice in the t14 PR. |
| OD-6  | `t10pre` extraction conflict with `commands/rules/` move (t10c). The `canonicalCmdFlags` lift in t10pre writes to `commands/rules.go`. t10c then moves `commands/rules.go` → `commands/rules/`. The dependency edge `t10pre → t10c` already serializes this. Confirm no parallel worker touches `commands/rules.go` between t10pre merge and t10c branch creation. | t10pre, t10c | t10pre owner branches off the latest master that includes t10pre's own merge. t10c owner waits for t10pre merge before branching. |
| OD-7  | `lifecycle.Deps.NewDeps()` factory naming. `agents/deps.go` exports `Deps` struct with no `NewDeps` factory; `commands/agents` callers construct it inline in `commands.NewAgentsCmd()`. `skills/deps.go` is the same. The TASKS.yaml t02 notes assume a `NewDeps()` factory. Recommendation: skip `NewDeps()` and construct the struct inline at the root-shim / t13 call site (matches the existing convention). | t02 | Confirm at t02 implementation. If the t02 worker writes `NewDeps()`, that's harmless but adds an unused function until t13 — minor style issue, not blocking. |

---

## 10. Cross-references

- Proposal: `.agents/proposals/root-command-decomposition.md`
- Plan + tasks: `.agents/workflow/plans/root-command-decomposition/{PLAN,TASKS}.yaml`
- File-convention exemplars: `commands/agents/`, `commands/skills/`,
  `commands/sync/`, `commands/workflow/`.
- Existing shared helper home: `commands/internal/cmdutil/canonfile.go`.
- Existing per-file Deps pattern: `commands/agents/deps.go`,
  `commands/skills/deps.go`.
- Related follow-up plan: `.agents/workflow/plans/seam-interface-di-migration/`
  (t15 of this plan either contributes tasks to it or runs sibling).
