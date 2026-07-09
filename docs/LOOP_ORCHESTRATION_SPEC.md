# Loop Orchestration Spec

Status: Draft
Last updated: 2026-05-26
Related:
- `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md`
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md`

## Purpose

Define the next layer above the focused loop agent: a read-mostly orchestrator that selects the next safe unit of work, derives bounded delegation slices, and folds useful observations back into plans, matrices, and proposal review.

The loop agent stays narrow. The orchestrator decides where to point it.

## Problem

The repo now has the building blocks for bounded coordination:

- canonical plans and tasks
- delegation contracts with `write_scope`
- merge-back artifacts
- workflow orient/status/health surfaces
- KG and CRG read paths

What is still missing is the layer that turns those primitives into a stable operating model:

- choose the next task from canonical state instead of stale checkpoint text
- derive safe slices before fanout so agents do not collide
- route small loop observations into the right durable artifact
- route larger cross-cutting changes into the proposal queue
- prefer graph-backed understanding over broad repo scans when the question is code-structure shaped

## Decision

The orchestrator should be a mixed system, not a single new super-agent.

### 1. Command layer

`dot-agents` owns deterministic read/write surfaces:

- `workflow next`
  - recommend the next actionable canonical task
  - accept scoped completion filters via `--plan <id>[,<id>...]`
  - return `null` when every scoped plan is drained, blocked, or paused
- `workflow eligible`
  - list every unblocked, non-delegated task across active (optionally `--plan`-scoped) plans, annotated with write-scope conflict detection and scope-evidence confidence
  - report `max_batch` / `conflict_graph` so a caller can safely fan out more than one task at once
  - the primitive `isp` and `ralph-orchestrate` read via `workflow eligible --json` before deciding fanout size; `workflow next` remains the single-task selector
- `workflow plan graph`
  - derive a dependency graph across plans, tasks, and blockers
- `workflow slices`
  - read canonical `SLICES.yaml` slice artifacts now, then later derive candidate parallel slices for a task
- `workflow fanout`
  - keep as the bounded write-scope contract creator
- `workflow merge-back`
  - keep as the return artifact writer: called by a legacy/consolidated
    compatibility worker today, and by the parent-invoked return/aggregation
    gate in the target named-reviewer model
- `workflow fold-back`
  - fold approved low-risk observations into plan notes, matrices, or lessons
- `workflow delegation closeout`
  - after `workflow merge-back`, parent archives delegation + merge-back under `.agents/history/<plan-id>/delegate-merge-back-archive/…`, removes active clutter, and reconciles canonical task/plan state (`accept` completes the task; `reject` blocks the task with an optional note)
  - scripted closeout must only auto-finalize tasks whose task-local review evidence resolves to `accept`; `reject` and `escalate` stay visible for parent review instead of being auto-accepted because artifacts merely exist
  - plan-bundle retirement under `.agents/history/<plan-id>/plan-archive/…` remains a separate follow-up (see plan Phase 7 notes)

### 2. Skill layer

Skills should compose the command surfaces into repeatable behavior:

- `orchestrator-session-start`
  - orient, compute next task, inspect graph context, decide whether to run directly or fan out
- `delegation-lifecycle`
  - bounded fanout, merge-back, and parent closeout flow
  - reads `.agents/active/delegation-bundles/<delegation_id>.yaml` as the persisted prompt/context/verification handoff (not ad hoc chat reconstruction)
- `iteration-close`
  - remains the persist and proposal closeout flow

### 3. Agent layer

Recommended operating roles:

- Orchestrator / product-owner / architect agent
  - read-mostly
  - chooses task, slices work, decides whether human review is required
- Loop worker agent
  - executes one bounded slice
  - stays focused on implementation and verification
- Optional verifier agent
  - validates merge-back output or higher-risk changes before task completion

### 4. Hook layer

Hooks should stay lightweight and non-authoritative:

- detect stale delegation contracts
- warn on pending merge-backs
- flag canonical task drift versus loop-state
- flag observations that were not folded back anywhere

Hooks should not choose work, mutate plans, or fan out agents.

## Canonical Artifact Direction

The orchestrator should reuse existing canonical artifacts where possible.

### Keep as-is

- `.agents/workflow/plans/<plan-id>/PLAN.yaml`
- `.agents/workflow/plans/<plan-id>/TASKS.yaml`
- `.agents/active/delegation/<task-id>.yaml`
- `.agents/active/merge-back/<task-id>.md`

### Additive artifacts

| Path | Purpose |
|------|---------|
| `.agents/workflow/plans/<plan-id>/SLICES.yaml` | read-first canonical slice artifact for Phase 3B, plus optional sub-task decomposition and fanout-readiness inputs for safe parallel work |
| `.agents/workflow/testing-matrix.yaml` | canonical verification targets and scenario coverage |
| `.agents/active/fold-back/<id>.yaml` | pending low-risk observation to reconcile into plans, matrix, or lessons |
| `.agents/active/delegation-bundles/<delegation-id>.yaml` | per-delegate worker/profile/prompt/context/verification bundle; inspectable handoff payload paired with the delegation contract |

### Delegation bundle workflow (orchestrator / delegation, Phase 8)

Phase 8 models handoff as three layers (do not collapse into a single ad hoc prompt):

1. **Dispatched stage instructions** — stable system-prompt guidance resolved by the parent/orchestrator (honor `write_scope`, trust canonical tasks, verification cadence, trace discipline).
2. **Project overlay** — repo-local files (plan locations, regression matrix, validation queue, project loop guidance).
3. **Per-delegation bundle** — `.agents/active/delegation-bundles/<delegation_id>.yaml`, validated by [`schemas/workflow-delegation-bundle.schema.json`](../schemas/workflow-delegation-bundle.schema.json).

**CLI status:** `workflow fanout` **writes** the delegation bundle to `.agents/active/delegation-bundles/<delegation_id>.yaml` (same `delegation_id` as the contract’s `id` field). Repeatable flags include `--delegate-profile`, `--project-overlay`, `--prompt`, `--prompt-file`, `--context-file`, `--feedback-goal`, `--scenario-tag`, `--regression-artifact`, `--validation-queue`, `--selection-reason`, plus optional `--require-negative-coverage` / `--sandbox-mutations` / `--verifier-retry-max` / `--lens-retry-max` for `verification.evidence_policy` (the retry-max flags set `verifier_chain_max` / `lens_chain_max` when > 0). File-backed flags must refer to paths inside the repo; `--regression-artifact` may name a not-yet-created file as long as it stays under the project tree.

**Manual workflow (optional):** you can still hand-edit a bundle after fanout if you need content the CLI does not yet model; prefer re-running fanout when possible so the contract and bundle stay aligned.

### Verifier routing authoring

When you want `workflow fanout` to choose a verifier chain automatically, author one exact routing key and reuse it consistently.

- Put the task-specific value in `TASKS.yaml` as `app_type`.
- Put the plan-wide fallback in `PLAN.yaml` as `default_app_type`.
- `da workflow task add --app-type ...` writes the same `TASKS.yaml app_type` field.
- The value must exactly match a key under `.agentsrc.json execution_profile.by_app_type`, and its `topology.verifier_sequence` slugs must each resolve under `stage_profiles.verifier` (per the `stage-profile-and-routing-consolidation` spec, shipped 2026-06-22). The flat `app_type_verifier_map` is a deprecated back-compat-only alias — legacy manifests still fold it into `execution_profile` on load, but it is no longer the mechanism to author against and is absent from this repo's live `.agentsrc.json`.
- Prefer one shared stack key such as `go-http-service` or `next-frontend` over mixing stack keys and repo names.
- If a Markdown plan, handoff, or design doc mentions verifier routing, repeat the exact YAML key being used instead of inventing a synonym.

Example:

```json
{
  "stage_profiles": {
    "verifier": {
      "unit": { "label": "Go unit tests", "prompt_files": [] },
      "api": { "label": "API/contract verification", "prompt_files": [] },
      "integration": { "label": "Integration suite", "prompt_files": [] }
    }
  },
  "execution_profile": {
    "by_app_type": {
      "go-http-service": {
        "topology": {
          "verifier_sequence": ["unit", "api", "integration"]
        }
      }
    }
  }
}
```

```yaml
# PLAN.yaml
default_app_type: go-http-service
```

```yaml
# TASKS.yaml
tasks:
  - id: request-validation-fix
    title: Fix request validation flow
    app_type: go-http-service
