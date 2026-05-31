# R4 — Code-task generation and evaluation harness — spec

**Status:** draft (2026-05-27)
**Scope:** project (dot-agents)
**Parent spec:** [`agent-run-scoring-observability-platform`](../agent-run-scoring-observability-platform/design.md)
**Plan:** `.agents/workflow/plans/r4-code-task-generation-eval/` (PLAN.yaml + plan-level design.md)
**Sibling specs:** [`r2-observability-dashboard`](../r2-observability-dashboard/design.md), [`r5-review-labeling-access`](../r5-review-labeling-access/design.md)

## Problem

R1 scores telemetry from real agent runs. That tells us how a wave behaved, but it does not measure how a given agent platform / model / prompt configuration *performs* on a comparable task set. Today there is no way to ask: *"is this agent better at Go than Python? Is the new prompt overlay an improvement or a regression? Does this model regress at high cyclomatic complexity?"* The umbrella spec's R4 done-criterion is "generator produces verifiable tasks across ≥3 languages with difficulty metadata; harness emits R1-scored outcomes." R4 closes the eval loop by generating tasks from the project's own Tree-sitter knowledge graph, running agents against them in an isolated sandbox, and recycling the result through R1 so eval outcomes share the rubric and the dashboard surface with production runs.

## Goals

1. Produce verifiable programming tasks the project can run an agent against without human authoring per task.
2. Run those tasks safely — host filesystem and per-task state isolated.
3. Score eval runs through the same rubric as production runs; eval is just another iteration-log root.
4. Visible from the dashboard ([`r2-observability-dashboard`](../r2-observability-dashboard/design.md)) so the score view is the eval view.
5. Reproducible: same TaskSpec + same agent + same sandbox baseline = comparable scores.

## Personas

- **Platform tuner.** Decides between agent platforms (claude, codex, others), prompt overlays, model versions; needs a controlled batch they can re-run and diff.
- **Rubric author.** Validates a proposed rubric change against a known eval batch before promoting it; wants the score-diff under the new version on the same TaskSpec set.
- **Regression watcher.** Runs the harness on a schedule (once R3 is live), compares the new score band against the rolling baseline, flags drift.

## Decisions

### D4.1 — Tasks are generated from the project's own knowledge graph

v1 generates tasks from the Tree-sitter knowledge graph the project already maintains, via per-language templates that select a target symbol and frame an "implement / extend / refactor" prompt around it. There is no human-authored task corpus in v1, and no external benchmark seed.

**Why:** the umbrella spec asks for ≥3 languages with difficulty metadata. Public benchmarks (HumanEval, MBPP) are predominantly Python; SWE-bench requires bespoke per-issue setup. A KG-derived generator exercises real call graphs from real repos the project already has indexed — that is the dot-agents differentiator. Benchmark seeds are not ruled out; they are a separable concern, deferred to v2 behind the same `TaskSpec` shape.

