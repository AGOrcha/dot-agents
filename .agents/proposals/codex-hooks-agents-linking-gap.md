# Proposal: Codex hooks + agents linking gap (audit) and #29 doc-change review

> **CORRECTION 2026-05-18 (post-verification):** Finding 2's claim "no production caller exists on master" was based on a STALE local `master` ref (04826b21/#13). Verified against real origin/master (efb19756): refresh.go:157 / add.go:531 / install.go:222 DO invoke RunSharedTargetProjection (via 8e86a35e). The real gap is NARROW — only `relinkImportedProjects` (import) + the doctor repair path lack the projection. The `shared-target-projection-wiring` plan is re-scoped accordingly; #29 doc text corrected. See [[stale-local-master-ref]] lesson.


- type: project-local audit / analysis proposal (read-only investigation)
- status: draft / for-review
- date: 2026-05-18
- scope: `internal/platform/` (codex.go, hooks.go, resource_plan.go),
  `commands/` (refresh.go, install.go, add.go), `docs/PLATFORM_DIRS_DOCS.md`
- branches inspected: `origin/master` (HEAD), `origin/docs-refresh` (PR #29)

## Problem

The maintainer, reading `docs/PLATFORM_DIRS_DOCS.md` as edited by the
docs-refresh agent on PR #29, suspected the doc describes a real gap as if
intended: Codex hooks not wired, and Codex agents rendered but not linked.
This audit establishes ground truth on master and judges whether #29
faithfully represents the code or launders a bug.

## Ground-truth findings (file:line evidence)

### Finding 1 — Codex hook rendering EXISTS in code (doc is stale)

`internal/platform/codex.go` fully implements Codex hook emission:

- `CreateLinks` calls `createHooksLinks` — `codex.go:160-163`
- `createHooksLinks` → `writeRepoHooks` + `writeUserHomeHooks` — `codex.go:205-210`
- `writeRepoHooks` renders `.codex/hooks.json` via `renderCodexHookConfig`
  through `emitPreferredHookFile` — `codex.go:212-229`
- `writeUserHomeHooks` renders `~/.codex/hooks.json` — `codex.go:231-244`
- `renderCodexHookConfig` is a real renderer with a Codex event map
  (`SessionStart`, `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`)
  — `hooks.go:616-634`, `hooks.go:701-732`

`codex.CreateLinks` is invoked by the production command path:
`commands/refresh.go:147`, `commands/install.go:199`, `commands/add.go:438`.

**Verdict for hooks:** Codex hooks ARE rendered and written (as a managed
regular file, not a symlink — `writeManagedFile`, `hooks.go:835`) for both
repo `.codex/hooks.json` and user `~/.codex/hooks.json`. Per official Codex
docs (`PLATFORM_DIRS_DOCS.md:36`: "hooks live in `.codex/hooks.json` and
`~/.codex/hooks.json`"), this is the correct native target and Codex
consumes it directly — no separate "link" step is required for hooks (the
file IS the native artifact). **The Codex-hooks claim is NOT a gap on
master. The doc table is simply stale.**

### Finding 2 — Codex agents: GENUINE render-without-link gap on master

Codex agent definitions are rendered to TOML, but the render is performed
by a code path the production command layer never invokes on master.

- `codex.CreateLinks` → `createAgentsLinks` only **prunes** stale tomls;
  it does NOT materialize them — `codex.go:151`, `codex.go:195-199`
  ("TOML files are materialized by CollectAndExecuteSharedTargetPlan;
  prune stale `.toml` ...").
- The actual repo-level materializer is the shared-target plan:
  `codex.SharedTargetIntents` → `BuildSharedCodexAgentTomlIntents`
  (`codex.go:401-411`, `resource_plan.go:412-420`), executed by
  `executeRenderSingleWrite` via the `codex-agent-toml` materializer
  (`resource_plan.go:181-189`, `resource_plan.go:25`).
- That plan is run only by `CollectAndExecuteSharedTargetPlan` /
  `RunSharedTargetProjection` / `BuildSharedTargetPlan` in
  `resource_plan.go:467-492`.
- **No production caller exists on master.** Grep of `commands/` and
  `cmd/` for `CollectAndExecuteSharedTargetPlan` /
  `RunSharedTargetProjection` / `BuildSharedTargetPlan` returns only
  test files (`internal/platform/resource_plan_test.go`) and unrelated
  doc-comments. `refresh.go`/`install.go`/`add.go` call only
  `projectsync.CreateProjectDirs`, `restoreFromResources`, and
  `p.CreateLinks` per platform.
- Git history confirms the wiring was authored but never merged to master:
  - `829a3490 commands: wire CollectAndExecuteSharedTargetPlan before
    platform loops` and `c78b470b platform: add RunSharedTargetProjection
    for refresh/install/add` exist **only on feature branches**
    (`feature/PA-cursor-projectsync-phase1-extract-293f`), not on
    `origin/master` (`git merge-base --is-ancestor 829a3490 origin/master`
    → NOT ON MASTER).
  - The shared-target machinery itself landed on master via
    `bdaf37ea feat(platform): resource model, shared target plan,
    projectsync` — i.e. the producer landed but the command-layer
    invocation did not.

**Consequence on master:**
- Repo-level `.codex/agents/*.toml` is **never produced** by
  `da refresh` / `da install` / `da add` (only pruned).
- User-level `~/.codex/agents/*.toml` IS still produced — `ensureUserAgents`
  → `writeCodexAgents` writes them directly without the shared plan
  (`codex.go:113-118`, `codex.go:168-183`, `codex.go:292-314`). So global
  agents reach `~/.codex/agents/`, but project-scoped agents do not reach
  the repo.
- The same dormant path also gates Claude's shared repo targets
  (`.claude/skills/*`, `.agents/skills/*`) — `claude.go:497-503` comments
  that those are "written by CollectAndExecuteSharedTargetPlan at the
  command layer before CreateLinks is called". That caller does not exist
  on master, so the blast radius is broader than Codex (project-scoped
  skill mirrors are also affected). Claude project agents still work via
  `claude.createAgentsLinks` → `syncScopedDirSymlinksTargets`
  (`claude.go:487-495`), which is independent of the shared plan — that is
  why Claude looks fine while Codex project agents are broken.

### Finding 3 — Codex agents are NOT placed in `.claude/agents/`

`internal/platform/codex.go` contains zero references to `.claude/agents/`
(grep: no matches). Codex renders to `.codex/agents/*.toml` (when the path
runs) and `~/.codex/agents/*.toml`. The official docs agree this is the
correct native location (`PLATFORM_DIRS_DOCS.md:34`). The doc's repeated
claim that "both Go and bash implementations currently place project
agents in `.claude/agents/`" for Codex is **factually wrong** regardless
of the projection-wiring gap.

### Intended-vs-actual verdict

| Claim | Intended? | Actual on master | Classification |
|-------|-----------|------------------|----------------|
| Codex hooks not wired | n/a | `.codex/hooks.json` + `~/.codex/hooks.json` ARE rendered/written | **Doc stale; code correct. Not a bug.** |
| Codex agents in `.claude/agents/` | no | Codex targets `.codex/agents/*.toml`, never `.claude/agents/` | **Doc factually wrong.** |
| Project `.codex/agents/*.toml` produced | yes (design + feature branch) | Never produced on master (renderer dormant — command-layer caller unmerged) | **Genuine bug: render-without-invoke regression/omission.** |

Net: the maintainer's instinct is **half right**. Codex hooks are fine
(doc is just stale). Codex *project* agents are a real bug — but the true
defect is not "rendered but not linked"; it is "the renderer is never
invoked by any production command on master," because the command-layer
wiring (`829a3490`) shipped only on a feature branch. User-scoped Codex
agents and Codex hooks are unaffected.

## Remediation scope (NOT implemented)

Two independent items:

1. **Wire the shared-target projection into the command layer (the real
   fix).** Land the equivalent of feature-branch `829a3490` /
   `c78b470b`: call `CollectAndExecuteSharedTargetPlan` (or
   `RunSharedTargetProjection` for dry-run parity) once per project in
   `commands/refresh.go`, `commands/install.go`, `commands/add.go`
   *before* the per-platform `CreateLinks` loop, passing the enabled
   platform set. Blast radius: 3 command files + restored coverage in
   `commands/*_test.go`; `internal/platform` is already complete and
   tested. This also restores Claude's shared `.claude/skills/` /
   `.agents/skills/` projection, so it is not Codex-only.
   - Risk: dry-run output and idempotence must be verified
     (`RunSharedTargetProjection(..., dryRun)` already exists for this).
   - This warrants a small **spec-light plan** (it is a known,
     designed-but-unmerged integration, not new design): one plan,
     ~3 tasks (wire refresh, wire install/add, regression + dry-run
     parity tests). No new spec needed — design intent is already
     captured by the resource-model work and the existing feature-branch
     commits; a `workflow/plans/<id>` with traceability to those commits
     is sufficient.

2. **Correct `docs/PLATFORM_DIRS_DOCS.md`** (see #29 review below). This
   is a bounded doc edit, no plan needed.

Recommended path: open one bounded plan for item 1 (cite `829a3490` /
`c78b470b` as the reference implementation to forward-port and verify on
master), and fix the doc separately/immediately for item 2.

## #29 doc-change review

Source of truth: `git diff origin/master origin/docs-refresh --
docs/PLATFORM_DIRS_DOCS.md`.

### What #29 actually changed

The docs-refresh agent's edits are confined to the
`## dot-agents Implementation Audit` section and are almost entirely a
**"bash retired, Go is sole impl"** cleanup:

- Adds a note that the legacy `src/` bash impl is retired and audit notes
  describe the Go implementation.
- Drops the `Bash implementation` column from the Hook Wiring Audit table
  and removes "both Go and bash" / "Bash also still lacks ..." phrasing
  from the path-strategy and Copilot rows.
- Promotes `.github/hooks/*.json` from "Go-only" to unqualified for
  Copilot (consistent with bash removal).

### Did it launder the gap?

**No new laundering, but it propagates two pre-existing inaccuracies that
predate #29 (already wrong on master):**

1. Codex path-strategy row (`PLATFORM_DIRS_DOCS.md:123`): #29 keeps
   "Codex-native subagents are documented under `.codex/agents/*.toml`,
   but ... the implementation currently places project agents in
   `.claude/agents/`." This is **factually wrong** (Finding 3) — Codex
   never targets `.claude/agents/`. #29 did not introduce this; it
   inherited it from master and merely rephrased "both Go and bash" →
   "the implementation". It did not correct it.
2. Codex hook-audit row (`PLATFORM_DIRS_DOCS.md:135`): #29 keeps
   "Codex | `.codex/hooks.json` | No | ... but [it] does not create
   `.codex/hooks.json`." This is **stale/wrong** (Finding 1) — code does
   render and write it. Again inherited from master, rephrased not fixed.

#29 does NOT assert the *agents-projection* bug (Finding 2) as
by-design — it does not mention the shared-target projection at all. So
#29 is not actively laundering Finding 2; it is silent on it. The doc's
real sin (both before and after #29) is the two inherited factual errors
about Codex.

### Lines that assert as correct/by-design something that is a gap or wrong

- `docs/PLATFORM_DIRS_DOCS.md:123` (Codex row, "Notable difference"):
  asserts Codex project agents go to `.claude/agents/`. **Wrong** — they
  target `.codex/agents/*.toml` (and on master are not produced at the
  repo level at all due to Finding 2).
- `docs/PLATFORM_DIRS_DOCS.md:135` (Codex hook row): asserts Go does not
  create `.codex/hooks.json`. **Stale/wrong** — Go renders and writes it
  (`codex.go:212-244`).
- (Cursor row `:121` carries the analogous `.claude/agents/` claim for
  Cursor — out of scope here but the same stale-audit pattern; flag for
  follow-up.)

### Verdict on #29's PLATFORM_DIRS_DOCS.md change

**AMEND (do not merge as-is; do not revert the file).** The bash-removal
cleanup in #29 is correct and worth keeping — reverting the whole file
change would be wrong. But #29 must not ship while the Codex rows still
state falsehoods. Either #29 amends those two rows, or they are fixed in a
fast-follow before #29's doc is treated as authoritative. Proposed exact
replacement text (apply via the doc, not by this audit):

- Replace the Codex row in **Current Path Strategy by Platform**
  (`PLATFORM_DIRS_DOCS.md:123`) with:

  > `| Codex | `AGENTS.md`, `.codex/config.toml`, `~/.codex/agents/*.toml`
  > (user scope), `~/.codex/hooks.json` + repo `.codex/hooks.json`,
  > `.agents/skills/` | Codex-native subagents are rendered as
  > `.codex/agents/*.toml` (not `.claude/agents/`). User-scoped agent
  > TOMLs are written directly; **repo-level `.codex/agents/*.toml` is
  > currently NOT produced on master because the shared-target
  > projection is not invoked by any command (see
  > `.agents/proposals/codex-hooks-agents-linking-gap.md`).** |`

- Replace the Codex row in **Hook Wiring Audit**
  (`PLATFORM_DIRS_DOCS.md:135`) with:

  > `| Codex | `.codex/hooks.json` | Yes | Renders and writes repo
  > `.codex/hooks.json` and user `~/.codex/hooks.json` via
  > `renderCodexHookConfig` (`internal/platform/codex.go`,
  > `internal/platform/hooks.go`); managed regular file, not a symlink. |`

  (Column count now matches the de-bashed table: Platform | Official |
  Go implementation | Notes.)

If the maintainer prefers minimal #29 churn, the acceptable alternative
is: merge #29's bash-removal as-is and immediately open the doc-fix
(item 2 above) plus the projection-wiring plan (item 1) so the doc is
corrected in the same release the bug is fixed. Reverting #29's file
change is NOT recommended (it would re-introduce stale bash references).

## Should this graduate to a spec→plan?

- **Item 1 (projection wiring):** Yes to a **plan**, no to a new spec.
  The design already exists (resource-model spec + feature-branch
  commits `829a3490`/`c78b470b`). One bounded `workflow/plans/<id>`
  forward-porting and verifying that wiring on master, ~3 tasks, with
  dry-run/idempotence regression. It is a designed-but-unmerged
  integration, not open design.
- **Item 2 (doc correction):** No plan — direct bounded edit to
  `docs/PLATFORM_DIRS_DOCS.md` (Codex rows, and audit the Cursor row for
  the same stale `.claude/agents/` pattern).
