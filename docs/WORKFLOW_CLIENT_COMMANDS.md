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
`da workflow` surface has ~34 subcommands — beyond the atoms shown, it also
includes `merge-back`, `fold-back`, `delegation` (closeout/gate), `contract`,
`drift`, `sweep`, `bundle`, `hook-sentinel`, `hook-outcome`, `archive-orphans`,
and `plan` verbs (`schedule`, `derive-scope`, `check-scope`), among others. Run
`da workflow --help` for the full surface.

`tier:` + `calls:` metadata lives in the package-doc comment of each T1 source file (see `commands/workflow/close_task.go` and `start_task.go`). A `TestTierDeclarationsPresent…` pin guards the markers so a refactor that strips them fails the suite immediately rather than silently breaking downstream lint.

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

`--no-commit` skips the final step; `--next-focus` overrides the auto-pick.

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

`--no-derive-scope` skips the sidecar derivation; `--no-commit` skips the final step. Fanout is intentionally **not** wired — the orchestrator typically decides direct-vs-delegated explicitly via `da workflow fanout` as a separate step.

## Iteration-close skill update (deferred)

**Currently deferred / not yet implemented** — the rewrite described below is
staged, not live. The `iteration-close` skill still uses its existing body.

The `iteration-close` skill — the T2 compound that wraps `close-task` — lives outside this repo at `~/.agents/skills/dot-agents/iteration-close/SKILL.md`. The rewrite as a thin `da workflow close-task --json` caller is staged for the next cross-repo update of the dot-agents skill pack. The proposed frontmatter:

```yaml
tier: compound
calls:
  - workflow-close-task
review_gate: default
attendance: unattended
```

The skill's body becomes: invoke `da workflow close-task --json` with the resolved plan/task, then render the returned `closeTaskResult` (iteration N, score value + band, sidecar path, next focus) back to the operator. The immediate-feedback loop closes inside the skill: the operator sees *"iteration N → score 0.7 fair → here is the breakdown"* the moment the iteration closes, while the context is still hot.

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

Task→PR matching reuses the bounded `strictBranchMatch` token rule, so a
task id never resolves a sibling's PR.

```text
da workflow journal snapshot          # capture current live state
da workflow journal recover           # verified resume view (re-grounded against reality)
da --json workflow journal recover    # structured RecoveryResult for skills/scripts
da workflow journal show --limit 50   # snapshot + recent events
da workflow journal prune --keep 200  # bounded retention
```
