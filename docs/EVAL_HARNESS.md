# Eval Harness

**Status:** active (partial — R2 visibility contract only)
**Owners:** dot-agents
**Related:** [`OUTCOME_SCORING_RUBRIC.md`](./OUTCOME_SCORING_RUBRIC.md) (the rubric the score
breakdown is produced under); [`internal/eval/store/store.go`](../internal/eval/store/store.go)
(the `eval-run.yaml` / `taskspec.yaml` writer); [`internal/eval/taskspec.go`](../internal/eval/taskspec.go)
(the `TaskSpec` type); [`internal/eval/scoringbridge/`](../internal/eval/scoringbridge/)
(the `iteration-log/iter-1.yaml` + `iter-1.score.yaml` writer)

The R4 eval harness generates a language-agnostic `TaskSpec`, runs an agent against it in a
sandbox, verifies the result, and scores the run through the production R1 scoring rubric. It
persists each run as a small set of YAML sidecar files under a canonical run directory.

> **Scope of this document.** Only the **R2 visibility contract** — the on-disk shape a
> dashboard consumes — is documented below. The full user guide (generating tasks, running the
> harness, interpreting results) is added by a sibling task and extends this same file; keep new
> sections additive and do not fold the visibility contract into them.

## R2 visibility contract

R2 is the dashboard that renders eval runs. It reads the run directory **as data** — it never
re-runs the harness or the scorer. This section is the contract R2 builds against: the run
directory layout, which stage owns each file, and the JOIN a dashboard performs to render a run
with its task metadata joined to its score breakdown.

A **frozen fixture run** that satisfies this contract ships at
[`internal/eval/store/testdata/r2contract-go-impl-pure-fn/`](../internal/eval/store/testdata/r2contract-go-impl-pure-fn/).
It was produced by the real store / scoring / scoringbridge / taskspec pipeline, so R2 can build
against its exact bytes without a harness run. The layout is pinned by
[`internal/eval/store/visibility_contract_test.go`](../internal/eval/store/visibility_contract_test.go),
so a change that breaks the contract breaks that test.

### Run directory layout

Each completed run is a directory `<repo-root>/.agents/eval/runs/<run-id>/`
(the path `store.RunDir` returns). It is **incrementally assembled** — no single stage owns the
whole directory; each stage writes only the files it owns, each with a per-file atomic write:

| File | Owner stage | Type it round-trips through |
| --- | --- | --- |
| `taskspec.yaml` | store (`WriteEvalRun`) | `eval.TaskSpec` — `eval.ParseTaskSpec` |
| `eval-run.yaml` | store (`WriteEvalRun`) | `store.PersistedEvalRun` |
| `iteration-log/iter-1.yaml` | score stage (`scoringbridge.ScoreRun`) | `scoring.IterationRecord` — `scoring.ParseIterationRecord` |
| `iteration-log/iter-1.score.yaml` | score stage (`scoring.WriteIterationScore`) | `scoring.PersistedScore` |

v1 scoring is 1-shot, so the iteration log always holds exactly `iter-1.*` (a run is a single
sample).

- **`taskspec.yaml`** is the full task: `task_id`, `language`, `difficulty`,
  `difficulty_signals`, `generated_from` (incl. `template_id`, the prompt template id), the full
  `prompt`, `solution_artifacts`, and the `verification` commands. It is the source of truth for
  task metadata.
- **`eval-run.yaml`** is the run aggregate + R9/R10 reproducibility block. It **denormalizes**
  the task metadata a run-list view needs (`task_id`, `language`, `difficulty`) and carries a
  **score summary** (`score.value` / `band` / `scored` / `rubric_version`) plus the `agent`
  identity (`harness`, `model`, `session_id`, `prompt_digest`, `output_digest`, durations) and
  the `verify` outcome (`passed`, `phase`, `exit_code`, `duration`). Its purpose is to let R2
  render a run row **without opening all four files**.
- **`iteration-log/iter-1.yaml`** is the R1-shaped iteration record (schema_version 2). Its
  `wave` field carries the **run id** (an eval run belongs to no plan), which is how the score
  joins back to the run.
- **`iteration-log/iter-1.score.yaml`** is the full explainable score: `value`, `band`,
  `scored`, `rubric_version`, and the per-signal `breakdown` (one row per rubric dimension, with
  `sub_score`, `nominal_weight`, `effective_weight`, `contribution`). It is the source of truth
  for the **score dimensions**.

### The join a dashboard performs

To render one run with its task metadata joined to its score breakdown, R2 joins the files on
three keys:

1. **Run-identity join** — `eval-run.yaml:run_id == <dir name> == iter-1.yaml:wave`. This ties
   the run aggregate to its iteration record and locates the run directory. The score sidecar
   carries no run id of its own; it is addressed by **directory containment plus iteration
   number** (`iteration-log/iter-1.score.yaml`, `iteration == 1`).
2. **Task-metadata join** — `eval-run.yaml:task_id == taskspec.yaml:task_id == iter-1.yaml:task_id`.
   This ties the denormalized run metadata to the canonical `TaskSpec`.
3. **Score-summary join** — `eval-run.yaml:score.*` is a summary of `iteration-log/iter-1.score.yaml`;
   they must agree on `value`, `band`, `scored`, and `rubric_version`. R2 renders the summary
   from `eval-run.yaml` and drills into `iter-1.score.yaml` for the breakdown.

### Field provenance

For every field R2 renders, the authoritative source and the join that reaches it:

| Rendered field | Provenance (file → field) | Join used |
| --- | --- | --- |
| run id | `eval-run.yaml:run_id` (== dir name == `iter-1.yaml:wave`) | run-identity (root) |
| language | `eval-run.yaml:language` (list view) — canonical `taskspec.yaml:language` | task-metadata |
| difficulty | `eval-run.yaml:difficulty` (list view) — canonical `taskspec.yaml:difficulty` | task-metadata |
| prompt id | `taskspec.yaml:generated_from.template_id` | task-metadata |
| prompt integrity | `eval-run.yaml:agent.prompt_digest` == `sha256(taskspec.yaml:prompt)` | task-metadata |
| score value / band | `eval-run.yaml:score.value` / `score.band` (summary) — full `iter-1.score.yaml:value` / `band` | score-summary |
| rubric version | `eval-run.yaml:score.rubric_version` == `iter-1.score.yaml:rubric_version` | score-summary |
| score dimensions | `iter-1.score.yaml:breakdown[]` (`signal`, `label`, `sub_score`, `effective_weight`, `contribution`) | run dir + iteration |
| verify outcome | `eval-run.yaml:verify.passed` / `phase` / `exit_code` / `duration` | run-identity |
| agent identity | `eval-run.yaml:agent.*` (also `iter-1.yaml:agent.*`) | run-identity |

`language` and `difficulty` appear on **both** `eval-run.yaml` (denormalized for the list view)
and `taskspec.yaml` (canonical); the contract test asserts they agree. The **prompt id** is the
human-readable `template_id` on `taskspec.yaml`; its integrity is pinned by
`eval-run.yaml:agent.prompt_digest`, which is `sha256` over the exact `taskspec.yaml:prompt`
bytes — so a dashboard can prove the rendered prompt is the one the agent actually ran.