**Rejected:** HumanEval-first (Python only; doesn't satisfy ≥3 languages without separate corpora); SWE-bench-first (heavy per-issue setup; orthogonal to "generator" framing); curated hand-authored corpus (does not scale; not reproducible from KG signals).

### D4.2 — Sandbox is filesystem isolation, not general untrusted-code execution

The harness runs each task in a scoped temporary working tree with environment variables pinned so the agent cannot escape into the user's real home or repo. This is enough to keep two simultaneous tasks from seeing each other's writes and to avoid mutating the host repo. It is **not** a security boundary against deliberately malicious code; the agents the project runs (claude, codex) are user-installed CLIs operating under the user's own privileges.

**Why:** docker pays seconds-per-task startup and host-divergence cost (Mac vs Linux); for verifiable-task scoring the sandbox just needs to keep state from leaking between tasks. The interface admits a swap to docker, firecracker, or a managed-worktree primitive without redesign.

**Rejected:** docker-first (cost not justified at v1 volumes); chroot/namespace (Mac doesn't have Linux namespaces); no sandbox (lets state leak between concurrent tasks).

### D4.3 — Three languages in v1: Go, Python, TypeScript

Go is the project's own language and has the lowest verification cost. Python is in the existing KG language set and bridges to a future benchmark-seed adapter. TypeScript adds meaningful divergence (gradual types, transpile step) and matches what the agent ecosystem (codex, claude) typically targets. Rust and others are plausible later; the per-language adapter pattern bounds the marginal cost to one file per language.

### D4.4 — Eval outcomes feed R1 unchanged

The harness emits an iteration record into an eval-namespaced iter-log root and calls the existing scoring pipeline. **There is no new rubric, no new scoring path, no new signal.** Eval is another input source for R1, not a parallel scoring system. This preserves the umbrella spec D3 invariant: any signal change requires a rubric version bump and is auditable.

**Why:** rubric versioning is the project's contract for "scores are comparable." If the eval harness scores with a different rubric, eval results are not comparable to production, and the dashboard view fragments. One pipeline, one rubric, two input sources.

### D4.5 — TaskSpec is versioned

Each generated task carries a `task_spec_version`. Schema evolution is explicit and auditable; consumers (verifier, runner, dashboard, future re-evaluators) bind to a version. v1 = TaskSpec v1.

### D4.6 — Eval-namespaced iter-log root

Eval runs write under `.agents/eval/runs/<run-id>/iteration-log/`, not into the active orchestration iter-log. Eval and production share the rubric but not the iter-log space. The dashboard reads both roots (see OQ1 in [`r2-observability-dashboard`](../r2-observability-dashboard/design.md)).

**Why:** eval iterations are not real wave iterations; mixing them pollutes orchestration history. A separate root also lets the eval harness be re-runnable without disturbing real iter-log retention.

### D4.7 — Tests are hidden from the agent

The TaskSpec includes a verification command; the test file itself is staged outside the agent's working scope and run after the agent finishes. The agent does not see the tests it will be verified against.

**Why:** prevents teach-to-the-test and matches the verifiable-task setup pattern the project uses for its own workers. An agent that generates a test alongside its implementation is not being measured on implementation quality.

### D4.8 — CLI-first; R3 registration follows

`da eval gen` and `da eval run` work standalone. When R3 ships, the harness becomes a registered background task; that wiring lives in the R3 plan, not in R4, so R4 is finite at the CLI surface.

## Requirements (behavioral)

1. **R1.** `da eval gen --language {go,python,typescript}` produces a TaskSpec sidecar with `task_spec_version`, language, difficulty band, KG-derived difficulty signals, prompt text, expected solution artifact path(s), and verification command set.
2. **R2.** Difficulty band is computed from reproducible KG signals (node count, edge count, a cyclomatic-complexity proxy) so re-running the generator on the same KG state yields the same band.
3. **R3.** `da eval run --task <spec> --agent <runner>` provisions a sandbox, runs the named agent against the prompt, runs the verification commands, and emits an iteration record + score sidecar into the eval-namespaced iter-log root.
4. **R4.** Two concurrent `da eval run` invocations cannot see each other's writes (filesystem isolation test).
5. **R5.** The eval iteration's score sidecar is loadable by the existing `da score iteration` CLI without modification.
6. **R6.** The dashboard surface ([`r2`](../r2-observability-dashboard/design.md)) renders an eval run with task metadata (language, difficulty, prompt-id) joined to the score breakdown.
7. **R7.** Per-language generator + verifier coverage is exercised by automated tests (unit + at least one integration test per language).
8. **R8.** The sandbox cleans up after itself; a failed run does not leak working trees or temporary directories on disk beyond a configured retention window.
9. **R9.** Agent runner identity (platform, model, prompt overlay) is captured in the eval-run sidecar so the platform-tuner persona can diff platforms.
10. **R10.** An eval run is fully described by `{TaskSpec, agent_runner_config, rubric_version}` — a future re-run with the same inputs is reproducible.

## Done criteria (verifiable)

1. `da eval gen --language go` produces a TaskSpec file that validates against the TaskSpec schema.
2. `da eval gen --language python` and `da eval gen --language typescript` likewise.
3. `da eval run --task <go-spec> --agent <runner>` runs end-to-end against a real agent and writes both an `iter-N.score.yaml` and an `eval-run.yaml` sidecar.
4. The score sidecar loads under `da score iteration` and reports a non-error score breakdown.
5. A test starts two `da eval run` invocations concurrently against different TaskSpecs and asserts neither's working tree contains files from the other's run.
6. The dashboard's per-run detail view renders eval runs alongside production runs with the task metadata visible.
7. Re-running the same `{TaskSpec, agent_runner_config}` pair under the same rubric version yields scores that differ only within the rubric's tolerance band (rubric-defined; not a flat zero-difference assertion since agent outputs are stochastic).
8. A failed run (verifier exits non-zero) still emits a score sidecar with the failure encoded as a signal — not a missing file.

## Open questions (must resolve before or during implementation)

1. **OQ1 — Per-platform agent runner coverage v1.** The umbrella has five platforms (claude, codex, cursor, gh-copilot, others). Which runners ship in v1? Recommendation: claude first (user's own primary setup), codex second; others stubbed behind the runner interface and added as follow-on per-runner tasks.
2. **OQ2 — Verification step stability under stochastic agents.** Agents are nondeterministic; a single eval run is a sample, not a measurement. How many runs per TaskSpec define a "score"? Recommendation: 1-shot in v1; aggregation across N runs is a v2 concern that wraps R4, not internal to it.
3. **OQ3 — KG staleness during generation.** The generator reads KG state; if the KG is mid-rebuild (per [[graphstore-concurrency-contract]] / R3's KG-staleness-refresh task), what does the generator do? Recommendation: read with the same staleness contract R3 specifies; do not block.
4. **OQ4 — Cost cap.** Long-running agent runs can rack up token cost. A per-run timeout exists; is there also a per-batch budget cap? Recommendation: yes, configurable in `.agentsrc.json` under an `eval.budget` field; default unlimited; CLI surface to override.
5. **OQ5 — Benchmark-seed adapter deferral.** Confirm v2 deferral; if the product wants HumanEval/SWE-bench parity now, the adapter task moves into R4 scope.
6. **OQ6 — Eval run retention.** How long do eval working trees and sidecars stick around? Recommendation: sidecars indefinite (small, drive the dashboard); working trees configurable, default 7 days, pruned on next `da eval run`.
7. **OQ7 — Hidden-test placement vs. agents that read the whole repo.** If the agent does `find / -name '*_test.go'`, the hidden test is no longer hidden. Sandboxing helps (the test lives outside the working tree the agent operates on) but the contract needs to be explicit at runner-design time.

## Deferred (explicitly out of scope)

- Benchmark seed adapters (HumanEval, MBPP, SWE-bench) — v2 against the same TaskSpec shape.
- General untrusted-code execution sandbox (docker / firecracker) — v2 if R4 ever needs to run untrusted third-party code.
- Multi-run aggregation per TaskSpec (statistical confidence intervals across N runs) — v2.
- Eval-driven rubric tuning automation — v2.
- Per-batch budget enforcement beyond a CLI cap — v2.
- A scheduled / cron eval harness — R3 owns scheduling; R4 ships the harness as a CLI.
- Per-language test framework selection beyond the v1 default (go test, pytest, node --test + tsc) — v2.
- Cross-repo task generation (only the active repo's KG seeds tasks in v1).

## Relationship to other specs and plans

- **[`agent-run-scoring-observability-platform`](../agent-run-scoring-observability-platform/design.md)** — parent; D3 (rubric versioning) is why R4 emits into R1 unchanged.
- **`r1-outcome-scoring`** (completed) — the scoring path R4 reuses; R4 must not introduce a parallel pipeline.
- **[`r2-observability-dashboard`](../r2-observability-dashboard/design.md)** — consumes R4's eval-namespaced iter-log root + the `eval-run.yaml` sidecar to render task + agent + score together.
- **[`r5-review-labeling-access`](../r5-review-labeling-access/design.md)** — eval runs are labelable in the same way production runs are; the human-label signal can mark "this eval run's score under-counts the actual outcome." No coupling beyond shared infrastructure.
- **`r3-background-worker-service`** (sibling plan) — future home for scheduled eval; R3 registration is post-R4.
- **`graph-backend-adapter-contract`** (draft) — R4's generator reads the KG; this contract pins how (per OQ3).
- **[[graphstore-concurrency-contract]]** — generator must respect this contract for KG read consistency.
- **[[worktree-platform]]** — candidate primitive for R4's sandbox; if it ships first, the v1 sandbox swaps to it behind the same interface.

## Candidate canonical-plan tasks (appendix; not yet materialized)

The plan-level `design.md` already sketches the implementation; this is the spec-side index against which the plan's tasks should be validated.

1. **t-taskspec-schema** — define the TaskSpec schema (v1), JSON schema, Go types.
2. **t-eval-iterlog-root** — namespace + writer for `.agents/eval/runs/<run-id>/iteration-log/`.
3. **t-generator-iface** — `Generator` interface + KG-template registry.
4. **t-generator-go** — Go template generator + difficulty signal extraction.
5. **t-generator-python** — Python template generator.
6. **t-generator-typescript** — TypeScript template generator.
7. **t-sandbox-iface** — `Sandbox` interface + the v1 worktree+tmpdir implementation + concurrent-isolation test.
8. **t-runner-iface** — `AgentRunner` interface; first runner implementation (claude per OQ1).
9. **t-verifier-iface** — `Verifier` interface; per-language implementations (go test, pytest, tsc+node --test).
10. **t-scoring-bridge** — emit R1-shape iteration record + call existing scoring pipeline; eval-run sidecar persistence.
11. **t-cli-eval-gen** — `da eval gen` command.
12. **t-cli-eval-run** — `da eval run` command.
13. **t-dashboard-eval-discovery** — extend dashboard's root-list config to include eval root (cross-plan dep on R2 OQ1).
14. **t-e2e-go-claude** — end-to-end test for Go + claude.
15. **t-budget-cap** — per-run timeout + per-batch budget cap per OQ4.
16. **t-cleanup-policy** — working-tree retention per OQ6.

The plan-level design at `.agents/workflow/plans/r4-code-task-generation-eval/design.md` already encodes the implementation tech for each of the above; this spec is the contract the plan is accountable to.
