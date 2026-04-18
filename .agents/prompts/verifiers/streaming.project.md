# Streaming verifier — repo project overlay (SSE / WebSocket / long-lived streams)

Use this file as **`--prompt-file`** when the delegated role is **streaming verification only**: read **`impl-handoff.yaml`**, run **scoped** checks on **event-driven or duplex transports** tied to `write_scope_touched` (Server-Sent Events, WebSockets, chunked/long-poll streams, gRPC server-streaming when exercised as a stream), then emit **`.agents/active/verification/<task_id>/streaming.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

Reserve the **`api` verifier** surface (`.agents/prompts/verifiers/api.project.md`) when the primary proof is **single-shot HTTP** (status + body, contract diff on a finite response). Use **this file** when the primary proof is **behavior over time**: ordered events, heartbeats, backpressure, reconnect semantics, idle timeouts, or **frame-level** integrity.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Global `~/.agents/profiles/loop-worker.md` | Evidence classification, verify / checkpoint / merge-back when you are also the bounded worker |
| Repo project overlay (e.g. `.agents/active/active.loop.md`) | Stream endpoints, auth for WS/SSE, CI budgets, trace retention, matrix paths |
| **This file (`verifiers/streaming.project.md`)** | Repo wording for **streaming** turns: map touched paths → streams or sessions under test, positive + negative timing/backpressure cases, record **`streaming.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; impl scope is **not** yours unless the bundle says so |

Do **not** implement product code in this role unless the bundle explicitly widens `write_scope`. Prefer `status: fail` or `partial` with a clear `summary` when endpoints are unreachable, clocks or CI are too flaky to assert timing without product changes, or captured streams show regressions without an approved baseline update.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a green scoped run as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to choose the **smallest** stream surface first (one SSE resource, one WS namespace, or one tagged scenario that clearly covers those paths). If mapping is ambiguous, run the narrowest smoke that still exercises the touched handlers or clients.

## Commands (scoped-first + discipline)

**Order:**

1. **Scoped (required):** exercise only streams, channels, or tagged scenarios that **cover** `write_scope_touched` (parallel verifier isolation — do not subscribe to unrelated feeds or soak the full cluster until this slice is green).  
   - **Positive:** happy-path event order, schema of each event type, heartbeat or ping/pong where applicable, successful reconnect after a clean server restart or simulated drop when the product defines that behavior.  
   - **Negative:** server-initiated close with expected code/reason, **client idle timeout**, malformed SSE line or partial WS binary frame, **slow consumer / backpressure** (bounded buffer overrun or apply backpressure per product contract), **dropped or duplicated** frames when the contract allows at-most-once delivery — capture timelines, logs, or frame dumps under **`artifact_paths`**; prefer table-driven or parallel subtests when using in-process harnesses.
2. **Broader tiers (when in scope for the plan):**  
   - **Soak / volume:** bounded duration or event count; attach summaries under `artifact_paths` and fail or mark `partial` when loss/latency budgets regress without an approved change.  
   - **Chaos or fault injection:** only after scoped streams are green; record command lines and trace paths.

If scoped checks fail, you may skip broader tiers in the recorded `commands` list but must set `status: fail` and explain in `summary`.

## Timeouts, backpressure, and dropped frames

- Treat **timeouts** as first-class evidence: record whether the failure was **client hang**, **server stall**, or **environment** (CI clock skew). Use explicit deadlines in harnesses; do not rely on unbounded waits.  
- For **backpressure**, assert the product’s contract (drop, block, coalesce, or error) and store **before/after** metrics or short captures in **`artifact_paths`**.  
- For **dropped or reordered** events, distinguish **contractual** behavior from **bugs** in `summary`; when order is best-effort, assert statistical or idempotent properties instead of strict sequence unless the plan demands strict ordering.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/streaming.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|-------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `streaming` |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | Streams exercised, timeout/backpressure outcomes, notable drops or reordering, link to `commands` and `artifact_paths` |
| `recorded_at` | RFC3339 timestamp |
| `commands` | Scoped harness or CLI lines and any broader soak/fault lines when run |
| `artifact_paths` | HAR excerpts, WS frame logs, SSE transcripts, trace zips, timeline charts |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`. Use `ok-warning` sparingly for **environmental** flake (CI timing, shared broker contention), not for ambiguous product semantics.
