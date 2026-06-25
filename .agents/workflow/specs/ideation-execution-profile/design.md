# Spec: ideation execution profile — verifiers + reviewers + kg-ideate

**Status:** SHIPPED (the `ideation` `execution_profile` entry below is live in
`.agentsrc.json`). Feeds the `config-relevance-profiles` plan; folded into PR #29.

> **Single-source note (2026-06-25).** The `verifier_sequence` / `lens_set` /
> topology *model* (the shape of these fields and how routing references the
> named profiles) is owned by `[[stage-profile-and-routing-consolidation]]`
> §3 (D2) — this spec does **not** restate it; it only specifies the *ideation
> app_type's values* within that model. The legacy `verifier_profiles` map this
> spec was written against has since been **retired into
> `stage_profiles.verifier`** (same plan, D5); references below are updated to the
> shipped `stage_profiles` surface.
>
> Reconciled vs shipped `.agentsrc.json`: ideation ships exactly as D1–D3, D6
> describe — `executors: 3`, `verifiers_per_executor: 3`, `reviewers: per_executor`,
> `verifier_sequence: [schema-check, citation-check, task-schedule]`,
> `lens_set: [architecture-standards, acceptance-invariants, adversarial]`
> (`lens_concurrency: parallel`). The three artifact-integrity verifier prompts
> all shipped.
**Depends on:** the execution-profile layer (shipped t1–t6) and the `cli-runner` verifier pattern
(t7) this mirrors. **Forward-references:** the proposed `kg-ideate` skill
(`.agents/proposals/kg-ideate-skill.yaml`, draft/deferred).
**Grounds in:** the verifier-profile taxonomy (`app-type-profiles/design.md` §11), the review-lens
set (architecture-standards / acceptance-invariants / adversarial), and existing tooling
(`da kg query` + citation adapter, `da config verify` + `schemas/*.schema.json`, `da workflow
eligible` dependency/cycle detection).

---

## 1. Problem & goal

The shipped `ideation` profile (t6) is a stub: `verifiers_per_executor: 0`, `reviewers: "0"`, a
single `acceptance-invariants` lens, and a relevance set that omits the skill ideation tasks
actually run. That encodes "ideation produces artifacts nobody checks," which is wrong: an ideation
task produces **design artifacts** — a spec, a plan, a TASKS DAG — that get turned into code and
other artifacts downstream. A bad citation, an invalid generated schema, or a cyclic task schedule
propagates silently into implementation.

**Goal:** make `ideation` a coherent profile that (a) routes to the KG-grounded authoring skill,
(b) **reviews the design** through the same three lenses used for code (applied to the artifact, not
to code), and (c) **verifies artifact integrity** — references resolve, generated schemas validate,
the task schedule is acyclic and its deps resolve.

## 2. Decisions (review these)

- **D1 — Relevance routes to `kg-ideate`.** `orchestrate.core = [kg-ideate, deep-research,
  plan-wave-picker]`; `situational = [article-extract]`; `noise = [test-runner, loop-worker]`
  (code-execution units are noise for design work). `kg-ideate` is draft/deferred — listing it is a
  forward reference (like `cli-runner` was), and `default_class: situational` keeps unlisted units
  safe. The profile also gains `verify` and `review` relevance stages.
- **D2 — Reviewers are the three lenses, applied to the design.** `lens_set =
  [architecture-standards, acceptance-invariants, adversarial]`, `lens_concurrency: parallel`;
  `topology.reviewers = "per_executor"` (each executor's divergent artifact gets the full lens
  panel). The lenses ask "does the *design* hold up" — architecture fit, acceptance/invariant
  coverage, adversarial "what if this design is wrong" — not code review. `review.core` lists the
  three lenses so the relevance facet shows them.
