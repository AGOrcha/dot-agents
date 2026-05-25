# R4: Code-task generation and evaluation harness — plan design

**Status:** draft (2026-05-25).
**Spec:** `.agents/workflow/specs/agent-run-scoring-observability-platform/design.md` (§4 done-criterion, R4 open questions).
**Depends on:** r1-outcome-scoring (shipped) for the scoring pipeline.
**Benefits from:** r3-background-worker-service (registers harness as a background task) and worktree-platform (managed sandbox primitive).

## Decisions

### D1 — Hybrid corpus, synthetic-from-KG first

v1 generates tasks from the Tree-sitter KG via per-language templates over KG queries. Benchmark seeding (HumanEval/SWE-bench) is a v2 adapter producing the same internal `TaskSpec` — deferred to a follow-on plan slice so v1 is finite.

Rationale: the umbrella spec's R4 done-criterion is "≥3 languages with difficulty metadata"; HumanEval is Python-only. KG-derived tasks exercise real call graphs from real repos — the dot-agents differentiator. Comparability-with-benchmarks is a separable concern: a future generator can ingest HumanEval JSONL and emit the same internal `Task` shape.

### D2 — Sandbox = git worktree + tempdir, behind a Sandbox interface

Throwaway scratch repo + ephemeral branch + tempdir for build artifacts + scoped env vars (`HOME=<scratch>`, `GIT_DIR=<scratch>`). Concurrent isolation proven by a "two simultaneous Provisions cannot see each other's writes" test.

Rationale: docker is heavy (~seconds per container start, host-specific Mac/Linux divergence). Worktrees are 10-100ms. The agents we run (claude, codex) are user-installed CLIs operating on a `cwd`; a scoped tempdir+worktree is filesystem isolation that's "good enough" for verifiable-task scoring without solving general untrusted-code execution. `Sandbox` interface admits a future `DockerSandbox` provider swap or the `worktree-platform` plan's managed-worktree primitive.

### D3 — Three languages in v1: Go, Python, TypeScript

- **Go** — primary repo language; existing KG fixture coverage; trivial verification (`go test`, `go build`).
- **Python** — already in KG language set; aligns with future HumanEval seed adapter.
- **TypeScript** — meaningful divergence from Go/Python (gradual types, transpile step); user's agent ecosystem (codex, claude) routinely targets TS.

Rust is plausible later; the per-language adapter pattern keeps the marginal cost bounded to one file per language.

### D4 — Versioned TaskSpec

TaskSpec carries `task_spec_version` so schema evolution is auditable. v1 = TaskSpec v1. Difficulty signals are KG-derived (node/edge counts, cyclomatic proxy) so they're reproducible.

### D5 — Eval outcomes feed R1 unchanged

The harness emits a synthetic `IterationRecord` shape into an eval-namespaced iter-log dir (`.agents/eval/runs/<run-id>/iteration-log/`), then calls `scoring.BuildSignalSets` + `Rubric.Score` + `WriteIterationScore` exactly as `da score run` does. **No new scoring path; no new rubric version** (eval is just another input source). This keeps R1's "versioned rubric" invariant intact.

### D6 — Persist eval-specific metadata as a sibling sidecar

`.agents/eval/runs/<run-id>/eval-run.yaml` holds the TaskSpec + verification result + agent output digest, alongside the R1 score sidecar. R2 reads the eval sidecar to render task / agent / score together; R1 only sees the score.

### D7 — CLI-first; R3 registration is a follow-on

`da eval run` works standalone (matches spec verification). When R3 ships, `evalcore.Harness` becomes a registered background task without changing its API.

## Architecture

