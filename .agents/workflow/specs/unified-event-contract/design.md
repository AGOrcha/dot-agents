# Unified pluggable event contract — spec


> **Consolidation update (2026-06-07) — `stage-profile-and-routing-consolidation`:** `verifier_profiles` + `reviewer_profiles` are now unified into one **typed** `stage_profiles` map (stage `executor`/`verifier`/`reviewer`/`orchestrator` → slug → `{label, prompt_files}`), and `app_type_verifier_map` is **retired** into `execution_profile.by_app_type.<type>.topology.verifier_sequence`. Legacy keys still load (folded, deprecated). Mentions of those keys below describe the pre-consolidation surface — read them as the new model.

Status: spec (promoted from `.agents/proposals/unified-pluggable-event-contract.md` +
the PR-event refinements, 2026-05-31).
Owns: the common envelope, the kind registry, and table-driven dispatch shared by daemon
events, hook sentinels, PR events, and future plug-ins. Implemented first by
layered-pr-fanout `lpf-event-foundation` (`internal/events`); graduates ownership to
r3-background-worker-service when that service lands.

## 1. Problem & goal

Three surfaces (daemon events, hook sentinels, future plug-ins) each "switch on a string tag and
hand-edit a central dispatcher per new kind." Maintainer review #160 named the convergence: adding
a new event/sentinel/PR/hook kind must NOT require a central code edit. Goal: **one envelope, one
registry, table-driven dispatch** — a new kind is a registration, not a dispatcher edit or an
`enum` bump.

## 2. Decisions (normative)

### D1 — Common envelope
Every event/sentinel/PR/hook payload shares:
`{ type, source, occurred_at, idempotency_key, payload }` — `type` is the registered kind (namespaced,
e.g. `event.pr.*`, `sentinel.*`, `event.metric.*`), `source` is the emitting surface, `payload` is the
kind-specific body. The envelope is stable; only `payload` varies by kind. `idempotency_key` makes
re-delivery safe.

### D2 — Kind registry (schema-additive, no enum)
Kinds are registered (object-keyed-by-name, mirroring `verifier_profiles`): schema-additive,
`additionalProperties` open, validated **at dispatch against the registry**, never an `enum` at
schema-load time. Built-in high-churn kinds are code-side defaults; project/org kinds are config.
Per `[[schema-usage]]`, any `.agentsrc.json` field carrying the registry touches all six AgentsRC
sync points and stays enum-free.

### D3 — Table-driven dispatch
Renderers, ingester, orchestrator-reaction, sentinel router, and plug-in host look the handler up by
`type` in the registry — no `switch`. A new kind is added by registering it.

### D4 — Validation disposition: control-plane reject, observational soft (refinement)
Each kind/namespace carries a **disposition**, defaulting to **reject**:
- **Control-plane kinds** (`event.pr.*`, `sentinel.*`, `lens.*`, the §3.4.5 auto-resume predicate
  kinds) **reject-on-unknown at emit time** — fail-fast and loud. These drive behavior (a status
  transition, a lens dispatch); an unregistered/misnamed control kind silently routing to a generic
  handler produces wrong-or-no behavior (a missed `merged`, a skipped lens) invisibly. Same reason an
  unknown `verifier_profile` must error, not no-op.
- **Observational kinds** (`event.metric.*`, display/render-only) **may fail-soft** to a generic
  handler — a dashboard rendering an unknown event as raw JSON is harmless.

This supersedes the proposal's blanket "fail-soft" lean (its OQ-3).

### D5 — Producer engine is generic; no per-platform files
Where a kind is *produced* by fetching an external surface, a single generic **producer engine**
(`exec`/`http` fetch + field-map + diff-and-emit) interprets a config block. There are **no
per-platform producer files**. A small built-in JSONPath subset (`.a.b`, array index, one equality
filter — ~100 lines, **no jq, no yq-lib**) drives the field maps. (See `pr-event-source` spec for the
first consumer.)

## 3. Requirements

- R1: an `internal/events` package providing the envelope type, the registry (register/lookup with
  per-kind disposition), and table-driven dispatch.
- R2: built-in code-side registration of the high-churn kinds (`event.pr.*`, `sentinel.*`, the daemon
  review/comment events, the auto-resume predicate kinds, the workflow-lifecycle kinds) with
  control-plane disposition; config-declared kinds default to observational/generic.
- R3: the generic `exec`/`http` producer engine + JSONPath subset (no external dep).
- R4: emit-time validation enforcing D4 (reject control-plane unknowns; soft observational).

## 4. Done criteria

- A new kind can be added by registration (config or one code-side `Register`) with zero dispatcher
  edits and zero `enum` change — proven by a test adding a kind and dispatching it.
- An unregistered control-plane `type` is rejected at emit time with a clear error; an unregistered
  observational `type` routes to the generic handler with a warning — both covered by tests.
- The `exec`/`http` producer engine resolves a field map via the built-in JSONPath subset with no
  external dependency added to `go.mod`.

## 5. Deferred / relationships

- The wire-format field names, registry storage location, and graduation into
  `r3-background-worker-service` follow the proposal's §5 deferrals.
- First consumers: `pr-event-source` spec (PR events), the daemon review/comment events
  (`[[monitor-pr-review-comment-routing]]` §4), and hook sentinels (#157).
- Full rationale + examples: `.agents/proposals/unified-pluggable-event-contract.md` and
  `.agents/proposals/pr-source-abstraction.md`.
