---
Plan: loop-agent-pipeline

Core paradigm shift from loop-runtime-refactor:
- Loop-runtime-refactor: one worker, does everything
- This plan: role-pure agents, artifact-driven handoffs, app-type dispatch

Where the previous section's notes clash with the new model, the new model wins. Specifically: verification_type in TASKS.yaml becomes verifier_sequence in the delegation
bundle (resolved from app_type at fanout time), and the worker no longer owns verification — that's a separate verifier invocation.
agent_type: claude
agent_session_id: 3676124e-1602-4126-9648-38ac0d12fad4
---
## The Pipeline Stages

┌─ ralph-pipeline (plan-completion outer loop) ─────────────────┐
│                                                               │
│  while unblocked_tasks:                                       │
│    Phase 1 — Orchestrate   discover → fanout → bundle         │
│    Phase 2 — Implement     impl-agent (write_scope only)      │
│    Phase 3 — Verify        typed verifiers (parallel per type)│
│                             → *.result.yaml artifacts         │
│    Phase 4 — Review        review-agent (consumes artifacts)  │
│    Phase 5 — Closeout      advance + delegation closeout      │
│    Phase 6 — Post-closeout orchestrator reasoning (optional)  │
└───────────────────────────────────────────────────────────────┘

---
## Phases

### Phase 1 — Plan-completion loop in ralph-pipeline

Add `RALPH_RUN_PLAN=<id>` mode. The outer control flow becomes:
while true; do
```
orchestrate → get bundles
dispatch impl-agents per bundle (parallel, RALPH_MAX_PARALLEL_WORKERS)
dispatch verifier-agents per bundle (parallel within task, sequential types)
run review-agent per task
closeout accepted tasks
break if no pending tasks remain (workflow orient clean)
```
done

No clash with new model — this is purely control flow. The discover_unblocked_tasks() in ralph-orchestrate already works; ralph-pipeline just needs to call it in a loop.

---
### Phase 2 — impl-agent: pure implementation AGENT.md

Extract the current loop-worker AGENT.md into a strict no-verification profile. The impl agent:
- Reads the bundle
- Implements write_scope
- Commits
- Writes a handoff stub to `.agents/active/verification/<task_id>/impl-handoff.yaml`
- Does not run tests, does not call /iteration-close — that chain has moved

impl-handoff.yaml (produced by impl-agent)
task_id: phase-X
commit_sha: abc123
write_scope_touched: [commands/workflow.go, commands/workflow_test.go]
impl_notes: "Added validateWorkflowIterLogEntry, sync.Once compile path"
ready_for_verification: true

The loop-worker SKILL.md (for human `/loop-worker` invocation) stays as-is — only AGENT.md changes.

---
### Phase 3 — Verification result schema + typed verifiers

New file: schemas/verification-result.schema.json — the standard output contract every verifier writes: `.agents/active/verification/<task_id>/<verifier_type>.result.yaml`\
```yaml
verifier_type: api | unit | ui_e2e | batch | streaming
task_id: ...
status: pass | fail | partial
gate_passed: true | false
artifacts: [{type: test_report, path: ...}, ...]
metrics: {}          # type-specific, optional
notes: ...
```

Verifier AGENT.md profiles (one per type, lives in `.agents/skills/verifiers/<type>/AGENT.md`):

┌───────────┬──────────────────────────────────────────────────────────┬────────────────────────────────────────────┐
│ Verifier  │                       What it does                       │           Key metrics/artifacts            │
├───────────┼──────────────────────────────────────────────────────────┼────────────────────────────────────────────┤
│ unit      │ go test ./... -race, focused packages first              │ pass/fail count, coverage%                 │
├───────────┼──────────────────────────────────────────────────────────┼────────────────────────────────────────────┤
│ api       │ Playwright API suite, starts server, runs contract tests │ P50/P95/P99, error_rate, HAR trace         │
├───────────┼──────────────────────────────────────────────────────────┼────────────────────────────────────────────┤
│ ui_e2e    │ Playwright browser, screenshots, visual diff vs baseline │ screenshot diffs, accessibility report     │
├───────────┼──────────────────────────────────────────────────────────┼────────────────────────────────────────────┤
│ batch     │ runs pipeline on fixture input, diffs output             │ expected_vs_actual diff, determinism check │
├───────────┼──────────────────────────────────────────────────────────┼────────────────────────────────────────────┤
│ streaming │ SSE/WS contract, timeout, backpressure                   │ frame latency P99, dropped_frames          │
└───────────┴──────────────────────────────────────────────────────────┴────────────────────────────────────────────┘

Each verifier is cold-started with just: task_id, path to impl-handoff.yaml, its own gate_config. It writes `<type>.result.yaml` then exits. No knowledge of other verifiers.

---
### Phase 4 — Review agent