```
                    ┌──────────────────────────────────────┐
                    │   da eval run --language <lang>      │
                    │   (commands/eval/eval.go)            │
                    └──────────────┬───────────────────────┘
                                   │
              ┌────────────────────┴────────────────────┐
              │                                         │
        ┌─────▼──────┐                          ┌───────▼─────────┐
        │  Generator │                          │     Harness     │
        │ (per-lang) │                          │     Driver      │
        └─────┬──────┘                          └───────┬─────────┘
              │                                         │
              │  TaskSpec                               │  AgentRun result
              │                                         │
        ┌─────▼──────────────┐                  ┌───────▼─────────────┐
        │ KG reader role     │                  │ Sandbox             │
        │ (CodeGraphReader)  │                  │ (worktree + tmpdir) │
        └────────────────────┘                  └───────┬─────────────┘
                                                        │
                                                ┌───────▼─────────────┐
                                                │ Agent runner        │
                                                │ (claude/codex/...)  │
                                                └───────┬─────────────┘
                                                        │
                                                ┌───────▼─────────────┐
                                                │ Verifier            │
                                                │ (go test / pytest / │
                                                │  tsc + node --test) │
                                                └───────┬─────────────┘
                                                        │
                                                ┌───────▼─────────────┐
                                                │ iter-log emitter +  │
                                                │ scoring.Rubric.Score│
                                                └───────┬─────────────┘
                                                        │
                                                  iter-N.score.yaml
                                                  (R1 sidecar → R2 reads)
```

Key seams:
- `internal/eval/generator.Generator` interface with per-language implementations registered in a registry.
- `internal/eval/sandbox.Sandbox` interface (Provision/Cleanup); v1 impl `worktreeSandbox`.
- `internal/eval/runner.AgentRunner` interface (Run(ctx, workdir, prompt) → AgentRun); v1 impls bind to existing `commands/agents/` machinery.
- `internal/eval/verifier.Verifier` per language (TestVerifier + BuildVerifier).
- `internal/eval/scoring_bridge.go` emits R1-shaped `IterationRecord` + `IterationObjectives` and calls `scoring.AssembleSignalSet` + `scoring.WriteIterationScore`.
- `internal/eval/store.go` persists `TaskSpec` + `EvalRun` outcome as a sibling sidecar under `.agents/eval/runs/<run-id>/`. R2 reads the eval sidecar dir to render task+score together.

## TaskSpec shape (versioned)

```yaml
task_spec_version: 1
task_id: kg-go-impl-001
language: go
difficulty: medium        # easy | medium | hard
difficulty_signals:
  cyclomatic_complexity: 7
  edge_count: 12
  involved_symbols: 4
generated_from:
  kind: kg_template       # kg_template | benchmark_seed (v2)
  template_id: impl-pure-fn
  kg_query:
    intent: code_context
    seed_symbol: "pkg/foo.Bar"
prompt: |
  Implement the function Bar(...) such that ... ; tests live at ...
solution_artifacts:
  - path: pkg/foo/bar.go
    role: target
verification:
  build_cmd: ["go", "build", "./..."]
  test_cmd:  ["go", "test", "./pkg/foo/..."]
  timeout_seconds: 120
```

Difficulty metadata is *derived from KG signals* (node count, edge count, cyclomatic complexity proxy from the underlying function), so it's reproducible and language-agnostic.

## Done criteria

- `da eval gen --language {go,python,typescript}` produces a valid TaskSpec sidecar.
- `da eval run --language go` runs an agent against one generated task end-to-end and writes a score sidecar consumable by `da score iteration`.
- ≥3 languages have generator + verifier coverage (unit + integration tests).
- Sandbox isolates host filesystem (concurrent test proves two eval runs cannot see each other's writes).
- Score sidecar references a TaskSpec; R2 can join task → score (consumer contract documented).

## Open decisions to escalate at orchestrator gate

1. **R3 coupling timing.** Plan ships CLI-first standalone. When R3 lands, register `Harness` as a background task — recommend that be an R3 task, not an R4 follow-on. Keeps R4 finite at the CLI surface.
2. **Per-platform agent runner coverage v1.** Recommend `clauderunner` first (demo-able against user's own setup). Codex + others stubbed behind `AgentRunner` interface; each becomes a per-runner follow-on task.
3. **Eval-namespaced iter-log dir vs main dir.** v1 writes under `.agents/eval/runs/<id>/iteration-log/` so eval runs don't pollute real iteration history. R2 must learn to query both roots.
4. **Benchmark-seed adapter (v2).** Confirm deferral. If product wants HumanEval comparability immediately, add `benchmark-seed-adapter` task to W4.
5. **Verification step authoring.** TaskSpec ships with *generated* tests (harness emits the test alongside the prompt) OR *hidden* tests (agent doesn't see the test; harness runs it after agent finishes). Recommend **hidden** — closer to real verifiable-task setup; prevents teach-to-the-test.
