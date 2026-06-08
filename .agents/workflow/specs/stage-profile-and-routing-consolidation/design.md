# Spec: stage-profile & routing consolidation — one `StageProfile` primitive + retire `app_type_verifier_map`

**Status:** draft (for review). Supersedes PR #40 (`config-v2-p1c-verifier-source-aware`), whose
source-aware `PromptFileRef` work is **absorbed** here as the prompt-composition shape inside
`StageProfile` (nothing from #40 is discarded).
**Date:** 2026-06-07.

---

## 1. Problem

Two forms of drift in the verifier/reviewer configuration surface, both surfaced by the PR #40 review.

**Primitive drift.** PR #40 added a `VerifierProfile` type (`Label` + source-aware
`PromptFiles []PromptFileRef`) and uses that *single* type for **both** `verifier_profiles` and
`reviewer_profiles` — its own doc comment calls the two maps "structurally symmetric." The codebase
already speaks in four stages:
- `commands/workflow/bundle.go` `Stage` = `impl` / `verify` / `review`;
- `internal/config/execution_profile.go` `Topology` = the **executor:verifier:reviewer** fan-out;
- relevance stages `orchestrate` / `verify` / `review`.

So the type is mis-named (a *stage* profile, not a *verifier* profile) and the two maps are a
duplicated pair. There is no addressable home for **executor** or **orchestrator** prompt
compositions even though both stages already exist structurally.

**Vestigial routing.** `app_type_verifier_map` (`{app_type: [verifier_slug,…]}`) is **already
superseded in design** by `execution_profile.by_app_type.<type>.topology` — the code comment states
verbatim that topology "supersedes the flat app_type_verifier_map (verifier_sequence moves here)." But
the migration was never finished: `da config relevance` reads `execution_profile`, while the
**delegation/fanout dispatch still reads the flat map** (`delegation.go:mappedVerifierSequence` →
`d.AppTypeVerifierMap[appType]`). The map is load-bearing only because its consumer was left behind.

## 2. Goal

Consolidate the three legacy surfaces — `verifier_profiles`, `reviewer_profiles`,
`app_type_verifier_map` — into the intended model so it works as designed, with **two cleanly
separated kinds**: a single **named-profile primitive** and a single **routing/topology** surface that
*references* it. Existing manifests keep working (read back-compat); the live config + fixtures migrate
now; the legacy keys are deprecated for a later hard cut.

## 3. The unified model (decisions)

**Kind 1 — `stage_profiles`: named prompt-composition primitives (the "what prompt").**
```jsonc
"stage_profiles": {
  "executor":     { "<slug>": { "label": "…", "prompt_files": [ /* PromptFileRef */ ] } },
  "verifier":     { "unit": {…}, "cli-runner": {…}, "schema-check": {…}, "citation-check": {…}, "task-schedule": {…} },
  "reviewer":     { "architecture-standards": {…}, "acceptance-invariants": {…}, "adversarial": {…} },
  "orchestrator": { "<slug>": {…} }
}
```

- **D1 — One `StageProfile` type, one `stage_profiles` map keyed by stage.** Go:
  `StageProfiles map[string]map[string]StageProfile` (`stage → slug → profile`). `StageProfile` is
  PR #40's `VerifierProfile` **unchanged** (`Label`, `PromptFiles []PromptFileRef`); the stage is the
  outer key, so `PromptFileRef` source-awareness is reused verbatim. The four stage keys —
  `executor` / `verifier` / `reviewer` / `orchestrator` — are the uniform agentic primitives.
- **D2 — Routing references the primitives; `app_type_verifier_map` retires into
  `execution_profile.topology`.** Kind 2 is `execution_profile.by_app_type.<type>.topology`:
  `verifier_sequence: [slug,…]` references `stage_profiles.verifier.<slug>` (ordered passes);
  `lenses.lens_set: [slug,…]` references `stage_profiles.reviewer.<slug>` (the reviewer lens panel);
  the `executors` / `verifiers_per_executor` / `reviewers` counts are unchanged. Executor and
  orchestrator are referenced **singularly** (one per task) — sequences exist only where ordering
  matters (verifier passes, reviewer lenses). `app_type_verifier_map[type]` folds into
  `topology.verifier_sequence`; the **delegation/fanout + app-types consumers migrate to read from
  execution_profile**, retiring the flat map's last live consumer.
- **D3 — Merge granularity is preserved for free.** `CategoryMapMerge` already "recurses into nested
  maps; per-key value uses CategoryScalar semantics" (`internal/config/resolver.go`). A two-level
  `stage_profiles` map deep-merges per `(stage, slug)`; the leaf `prompt_files` array is
  last-writer-wins — identical to today's one-level `verifier_profiles` behaviour, one level deeper.
  `stage_profiles` joins `fieldCategories` as `CategoryMapMerge`; the legacy keys stay mapped for
  transitional resolution.
