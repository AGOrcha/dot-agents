# Phase B — app-type-aware pipeline (answer + integration)

## Q: Are the different app_types handled properly in the pipeline I wrote?
**No — the D14 pipeline was go-cli HARDCODED.** The YAML baked in verifier_sequence
`[unit, cli-runner]` + the 4 go-cli lenses as fixed stages. It does NOT adapt to a task's app_type.
For an ideation/docs/web/meta task it would run the WRONG verifiers/lenses. That was fine for the
D14 proving slice (a go-cli task) but is wrong as the general loop.

## What the repo actually declares (grounded)
`.agentsrc.json execution_profile.by_app_type` has exactly **3** profiles (da ships empty defaults):
| app_type | verifier_sequence | lens_set | concurrency |
|---|---|---|---|
| go-cli | unit, cli-runner | architecture-standards, acceptance-invariants, adversarial, cross-harness-adversarial | gated |
| ideation | schema-check, citation-check, task-schedule | architecture-standards, acceptance-invariants, adversarial | parallel |
| docs | schema-check, citation-check, cli-runner | architecture-standards, acceptance-invariants | parallel |

**Missing / referenced-but-undeclared:** `web` (r2-dashboard tasks carry `app_type: web` but there's
NO profile → resolves to empty/default: no verifiers, default gate, no lenses). `docs/web`, `meta`,
`daemon` are not declared at all. So the loop CANNOT correctly handle those until their profiles are
authored — a task that names them silently gets the empty/default pipeline.

## Fix part 1 — make the swarm PROFILE-DRIVEN (infra)
Resolve the profile at runtime instead of hardcoding. Within the swarm-extension's STATIC DAG
(waves fixed at parse, no cycles/conditionals), realize it as **fixed max slots dynamically assigned**:
- **Stage 0 `profile-resolve`**: read the task's app_type; `da config relevance --filter topology
  --app-type <t> --json` + `--filter lenses` → write `COORD/profile.json`
  {verifier_sequence[], lens_set[], lens_concurrency}.
- **Verifier slots v1..v3** (max seq len = 3): slot k reads profile.json; if `k <= len(seq)` run
  `resolve-prompt --kind verifier --slug seq[k]`; else write SKIP (no-op). Sequential (ordered).
- **Reviewer slots r1..r4** (max lens_set = 4): slot k runs lens_set[k] or no-ops; ordering per
  lens_concurrency (gated ⇒ sequential w/ defensive upstream-SKIP; parallel ⇒ one wave).
- **ready_gate**: unchanged (evidence check + PR + owner-held).
This handles go-cli/ideation/docs TODAY (profiles exist) with ONE YAML, app_type-driven.

## Fix part 2 — author the missing app_type PROFILES (config; a `meta` task)
Each needs a profile in `.agentsrc.json` (+ any new `stage_profiles` verifier/reviewer prompts it
references). Proposed (to reconcile against the actual work + PLATFORM_DIRS_DOCS + specs):
- **web** (go http service + ui + ui-e2e → r2-dashboard): verifier_sequence `[unit, integration,
  ui-e2e]` (go unit → http-handler integration → browser/playwright e2e); lens_set
  `[architecture-standards, acceptance-invariants, adversarial, cross-harness-adversarial]` + a
  `web-a11y`/`design` lens; gated. NEW stage_profiles: `integration`, `ui-e2e` verifier prompts.
- **docs/web** (docs site): verifier_sequence `[schema-check, citation-check, link-check, build-site]`;
  lens_set `[architecture-standards, acceptance-invariants]` + a11y; parallel. NEW: `link-check`,
  `build-site`.
- **meta** (self-refinement LOOP / pipeline definitions + orchestrator/subagent defs — the category
  for the loop machinery itself): verifier_sequence `[schema-check, pipeline-lint]` (validate YAML/
  agent-defs + dry-run the pipeline/DSL); lens_set `[architecture-standards, acceptance-invariants,
  adversarial]` — reviewed for LOOP-SAFETY (a bad meta change can wedge the loop). NEW: `pipeline-lint`.
- **daemon / bg-service** (COMING — high-bandwidth/high-freq comms): verifier_sequence `[unit, race,
  integration, load-soak, comms-contract]`; lens_set `[architecture-standards, adversarial,
  cross-harness-adversarial, concurrency-safety]`; gated. Author when the service lands; flag as
  pending (no profile yet).

Authoring these is itself a **meta** task the loop should run first (config work → its own PR),
because the loop can't correctly process web/docs-web/meta/daemon tasks until the profiles exist.

## Orchestration note (user refinement)
`meta` = the self-refinement loop / pipeline definitions (not just "orchestrator subagent def"). With
the swarm pipeline BEING the orchestration, a dedicated long-running orchestrator subagent is largely
unnecessary — "orchestrator" becomes a role/stage-profile for orchestrator-TYPE tasks the pipeline
runs, and the profile-resolve + gate stages carry the coordination. Simpler topology.

## Specs to mirror (Phase 2 loop, not just profile stages)
- `config-distribution-model` — the execution_profile/stage_profiles model (done: resolved via
  `da config relevance`).
- `layered-pr-fanout/design.md` — the LOOP: verifier-green → awaiting_agent_review (unblocks
  downstream ELIGIBILITY); lens accept → awaiting_owner_review; **lens reject → back to in_progress**
  (the fold-back→executor loop, realized as bounded `pipeline` target_count iterations); slot accounting.
- `graph-backend-adapter-contract` — base/lineage resolution for layered multi-dep fanout.
