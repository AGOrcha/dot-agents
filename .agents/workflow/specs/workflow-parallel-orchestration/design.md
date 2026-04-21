# Workflow Parallel Orchestration Surface — Design Spec

**Status:** active  
**Written:** 2026-04-20  
**Plan:** workflow-parallel-orchestration  
**Related:** plan-archive-command spec §3, isp-prompt-orchestrator.plan.md, orchestrator.loop.md

---

## 1. Problem Statement

The orchestrator pipeline (ISP and ralph) is intended to fan out multiple independent tasks in
parallel when eligible — but in practice it serializes to one task at a time. The root cause is
structural: every command surface that informs orchestrator decisions returns at most one task.

**Specific gaps:**

1. **`workflow next` is single-task by design.** It returns the single highest-priority
   unblocked task across active plans. There is no way to ask "give me everything unblocked
   right now" across the full active plan set.

2. **No cross-plan eligibility view.** When the orchestrator manages `plan-A` and `plan-B`
   simultaneously, tasks from both may be unblocked at the same time. `workflow next` surfaces
   only the winner. The others are invisible until that task completes.

3. **`max_parallel_workers` is referenced in the ISP prompt but undefined as data.** The prompt
   says "up to the parallel worker limit" with no command, flag, or field to look it up. The
   orchestrator has no ceiling to reason about.

4. **Write-scope conflict detection doesn't exist.** Even if the orchestrator could see multiple
   eligible tasks, it has no way to determine which ones can safely run in parallel (no
   overlapping write scopes) without doing ad hoc analysis inside its own context.

5. **`workflow complete` only reports `actionable | locked | paused | drained`.** "Actionable"
   means at least one task is available — not how many, not which ones, not whether parallel
   fanout is possible.

The ISP prompt attempts to work around this with instructions like "select every non-overlapping
task... up to the parallel worker limit" but provides no mechanism for the agent to actually
discover what those tasks are or what the limit is. The instruction exists; the data does not.

---

## 2. Goals

1. Give orchestrators a single command that returns all currently eligible tasks across the
   active (scoped) plan set, with write-scope conflict annotations and a pre-computed max
   parallel batch.
2. Define `max_parallel_workers` as a concrete, readable workflow preference.
3. Keep `workflow next` unchanged — it remains the right surface for human-readable
   single-task queries and serialized completion mode.
4. Update the ISP prompt to use the new surface for parallel fanout decisions.

---

## 3. Decisions

### 3.1 New command: `workflow eligible`

**Decision:** add `workflow eligible` as a top-level workflow subcommand (not nested under
`plan`). It is the machine-facing counterpart to `workflow next`.

```
dot-agents workflow eligible
dot-agents workflow eligible --plan plan-archive-command,workflow-parallel-orchestration
dot-agents workflow eligible --plan plan-archive-command --limit 3
dot-agents --json workflow eligible --plan plan-archive-command
```

**Rationale:** `eligible` is a session-level, cross-plan operation. Nesting it under `plan`
would suggest it's scoped to one plan's static structure, which it is not. It belongs beside
`next`, `complete`, and `health` as a runtime control-plane read.

### 3.2 `workflow next` is unchanged

**Decision:** `workflow next` continues to return exactly one task — the highest-priority
unblocked task. `selectNextCanonicalTask` becomes a thin wrapper over the new
`selectAllEligibleTasks` function (returns `[0]`). No behavior change.

**Rationale:** `next` is used in many places (orchestrator startup, human "what now?" queries,
`workflow complete` state derivation). Changing its shape would break all of them. `eligible`
is additive.

### 3.3 Eligibility criteria (matches `workflow next` exactly)

A task is eligible for `workflow eligible` if and only if it would be eligible for `workflow
next`:
- Plan `status == "active"`
- Task `status == "pending"` or `"in_progress"`
- No incomplete dependencies (all `depends_on` tasks are `completed` or `cancelled`)
- No active delegation lock on this task

