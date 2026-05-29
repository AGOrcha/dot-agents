# Observability cluster design summary (R2, R4, R5)

**Status:** design-pass summary (2026-05-27)
**Author:** designer-A
**Scope:** project (dot-agents)
**Inputs:** umbrella spec [`agent-run-scoring-observability-platform`](../workflow/specs/agent-run-scoring-observability-platform/design.md), three new specs (this pass).

## Specs landed in this pass

- `.agents/workflow/specs/r2-observability-dashboard/design.md` — real-time dashboard over R1 scores + iter-log telemetry; SSE push; read-only.
- `.agents/workflow/specs/r4-code-task-generation-eval/design.md` — KG-templated task generator + sandboxed agent runner + R1-pipelined eval scoring.
- `.agents/workflow/specs/r5-review-labeling-access/design.md` — human label sidecars + new `human_label` signal in a major rubric version bump + RBAC + hash-chained audit log.

Each spec follows the four-tier artifact model (`workflow-artifact-model.md`): problem → goals → personas → decisions → requirements → done criteria → open questions → deferred → cross-references → candidate tasks appendix.

## Shared infrastructure surfaces (all three depend)

1. **R3 service host + publish primitive.** All three plans mount inside the R3-hosted Go service. R2 needs the publish primitive to push iteration-scored events; R5 mounts its write routes alongside R2's read routes; R4's CLI-first surface stands alone in v1 but becomes a registered background task once R3 lands.
2. **The existing R1 iter-log + score sidecar shape.** R2 reads it. R4 emits into an eval-namespaced root (`.agents/eval/runs/<run-id>/iteration-log/`). R5 adds a sibling sidecar (`iter-N.labels.yaml`) and a new signal that the rubric extractor picks up. **No spec proposes a new persistent store in v1** — every read path is filesystem-addressable.
3. **Iter-log root discovery configuration.** R2 must learn to query a list of roots (active + eval). The contract lives in R2's spec (OQ1) and is a precondition for R4's dashboard visibility done-criterion.
4. **Rubric versioning invariant (umbrella D3).** R4 does NOT bump the rubric (eval is just another input). R5 DOES bump (adds `human_label` signal). Both respect the contract that prior-version scores stay queryable and unchanged.
5. **HTTP middleware chain inside R3.** R5 owns auth + audit middleware; R2 + R4 mount under it (R2 is read-only and binds loopback in v1, so auth is initially a no-op; R5 turns it on).

## Cross-plan touchpoints (explicit)

| From | To | Touchpoint | Owner |
|---|---|---|---|
| R4 | R2 | Eval iter-log root discoverable by dashboard | R2 OQ1 + R4 done-criterion #6 |
| R5 | R2 | Payload augmentation (`labels[]`) behind R2's composition point | R2 D2.5; R5 D5.5 |
| R5 | R2 | Two SPA routes (`/review`, `/review/:iteration`) inside R2's bundle | R5 D5.6 |
| R5 | R4 | Eval iterations are labelable | R5 OQ5 |
| R5 | R1 | New `human_label` signal in next rubric major version | R5 D5.2 |
| R2 + R5 | R3 | HTTP host + middleware mount points | umbrella D2 |
| R4 | R3 | Background-task registration (post-v1) | R4 D4.8 |

## Proposed sequencing

**Highest leverage first.** R3 unblocks both R2 and R5; R2 unblocks the visible part of R4 + the UI surface for R5; R4 is independent in v1 (CLI-only) and can run in parallel.

```
R3 (service host + publish primitive)              ── prereq for everything visible
 ├── R2 (dashboard surface, read-only)             ── must land before R5 UI
 │    └── R5 (labels + RBAC + audit)               ── consumes R2 surface, mounts in R3
 └── R4 (eval harness, CLI-first)                  ── parallelizable with R2
       └── R4-dashboard-bridge (R2's OQ1)          ── small task, lands once both exist
```

**Concretely:**