```

```text
da workflow task add example-service-rollout --id request-validation-fix --title "Fix request validation flow" --app-type go-http-service
```

`workflow fanout --plan example-service-rollout --task request-validation-fix` will read `TASKS.yaml app_type` first, fall back to `PLAN.yaml default_app_type` if needed, and then look up `execution_profile.by_app_type.<app_type>.topology.verifier_sequence` (validating each slug against `stage_profiles.verifier`).

**Legacy full-slice closeout responsibilities** — bundle
`closeout.worker_must` / `closeout.parent_must` describe the compatibility
`loop-worker` path:

| Role | Schema token | Command |
|------|----------------|---------|
| Worker | `workflow_verify_record` | `da workflow verify record …` |
| Worker | `workflow_checkpoint` | `da workflow checkpoint …` |
| Worker | `workflow_merge_back` | `da workflow merge-back …` |
| Parent | `workflow_delegation_closeout` | `da workflow delegation closeout --plan <id> --task <id> --decision accept|reject [--note …]` (accepted closeout reconciles canonical completion) |

**Staged pipeline responsibility** is narrower: `impl` writes
`impl-handoff.yaml` and stops; each verifier writes its result artifact and
stops. In the current compatibility runner, consolidated `review` writes
`review-decision.yaml` plus the merge-back return artifact and stops. The
target named-reviewer model instead has each reviewer write typed evidence
and stop; a deterministic parent-invoked return/aggregation gate constructs
the consolidated decision and merge-back return packet. The parent gate alone
runs delegation closeout, which completes accepted delegated tasks.
`workflow advance` is for direct, non-delegated work, not an additional
delegated-closeout step.
A merge-back artifact is child output for parent review, not child-owned
closeout.

### Plan lifecycle

Canonical plan bundles need a terminal lifecycle, not just task-level completion.

- `draft`, `active`, and `paused` plans live under `.agents/workflow/plans/<plan-id>/`
- `completed` means execution is done but the bundle still lives in `.agents/workflow/plans/<plan-id>/` long enough for final verification, fold-back, and delegation closeout
- `archived` means the canonical bundle has been retired out of `.agents/workflow/plans/` and preserved under plan-owned history

Archive preconditions:

- every canonical task is terminal, and required work is `completed` rather than merely abandoned
- no active delegation contracts, pending merge-backs, or pending fold-back artifacts still point at the plan
- `PLAN.yaml` has already been reconciled to a terminal closeout state: `status: completed`, `current_focus_task: ""`, and final summary/notes written

Archive action:

- write a final archived copy of the bundle under `.agents/history/<plan-id>/plan-archive/<yyyy-mm-dd>/`
- preserve `PLAN.yaml`, `TASKS.yaml`, optional `SLICES.yaml`, and the human narrative plan doc when present
- stamp the archived copy's `PLAN.yaml` with `status: archived` and an updated timestamp
- remove the source bundle from `.agents/workflow/plans/<plan-id>/` so active-plan discovery surfaces no longer treat it as a live canonical plan

This keeps `.agents/workflow/plans/` reserved for live canonical plans while `.agents/history/<plan-id>/` becomes the durable record for completed work.

### Graph model

The spec dependency graph should be derived, not hand-maintained.

Inputs:

- `PLAN.yaml`
- `TASKS.yaml`
- optional `SLICES.yaml`
- active delegations
- merge-back artifacts

Derived graph edges:

- plan -> plan dependency
- plan -> task containment
- task -> task dependency
- task -> slice containment
- slice -> write scope
- slice -> delegation contract
- task -> merge-back artifact

This avoids creating another manual source of truth that would drift.

## Selection Model

`workflow next` is the first orchestrator primitive.

Selection order:

1. active canonical plan with current focus task already `in_progress`
2. active canonical plan with another `in_progress` unblocked task
3. active canonical plan with current focus task `pending` and unblocked
4. active canonical plan with first `pending` unblocked task

Guardrails:

- skip tasks with active delegations
- skip tasks whose dependencies are not completed
- prefer canonical tasks over checkpoint `next_action`

### Batch Selection Mode

`workflow eligible` extends the same guardrails to multi-task, parallel-fanout selection instead of picking one best candidate:

- returns every unblocked, non-delegated task across the (optionally `--plan`-scoped) active plans
- annotates each task with `conflicts_with` (write-scope overlap detection), `has_evidence` / `evidence_confidence` (scope-evidence sidecar), and `write_scope_declared`
- derives `max_batch`: the largest conflict-free subset of eligible tasks, bounded by the `execution.max_parallel_workers` preference or an explicit `--limit`
- is the primitive `isp` and `ralph-orchestrate` read via `workflow eligible --json` to decide how many bundles to fan out in one pass

### Scoped Completion Mode

`RALPH_RUN_PLAN` is the repo-level driver for plan-completion loops.

- When set, completion drivers must stay inside the comma-separated scoped plan list.
- `workflow complete --json --plan <id>[,<id>...]` is the scoped completion probe. It reports `actionable`, `locked`, `paused`, or `drained` so the runtime can tell a planning pause apart from a real drain.
- `workflow next --plan <id>[,<id>...]` remains the selector for the next actionable task inside that scope.
- `paused` plans act as planning locks: they stay in canonical state, but completion drivers treat them as unavailable until a human or planning artifact reactivates them.
- Closeout and fanout should ignore merge-backs and bundles that fall outside the scoped plan set during a completion run.

## Slice Model

`SLICES.yaml` should support bounded parallel work below one canonical task.

Suggested slice fields:

- `id`
- `parent_task_id`
- `title`
- `summary`
- `depends_on`
- `write_scope`
- `verification_focus`
- `owner`
- `status`

Slice creation rules:

- derive from disjoint `write_scope`
- prefer file-tree or subsystem boundaries
- allow CRG communities and impact radius to refine boundaries
- treat `SLICES.yaml` as the canonical slice artifact and `workflow fanout` as the readiness gate that decides whether a slice is safe to delegate
- do not slice a task until the command layer can prove scopes do not overlap

## Fold-Back Policy

Loop agents produce useful observations that should not remain stranded in loop-state forever.

### Auto-fold candidates

Small, repo-local, low-risk items:

- testing matrix additions
- plan note clarifications
- lesson updates
- scenario tag or trace hygiene

### Proposal-required candidates

Bigger or shared-behavior changes:

- skill behavior changes
- hook/rule changes
- repo-wide workflow defaults
- cross-repo conventions

Those should become review proposals in the queue selected by `~/.agents/rules/dot-agents/proposal-routing.md`.

## KG / CRG Direction

The orchestrator should default to graph-backed understanding when the question is code-structure shaped.

Near-term command direction:

- `workflow graph query` forwards **code-structure intents** (see table below; includes `symbol_lookup`, `impact_radius`, `change_analysis`, `tests_for`, `callers_of`, `callees_of`, `community_context`, `symbol_decisions`, `decision_symbols`) to `kg bridge query` — same behavior as invoking `da kg bridge query --intent …` from the repo.
- keep `kg changes`, `kg impact`, `kg communities`, and `kg flows` as direct escape hatches

Practical rule:

- use graph-first lookup for symbols, callers/callees, blast radius, tests, and decision links
- fall back to `rg` only when the graph is absent, stale, or the question is raw text shaped

### KG-First Query Routing

`workflow graph query` distinguishes two intent families. Summary:

| Intent | Routing | Backing |
|--------|---------|---------|
| `plan_context`, `decision_lookup`, `entity_context`, `workflow_memory`, `contradictions` | Workflow graph bridge (requires `.agents/workflow/graph-bridge.yaml` with `enabled: true`) | `LocalGraphAdapter` over configured `graph_home` (KG notes tree) |
| `symbol_lookup`, `impact_radius`, `change_analysis`, `tests_for`, `callers_of`, `callees_of`, `community_context`, `symbol_decisions`, `decision_symbols` | Subprocess: same as `da kg bridge query --intent <intent> …` | CRG / code graph via `kg bridge` |

Details:

1. **Workflow / KG-note intents** — served when the bridge config is enabled, using `graph_home` and `LocalGraphAdapter`.

2. **Code-structure intents** — **not** handled on the workflow-local filesystem bridge path. The CLI forwards to the same entry point as a manual invocation:

   `da kg bridge query --intent <intent> <query>`

   The child process uses the project working directory as `Dir`, connects stdout and stderr to the parent, and receives the global `--json` flag when the parent was run with `--json` (so JSON output shape matches `kg bridge query`).

   The workflow-local `--scope` flag applies to note-oriented queries on the filesystem bridge only; it is not passed through to the kg subprocess today. If `kg bridge query` gains a compatible `--scope`, the forwarder can pass it through without duplicating semantics here.

Orchestrator agents should prefer `workflow graph query` for both families so dispatch stays centralized. Use `grep` / `glob` only when the graph is absent, stale, or the question is raw text shaped.

This keeps a single implementation for code-structure queries (CRG / structural graph behavior in `kg bridge`) while leaving note-oriented workflow queries on the filesystem bridge.

## Initial Product Slices

Phase 3B/3C correspond to items 4 and 5 below: define the canonical slice artifact first, then gate delegation on fanout readiness checks.

1. Ship `workflow next` as the first deterministic selection primitive.
2. Add `orchestrator-session-start` skill that chains orient -> next -> graph readback -> fanout decision.
3. Add plan/task graph rendering before any auto-slicing.
4. Add read-first `SLICES.yaml` support through `workflow slices` and graph rendering.
5. Add slice artifacts and fanout-from-slice readiness checks.
6. Add fold-back reconciliation for loop observations and testing-matrix updates.
7. Route code-structure questions through `workflow graph query` → `kg bridge` (implemented); keep extending `kg bridge` capabilities as CRG evolves.
8. Add delegation closeout so completed delegation and merge-back artifacts reconcile cleanly into task and plan state.
9. Add per-delegate prompt/context bundle inputs so fanout can hand sub-agents reproducible prompts and files.

### Phase 8: Delegation bundle direction

Phase 8 should formalize delegation handoff as a three-layer model rather than treating one giant prompt as the interface.

#### D5 — `--project-overlay` vs `--prompt` / `--prompt-file` (do not collapse)

**Resolution (locked in `decisions.1.md` for this plan):** `workflow fanout` maps these to **different** parts of the delegation bundle:

| Input | Bundle path | Role |
|------|-------------|------|
| `--project-overlay` | `worker.project_overlay_files` | How this role runs in the repo (AGENT.md-like, durable) |
| `--prompt` (repeatable) | `prompt.inline` | What to do for **this** delegation (runtime) |
| `--prompt-file` (repeatable) | `prompt.prompt_files` | File-backed per-delegation prompt, still distinct from the overlay file |

`bin/tests/ralph-orchestrate` must **not** pass the same file as both `--project-overlay` and `--prompt-file`. Staged auto-fanout defaults to a stage-neutral inline `--prompt` and no implicit project overlay; callers may provide a stage-safe `RALPH_PROJECT_OVERLAY` or `RALPH_DELEGATION_PROMPT_FILE` when one has been materialized. **Role-aware staged dispatch** picks the right stage-safe overlay and prompt file for **impl** vs **verifier** vs **review** (see *Repo prompt files — do not collapse roles* below); the orchestrator uses `.agents/active/orchestrator.loop.md`, legacy no-stage `loop-worker` loads `active.loop.md` directly, and typed stages must not inherit that full-slice file.

#### 1. Shared stage instructions and legacy loop-worker profile

Reusable dispatch-time guidance, using `~/.agents/profiles/loop-worker.md`
as donor material until native stage agents are materialized:

- bounded worker discipline: honor `write_scope`, trust canonical task state, avoid mutating shared workflow state directly
- verification discipline: run focused tests first, then broader regression only as justified
- trace discipline: record a concrete `feedback_goal`, use scenario tags, and classify evidence/results
- staged-boundary discipline: child stages emit their assigned typed artifact
  and stop; the parent gate owns delegation closeout and canonical
  reconciliation
- legacy full-slice `loop-worker` closeout discipline only:
  - worker records `workflow verify record`
  - worker records `workflow checkpoint`
  - worker returns `workflow merge-back`
  - parent runs delegation closeout / archive once accepted; that command
    reconciles canonical task state

The shared stage instruction base should contain only stable bounded-stage
discipline. It is **not** an `app_type` profile: profiles in
`.agents/workflow/specs/app-type-profiles/design.md` are versioned pipeline
configuration selecting verifier chains, review kinds, and graph backends.
Per `.agents/proposals/agent-context-resolution-architecture.md`, the parent
orchestrator owns resolving and injecting stage instructions and project
overlays at dispatch. The compatibility `loop-worker` profile may retain
full-slice closeout behavior, but staged `impl`, verifier, and reviewer agents
must not inherit `/iteration-close` or merge-back duties from it.

#### 2. Project overlay

Repo-local guidance layered on top of the dispatched stage instructions:

- plan and loop-state locations
- preferred verification surfaces
- quality gates and hook expectations
- regression matrix path
- higher-layer validation queue path
- project-specific scenario families and verification heuristics

The recovered `.agents/active/active.loop.md` is still a legacy/full-slice
overlay because it includes iteration-close procedure. Only a trimmed,
stage-safe derivative of that content belongs in typed staged agents.

**Repo prompt files — do not collapse roles**

- **`loop-worker` role** (legacy full-slice worker that runs tests, `workflow verify record`, `workflow checkpoint`, and `workflow merge-back`): optional repo prompt file is a *worker* overlay (e.g. `.agents/prompts/loop-worker.project.md` if your repo adds one). Same three-layer stack: legacy global profile → project overlay → bundle.
- **`impl-agent` role** (implementation slice only; hands off to verifiers): use **`.agents/prompts/impl-agent.project.md`** as the repo-owned impl surface. It combines with the dispatch-injected shared stage instruction base, not legacy `loop-worker` closeout instructions; it adds repo wording for implementation + **`impl-handoff.yaml`** emission.
- **`unit` verifier role** (Go test verification only): use **`.agents/prompts/verifiers/unit.project.md`** as the repo-owned unit surface. It consumes **`impl-handoff.yaml`**, runs **scoped** `go test` over packages implied by `write_scope_touched`, then the **full** Go suite per **D12** (`go test ./... -race -count=1 -timeout=300s`), and writes **`.agents/active/verification/<task_id>/unit.result.yaml`** with `verifier_type: unit` and the canonical fields in **`schemas/verification-result.schema.json`**. Scoped-first discipline matches **D12** (parallel verifier isolation): verifiers do not broaden packages beyond what `write_scope_touched` justifies until after that slice is green.
- **`api` verifier role** (HTTP / contract / API-style verification): use **`.agents/prompts/verifiers/api.project.md`** as the repo-owned API surface. It consumes **`impl-handoff.yaml`**, runs **scoped** checks first (handlers, `httptest`, or targeted HTTP calls tied to `write_scope_touched`), then optional broader **contract** (OpenAPI / golden response / schema diff) and **performance** artifact passes when the plan calls for them, and writes **`.agents/active/verification/<task_id>/api.result.yaml`** with `verifier_type: api` and the canonical fields in **`schemas/verification-result.schema.json`**. When Playwright or similar drives **network-visible** behavior, treat captured HAR / traces / HTML reports as **`artifact_paths`** and keep the same scoped-first rule: do not broaden to unrelated routes or full suites until the slice covering `write_scope_touched` is green.
- **`ui-e2e` verifier role** (browser / DOM / visual / accessibility verification): use **`.agents/prompts/verifiers/ui-e2e.project.md`** as the repo-owned UI surface. It consumes **`impl-handoff.yaml`**, runs **scoped** browser flows first (pages, components, or tagged suites implied by `write_scope_touched`), then optional broader **visual regression** (screenshots, image diffs, snapshot reports), **accessibility** audits (for example axe output with a configured WCAG level), and **cross-browser** or full navigation suites when the plan calls for them, and writes **`.agents/active/verification/<task_id>/ui-e2e.result.yaml`** with `verifier_type: ui-e2e` and the canonical fields in **`schemas/verification-result.schema.json`**. Prefer the **`api` verifier** prompt when the primary evidence is **HTTP-level** (HAR, intercepted responses); prefer **`ui-e2e`** when the primary evidence is **DOM, layout, screenshots, keyboard interaction, or a11y violations**. Scoped-first discipline matches other verifier roles: do not broaden to unrelated suites until the slice covering `write_scope_touched` is green.
- **`batch` verifier role** (fixture-driven, golden, or multi-record batch verification): use **`.agents/prompts/verifiers/batch.project.md`** as the repo-owned batch surface. It consumes **`impl-handoff.yaml`**, runs **scoped** batch or fixture jobs first (golden directories, CSV/JSON fixture trees, snapshot or CLI **expected-vs-actual** diffs, schema validation batches, or matrix rows tied to `write_scope_touched`), then optional broader **full fixture passes** or **volume/regression matrix** tiers when the plan calls for them, and writes **`.agents/active/verification/<task_id>/batch.result.yaml`** with `verifier_type: batch` and the canonical fields in **`schemas/verification-result.schema.json`**. Prefer the **`unit` verifier** prompt when the primary evidence is **`go test`** and in-process Go coverage; prefer **`batch`** when the primary evidence is **file-backed or multi-record comparisons** (diff artifacts, golden updates, tabular mismatch reports). Scoped-first discipline matches other verifier roles: do not broaden to unrelated datasets or jobs until the slice covering `write_scope_touched` is green.
- **`streaming` verifier role** (SSE, WebSocket, or other long-lived / event-ordered verification): use **`.agents/prompts/verifiers/streaming.project.md`** as the repo-owned streaming surface. It consumes **`impl-handoff.yaml`**, runs **scoped** stream checks first (one SSE resource, one WS channel, or tagged stream scenarios tied to `write_scope_touched`), then optional broader **soak**, **fault-injection**, or **multi-session** tiers when the plan calls for them, and writes **`.agents/active/verification/<task_id>/streaming.result.yaml`** with `verifier_type: streaming` and the canonical fields in **`schemas/verification-result.schema.json`**. Evidence should capture **behavior over time**: event ordering, heartbeats, **timeouts**, **backpressure** or slow-consumer behavior, **dropped or duplicated** frames when the contract allows, reconnect semantics, and **artifact_paths** pointing to transcripts, frame logs, HAR excerpts, or trace archives. Prefer the **`api` verifier** prompt when the primary evidence is **finite HTTP** responses; prefer **`streaming`** when the primary evidence is **duplex or incremental** delivery. Scoped-first discipline matches other verifier roles: do not broaden to unrelated feeds or full-cluster soak until the slice covering `write_scope_touched` is green.
- **`review` role** (current compatibility staged decision/return-artifact producer after verifiers): use **`.agents/prompts/review-agent.project.md`** as the repo-owned review surface. It reads verifier **`*.result.yaml`** files (and **`impl-handoff.yaml`** when relevant) and performs a **two-lens review**: **phase 1** is the broad product/domain/stability pass ("is the slice moving in the right direction and stable enough for the delegated scope?"), while **phase 2** is the tech-lead / architecture pass ("does the implementation respect repo standards, contracts, and architectural intent?"). Until named reviewer stages and a return/aggregation gate land, the consolidated reviewer records **`da workflow verify record --kind review ...`** so the CLI writes **`.agents/active/verification/<task_id>/review-decision.yaml`**, then writes **`.agents/active/merge-back/<task_id>.md`** as its return artifact for the parent gate. It does not run delegation closeout or advancement. In the target model, each named reviewer writes only typed evidence and the parent-invoked return gate writes the consolidated decision and merge-back packet. Prefer **flags** over hand-editing YAML so **escalation** always carries **`--escalation-reason`** when the consolidated outcome is **escalate**.

**`I_S_P` (Interactive Staged Pipeline)** is the interactive/manual counterpart to the scripted staged pipeline: it should follow the same task-scoped chain (`impl -> verifier(s) -> review -> parent gate`), but allow a human or interactive agent to drive the stages stepwise instead of relying on one fully scripted pass. `I_S_P` is a conceptual workflow shape, not a promise that one specific script name or prompt file is permanently canonical. It should evolve alongside `ralph-pipeline` and future `da workflow` command ownership. When scripted control-plane semantics change, `I_S_P` must be updated in the same wave unless the spec records an intentional divergence; review-gate, retry/fallback, pause, and hard-stop behavior should not silently drift between the interactive and scripted staged runtimes. Resumable terminal stage failures (for example provider usage limits) should restart a fresh session for the failed stage only, prefer fallback role-specific bins before generic fallbacks, and preserve already-completed earlier stage artifacts; non-resumable hard stops should remain visible and block silent continuation.

**Legacy loop-worker mode (`bin/tests/ralph-worker` without `--stage`)** remains the old single-worker fallback: it loads the `loop-worker` skill, inlines the project overlay, and expects iteration-close / merge-back. It does **not** load `impl-agent.project.md`. Keep it available for backward compatibility, but do not treat it as the target staged runtime model.

#### 3. Delegation bundle

Per-delegation persisted payload at `.agents/active/delegation-bundles/<delegation_id>.yaml` (written by `workflow fanout` with Phase 8 flags — see **Delegation bundle workflow** under *Canonical Artifact Direction*):

- chosen plan/task/slice and selection reason
- owner plus worker profile reference
- project overlay file references
- prompt text and prompt files
- context files
- verification plan, feedback goal, scenario tags, and closeout expectations

This bundle is the transport/persistence layer for a specific delegation, not the definition of the worker itself.

### Phase 8: Reusable staged testing additions

The dispatch-injected shared stage instruction base should carry six reusable
testing/verification additions that are not repo-specific:

1. `feedback_goal` — every delegated iteration states the concrete question the evidence run must answer.
2. `scenario_tags` with stable coverage families and paired-state guidance.
3. `regression_matrix` support — a repo may point at one or more durable matrix artifacts for scenario/run-variant tracking.
4. `higher_layer_validation_queue` support — queue features that are code-complete and automated-check complete but still deserve manual/live validation.
5. evidence/result `classification` taxonomy such as `ok`, `ok-empty`, `ok-warning`, `retry-recovered`, `impl-bug`, `tool-bug`, `missing-feature`, and `blocked`.
6. `sandbox_policy` for destructive or stateful verification, so a worker can prove mutating behavior without touching the user's live home/project state.

Shared staged behavior should also require negative-path coverage
whenever the delegated change introduces new failure modes. The legacy
`loop-worker` profile can consume the same discipline without becoming the
instruction base for staged children.

### Phase 8: Impl-handoff contract (impl-agent → verifiers)

The **impl-agent** role writes a small YAML handoff beside verification artifacts so gates do not depend on chat logs:

**Path:** `.agents/active/verification/<task_id>/impl-handoff.yaml`

| Field | Type | Purpose |
|-------|------|---------|
| `task_id` | string | Canonical task id (matches delegation bundle) |
| `commit_sha` | string | Commit the verifier should evaluate |
| `write_scope_touched` | string[] | Paths actually modified; drives scoped tests and TDD-fresh gates |
| `ready_for_verification` | boolean | `true` when implementation is complete and the tree is ready for verifier entry |
| `tests_unchanged_justified` | boolean (optional) | When `true`, documents that tests were intentionally not changed under `write_scope_touched` (e.g. doc-only work); omit or `false` when tests were added/updated |
| `impl_notes` | string | Short cold-start context for verifiers |

Pre-verifier policy can require: either a test file touch under `write_scope_touched`, or `tests_unchanged_justified: true` with an allowed reason. Repo wording for the impl role lives in **`.agents/prompts/impl-agent.project.md`**; shared staged behavior belongs in dispatch-injected stage instructions. The `loop-worker` profile and skill remain compatibility surfaces for full-slice execution only.

### Phase 8: Canonical artifact and schema

Use a sibling artifact rather than overloading the core delegation contract:

- contract file: `.agents/active/delegation/<parent_task_id>.yaml` (unique delegation id is the contract’s `id` field)
- bundle file: `.agents/active/delegation-bundles/<delegation_id>.yaml` (`delegation_id` should match the contract `id`)
- schema: `schemas/workflow-delegation-bundle.schema.json`

The schema should be embedded into the binary alongside the other repo-local schemas so later runtime validation can bind directly to the canonical artifact contract.

### Phase 8: Legacy full-slice candidate command shape

```bash
da workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --owner <delegate-name> \
  --write-scope "commands/,internal/platform/" \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "Does fold-back create/list persist small and proposal routes cleanly?" \
  --scenario-tag canonical-plan-present \
  --scenario-tag workflow-fold-back-small \
  --regression-artifact .agents/workflow/testing-matrix.yaml \
  --validation-queue .agents/active/live-testing-queue.md \
  --prompt "Implement Phase 6 only; keep write-scope tight" \
  --prompt-file .agents/prompts/loop-worker.project.md \
  --context-file .agents/workflow/plans/loop-orchestrator-layer/TASKS.yaml \
  --context-file docs/LOOP_ORCHESTRATION_SPEC.md
