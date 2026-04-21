# Workflow Parallel Orchestration — Canonical Plan

**Plan ID:** workflow-parallel-orchestration  
**Status:** active  
**Spec:** [design.md](../../specs/workflow-parallel-orchestration/design.md)

---

## Why this exists

The ISP orchestrator pipeline is intended to fan out multiple independent tasks in parallel but
in practice serializes to one task at a time. Root cause: `workflow next` returns exactly one
task by design; there is no way to ask "give me everything unblocked across all active plans."
The ISP prompt references a "parallel worker limit" that has no backing data. There is no
write-scope conflict detection to determine which tasks are safe to run simultaneously.

This plan closes all three gaps: a new `selectAllEligibleTasks` function (with `workflow next`
becoming a thin wrapper), a `workflow eligible` command that surfaces the full unblocked set
with conflict annotations and a pre-computed safe batch, a `max_parallel_workers` workflow pref,
and a redesign of the `orchestrator-session-start` skill and ISP prompt into a proper two-skill
pipeline that uses the new data surface.

---

## Key decisions and invariants (do not reopen without a fold-back)

1. **`workflow next` is unchanged.** It returns exactly one task. `selectNextCanonicalTask`
   becomes a wrapper over `selectAllEligibleTasks` returning `[0]`. All existing callers of
   `workflow next` continue to work without change.

2. **`workflow eligible` is additive, not a replacement.** It is the machine-facing counterpart
   to `workflow next` for orchestrator fanout decisions. Human "what now?" queries still use
   `workflow next`.

3. **Eligibility criteria are identical between next and eligible.** The two commands agree on
   what's actionable. The only difference is how many results they return.

4. **Greedy `max_batch` is sufficient.** Optimal maximum independent set is NP-hard. Task counts
   per wave are small (2–5). Greedy ordered by priority is predictable and correct enough.

5. **Default `max_parallel_workers` is 1.** Existing behavior is serialized. Opt-in explicitly
   by setting the pref. Do not change the default without testing multi-bundle fanout first.

6. **ISP prompt becomes a skill; `orchestrator-session-start` chains to it.** Both skill updates
   via `/skill-architect`. The user's session entrypoint is `orchestrator-session-start <plan-ids>`;
   that skill gathers eligible data and chains to the ISP skill for decision + action. No re-query
   inside the ISP skill.

7. **`plan-wave-picker` is an interpreter, not a computer.** After p7, the skill calls
   `workflow eligible --json`, reads `max_batch` (pre-computed in Go), and annotates each task
   with a readiness label from `evidence_confidence`. All deterministic logic stays in Go.

8. **Ralph pipeline is out of scope for this plan.** Ralph uses shell-level parallelism and does
   not consume ISP skills. The data produced by `workflow eligible` is useful to ralph, but
   ralph integration is a separate follow-on.

---

## Task sequence

```
p1-select-all-eligible
  └─► p2-conflict-detection
        └─► p3-eligible-cmd
              └─► p4-max-parallel-pref
                    └─► p5-isp-prompt-update
                          ├─► p6-tests
                          └─► p7-skill-architect-wave-picker
```

Linear chain — each task builds directly on the prior. p6 and p7 can run in parallel after p5.

---

## Out of scope

- Changing `workflow next` output shape or behavior
- Optimal (non-greedy) max independent set for `max_batch`
- Cross-machine or cross-repo parallel orchestration
- Automatic wave advancement (orchestrator-driven, not command-driven)
- Per-plan `max_parallel_workers` override
- Ralph pipeline integration

---

## Ralph Pipeline Notes

**Direct impact: medium. The data is highly relevant to ralph even though ISP is the target.**

### `workflow eligible` is useful to ralph today (but not wired)

Ralph currently has no cross-plan conflict detection. It launches tasks via shell job control
(`&` + `wait`) based on its own sequencing logic, with no awareness of write-scope overlaps
between concurrent tasks. After this plan, `workflow eligible --json` returns `max_batch` —
the pre-computed maximal non-conflicting task set. Ralph could call this command and use
`max_batch` to determine which tasks to run as parallel shell jobs, getting the same safety
guarantee ISP gets without any Go changes to ralph itself.

### `max_parallel_workers` pref is global — ralph should respect it

The pref lives in the workflow prefs system (shared + local layers). Ralph today has its own
concurrency ceiling (likely a hardcoded constant or env var). After this plan, the canonical
ceiling is `max_parallel_workers`. Ralph should read this pref via `workflow prefs get
max_parallel_workers` and use it as its job ceiling, giving operators a single knob for both
ISP and ralph concurrency.

### ISP skill conversion has no direct ralph impact

The `orchestrator-session-start` + ISP skill chain is entirely within the Claude Code / cursor
interactive pipeline. Ralph does not invoke skills or read the ISP prompt. No changes needed.

### Insight: ralph as a `workflow eligible` consumer

The cleanest path to giving ralph real parallel safety: add a ralph step that calls
`workflow eligible --json --plan <scope>` at the top of each session, parses `max_batch`, and
uses that as the parallel job list instead of ralph's current sequential or ad-hoc parallel
logic. The conflict detection guarantee (`max_batch` has zero pairwise write-scope conflicts)
would eliminate the class of ralph-induced file conflicts that currently require manual
intervention. This is a ralph-side change, not a workflow command change — it's achievable
immediately after `workflow eligible` lands without any additional plan work.
