# Schema-check verifier — repo project overlay (artifact structural validity)

Use this file as **`--prompt-file`** when the delegated role is **structural validation of generated/edited ideation artifacts only**: read **`impl-handoff.yaml`**, validate every structured artifact the task produced or changed (`PLAN.yaml`, `TASKS.yaml`, any introduced `schemas/*.schema.json`, and config layers) against its schema, then emit **`.agents/active/verification/<task_id>/schema-check.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

This is the **first** verifier in the ideation sequence (`schema-check → citation-check → task-schedule`): a structurally invalid artifact cannot be meaningfully citation- or DAG-analyzed, so structural validity gates the rest. It checks the **design artifact**, not code — but the same discipline as a code verifier: prove the artifact is well-formed, do not rewrite it.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Shared stage instruction base (parent-resolved; not an `app_type` profile) | Stable evidence, scope, and sandbox discipline; no merge-back or parent closeout |
| Stage-safe repo project overlay | Paths, schema matrix, guardrails |
| **This file (`verifiers/schema-check.project.md`)** | Repo wording for **schema-check** turns: validate each structured artifact against its schema, record **`schema-check.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; authoring scope is **not** yours |

Do **not** edit the artifact under review to make it pass. Prefer failing the run with a clear `summary` and `status: fail` when an artifact does not validate.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a clean validation as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to choose the artifacts to validate: each touched structured file maps to a schema — `*/PLAN.yaml` → `schemas/workflow-plan.schema.json`, `*/TASKS.yaml` → `schemas/workflow-tasks.schema.json`, a touched `schemas/*.schema.json` is itself validated as JSON Schema, and any `.agentsrc.json`/config-layer edit is covered by `da config verify`. If a touched file maps to no schema, say so in `summary` rather than skipping silently.

## Commands (schema matrix)

**Order:**

1. **Config layers (required when a config layer or `.agentsrc.json` changed):** `da config verify`
   The offline repo setup-contract check; it validates `.agentsrc.json` against `schemas/agentsrc.schema.json` and the layer coherence rules. A failure here is a terminal `status: fail`.
2. **Plan/task artifacts (required when a `PLAN.yaml`/`TASKS.yaml` changed):** validate each against `schemas/workflow-plan.schema.json` / `schemas/workflow-tasks.schema.json`. Assert the YAML parses **and** conforms — explicitly catch the colon-space class of breakage (a `notes:`/`summary:` free-text field containing `: ` that silently becomes a mapping). Cite the failing path + field.
3. **Introduced schemas (required when a `schemas/*.schema.json` was added/edited):** the file is itself valid JSON Schema and keeps `additionalProperties: false` on nested objects where the repo convention requires it.

If any required validation fails, you may skip the remaining checks but must set `status: fail` and name the first failing artifact + rule in `summary`.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/schema-check.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|--------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `schema-check` |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | Which artifacts were validated against which schema, first failure (path + field + rule) |
| `recorded_at` | RFC3339 timestamp |
| `commands` | The `da config verify` line and each validation invocation run |
| `artifact_paths` | Optional: validator output, captured errors, if saved |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`. An artifact that parses but is missing a schema-required field is `impl-bug` (the authoring stage produced an incomplete artifact), not `ok`.