- **D4 — Reference validation generalizes.** `validateVerifierProfileRefs` (delegation.go) validates
  `verifier_sequence` slugs against `stage_profiles.verifier` and `lens_set` slugs against
  `stage_profiles.reviewer`, replacing the flat `verifier_profiles` lookup.
- **D5 — Back-compat: read + migrate + deprecate (no hard break day one).** A normalization/fold step
  makes legacy manifests still load: `verifier_profiles → stage_profiles.verifier`,
  `reviewer_profiles → stage_profiles.reviewer`, and `app_type_verifier_map[type] →
  execution_profile.by_app_type[type].topology.verifier_sequence` (creating the `by_app_type` entry
  when absent, only when the new field is unset). Canonical marshal emits **only** the new keys. The
  three legacy keys remain in `agentsRCKnown` as deprecated read aliases (removed in a later cut). The
  live `.agentsrc.json` (5 verifier + 3 reviewer profiles) and Go testdata/fixtures migrate now.
  - **Open (resolve in plan):** fold at `AgentsRC.UnmarshalJSON` vs an effective-layer helper.
    Effective-layer is safer w.r.t. scope merge (legacy keys may appear at different layers); the
    `app_type_verifier_map` fold in particular spans an untyped (ExtraFields) key and a typed
    (`*ExecutionProfile`) field, so post-merge normalization is the likely home.

## 4. Requirements (behavioral)

1. `verifier_profiles` and `reviewer_profiles` resolve identically before/after via
   `stage_profiles.{verifier,reviewer}` — `da workflow resolve-prompt` output is unchanged for the
   existing slugs.
2. `--kind executor` and `--kind orchestrator` newly resolve through `stage_profiles`.
3. Delegation/fanout resolves the per-app_type verifier sequence through
   `execution_profile.topology.verifier_sequence`, with **no** `app_type_verifier_map` present.
4. A manifest containing any of the three legacy keys still loads (folded), and `MarshalJSON`
   round-trips it to the new keys only.
5. Scope merge of `stage_profiles` preserves per-`(stage, slug)` granularity (D3).

## 5. Done criteria

1. `internal/config` exposes `StageProfile` + `StageProfiles` with full marshal/unmarshal/known-map/
   merge wiring + the legacy fold; `go test ./internal/config/...` green incl. a legacy-fold round-trip.
2. `da workflow resolve-prompt` resolves all four stages; existing verifier/reviewer slugs unchanged.
3. `da workflow app-types` + a `da workflow fanout` dry-run resolve verifier sequences from
   `execution_profile` with `app_type_verifier_map` removed from the live manifest.
4. `.agentsrc.json` + fixtures migrated; `schemas/agentsrc.schema.json` defines `stage_profiles` and
   marks the three legacy props deprecated.
5. Full `go test ./...` + `go vet ./...` green; gofmt clean; per-file 95% coverage gate holds.
6. PR #40 closed as superseded; the new PR carries PromptFileRef forward.

## 6. Open questions

- Fold placement (UnmarshalJSON vs effective-layer) — D5 leans effective-layer; confirm in plan.
- Do executor/orchestrator ever need *sequences* (vs singular references)? Current answer: no — leave
  topology sequencing to verifier/reviewer; revisit if multi-executor tournaments need ordered slugs.
- Schema deprecation signal: JSON Schema has no native `deprecated` enforcement — use `description` +
  a lint warning in `da config verify` (out of scope here; note for follow-up).

## 7. Out of scope / deferred

- Hard removal of the three legacy keys (a later cut once downstream repos/scopes migrate).
- Docs sweep (`docs/VERIFIER_REVIEWER_TEMPLATES.md`, `docs/CONFIG_RELEVANCE.md`, scaffold starter
  skill instructions, specs/proposals naming the old keys) — accurate while aliases read; separate commit.
- Native executor/orchestrator *dispatch* (spawning those stage agents from `stage_profiles`) — this
  spec only makes them addressable; dispatch is the `staged-profile-dispatch-and-return-gate` line.

## 8. Relationships

- **PR #40 / config-v2-p1c** — superseded; its `PromptFileRef` + `$defs/promptFileRef` + tests are the
  base this builds on.
- **app-type-profiles / ideation-execution-profile / cli-runner-verifier** — define the profile
  taxonomy and the execution_profile facets this consolidation routes through.
- **work-tracking-storage-abstraction §3A** — `stage_profiles` is the operational/semantic node type
  work results correlate against in the KG feedback loop; cleaner primitives ⇒ a cleaner feedback graph.
- **config-distribution-model §15 (scope ladder)** — `stage_profiles` and `execution_profile` are
  scope-mergeable layers (org→team→project).
- **knowledge-architecture-graph-views** — `stage_profiles` (executor/verifier/reviewer/orchestrator)
  is the operational-view encoding of the "how we do things" model.
