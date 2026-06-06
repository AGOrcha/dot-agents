# Citation-check verifier — repo project overlay (references resolve)

Use this file as **`--prompt-file`** when the delegated role is **reference-integrity verification of ideation artifacts only**: read **`impl-handoff.yaml`**, confirm that every reference the artifact makes **resolves** (and, where the citation adapter is active, is actually **supported** rather than contradicted), then emit **`.agents/active/verification/<task_id>/citation-check.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

This is the **second** verifier in the ideation sequence (`schema-check → citation-check → task-schedule`): it runs once the artifact is known structurally valid. It checks the **design artifact**, not code — a dangling `[[wikilink]]`, a missing file path, or a claim citing a source that does not support it propagates silently into implementation if not caught here.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Shared stage instruction base (parent-resolved; not an `app_type` profile) | Stable evidence, scope, and sandbox discipline; no merge-back or parent closeout |
| Stage-safe repo project overlay | Paths, reference taxonomy, guardrails |
| **This file (`verifiers/citation-check.project.md`)** | Repo wording for **citation-check** turns: resolve each reference, flag unsupported/contradicted claims, record **`citation-check.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; authoring scope is **not** yours |

Do **not** add or fix references in the artifact under review. Prefer failing the run with a clear `summary` and `status: fail` when a reference does not resolve.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a clean pass as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to bound the set of artifacts whose references you check. Enumerate the reference classes present: `[[wikilinks]]`, repo-relative **file paths**, **KGNote IDs**, cross-spec / `§`-section / proposal references. If a reference class is present that you cannot resolve with the tools below, record it as `unknown` rather than passing it.

## Commands (resolve → support)

**Order:**

1. **Resolution (required):** for each reference, prove the target exists.
   - **File paths / `§` refs:** the path (and section, where addressable) exists in the tree.
   - **`[[wikilinks]]` / KGNote IDs:** resolve via `da kg query --intent repo_context "<term-or-id>"`; an id or note that returns no node is a **dangling reference**.
2. **Support (required where the citation adapter is active — `dotagents-builtin:graph/citation@^1.0`):** for claims that cite a source, run the adapter's named queries to check the claim is grounded:
   - `claims_citing_source` — the cited source actually backs the claim.
   - `contradicting_claims` — no indexed claim contradicts it.
   A claim whose cited source does not support it (or that is contradicted) is a `fail`. Where the adapter is **not** active, record support checks as `unknown` and pass resolution-only — say so in `summary`.

If a required resolution fails, you may stop and set `status: fail`, naming the first dangling reference in `summary`.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/citation-check.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|--------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `citation-check` |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | Reference classes checked, counts resolved/dangling, any unsupported/contradicted claim |
| `recorded_at` | RFC3339 timestamp |
| `commands` | The `da kg query` lines and any citation-adapter queries run |
| `artifact_paths` | Optional: captured query output, if saved |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`. A dangling reference is `impl-bug` (the authoring stage cited something that does not exist); the citation adapter being unavailable is `blocked` for the support half, not `ok`.
