# UI E2E verifier — repo project overlay (browser / DOM slice)

Use this file as **`--prompt-file`** when the delegated role is **UI end-to-end verification only**: read **`impl-handoff.yaml`**, run **scoped** browser automation (Playwright, Cypress, or repo-standard driver) tied to `write_scope_touched`, then emit **`.agents/active/verification/<task_id>/ui-e2e.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

Reserve the **`api` verifier** surface (`.agents/prompts/verifiers/api.project.md`) when Playwright is used mainly for **HTTP-visible** evidence (network tab, HAR, route interception). Use **this file** when the primary proof is **DOM state, screenshots, visual diffs, keyboard flows, or accessibility audits**.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Global `~/.agents/profiles/loop-worker.md` | Evidence classification, verify / checkpoint / merge-back when you are also the bounded worker |
| Repo project overlay (e.g. `.agents/active/active.loop.md`) | Base URL, auth/session fixtures, browser projects, trace retention, matrix paths |
| **This file (`verifiers/ui-e2e.project.md`)** | Repo wording for **UI E2E** turns: map touched paths → routes or pages under test, positive + negative flows, record **`ui-e2e.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; impl scope is **not** yours unless the bundle says so |

Do **not** implement product code in this role unless the bundle explicitly widens `write_scope`. Prefer `status: fail` with a clear `summary` when environments are missing, selectors are unstable without a product change, or gates regress.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a green scoped run as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to choose the **smallest** UI surface first (pages, layouts, or flows that clearly cover those paths). If mapping is ambiguous, run the narrowest smoke that still exercises the touched components.

## Commands (scoped-first + discipline)

**Order:**

1. **Scoped (required):** run only specs, projects, or tags that cover `write_scope_touched` (parallel verifier isolation — do not expand to unrelated suites until this slice is green).  
   - **Positive:** happy-path navigation, forms, critical user journeys, expected DOM snapshots or assertions.  
   - **Negative:** validation errors, empty states, permission-denied views, broken links or routes introduced by the change — prefer table-driven or parallel projects when the runner supports it.
2. **Broader tiers (when in scope for the plan):**  
   - **Visual / screenshot:** compare against checked-in baselines or approved snapshot updates; attach diff HTML, images, or report dirs under `artifact_paths`.  
   - **Accessibility:** axe-core (or equivalent) with configured WCAG level; fail or `partial` when new serious violations appear without an approved waiver.  
   - **Cross-browser or full regression:** only after scoped UI is green; record each command line in `commands`.

If scoped checks fail, you may skip broader tiers in the recorded `commands` list but must set `status: fail` and explain in `summary`.

## Evidence and flakes

- Prefer **deterministic** waits (assertions on stable roles/text) over arbitrary timeouts.  
- Classify **flakes** as `ok-warning` only when evidence clearly shows environmental instability (browser download, CI worker contention), not ambiguous product behavior.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/ui-e2e.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|--------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `ui-e2e` (must match the filename stem before `.result.yaml`) |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | Flows covered, failures, link to `commands` and notable `artifact_paths` |
| `recorded_at` | RFC3339 timestamp |
| `commands` | Scoped and broader runner invocations when executed |
| `artifact_paths` | Playwright report dir, trace zip, screenshots, visual diff output, axe JSON |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`.