This ensures `eligible` and `next` agree on what's actionable. The only difference is `next`
picks one; `eligible` returns all.

### 3.4 Write-scope conflict detection

**Decision:** `eligible` annotates each task with `conflicts_with: []string` (task IDs from
the same eligible set whose write scope overlaps). It also returns `max_batch: []string` — the
largest subset of eligible tasks with zero pairwise conflicts, computed greedily ordered by
task priority.

**Conflict rule:** two tasks conflict if any path in one task's `write_scope` is a prefix of
or equal to any path in the other task's `write_scope` (directory-aware).

Examples:
- `commands/workflow/plan_task.go` and `commands/workflow/fs.go` → no conflict (distinct files)
- `commands/workflow/` and `commands/workflow/plan_task.go` → conflict (directory contains file)
- `commands/workflow/` and `commands/workflow/` → conflict (same directory)
- `.agents/prompts/isp.prompt.md` and `commands/workflow/prefs.go` → no conflict

**Rationale for greedy `max_batch`:** optimal maximum independent set is NP-hard on general
graphs. In practice the conflict graph for workflow tasks is sparse (most tasks touch distinct
areas), so greedy is sufficient and predictable.

### 3.5 `max_parallel_workers` as a workflow preference

**Decision:** add `max_parallel_workers` as a valid key in the workflow prefs system.

- Type: integer string, range 1–8. Default: `"1"` (safe — serialized behavior until explicitly
  raised).
- Set per-project: `workflow prefs set-local max_parallel_workers 3`
- Set shared: `workflow prefs set-shared max_parallel_workers 2`
- `workflow eligible` uses this as the default `--limit` when no flag given.
- `--limit N` on `workflow eligible` overrides the pref for that call only.

**Rationale for default 1:** existing orchestrator behavior is serialized. Changing the default
to >1 without updating the ISP prompt and testing multi-bundle fanout would be unsafe. Opt-in
explicitly.

### 3.6 `workflow next` as a `selectAllEligibleTasks` wrapper

**Decision:** refactor `selectNextCanonicalTask` to call `selectAllEligibleTasks` and return
`results[0]` (after priority sort). `selectAllEligibleTasks` owns the eligibility logic;
`selectNextCanonicalTask` owns only the priority selection. This eliminates the current
duplication between the two functions.

### 3.7 ISP prompt parallel fanout mode trigger

**Decision:** parallel fanout mode is triggered when:
1. `workflow eligible --plan <scope>` returns `max_batch` length > 1, AND
2. No active delegation bundles currently exist for the scoped plan set

Condition 2 prevents the orchestrator from launching a second parallel wave before the first
wave's delegations have closed out. The orchestrator should wait for all active delegations to
resolve before re-querying eligible and launching the next wave.

**Rationale:** launching a new wave while prior delegations are open risks write-scope conflicts
even when the static graph says tasks are independent — a running worker may create intermediate
files outside its declared scope.

### 3.8 ISP skill conversion and orchestrator-session-start chaining

**Decision:** convert `.agents/prompts/isp.prompt.md` into a proper skill at
`.agents/skills/isp/`. Update `orchestrator-session-start` via `/skill-architect` to gather
eligible task data and chain to the ISP skill. Use `/skill-architect` for both skill updates.

**Rationale:** the ISP prompt is functionally a skill — it has a defined entry point, a step
sequence, and a completion contract. Converting it makes the session entrypoint explicit: the
user (or pipeline) invokes `orchestrator-session-start` with plan IDs; that skill handles all
orientation + data gathering then hands off to the ISP skill for decision + action. The user
no longer needs to paste the ISP prompt — pasting `orchestrator-session-start <plan-ids>` is
the full entrypoint.