1. **R3 first** (separate plan, not in this design pass) — its publish primitive is R2's hard dependency. R2 can author handlers + UI against a placeholder publisher; the integration smoke gates on the real one.
2. **R2 + R4 in parallel** once R3's interface is pinned. R2 owns the surface; R4 stands alone at the CLI and only needs R1 (shipped).
3. **R5 after R2 ships the SPA shell**, the API client + auth-header plumbing, and at least one route detail view R5 can wrap. R5's middleware can land alongside R2's surface (no API client work blocks it) — UI work is the gate.
4. **Cross-plan finisher tasks:**
   - R2's `t-dashboard-eval-discovery` (eval iter-log root) — once R4 emits.
   - R5's `t-eval-labeling-bridge` — once R4 emits.
   - R3's eval-as-background-task registration — once R4 is stable.

## Risks the cluster shares

- **R3 publisher shape unknown.** All three spec verifications presuppose R3 has settled on a publisher interface. Mitigation: design against a tiny placeholder (`Publish(topic, payload)`), gate production wiring on R3 milestone.
- **Score-sidecar staleness window.** R2 OQ2 + R5 D5.2: handlers must tolerate "iter-log written, score sidecar not yet written." Same window matters for R4 emission and R5 signal extraction.
- **Iter-log root multiplication.** R4 adds the eval root; future waves may add historical roots. The root-discovery contract in R2 must be versioned, not bolted on per consumer.
- **Rubric version proliferation.** R5 bumps the rubric; future product changes will bump again. Score-version reproducibility (re-load any prior version) must be invariant — R1 already promises this; R5's bump is the first stress test.
- **Auth surface activation.** R5 turns RBAC on. Before activation, R2's v1 anonymous loopback is the only deployment; after activation, every internal caller (R4 CLI, any local scripts) needs a token if it talks to the API. Document the cutover at R5's "activation" task.

## Cross-references to other live work

- **`r1-outcome-scoring`** (completed) — the substrate for all three.
- **`r3-background-worker-service`** (sibling plan, no spec yet) — the design-pass priority *after* this one; without R3, R2 and R5 are stranded.
- **[[verifier-owns-ci-watch-shift-left]]** — once R2 is live, the `pr-ci.result.yaml` outputs become a candidate dashboard signal. R5's `da review audit verify` is a candidate CI gate in the project overlay.
- **[[worker-owns-pr-readiness-loop]]** — worker briefings should explicitly exclude label submission (R5 D5.7; labels are a human action).
- **[[validate-bundle-against-head]]** — when these specs go to plan-fanout, the planner must HEAD-validate write_scope paths: `internal/scoring/` is shared; concurrent plans touching it need explicit sequencing.
- **`codex-019e6245-examination-and-sequenced-plan`** (proposal) — its `config-explain-live-surface` proposal is the natural place R5's `auth.users_file` and bound-interface state become introspectable. Not a blocker; a future integration.

## What this pass deliberately did NOT do

- **Author plan-level tasks.** Each spec includes a "candidate canonical-plan tasks" appendix as a checklist for the planner, not a materialized plan. The user will materialize tasks via `da workflow plan create` after reviewing the specs.
- **Modify Go code or scaffold files.** Read-only investigation + design-doc authoring per the mandate.
- **Resolve every open question.** Each spec leaves OQs for implementation-time resolution per the artifact model.
- **Touch the plan-level `design.md` files** under `.agents/workflow/plans/<id>/`. Those exist and overreach into plan territory (file paths, function names); a future planner should reconcile them against these specs. The specs are authority.

## Suggested next action for the user

1. Read each spec (~1000-1500 words each); confirm decisions D2.* / D4.* / D5.* match intent.
2. If decisions look right, the planner can materialize each plan via the existing `PLAN.yaml` + `TASKS.yaml` files (already present from the prior design pass) — these specs are the *contract* the existing plans should be re-validated against.
3. The plan-level `design.md` files under `plans/<id>/` contain implementation tech (Vite, React, TanStack, fsnotify, worktree sandbox) that is correct but spec-level overreach; either accept the overreach as "plan elaborates on spec" or trim to keep specs and plans cleanly separated per `workflow-artifact-model.md`.
