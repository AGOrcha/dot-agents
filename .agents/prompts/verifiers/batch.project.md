# Batch verifier — repo project overlay (fixture / golden / multi-record slice)

Use this file as **`--prompt-file`** when the delegated role is **batch verification only**: read **`impl-handoff.yaml`**, run **scoped** fixture-driven or table-driven checks tied to `write_scope_touched`, optionally broader regression matrices or full dataset passes when the plan calls for them, then emit **`.agents/active/verification/<task_id>/batch.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

**Not** the **`unit` verifier** (`.agents/prompts/verifiers/unit.project.md`): reserve **unit** for **`go test`** and in-process Go coverage. Use **this file** when the primary proof is **file-backed or multi-record batches** — golden directories, CSV/JSON fixtures, snapshot or CLI diff output, schema validation over fixture trees, or job runners that compare **expected vs actual** artifacts at scale.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Global `~/.agents/profiles/loop-worker.md` | Evidence classification, verify / checkpoint / merge-back when you are also the bounded worker |
| Repo project overlay (e.g. `.agents/active/active.loop.md`) | Fixture roots, matrix paths, sandbox data dirs, tool binaries, diff tolerances |
| **This file (`verifiers/batch.project.md`)** | Repo wording for **batch** turns: map touched paths → fixtures or jobs under test, positive + negative cases, record **`batch.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; impl scope is **not** yours unless the bundle says so |

Do **not** implement product code in this role unless the bundle explicitly widens `write_scope`. Prefer `status: fail` with a clear `summary` when fixtures are missing, tools are not installed, or expected-vs-actual diffs show regressions without an approved baseline update.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a green scoped run as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to choose the **smallest** fixture set or batch job that still covers those paths (directory or glob implied by touched files). If mapping is ambiguous, run the narrowest smoke batch that still exercises the changed inputs or generators.

## Commands (scoped-first + discipline)

**Order:**

1. **Scoped (required):** run only jobs, targets, tags, or fixture subsets that **cover** `write_scope_touched` (parallel verifier isolation — do not expand to unrelated datasets until this slice is green).  
   - **Positive:** happy-path rows or files, valid schemas, successful CLI exits with matching golden output or checksums.  
   - **Negative:** corrupt rows, missing required fields, version skew, intentional bad fixtures — capture **expected-vs-actual** diffs (unified diff, JSON diff, tabular report) under paths listed in `artifact_paths`; prefer table-driven or sharded runs when the runner supports them.
2. **Broader tiers (when in scope for the plan):**  
   - **Full fixture tree or matrix row:** after scoped green; record each command line in `commands`.  
   - **Performance or volume:** bounded batch size or time budget; attach summaries under `artifact_paths` and fail or mark `partial` when budgets regress without an approved change.

If scoped checks fail, you may skip broader tiers in the recorded `commands` list but must set `status: fail` and explain in `summary`.

## Expected-vs-actual artifacts

- Persist machine-readable diffs (for example `.diff`, `.json` diff reports, HTML comparison reports) and point to them from **`artifact_paths`**.  
- When baselines update intentionally, say so in `summary` and ensure the merge includes the updated golden or approved waiver reference — do not silently widen tolerances in the verifier role.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/batch.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|-------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `batch` |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | Batches run, diff highlights, link to `commands` and notable `artifact_paths` |
| `recorded_at` | RFC3339 timestamp |
| `commands` | Scoped and broader runner invocations when executed |
| `artifact_paths` | Golden dirs, diff outputs, batch logs, matrix reports |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`.
