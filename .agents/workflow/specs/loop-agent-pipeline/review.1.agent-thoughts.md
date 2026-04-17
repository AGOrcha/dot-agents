---
agent_type: claude
agent_session_id: c37d1d2d-f403-4e8b-9c37-07f02852ce38 
---
# Review: loop-agent-pipeline plan
---
What's already done (from loop-runtime-refactor)                                                                                                                              
                                                                                                                                                                            
The infrastructure this plan builds on is almost entirely shipped:                                                                                                            
- ralph-orchestrate, ralph-cursor-loop, ralph-closeout, ralph-pipeline — all exist in bin/tests/                                                                              
- loop-worker AGENT.md + SKILL.md — exists in .agents/skills/loop-worker/                                                                                                     
- workflow checkpoint --log-to-iter, iter-N.yaml schema, overlay split — all done                                                                                             
- verifier_profiles and app_type_verifier_map fields — not in agentsrc schema yet (see below)
---
## Missed code commands
1. workflow merge-back is orphaned in the new impl-agent flow
The impl-agent is stripped of /iteration-close, so it never calls workflow merge-back. But ralph-closeout scans .agents/active/merge-back/*.md as its trigger. Nothing in the plan specifies who creates that artifact now. Either:
- The impl-agent calls workflow merge-back independently (without checkpoint/verify), or
- ralph-closeout needs to change its trigger to scan review-decision.yaml with decision: accept instead.

This is a breaking change to ralph-closeout.sh that the plan doesn't call out.
2. workflow verify record has no owner in the new pipeline
Currently /iteration-close → workflow verify record → audit trail. If impl-agent skips iteration-close, verify record never runs. The plan moves evidence to *.result.yaml files but doesn't say whether the review-agent calls workflow verify record after writing review-decision.yaml. The audit trail gap should be explicit.
3. workflow checkpoint --log-to-iter has no owner either
Who creates the iter-N.yaml stub for impl-agent iterations? The plan is silent. If impl-agent doesn't call it, the two-author protocol
(Phase 5 of loop-runtime-refactor) is bypassed for the new pipeline.                  

4. workflow fanout has no --verifier-sequence flag                              

Phase 5 (p5-fanout-dispatch) says fanout resolves app_type → verifier_sequence and writes it into the bundle. But the current workflow
fanout CLI has no such flag, and workflow-delegation-bundle.schema.json has no verifier_sequence field. This is a CLI change that needs to
be in the task's write_scope:
write_scope:
- commands/workflow.go          # add --verifier-sequence flag to fanout
- schemas/workflow-delegation-bundle.schema.json
- schemas/workflow-plan.schema.json  # add app_type field

None of these three schema files are listed in p5's write_scope.

5. ralph-pipeline outer loop break condition is underspecified                        

Phase 1 shows break if no pending tasks remain (workflow orient clean) but doesn't give the exact check. The reliable command is:
`da --json workflow tasks "$RALPH_RUN_PLAN" | python3 -c "import json,sys; t=json.load(sys.stdin); sys.exit(0 if not any(x.get('status')=='pending' for x in t) else 1)"`
`workflow orient` output is narrative text — parsing "clean" from it is fragile.

6. workflow fold-back create in Phase 6 (post-closeout) has no concrete commands

The plan describes what the post-closeout orchestrator decides but doesn't give the CLI calls. Each reasoning path needs a command:
Coverage regression:
    `da workflow fold-back create --plan "$plan_id" --observation "coverage regression: $(diff baseline unit.result.yaml)" --propose`

Schema drift: 
    `da workflow fold-back create --plan "$plan_id" --observation "schema drift: *.schema.json changed, blocking tasks need re-evaluation" --propose`

Without these, the Phase 6 script is a narrative wrapper with no actionable output.

7. dot-agents refresh doesn't support registry source type yet                        
Phase 7 adds "type": "registry" to sources. The current dot-agents refresh only handles local and git. This is a CLI change. The plan should
note commands/ as write_scope for p7.

---                                       
## Schema gaps (all missed from write_scope)

┌─────────────────────┬────────────────────────────────────────────────────────────────────────────────────────────────────┐
│      Plan task      │                                       Missing schema update                                        │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ p5-fanout-dispatch  │ schemas/workflow-delegation-bundle.schema.json — add verifier_sequence                             │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ p5-fanout-dispatch  │ schemas/workflow-plan.schema.json — add app_type                                                   │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ p5-fanout-dispatch  │ schemas/agentsrc.schema.json — add verifier_profiles, app_type_verifier_map                        │
├─────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ p7-external-sources │ schemas/agentsrc.schema.json — add packages (in git source), source_priority, registry source type │
└─────────────────────┴────────────────────────────────────────────────────────────────────────────────────────────────────┘

The pattern from loop-runtime-refactor is: schema changes go in the same task as the feature (see phase-5d for iter-log schema). Follow
that.                                     

---                                       
Task dependency gap                       

p5-fanout-dispatch lists depends_on: [p3b, p3c, p4] but p3a (verification-result schema) is the contract that p5 writes into the bundle. Add
p3a to p5's depends_on.                  

---                                       
## Workflow notes worth adding               

impl-agent vs loop-worker identity conflict                                           

The plan says "only AGENT.md changes" for the impl-agent. But loop-worker's AGENT.md is what Pattern E orchestrators currently call via
Agent(subagent_type='loop-worker'). If you strip iteration-close from AGENT.md, any existing orchestrator call via Pattern E gets a worker
that never closes out.                    

The clean solution: create `.agents/skills/impl-agent/` as a separate skill with its own AGENT.md. loop-worker AGENT.md stays untouched.
Pattern E orchestrators choose which to call based on whether the pipeline runs verification separately.

### Parallel verifier isolation — unit verifier                                           

The unit verifier runs `go test ./... -race`, which covers the entire codebase. If two workers modified overlapping packages and their
verifiers run in parallel, results are non-deterministic. Add a note to the unit verifier AGENT.md: unit verifiers for the same plan must be
serialized, or each verifier should run only the packages touched by its write_scope_touched from the impl-handoff.

### Verification directory lifecycle

The plan defines `.agents/active/verification/<task_id>/` as the artifact directory but never says:                                                                             
- Who creates it (impl-agent? ralph-pipeline before spawning verifiers?)
- When it's cleaned up (after delegation closeout? during archive step?)
ralph-closeout should archive it to `.agents/history/` alongside the merge-back, and then delete it.

### RALPH_RUN_PLAN scoping 
discover_unblocked_tasks() in ralph-orchestrate currently iterates all active plans. With RALPH_RUN_PLAN=<id>, it should filter to only that
plan. Add this to Phase 1's notes:
`[[ -n "${RALPH_RUN_PLAN:-}" ]] && plan_ids="$RALPH_RUN_PLAN" `

### Playwright for parallel api/ui_e2e verifiers

Phase 3c/3d AGENT.md profiles need:\
`playwright install --with-deps chromium  # one-time setup check`\
And per-verifier port assignment to avoid conflicts when multiple api verifiers run in parallel. Add PLAYWRIGHT_BASE_PORT env var convention
to verifier AGENT.md.

### go test flag in unit verifier

The plan's table shows -race but the AGENT.md profile for the unit verifier should explicitly list:
`go test ./... -race -count=1 -timeout=300s`
-count=1 disables caching so verifier results are always fresh per run.

---
## Summary

The plan is solid architecturally. The main execution risks:

1. merge-back artifact ownership — decide now: impl-agent calls `workflow merge-back` directly, or ralph-closeout pivots to
review-decision.yaml. Both work, but it must be explicit.                      
2. Schema write_scope — p5 and p7 each need 2-3 schema files added to their write_scope or they'll produce schema-invalid agentsrc/bundle
files.
3. impl-agent separate skill — don't modify loop-worker AGENT.md in place; create impl-agent as its own skill to avoid breaking existing
Pattern E callers.