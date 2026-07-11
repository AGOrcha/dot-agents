APPROVE

# Architecture-standards review — wire-managed-gitignore-autofill (D14/R8)

Stage: review (architecture-standards) (`swarm-da-inner-loop-d14-review_architecture_standards-0`).
Upstream gate: verify-cli-runner=PASS (verify-unit=PASS, impl=DONE, commit `2bba968`). Gate honored.
Resolved prompt: `.agents/prompts/reviewers/architecture-standards.project.md` (repo-local,
`da --json workflow resolve-prompt --kind reviewer --slug architecture-standards`; base + lens layers
unresolved/absent, only the dot-agents project overlay exists).
Scope: READ-ONLY. `git -C .agents/worktrees/d14 diff origin/master` (8 files, +283/-16) + changed files.

Lens: architecture-standards. Verdict: **APPROVE** (lens: architecture-standards).
No BLOCKER/HIGH findings. Notes below are LOW/observational and do not gate.

## What was judged

The four architecture pillars named in the task are all satisfied:

### 1. Per-platform output seam design — SOUND
`internal/platform/platform.go` introduces `ManagedOutputReporter` (optional interface,
`ManagedOutputs() []string`) + `staticManagedOutputs` fallback table + `CollectManagedOutputs(platforms)`
aggregator. The optional-interface-with-type-assertion pattern matches existing subpackage conventions
in this package (`LinkCounter`, `UserConfigReporter`), so package boundaries and command-vs-library
separation hold: the dynamic surface (copilot) co-locates its output list via the interface, the stable
single-owned-dir platforms live in the central table. Copilot's `ManagedOutputs()` correctly enumerates
its cross-cutting fanout (`.github/…`, `.vscode/mcp.json`, per-machine `.github/hooks/*.json`, and its
rendered `.claude/settings.local.json` compat file) rather than a single directory — the exact case the
interface exists for. `CollectManagedOutputs` returns the raw union and defers normalization to the
links layer (correct layering: collector doesn't sort/dedup/filter).

### 2. Single EnsureManagedGitignore call — CONFIRMED
Exactly one production call site: `commands/refresh.go` `ensureManagedGitignoreForRefresh` →
`links.EnsureManagedGitignore(path, outputs)`, invoked once per project from `refreshOneProject`
(grep across `commands/` + `internal/` shows no other callers besides tests). Keyed off the
config-enabled platform set (not install state), so the committed block is machine-independent. Dry-run
previews without writing; write failure flips `projectFailed` so the success stamp is withheld — proper
error propagation, no swallowed failure.

### 3. neverIgnored contract respected — CONFIRMED (belt-and-suspenders)
Neither the static table nor copilot's `ManagedOutputs()` ever emits `.agentsrc.json`/`.agentsrc.lock`
(documented explicitly in the table comment), AND `links.normalizeIgnoreEntries` filters `neverIgnored`
regardless of caller input. The committed resolved-state contract stays tracked. Asserted by
`TestCollectManagedOutputs_…` (collector never lists the contract) and `TestRunRefresh_…` (block never
ignores it) plus the pre-existing `TestEnsureManagedGitignore_NeverIgnoresCommittedContract`. The
`.gitignore` diff drops the ad-hoc root `.agentsrc.lock` ignore — consistent: the lock is now a tracked
contract file (its `+36` content lands in this same diff).

### 4. No hardcoded paths — CONFIRMED
`refresh.go` holds zero path literals; it delegates entirely to `platform.CollectManagedOutputs`, so the
command layer never learns any platform's surface. Path knowledge lives in the platform package. The
table/copilot entries use the platforms' own exported-scope constants where they exist
(`cursorDir`, `claudeDir`, `claudeMCPFile`, `codexDir`, `codexAgentsMarkdown`, `opencodeDir`,
`opencodeJSON`, `antigravityDir`, and copilot's `copilot*` consts); the few bare literals
(`.cursorrules`, `.cursorignore`, `CLAUDE.md`) match the exact convention already used verbatim in
`cursor.go`/`claude.go` (no constant exists for those in the surrounding code either), so this introduces
no new naming/data-shape divergence — it mirrors the subpackage.

Naming/data shapes: new identifiers (`ManagedOutputReporter`, `ManagedOutputs`, `staticManagedOutputs`,
`CollectManagedOutputs`, `ensureManagedGitignoreForRefresh`) are additive and match surrounding style;
no AgentsRC field or state-struct changes, so the schema-usage lifecycle is not implicated.

## Notes (LOW / observational — non-blocking)

- N1 (DRY / drift risk): the hybrid seam means the static platforms' output paths are declared in the
  `staticManagedOutputs` table *separately* from where each platform already encodes those same owned
  paths in its `CreateLinks`/audit/`CountLinks` methods. If a static platform later adds/renames an
  output surface, the table must be updated in lockstep or the managed block silently under-covers.
  The copilot path (interface, co-located) does not have this hazard. A future consolidation — every
  platform implementing `ManagedOutputReporter` and retiring the table — would make the seam uniform and
  remove the drift surface. Deliberate, documented tradeoff for now; not a blocker.
- N2 (hygiene, out of this lens's core, flagging for the gate): this repo's own `.gitignore` loses the
  root `.github/hooks/*.json` rule, but no dot-agents-managed block is present in that file — those
  per-machine hook manifests are only re-covered once `da refresh` regenerates the block on this repo.
  Verify (cli-runner) already dogfooded the end-to-end block generation clean, and `.agentsrc.lock` is
  now committed intentionally, so this is a timing/repo-hygiene note, not an architecture defect.

Verdict: **APPROVE** (lens: architecture-standards).