**Orchestrator-session-start skill responsibilities (updated via `/skill-architect`):**
1. Accept plan IDs as input scope
2. Run `workflow eligible --json --plan <scope> --limit <max_parallel_workers>` — gather full
   eligible set with evidence scores (`has_evidence`, `evidence_confidence` per task)
3. Check active delegation state (any open bundles in the scoped plan set?)
4. Present orientation summary: eligible count, `max_batch` candidates, per-task evidence confidence
5. Chain to ISP skill, passing pre-gathered context — ISP skill does not re-query

**ISP skill responsibilities (converted from `.agents/prompts/isp.prompt.md`):**
- Step 1 (startup): simplified — orientation already done by `orchestrator-session-start`
- Step 2 (task selection): uses pre-gathered `eligible` output directly; `max_batch` is the
  parallel fanout candidate set; parallel mode trigger is already resolved (§3.7 — data present)
- Step 3 (direct vs fanout): unchanged
- Step 4 (fanout): in parallel mode, create one bundle per task in `max_batch`. For each task
  where `evidence_confidence` is `"medium"` or `"high"`, load sidecar `required_reads` and
  `decision_locks` into the bundle context. For `"low"` or `"none"`, note thin context in the
  bundle header. Each bundle runs its staged chain independently
- Step 5 (staged runtime): unchanged; subagent spawn discipline unchanged

**Scope:** Steps 2 and 4 of the ISP skill change behavior; Steps 1, 3, 5 carry over from
`isp.prompt.md` with minimal change. Ghost reference "parallel worker limit" removed before
conversion; replaced with `max_parallel_workers` pref via `workflow eligible` call.

---

## 4. Requirements

### 4.1 `workflow eligible` command surface

```
workflow eligible [--plan <id>[,<id>...]] [--limit N]
```

Output (human-readable):
```
  Eligible tasks (3 found, 2 can run in parallel — max_parallel_workers=2)

  plan-archive-command / p0-extract-fs-helpers  [pending]
    write_scope: commands/workflow/fs.go, commands/workflow/delegation.go
    conflicts_with: (none)

  plan-archive-command / p1-historybasedir-helper  [pending]
    write_scope: commands/workflow/state.go
    conflicts_with: (none)

  workflow-parallel-orchestration / p1-select-all-eligible  [pending]
    write_scope: commands/workflow/plan_task.go
    conflicts_with: (none)

  Max parallel batch: p0-extract-fs-helpers, p1-historybasedir-helper
    (p1-select-all-eligible excluded: limit=2)
```

Output (JSON `--json`):
```json
{
  "eligible_tasks": [
    { "plan_id": "...", "task_id": "...", "title": "...", "status": "...",
      "write_scope": [...], "conflicts_with": [...], "priority": 0,
      "has_evidence": true,
      "evidence_confidence": "medium" }
  ],
  "max_batch": ["p0-extract-fs-helpers", "p1-historybasedir-helper"],
  "max_parallel_workers": 2,
  "total_eligible": 3
}
```