```

### Phase 8: Legacy full-slice bundle example

```yaml
schema_version: 1
delegation_id: del-phase-6-20260412T213000Z
plan_id: loop-orchestrator-layer
task_id: phase-6-fold-back-reconciliation
slice_id: ""
owner: worker-a

worker:
  profile: loop-worker
  profile_version: 1
  project_overlay_files:
    - .agents/active/active.loop.md

selection:
  selected_by: orchestrator-session-start
  selected_at: "2026-04-12T21:30:00Z"
  reason: "first pending unblocked canonical task"

scope:
  write_scope:
    - commands/workflow.go
    - commands/workflow_test.go
  constraints:
    - "Do not mutate shared workflow state outside the delegated task"

prompt:
  inline:
    - "Implement only the selected task."
  prompt_files:
    - .agents/prompts/loop-worker.project.md
    # impl-only: use .agents/prompts/impl-agent.project.md instead; do not load impl-agent for legacy loop-worker mode (ralph-worker without --stage)

context:
  required_files:
    - .agents/workflow/plans/loop-orchestrator-layer/PLAN.yaml
    - .agents/workflow/plans/loop-orchestrator-layer/TASKS.yaml
  optional_files:
    - .agents/active/loop-state.md
    - docs/LOOP_ORCHESTRATION_SPEC.md