review-agent AGENT.md: cold-started with the path to `.agents/active/verification/<task_id>/`. Reads all `*.result.yaml` files, evaluates against gate configs, writes a structured decision: `.agents/active/verification/<task_id>/review-decision.yaml`
```yaml
task_id: ...
decision: accept | reject | escalate
failed_gates: [...]
escalation_reason: null | "P99 above threshold on api verifier"
reviewer_notes: "All 3 verifiers passed. UI screenshots within 2px delta."
```

The review agent never sees the implementation stream — only the structured result artifacts. This is the key separation from the old `RALPH_CLOSEOUT_AUTO=0` pattern which spawned an orchestrator to read implementation prose.

---
### Phase 5 — App-type dispatch in workflow fanout + .agentsrc.json

New field in PLAN.yaml: app_type: [go_cli, api, ui]

New section in .agentsrc.json (extends existing sources and agents fields):

```json
{
    "verifier_profiles": {
        "unit":      "agents/verifiers/unit",
        "api":       "agents/verifiers/api-sre",
        "ui_e2e":    "agents/verifiers/ui-e2e",
        "batch":     "agents/verifiers/batch",
        "streaming": "agents/verifiers/streaming"
    },
    "app_type_verifier_map": {
        "go_cli":   ["unit"],
        "api":      ["unit", "api"],
        "ui":       ["unit", "ui_e2e"],
        "batch":    ["unit", "batch"],
        "streaming":["unit", "streaming"]
    }
}
```
`workflow fanout` resolves app_type → verifier_sequence and writes it into the delegation bundle. Tasks can override with verifier_override: [unit, api] if they need a
non-standard set.

---
Phase 6 — Post-closeout orchestrator reasoning

The previous section's orchestrator reasoning list maps cleanly to a new ralph-post-closeout script triggered with RALPH_POST_CLOSEOUT_REVIEW=1. The orchestrator agent in
this pass handles:

1. Cross-task conflict detection — diff-level analysis when two parallel workers touched overlapping files
2. Coverage regression — compares unit.result.yaml coverage delta against baseline (catches "worker passed its own tests but regressed a sibling")
3. Schema drift — if any *.schema.json changed, re-evaluate blocking tasks
4. Fold-back triage — reads all pending fold-back proposals, decides which become tasks vs observations
5. Budget escalation — worker hit iteration cap; orchestrator decomposes or marks blocked

This is distinct from the Review agent (Phase 4), which is per-task and artifact-driven. Post-closeout is the orchestrator's cross-task reasoning pass.

---
Phase 7 — External profile sources in .agentsrc.json

The sources array already supports git. Extend to support named package selection within a source and version pinning:

{
"sources": [
    {"type": "local"},
    {
    "type": "git",
    "url": "git@github.com:myorg/agent-profiles.git",
    "ref": "main",
    "packages": ["sre-verifier", "playwright-api"]   // ← new: pull specific dirs only
    },
    {
    "type": "registry",                              // ← new type
    "url": "https://agents.example.com/v1",
    "packages": ["ui-verifier@1.2"]
    }
],
"source_priority": ["local", "git:myorg/agent-profiles", "registry"]  // ← new
}

dot-agents refresh fetches declared packages into ~/.agents/profiles/cache/<source_name>/ and links them. Conflict resolution follows source_priority order. This lets teams
share verifier profiles across repos without copying files.

---

Task Breakdown (TASKS.yaml order)

┌─────────────────────┬────────────────────────────────────────────────────────────────────────────┬──────────────┐
│         ID          │                                   Title                                    │  Depends on  │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p1-pipeline-loop    │ ralph-pipeline: RALPH_RUN_PLAN outer loop mode                             │ —            │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p2-impl-agent       │ impl-agent AGENT.md — strip verification from impl context                 │ —            │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p3a-result-schema   │ schemas/verification-result.schema.json                                    │ —            │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p3b-unit-verifier   │ unit verifier AGENT.md + result.yaml write                                 │ p3a          │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p3c-api-verifier    │ api verifier AGENT.md + Playwright + P95/P99 metrics                       │ p3a          │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p3d-ui-verifier     │ ui_e2e verifier AGENT.md + screenshot diff + a11y                          │ p3a          │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p3e-batch-verifier  │ batch verifier AGENT.md + fixture diff                                     │ p3a          │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p4-review-agent     │ review-agent AGENT.md consuming *.result.yaml                              │ p3a          │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p5-fanout-dispatch  │ app_type in PLAN.yaml + verifier_profiles in .agentsrc + fanout resolution │ p3b, p3c, p4 │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p6-post-closeout    │ ralph-post-closeout script + orchestrator reasoning pass                   │ p1           │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────┼──────────────┤
│ p7-external-sources │ .agentsrc registry type + packages filter + source_priority                │ —            │
└─────────────────────┴────────────────────────────────────────────────────────────────────────────┴──────────────┘

p1, p2, p3a, p7 are independent and can fan out in parallel immediately. p3b–p3e and p4 unblock after p3a. p5 is the integration point that wires everything together.

---