`has_evidence` is a fast boolean: does `.agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml`
exist? Useful for quick filtering without parsing the sidecar.
`evidence_confidence` is `"none"` (no sidecar) | `"low"` | `"medium"` | `"high"` (from the
sidecar's `confidence` field). `"none"` and `has_evidence: false` always co-occur.

### 4.2 Conflict detection requirements

- Prefix match is directory-aware: `"commands/workflow"` conflicts with
  `"commands/workflow/plan_task.go"` but NOT with `"commands/workflow_test/"`.
  Implementation must use directory-component matching, not raw `strings.HasPrefix` — a
  naive prefix check would incorrectly flag `"commands/workflow"` as a prefix of
  `"commands/workflow_test/"`. Normalize by appending `/` before comparing.
- Empty `write_scope` on a task → no pairwise conflicts with any other task. The conflict
  rule is about declared paths; empty scope has no paths to conflict on.
- Conflict is symmetric: if A conflicts with B, B conflicts with A.
- **Empty write_scope caution flag (applies to code and non-code tasks equally):** when a
  task with empty `write_scope` appears in the eligible set, annotate it:
  - Human output: append `[no write_scope declared]` after the task row
  - JSON output: add `"write_scope_declared": false` field on the task object (default `true`)
  - Rationale: a task creating new files or changing skill/prompt/YAML artifacts with no
    declared scope is a scheduling risk — the orchestrator should prefer running it alone.
    The annotation is informational; it does not change conflict computation.

### 4.3 `selectAllEligibleTasks` function contract

- Same inputs as `selectNextCanonicalTask`: `projectPath string, explicitPlanID string`
- Returns `[]workflowNextTaskSuggestion` — reuses existing struct, no schema change
- Priority ordering: same rules as `selectNextCanonicalTask` (in_progress+focus > in_progress >
  focus+pending > pending)
- `selectNextCanonicalTask` becomes: `tasks, err := selectAllEligibleTasks(...); return tasks[0]`
- **Cross-plan dependency resolution:** a `depends_on` entry containing `/` is a cross-plan
  reference (`<plan-id>/<task-id>`). Resolution: split on first `/`, load
  `workflow/plans/<planID>/TASKS.yaml`, find the task by ID, check `status == "completed"`.
  A cross-plan dep that cannot be loaded (missing plan or task) is treated as unsatisfied —
  the dependent task is excluded from the eligible set and a warning is emitted.

### 4.4 Prefs requirements

- `max_parallel_workers` added to the valid key set in `prefs.go`
- `resolvePreferences()` returns it with default `"1"` when not set
- `workflow prefs` human output shows it alongside existing keys
- Integer validation: must parse as int in range [1, 8]; error otherwise

### 4.5 ISP skill + orchestrator-session-start skill requirements

- `orchestrator-session-start` redesigned via `/skill-architect`:
  - Accepts plan IDs as scope input
  - Calls `workflow eligible --json --plan <scope>` and captures full output including
    `has_evidence` and `evidence_confidence` per task
  - Checks active delegation state before chaining
  - Chains to ISP skill with pre-gathered context; ISP skill does not re-query
- `isp.prompt.md` converted to skill at `.agents/skills/isp/` via `/skill-architect`
- ISP skill Step 2 uses pre-gathered eligible output — `max_batch` is the fanout candidate set;
  parallel mode trigger already resolved by the time ISP skill is entered
- ISP skill Step 4 loads sidecar `required_reads` / `decision_locks` into bundle when
  `evidence_confidence` is `"medium"` or `"high"`; notes thin context for `"low"` / `"none"`
- Single-task path (`max_batch` length == 1) falls back to existing Step 4 behavior unchanged
- Ghost reference "parallel worker limit" removed before conversion; replaced with
  `max_parallel_workers` pref via the `workflow eligible` call in `orchestrator-session-start`

---

## 5. Out of Scope

- Changing `workflow next` output shape or behavior.
- Optimal (non-greedy) maximum independent set for `max_batch`.
- Cross-machine or cross-repo parallel orchestration.
- Automatic wave advancement (orchestrator waiting on all wave-N delegations before wave-N+1
  is still driven by the orchestrator prompt, not by a new command).
- Per-plan `max_parallel_workers` override (global pref is sufficient for now).
- Ralph pipeline integration (ralph uses its own shell-level parallelism; ISP is the target).

---

## 6. Open Questions

**Q1. Should `workflow complete` surface eligible count?**
Today `workflow complete --plan X` returns `state: actionable` when at least one task is
available. Should it also return `eligible_count: N` and `max_batch_size: M`? Would give the
orchestrator a lightweight probe before calling `eligible`.
_Lean yes, low cost. Defer until eligible is implemented and we see if the probe matters._

**Q2. What is the right default for `max_parallel_workers`?**
Default 1 is safe but means no parallel fanout until explicitly opted in. Default 2 would make
parallel fanout the norm for repos where eligible returns multiple tasks.
_Stay at 1 for now. Raise after ISP prompt is updated and tested._

**Q3. Should empty `write_scope` tasks conflict with everything or nothing?**
_Resolved (see §4.2):_ empty scope → no pairwise conflicts. Annotate with
`[no write_scope declared]` / `write_scope_declared: false` in output so the orchestrator
can choose to serialize the task. Applies equally to code and non-code tasks (skill files,
prompts, YAML artifacts). Enforcement at check-scope time: if a task's write_scope is empty
but files were changed, check-scope exits 1 with a warning ("write_scope is empty but N
files were changed — declare intended paths in TASKS.yaml write_scope").

**Q4. Is greedy `max_batch` sufficient, or do we need priority-weighted selection?**
The greedy algorithm picks tasks in priority order and adds each if it doesn't conflict with
already-selected tasks. This may exclude a higher-value pair in favor of an earlier-selected
singleton. In practice, task count per wave is small (2–5) so this is unlikely to matter.
_Defer optimization until there's a concrete case where greedy produces a suboptimal batch._

---

## 7. Done Criteria

| Criterion | Verifiable by |
|---|---|
| `workflow eligible` returns 2 tasks for plan-archive-command fixture (p0+p1 both pending, no deps) | unit test |
| `workflow eligible --limit 1` returns max_batch of length 1 | unit test |
| Tasks with overlapping write scopes appear in each other's `conflicts_with` | unit test |
| `max_batch` contains no conflicting pairs | unit test |
| `workflow next` output unchanged after `selectAllEligibleTasks` refactor | unit test |
| `workflow prefs` shows `max_parallel_workers` key with default 1 | unit test |
| `workflow eligible` respects `max_parallel_workers` pref as default limit | unit test |
| ISP skill Step 2 uses pre-gathered `workflow eligible` output; no ghost "parallel worker limit" text | readback |
| `orchestrator-session-start` chains to ISP skill after gathering eligible + evidence data | readback |
| ISP skill conversion preserves all step semantics from `isp.prompt.md` except Step 1 (simplified) | readback |
| `plan-wave-picker` skill redesigned via `/skill-architect` to call `workflow eligible --json` and interpret evidence scores as readiness labels | readback |
| Empty `write_scope` tasks in eligible output show `[no write_scope declared]` in human output and `write_scope_declared: false` in JSON | unit test |
| `go test ./...` passes | CI |

---

## 8. Relationship to Other Work

- **plan-archive-command spec**: `workflow plan schedule` (per-plan wave view) is the
  design-time companion. Schedule shows static wave structure; eligible shows runtime state.
- **orchestrator.loop.md**: the "wave selection" section references `plan-wave-picker` skill
  for multi-plan priority. `workflow eligible` makes this deterministic — skill may be
  simplified or replaced once eligible is live.
- **isp-prompt-orchestrator.plan.md**: the update to `.agents/prompts/isp.prompt.md` is p5
  of this plan. That stale plan.md's goal is now captured here.
- **loop-agent-pipeline**: the broader staged orchestration system this builds on. `eligible`
  does not change stage sequencing — it only changes how the orchestrator selects which tasks
  to hand to the staged runtime in one pass.
- **planner-evidence-backed-write-scope**: `workflow eligible` surfaces `evidence_confidence`
  per task by reading the sidecar at `evidence/<task_id>.scope.yaml`. Until PE-WS
  `derive-scope-command` lands (and its prerequisite `kg-freshness-impl` from
  kg-command-surface-readiness), all tasks will show `evidence_confidence: "none"` and
  `has_evidence: false`. The ISP skill degrades gracefully (notes thin context in the bundle
  header). Operators deploying WPO immediately after it lands should expect all-`"none"`
  evidence fields until both PE-WS and kg-freshness-impl mature. This is not a WPO defect —
  it is the expected bootstrap state.
