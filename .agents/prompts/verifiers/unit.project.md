# Unit verifier — repo project overlay (Go test slice)

Use this file as **`--prompt-file`** when the delegated role is **unit verification only**: read **`impl-handoff.yaml`**, run **scoped** Go tests tied to `write_scope_touched`, then the **full configured Go suite** (D12), and emit **`.agents/active/verification/<task_id>/unit.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Global `~/.agents/profiles/loop-worker.md` | Habits: evidence classification, verify / checkpoint / merge-back when you are also the bounded worker |
| Repo project overlay (e.g. `.agents/active/active.loop.md`) | Paths, matrices, guardrails |
| **This file (`verifiers/unit.project.md`)** | Repo wording for **unit** turns: map touched paths → packages, positive + negative cases, record **`unit.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; impl scope is **not** yours unless the bundle says so |

Do **not** implement product code in this role unless the bundle explicitly widens `write_scope`. Prefer failing the verification run with a clear `summary` and `status: fail` when the tree is broken.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a green scoped run as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to choose **scoped** packages (D12 — parallel verifier isolation): only `go test` packages that **cover** those paths (directory of each touched file → `./that/package/...`). If mapping is ambiguous, include the smallest superset that obviously contains the edits.

## Commands (D12 + discipline)

**Order:**

1. **Scoped (required):** `go test -race -count=1 -timeout=120s <packages-from-write_scope_touched>`  
   - **Positive:** default / happy-path packages build and pass.  
   - **Negative:** where the change introduces failure modes, run targeted `-run` subtests or packages that assert errors (table-driven / parallel subtests preferred).
2. **Full suite (required for final pass):**  

   `go test ./... -race -count=1 -timeout=300s`

   `-count=1` disables test cache; **`-timeout=300s`** caps wall time for the full tree.

If scoped tests fail, you may skip the full suite in the recorded `commands` list but must set `status: fail` and explain in `summary`.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/unit.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|--------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `unit` |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | What ran, key failures, link to `commands` |
| `recorded_at` | RFC3339 timestamp |
| `commands` | Include scoped line(s) and the full `./...` line when run |
| `artifact_paths` | Optional: test log paths, coverage outputs, if captured |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`.