verification:
  feedback_goal: "Does fold-back create/list persist small and proposal routes cleanly?"
  scenario_tags:
    - canonical-plan-present
    - workflow-fold-back-small
  regression_artifacts:
    - .agents/workflow/testing-matrix.yaml
  higher_layer_validation_queue: .agents/active/live-testing-queue.md
  focused_commands:
    - go test ./commands
  regression_commands:
    - go test ./...
  evidence_policy:
    require_negative_coverage: true
    sandbox_mutations: true        # also enables adversarial-lens active probing
    verifier_chain_max: 3          # verifier retry budget (renamed from primary_chain_max)
    lens_chain_max: 2              # per-lens review retry budget

closeout:
  worker_must:
    - workflow_verify_record
    - workflow_checkpoint
    - workflow_merge_back
  parent_must:
    - workflow_delegation_closeout
```

### Phase 8: Rules

- dispatch-injected shared stage instructions stay reusable; project overlays and delegation bundles must not fork that behavior ad hoc
- the legacy `loop-worker` profile is not injected into staged child agents because its closeout sequence would violate stage stop conditions
- prompt/context inputs must be delegation-specific so different sub-agents can receive different bundles
- repeatable flags are preferable to comma-separated prompt/context strings
- the bundle must be inspectable after fanout so the handoff can be reproduced and audited
- the worker should read from the persisted bundle rather than reconstructing context from memory
- regression matrix and validation queue references are optional at the schema level but should be supported consistently where a repo uses them
- negative-path coverage is required when the delegated change introduces new failure modes
- child artifact production and parent closeout responsibilities must remain distinct; accepted delegated work is completed by parent-run delegation closeout, without a second `workflow advance`; orphaned merge-backs without a contract must fail parent gating

### Phase 8: Acceptance shape

- a parent can resolve an app-type pipeline profile separately from stable stage instructions and repo-local project overlays
- a parent can supply inline prompts, prompt files, and multiple context files
- a delegated worker receives reproducible verification metadata, not just prose instructions
- two different delegated sub-agents can receive different prompt/context/testing bundles without colliding
- the resulting bundle is inspectable from repo artifacts and backed by an embedded schema
