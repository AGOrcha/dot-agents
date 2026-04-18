# API verifier — repo project overlay (HTTP / contract slice)

Use this file as **`--prompt-file`** when the delegated role is **API verification only**: read **`impl-handoff.yaml`**, run **scoped** HTTP or in-process API checks tied to `write_scope_touched`, optionally broader contract or smoke passes, then emit **`.agents/active/verification/<task_id>/api.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Global `~/.agents/profiles/loop-worker.md` | Evidence classification, verify / checkpoint / merge-back when you are also the bounded worker |
| Repo project overlay (e.g. `.agents/active/active.loop.md`) | Base URLs, auth fixtures, perf budgets, matrix paths |
| **This file (`verifiers/api.project.md`)** | Repo wording for **API** turns: map touched paths → routes or packages under test, positive + negative HTTP cases, record **`api.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; impl scope is **not** yours unless the bundle says so |

Do **not** implement product code in this role unless the bundle explicitly widens `write_scope`. Prefer failing the verification run with a clear `summary` and `status: fail` when the service is unreachable, contracts drift, or budgets are exceeded.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a green scoped run as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to choose the **smallest** API surface to exercise first (handlers, OpenAPI operations, or client flows that clearly cover those paths). If mapping is ambiguous, include the narrowest smoke that still proves the touched code paths.

## Commands (scoped-first + discipline)

**Order:**

1. **Scoped (required):** exercise only the routes, RPCs, or `httptest` suites that **cover** `write_scope_touched` (parallel verifier isolation — do not hit unrelated environments until the scoped slice is green).  
   - **Positive:** expected status codes, happy-path payloads, idempotent retries where applicable.  
   - **Negative:** invalid auth, malformed bodies, missing required fields, rate-limit / quota responses — use table-driven or parallel subtests when using Go `httptest`; for external runners, capture failing cases in logs referenced from `artifact_paths`.
2. **Broader contract or perf (when in scope for the plan):**  
   - **Contract:** OpenAPI / JSON Schema diff against checked-in specs, golden response files, or consumer-driven fixtures — record command lines and diff paths.  
   - **Performance:** bounded load or latency checks (for example `k6`, `vegeta`, framework perf hooks) with explicit budgets; attach reports under `artifact_paths` and fail the run when budgets regress without an approved change.

If scoped checks fail, you may skip broader tiers in the recorded `commands` list but must set `status: fail` and explain in `summary`.

## Browser-style API checks (Playwright and similar)

When the repo uses **Playwright** (or another browser driver) primarily to exercise **HTTP-visible** behavior (SPA + API, or network tab assertions):

- Treat **network assertions** (HAR, route interception, response status/body) as the API evidence surface; still align artifacts with **`artifact_paths`** (HAR, trace zip, HTML report).
- Keep **scoped-first**: limit routes or user flows to those implied by `write_scope_touched` before full navigation suites.
- Prefer **deterministic** waits and stable selectors; classify flakes as `ok-warning` only when the evidence clearly shows environmental instability, not product ambiguity.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/api.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|--------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `api` |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | What ran, key failures, link to `commands` and notable `artifact_paths` |
| `recorded_at` | RFC3339 timestamp |
| `commands` | Include scoped line(s) and any broader contract/perf lines when run |
| `artifact_paths` | OpenAPI / golden diffs, HAR, Playwright report dirs, perf summaries |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`.
