APPROVE

# Acceptance-invariants review — wire-managed-gitignore-autofill (D14/R8)

Stage: review (acceptance-invariants) (`swarm-da-inner-loop-d14-review_acceptance_invariants-0`).
Upstream gate: review-architecture-standards=APPROVE (verify-cli-runner=PASS, verify-unit=PASS,
impl=DONE, commit `2bba968`). Gate honored — proceeded.
Resolved prompt: `.agents/prompts/reviewers/acceptance-invariants.project.md` (repo-local overlay;
`da --json workflow resolve-prompt --kind reviewer --slug acceptance-invariants`; base + lens layers
unresolved/absent, only the dot-agents project overlay exists).
Scope: READ-ONLY. `git -C .agents/worktrees/d14 diff origin/master` (8 files, +283/-16) + changed
files + `git ls-files`/`git check-ignore` in the slice worktree.

Lens: acceptance-invariants. Verdict: **APPROVE** (lens: acceptance-invariants).
Every task ACCEPTANCE item is met, each with fault-sensitive proof; no BLOCKER/HIGH. Notes are
LOW/observational and do not gate.

## Acceptance criteria — each CONFIRMED

### 1. Managed block contains hooks — CONFIRMED
`(*copilot).ManagedOutputs()` (internal/platform/copilot.go) emits
`copilotGitHubDir + "/" + copilotHooksDir + "/*.json"` → literal `.github/hooks/*.json`
(consts resolve: `copilotGitHubDir=".github"`, `copilotHooksDir="hooks"`). It flows through
`platform.CollectManagedOutputs` (ManagedOutputReporter branch) into the single
`links.EnsureManagedGitignore` call wired in `refreshOneProject`. Proven end-to-end, not asserted
in the abstract: verify-cli-runner dogfood rendered 10 real per-machine hook files under
`.github/hooks/` and `git check-ignore -v` attributed them to the managed-block line
(`.gitignore:14:.github/hooks/*.json`), NOT an ad-hoc rule. `TestRunRefresh_WritesManagedGitignoreBlock`
asserts the pattern lands inside the markers; verify-unit's mutation revert (drop the
ManagedOutputReporter branch) made both tests fail with `missing ".github/hooks/*.json"` — the
assertion genuinely defends copilot's dynamic fanout reaching the block.

### 2. .agentsrc.lock tracked — CONFIRMED
New file committed in this diff (`.agentsrc.lock`, 36 lines: `inputs_digest`, `lock_version`, 5
profile `units`). In the slice worktree `git ls-files -- .agentsrc.lock` returns it (tracked) and
`git check-ignore .agentsrc.lock` exits 1 (NOT ignored). It is `neverIgnored` in
internal/links/gitignore.go (`neverIgnored[".agentsrc.lock"]=true`) and `normalizeIgnoreEntries`
filters it (and `.agentsrc.json`) back out of the block regardless of caller input (belt-and-
suspenders). Tests assert both contract files are absent from the block
(`TestRunRefresh_...`, `TestCollectManagedOutputs_...`). verify-cli-runner A3 confirmed `git add -A`
stages it as `A  .agentsrc.lock`.

### 3. Byte-stable refresh — CONFIRMED
`links.EnsureManagedGitignore` regenerates (not appends) a normalized/sorted/de-duped block and
preserves user-authored lines outside the markers. `TestRunRefresh_...` runs refresh twice and
asserts `second == first` byte-for-byte. verify-cli-runner A4 dogfood: `.gitignore` sha1 unchanged
across a 2nd AND 3rd `da refresh`; `diff` empty. NOTE (non-blocking, already logged by verify): the
committed `.agentsrc.lock` itself is NOT byte-stable across refreshes, but the drift is
timestamps-only (`refreshedAt`/`fetched_at`/`last_checked_at`); structural content
(`inputs_digest`, `units`) is stable and OUTSIDE the D14 managed-.gitignore contract, which IS
fully byte-stable. Pre-existing lock-metadata churn, not this task's deliverable.

### 4. Lesson corrected — CONFIRMED
`.agents/lessons/classify-generated-files-before-cleanup/LESSON.md`: the WRONG
"lock files → gitignore" guidance is removed. Old item 3 lumped "hook manifests … lock files" →
"gitignore"; new item 3 is per-machine WIRING only → ignore via the da-managed `.gitignore` block
(never an ad-hoc root rule), and a NEW item 4 states the committed resolved-state contract
(`.agentsrc.lock`/`.agentsrc.json`, uv.lock analog) → TRACK, never ignore, citing
`links.EnsureManagedGitignore` `neverIgnored`. The routing checklist and the "distinguish" bullet
are updated in lockstep, and the index.md row is rewritten to match. The
config-vs-telemetry-vs-stale classification is preserved.

### 5. Both #381 root rules removed — CONFIRMED
`.gitignore` diff deletes the entire trailing #381 stanza: BOTH `.github/hooks/*.json` AND
`.agentsrc.lock` (plus their comments) are gone (-8 lines). The hooks are now covered by the
managed block (criterion 1); the lock is now a tracked contract (criterion 2). Consistent cutover,
no shim/leftover rule.

## Platform invariants (acceptance-invariants lens) — held

- **Managed-link / link-model:** the change does NOT write any final platform path directly; it
  delegates path knowledge to `platform.CollectManagedOutputs` (interface + static table) and the
  `links` layer. `refresh.go` holds zero path literals. The managed-generation flow is untouched;
  this only adds the `.gitignore` ignore-surface derived from the same enabled-platform set.
- **Config-v2 coherence:** no new `AgentsRC` fields, no struct/Unmarshal/Marshal/`agentsRCKnown`/
  schema changes — the schema-usage lifecycle is not implicated. The committed lock carries
  `inputs_digest` + per-unit digests; committing it strengthens (not breaks) lock/outputs parity.
- **Cross-OS contracts:** `.gitignore` patterns are forward-slash by spec on every OS; the new
  code builds patterns with literal `"/"` concatenation and contains NO inline `runtime.GOOS`.
  Cross-OS-identical, consistent with `leverage-cross-platform-fs-helpers`.
- **Workflow artifact model:** done criteria trace to config-distribution-model/design.md §15
  (D14/R8) → plan → TASKS.yaml `notes`; tiers not collapsed.

## Notes (LOW / observational — non-blocking)
- N1: `.agentsrc.lock` timestamp churn (above) is real but out of this contract's scope; carried
  forward as a finding, not a defect here.
- N2 (inherited from architecture-standards N1): the hybrid static-table + interface seam is a
  documented drift-risk tradeoff; does not affect acceptance.

Verdict: **APPROVE** (lens: acceptance-invariants).