- **D3 — Three artifact-integrity verifiers.** `verifier_sequence = [schema-check, citation-check,
  task-schedule]`, `verifiers_per_executor: 3` (one pass per profile — coherent, like go-cli's 2).
  `schema-check` runs **first** (Q1): a structurally invalid artifact can't be citation- or
  DAG-analyzed, so structural validity gates the rest. Each is a new verifier-class profile (prompt
  + `stage_profiles.verifier` entry — the slug map that retired the legacy `verifier_profiles`,
  see `[[stage-profile-and-routing-consolidation]]`), modeled on `cli-runner`/`unit`.
- **D4 — Verifiers orchestrate existing tooling; no new Go.**
  - **`citation-check`** — every reference in the artifact resolves: `[[wikilinks]]`, file paths,
    KGNote IDs, cross-spec/§-proposal refs. Uses `da kg query` and, where the citation adapter is
    active (`dotagents-builtin:graph/citation@^1.0`), its named queries `claims_citing_source` /
    `contradicting_claims` to flag unsupported or contradicted claims.
  - **`schema-check`** — generated/edited structured artifacts validate against their schema:
    `PLAN.yaml`/`TASKS.yaml` and any `schemas/*.schema.json` the artifact introduces; runs
    `da config verify` when config layers changed. Catches the YAML colon-space class of breakage.
  - **`task-schedule`** — the task DAG is sound: `depends_on` ids resolve (incl. the
    `<plan-id>/<task-id>` cross-plan form), there are no cycles, and `da workflow eligible` reports
    a consistent ready/blocked partition with no conflicts.
- **D5 — Typed results per verifier.** Each writes
  `.agents/active/verification/<task_id>/<verifier>.result.yaml` against
  `schemas/verification-result.schema.json` (`verifier_type` ∈ {citation-check, schema-check,
  task-schedule}; all slug-valid). No schema change.
- **D6 — Executors stay 3.** Divergence is the point of ideation; the verify/review gates now make
  that divergence safe instead of unchecked.

## 3. Requirements (behavioral)

1. `da config relevance --filter topology --app-type ideation` resolves `executors: 3`,
   `verifiers_per_executor: 3`, `reviewers: per_executor`, `verifier_sequence: [schema-check,
   citation-check, task-schedule]`.
2. `da config relevance --filter lenses --app-type ideation` resolves the three lenses, parallel.
3. `da config relevance --filter units --app-type ideation` shows `kg-ideate` core in orchestrate
   and the three lenses core in review; `test-runner`/`loop-worker` suppressed as noise.
4. Each ideation verifier produces a schema-valid result and fails (`status: fail`) when its class
   of integrity breaks (unresolved citation / invalid schema / cyclic or dangling dep).
5. `da config verify` stays green.

## 4. Done criteria

1. Three prompts under `.agents/prompts/verifiers/` (`citation-check.project.md`,
   `schema-check.project.md`, `task-schedule.project.md`), each mirroring the verifier-prompt
   contract (role boundary / preconditions / commands / result artifact / evidence classification).
2. `stage_profiles.verifier` (the slug map that retired `verifier_profiles`) gains `citation-check`,
   `schema-check`, `task-schedule` entries. **(Done — all three are live.)**
3. The `ideation` `execution_profile` entry is revised per D1–D3, D6.
4. `docs/CONFIG_RELEVANCE.md` ideation examples updated (verifiers + reviewers, not "no gate").
5. All resolution/verify checks in §3 pass; config + workflow + internal suites stay green.

## 5. Out of scope / deferred

- **Building the `kg-ideate` skill itself** — separate proposal (draft/deferred). This profile only
  references it; the skill's two-phase implementation is its own work.
- The first-class **versioned** `stage_profiles.verifier` layer (app-type-profiles §11.4–11.5) —
  these ship in today's unversioned slug map.
- A standalone Go command per verifier — they are prompt-driven, like `unit`/`cli-runner`.
- Deep semantic contradiction detection beyond what the citation adapter ships today.

## 6. Resolved decisions (were open questions)

- **Q1 → schema-check first.** Order is `schema-check → citation-check → task-schedule`: structural
  validity gates the citation and DAG analysis (a malformed artifact can't be meaningfully parsed
  for either).
- **Q2 → `reviewers: per_executor`.** The three-lens panel runs against each executor's divergent
  artifact, so it scales if `executors` changes rather than pinning a fixed count.

## 7. Relationships

- **config-relevance-profiles / skill-relevance-filter** — the profile this completes.
- **cli-runner-verifier (t7)** — the verifier-profile pattern this mirrors.
- **kg-ideate-skill proposal** — the ideation authoring skill referenced in relevance.core.
- **app-type-profiles §11** — verifier-profile taxonomy + future versioned layer.
- **graph-backend-adapter-contract §13.4** — the citation adapter `citation-check` leans on.
