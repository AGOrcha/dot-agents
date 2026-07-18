# Dashboard subsystem consolidation + event-bus security posture

Produced by the **kg-ideate** pipeline (2026-07-18) from the design conversation on
making the dashboard a first-class `da` subsystem and hardening its event bus.
Spec: `.agents/workflow/specs/dashboard-subsystem-and-bus-security/design.md`.

## Phase 1 — briefing (kg-ideate)

- **KG traversal:** `da kg query` intents run; graph holds code nodes, no SDD decision
  nodes for this topic — grounded in prior specs + lessons, not fabricated. Impact
  radius (from structure): `cmd/da-dashboard/`, `internal/dashboard/{events,handlers,
  server,store,watch}`, `internal/service/*`, `commands/service/` (landing), a new
  `commands/dashboard/`, `.github/workflows/dashboard.yml`.
- **Prior decisions:** `r3-background-worker-service` D1 (`da service` subcommand, "no
  second binary"); `r2-observability-dashboard` Goal 5 ("same binary, no separate
  deploy"), D2.1 (read-through, read-only v1), D2.2 (unidirectional SSE,
  reconnect-refetch), OQ5 (bounded-buffer drop=>disconnect); `unified-event-contract`
  D1-D4 (envelope, kind registry, table-driven dispatch, control-plane fail-closed).
- **Contradiction:** "one binary / no separate deploy" (r3 D1 + r2 Goal 5) vs a slim
  separately-deployable prod runtime (Docker/Podman client<->runtime split). The current
  `cmd/da-dashboard` separate root already DRIFTS from "no second binary."
- **Lessons:** `cf-worker-auth-gate-fail-closed` (D6 exposure checklist),
  `verify-plan-readiness-against-canonical-ref`, `prototype-experiment-fidelity-gate`
  (D3/D7 prototypes).
- **Gaps:** Podman/daemonless prior-art not in corpus (noted, not blocking); bus DoS
  beyond slow-consumer (cap + coalescing) unspecced; beyond-loopback exposure model
  undecided; hard capability boundary not enforced.

## The decisions (spec)

- **D1** dashboard = `da` subsystem; retire the separate root. **D2** one shared core,
  two composition targets (dev in-process / R3 runtime mount). **D3** slim
  separately-deployable prod runtime as a **forward-door seam now, artifact later**
  (import/build boundary excludes the fat toolchain). **D4** egress-only, one-way
  runtime->broker bridge. **D5** capability = deny-by-default + explicit strictly-cased
  allowlisted write-operation paths (no ambient write; runtime enforcement a forward
  requirement). **D6** loopback default + fail-closed beyond. **D7** DoS governance
  (subscriber cap + high-freq coalescing atop drop=>disconnect). **D8** payload hygiene.

## Task graph (dep-ordered)

Sequential concurrency (PLAN.yaml) — the D3 fork gates the seam.

1. `t1-dashboard-subsystem` — `da dashboard` (serve/open/status) over the shared core.
2. `t2-retire-standalone-root` — retire `cmd/da-dashboard` [dep t1].
3. `t3-boundary-mechanism-decision` — **HARD fork, ideation-cycle**: build-tag vs
   separate-module vs dedicated-entrypoint for the slim runtime. Gates t4.
4. `t4-slim-runtime-seam` — implement the ratified boundary + import-guard [dep t3;
   write_scope finalized by t3].
5. `t5-capability-writepaths` — deny-by-default + registered write-path mechanism [dep t1].
6. `t6-oneway-egress-bridge` — one-way bridge, egress-only/GET-only [dep t1].
7. `t7-loopback-failclosed` — loopback-default bind, fail-closed beyond [dep t1].
8. `t8-dos-cap-coalescing` — subscriber cap + coalescing (needs a rate measurement first).
9. `t9-payload-hygiene` — no secrets in browser-bound events [dep t6].
10. `t10-multiuser-authz-gate` — fail-closed multi-user precondition (impl deferred to r5)
    [dep t7].
11. `t11-docs-align` — docs to the shipped subsystem + posture [dep t1, t2].
12. `t12-verify-close` — verify spec done-criteria + close [dep t2,t4,t5,t6,t7,t8,t9,t10,t11].

## Cross-plan relations

- **Amends** `r2-observability-dashboard` Goal 5 and `r3-background-worker-service` D1
  ("no separate deploy / no second binary") — narrowed to one dev entry + one shared
  core + a seam for a slim prod runtime.
- **Inherits** `unified-event-contract` D1-D4 for the bus envelope + fail-closed dispatch.
- **Complements** `worktree-platform` (code-plane isolation) and `git-ref-work-backend`
  (coordination-plane) — this plan owns the observability read-plane.
- **Defers** in-service authn/z + multi-user stream authz to `r5-review-labeling-access`.

## Phase 4 — execution handoff

`kg-ideate` produces no code. Direct-vs-fanout: `t3` runs through **ideation-cycle**
(empirical prototype under the fidelity gate + cross-harness audit) and MUST land before
`t4`. `t1`/`t2` are a bounded consolidation slice; `t5`-`t10` are independent bus-security
slices that could fan out once `t1` lands (non-overlapping scopes within
`internal/dashboard/`), but are authored sequential-by-default because the coalescing
policy (t8) still needs a rate measurement. Plan is **draft** until the spec (PR #428) is
reviewed; activate before the loop picks up `t1`/`t3`.
