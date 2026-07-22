---
title: Workflow Client Commands
description: The start-task and close-task client commands that compose workflow primitives.
sidebar:
  order: 6
---

# Workflow client commands

`da workflow close-task` and `da workflow start-task` are the two **client commands** in the workflow surface — T1 molecules under the skill-tiering-contract that compose existing T0-atom primitives into the start-of-iteration and end-of-iteration sequences operators (and skills) repeat every cycle.

## Layering against skill-tiering-contract

The composition rules we want enforced are restatements of the tier invariants in `.agents/workflow/specs/skill-tiering-contract/design.md` §4. There is **no parallel "primitive vs client vs skill" axis** — every layer maps to a tier the contract already names.

| Layer | Contract tier | Examples (illustrative — not exhaustive) |
|-------|---------------|----------|
| Primitive | **T0 atom** | `workflow verify record`, `workflow advance`, `workflow plan update`, `workflow checkpoint --log-to-iter`, `workflow commit`, `workflow plan derive-scope`, `score iteration`, `score iteration --recompute` |
| Client command | **T1 molecule** | `workflow close-task`, `workflow start-task` |
| Skill | **T2 compound** | `iteration-close`, `orchestrator-session-start`, `isp` |

The primitive examples above are a representative slice, not the full set. The
`da workflow` surface ships 36 top-level subcommands (several with their own
sub-verbs); the [full command surface](#full-da-workflow-command-surface) below
groups every one. Run `da workflow --help` for the generated listing.

`tier:` + `calls:` metadata lives in the package-doc comment of each T1 source file (see `commands/workflow/close_task.go` and `start_task.go`). A `TestTierDeclarationsPresent…` pin guards the markers so a refactor that strips them fails the suite immediately rather than silently breaking downstream lint.

## Full `da workflow` command surface

Every top-level `da workflow` subcommand, grouped by role. Sub-verbs are listed
in parentheses. `close-task` and `start-task` are the T1 client commands
detailed elsewhere on this page; everything else is a T0 primitive (or a
primitive family). `--json` is available on the machine-readable verbs.

**Session state & orientation**

| Command | What it does |
|---------|--------------|
| `status` | Show workflow state for the current project (includes the Work Tracking block). |
| `orient` | Render session orient context for the current project. |
| `health` | Show a workflow health snapshot. |
| `checkpoint` | Write a checkpoint (`--message`, `--verification-status`/`--verification-summary`, `--log-to-iter N [--role] [--verifier-type]`). |
| `log` | Show recent checkpoint log entries (`--all`). |
| `journal` (`snapshot`, `recover`, `show`, `prune`, `append`) | Session-handoff journal — see [below](#session-handoff-journal-da-workflow-journal). |

**Selection & planning**

| Command | What it does |
|---------|--------------|
| `plan` (`show`, `graph`, `create`, `update`, `archive`, `schedule`, `derive-scope`, `check-scope`) | List canonical plans; sub-verbs show/render/create/update/archive plans, print the wave schedule, and derive/check scope-evidence sidecars. Bare `plan` lists all canonical plans. |
| `task` (`add`, `update`, `rename`, `supersede`) | Add, update, rename, or supersede tasks within a canonical plan (rename/supersede repoint every dependency; both take `--dry-run`/`--json`). |
| `tasks <plan-id>` | Show the tasks for a canonical plan. |
| `slices <plan-id>` | Show the slices for a canonical plan. |
| `eligible` | List all unblocked eligible tasks across active plans with conflict detection (`--plan`, `--limit`). |
| `slots` | Show the slot ledger (occupied / awaiting-owner / blocked) across active plans (`--plan`). |
| `next` | Suggest the next actionable canonical task (`--plan`). |
| `complete --plan <ids>` | Probe scoped plan-completion state. |
| `advance <plan-id> --task --status` | Advance a task's status within a canonical plan (`--commit-state` to stage + commit atomically). |

**Verification & scoring**

| Command | What it does |
|---------|--------------|
| `verify` (`record`, `log`) | Manage the verification log: `record` a test/lint/build/format/custom/review run; `log` shows entries (`--all`). |

**Delegation & fanout**

| Command | What it does |
|---------|--------------|
| `fanout` | Delegate a task to a sub-agent with a bounded write scope (bundle-producing; many flags — see `--help`). |
| `contract` | Materialize and inspect delegation contracts for direct orchestrator work. |
| `bundle` (`stages`) | Inspect delegation bundle artifacts; `stages` expands a bundle into the ordered impl → verifier(s) → review stage list. |
| `merge-back --task --summary` | Record a sub-agent's completed work as a merge-back artifact (`--commit-state`). |
| `delegation` (`closeout`, `gate`) | Parent-driven delegation lifecycle: `closeout` archives merge-backs and reconciles task state; `gate` evaluates review evidence into accept/reject/escalate. |
| `fold-back` (`create`, `update`, `list`) | Route loop observations into TASKS.yaml notes, plan summary, or a `~/.agents` proposal. |

**Profiles, prompts & graph**

| Command | What it does |
|---------|--------------|
| `prefs` (`show`, `set-local`, `set-shared`) | Show resolved workflow preferences; set a user-local override or propose a shared change. |
| `app-types` | List available `app_type` values for the current repo (`--verbose`, `--format`). |
| `resolve-prompt --kind --slug` | Resolve a stage profile's composed (base-first, scope-resolved) `prompt_files`. |
| `pipeline` | Emit the materialized per-task pipeline from the profile IR. |
| `graph` (`query`, `health`) | Query knowledge-graph context by bridge intent; `health` reports bridge adapter state. |

**Persistence & maintenance**

| Command | What it does |
|---------|--------------|
| `commit` | Stage and commit workflow-state changes — managed roots plus declared session paths (`--dry-run`, `--include`). Idempotent. |
| `drift` | Detect workflow drift across managed repos (read-only). |
| `sweep` | Plan and optionally (`--apply`) fix workflow drift across managed repos. |
| `archive-orphans` | Sweep stale active merge-back/delegation artifacts after a plan archive. |
| `hook-sentinel` | Write/read/clear hook sentinels declaring per-skill stop-gate context. |
| `hook-outcome` | Append hook gate outcomes to the active iteration's sidecar. |
| `state-ref` (`reconcile`) | Inspect and reconcile the machine coordination state ref (`refs/agents/state`) — see [below](#coordination-state-reconcile-da-workflow-state-ref-reconcile). |

**Client commands (T1)**

| Command | What it does |
|---------|--------------|
| `start-task <plan-id> --task` | Start-of-iteration client command: activate plan → focus task → derive scope → commit. |
| `close-task <plan-id> --task` | End-of-iteration client command: checkpoint → score → advance → focus → commit. |

## Composition rules (= tier invariants)

1. **A client command must be expressible as a pipeline of primitives.** Restates T1 molecule's invariant: *"`calls:` lists the atoms it invokes; runtime agent judgment bounded to picking among declared atoms"*. If a client command has secret state that cannot be mapped onto a primitive, it belongs in a new primitive.
2. **Primitives stay machine-grade.** Restates T0 atom's invariant: *"~deterministic; output shape is schema-specified"*. JSON output is first-class, no interactive prompts. Client commands may be chattier (a human-readable summary; `--json` for structured output).
3. **One direction of dependency.** Client → primitives, never the reverse. The tier number is also a dependency-direction marker — a lower tier never calls a higher one.
4. **Skills call clients when they exist, primitives otherwise.** Restates T2 compound's invariant: *"`calls:` lists the molecules it orchestrates; may also call atoms directly (uncommon)"*. The migration path is: write the primitive → watch the call sequence repeat in sessions → promote the recurring sequence to a client command when the pattern is undeniable.

## Relation to the workflow engine lifecycle diagram

`docs/PROJECT_DIAGRAMS.md` §5 already shows the workflow engine in lifecycle tiers (Authoring → Selection → Execution 3a/3b → Close → Archive). The composition-tier model above is **orthogonal** to that lifecycle model:

- A close-time client command (T1) lives in lifecycle Tier 4 (Close).
- A start-time client command (T1) lives in lifecycle Tier 2 (Selection) plus the bridge into 3a/3b.
- A T0 atom can live in any lifecycle tier — `verify record` is Tier 4, `plan derive-scope` is Tier 2, `workflow commit` is Tier 4.

The two diagrams answer different questions. Composition tier: *what calls what?* Lifecycle tier: *when in the iteration does this run?*

## Equivalent primitive pipelines

Every client command's `--help` `Example` block shows the call. The expanded sequences are documented here once so callers can see the layer:

### `workflow close-task`

```text
# What close-task does, expanded into the primitive pipeline it invokes:

da workflow checkpoint --log-to-iter <N> --role impl
    # N picked by NextIterationNumber(iter-log-dir) — see iter_log_autoderive.go
da score iteration <N> --recompute
    # writes iter-N.score.yaml; same writer as workflow-client-commands score-current task
da workflow advance <plan-id> --task <task> --status completed
da workflow plan update <plan-id> --focus <next-eligible>
    # next-eligible picked by selectAllEligibleTasks on the same plan
da workflow commit
    # honors commit.disable per-project opt-out
```

`--no-commit` skips the final commit step; `--next-focus <task>` overrides the
auto-picked next focus. `--score-recompute` scopes the score step —
`current` (default; the just-closed iteration only), `recent-N`, or `all`.
`--repo-dir <path>` sets the repository root used for git topology (defaults to
the current working directory), and `--transcript-dir <path>` (repeatable) names
agent transcript roots for token backfill during scoring.

The underlying `workflow commit` primitive takes `--dry-run` (print the staging
path set + generated commit message; make no changes) and `--include <path>`
(repeatable; declare additional session-touched state files to consider for
staging). It is idempotent — a second run with no new mutations is a clean
no-op.

### `workflow start-task`

```text
# What start-task does:

da workflow plan update <plan-id> --status active
da workflow plan update <plan-id> --focus <task>
da workflow plan derive-scope <plan-id> <task> [--seed-symbol …] [--seed-path …]
da workflow commit
```

`--no-derive-scope` skips the sidecar derivation; `--no-commit` skips the final step; `--dry-run` (or the global `-n`) previews the whole chain without activating the plan, focusing the task, deriving scope, or committing. Fanout is intentionally **not** wired — the orchestrator typically decides direct-vs-delegated explicitly via `da workflow fanout` as a separate step.

## Iteration-close skill

The `iteration-close` skill — the T2 compound that wraps `close-task` — ships in
the dot-agents starter skill pack
(`internal/scaffold/home/starter/skills/global/iteration-close/`, installed to
`~/.agents/skills/dot-agents/iteration-close/` by `da init` / `da refresh`).

On the **direct** path the skill closes the iteration through
`da workflow close-task <plan> --task <task> --json` — one call that writes the
iter-log `impl` block, scores the iteration, advances the task to `completed`,
refocuses the plan to the next eligible task, and commits the workflow state —
then renders the returned `closeTaskResult` (iteration N, score value + band,
sidecar path, next focus) back to the operator. The immediate-feedback loop
closes inside the skill: the operator sees *"iteration N → score 0.7 fair → here
is the breakdown"* the moment the iteration closes, while the context is still hot.

`close-task` covers only the direct-path tail, so the skill stays a T2 compound
that also drives the surrounding atoms directly (which the tiering contract
permits): `verify record --kind test`, `/self-review` + `verify record --kind
review` + `checkpoint --role review`, and the narrative `checkpoint --message`.
The **delegated** path does **not** use `close-task` — advancing to `completed`
is the parent's call after review — so a delegated worker runs `checkpoint
--log-to-iter N --role impl` then `workflow merge-back`, and the parent completes
the task via `workflow delegation closeout`.

## Coordination-state backend surfacing (`da workflow status`)

Coordination state (a plan's `PLAN.yaml` + the per-task status blobs) has a
**storage plane** selected by `work_tracking.backend` in `.agentsrc.json`. The
plane is documented in full under [Work tracking](./LAYERED_CONFIG_GUIDE.md#work-tracking-coordination-state-plane);
the short version:

- **`local` (default)** — the per-worktree working copy is the source of truth.
  Byte-for-byte today's behaviour.
- **`git-ref` (opt-in)** — coordination state is read from and mirrored to
  `refs/agents/state`, a local, read-your-writes-safe ref that is orthogonal to
  the code branch. The default stays `local`; flipping the default is a gated
  cutover tracked by `.agents/active/state-ref-transition.md`.

`da workflow status` (and `da workflow orient`) surface a **Work Tracking**
block so operators and orchestrators can see which store is authoritative — the
active `backend` and whether `refs/agents/state` is the live coordination SOT.
Agents still read the projected working-copy files regardless of backend; the
surfacing reports the *source of truth* those projections derive from.

```text
Work Tracking
  backend: local
  coordination SOT: working copy (local)
```

```text
Work Tracking
  backend: git-ref
  coordination SOT: refs/agents/state (live)
```

`da --json workflow status` carries the same data under the `work_tracking`
object (`backend`, `state_ref_sot`, `state_ref`) for skills and scripts.

## Coordination-state reconcile (`da workflow state-ref reconcile`)

Under the `git-ref` backend, `refs/agents/state` must hold every plan's canonical
coordination state. `da workflow state-ref reconcile` re-mirrors each plan's
working-copy `TASKS.yaml`/`PLAN.yaml` onto `refs/agents/state` — idempotent and
tree-equality guarded, so a consistent run writes no ref commit. Use it to bring a
ref that was seeded before some plans existed (or a plan mutated while the backend
was `local`) back into sync, and always run it before flipping
`work_tracking.backend` to `git-ref` repo-wide so the ref is a complete mirror
rather than a stale subset of the active plans.

- `--dry-run` — report which plans would be seeded without writing to the ref.
- `--json` — emit the reconcile report as JSON.

## Session-handoff journal (`da workflow journal`)

The session-handoff journal is an append-only, crash-survivable event log plus a
deterministic live-state snapshot, kept **off** the git-tracked tree under the XDG
state directory (`<XDG_STATE_HOME>/dot-agents/journal/<repo-fingerprint>/`). State-
mutating `da workflow` commands append one typed event on success, so a session
resumed after a context compaction or crash can re-inject state from durable file
state — re-verified against current reality — instead of re-grounding from scratch.

The engine lives in `internal/journal`; `da workflow journal` is its user-facing
surface. Every subcommand operates on the current repository and supports `--json`.

| Subcommand | What it does |
|------------|--------------|
| `journal snapshot` | Capture the deterministic live-state snapshot (active plans + task statuses, the pending-unblocked projection, in-flight delegations, pending merge-backs) to `snapshot.json`. |
| `journal recover` | Build the **verified** recovery view: reconstruct from the snapshot watermark + event replay, then re-verify each item against current reality. Renders the verified / changed / missing / unverified items, the trust gradient, the canonical-vs-in-PR locus (with enriched coords), quarantined identity conflicts, and the bundle freshness label. |
| `journal show` | Display the current snapshot summary plus the recent event log (`--limit N`, default 20; `--all` to lift the cap). |
| `journal prune` | Drop events beyond a bounded retention (`--keep N`, default 1000); keeps the newest N. The rewrite is atomic (temp-then-rename) under the journal's advisory lock; honors the global `-n` dry-run. |
| `journal append` | Low-level: append one raw event (`--command`, `--actor`, `--event-type`, `--input`/`--observed` JSON). Bypasses the typed per-command schemas (raw Emit path, still size-capped); intended for the reasoned-overlay and testing, not routine workflow mutation. |

### Verification sources (how `recover` re-verifies)

`journal recover` re-verifies each reconstructed item against an **ordered** list of
sources — authoritative store/service-backed first, local fallback last — and
enriches the snapshot's placeholder locus coordinates (PR `0`, the `canonical`
sentinel ref) with the resolved reality:

- **`gh` (authoritative)** — resolves PR state/number + merge sha. Open PRs flow
  through the existing `event.pr.*` producer; a merged task's merge sha comes from a
  narrow `gh pr list --state merged` read. A `gh`/network failure makes the source
  unavailable, so recovery falls through to the local fallback.
- **`local` (non-authoritative)** — confirms a task's existence + canonical status
  from the live `TASKS.yaml` when `gh` is unavailable. It corroborates the locus arm
  and status (medium trust) but never resolves authoritative coordinates.

`journal recover`'s Task→PR matching uses the stricter `strictBranchMatch` rule —
a complete, bounded token that binds the full `<plan>-<task>` identity when the
plan is known — so a task id never resolves a sibling's (or a cross-plan
look-alike's) PR.

```text
da workflow journal snapshot          # capture current live state
da workflow journal recover           # verified resume view (re-grounded against reality)
da --json workflow journal recover    # structured RecoveryResult for skills/scripts
da workflow journal show --limit 50   # snapshot + recent events
da workflow journal prune --keep 200  # bounded retention
```
