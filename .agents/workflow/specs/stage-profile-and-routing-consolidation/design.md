# Spec: stage-profile & routing consolidation — one `StageProfile` primitive + retire `app_type_verifier_map`

**Status:** SHIPPED (2026-06-22). This spec is now the **canonical owner** of the
`stage_profiles` primitive and the `execution_profile.topology.verifier_sequence` /
`lenses.lens_set` routing model — sibling specs (ideation-execution-profile,
lens-evidence-policy) reference these sections rather than restating the model.
Supersedes PR #40 (`config-v2-p1c-verifier-source-aware`), whose source-aware
`PromptFileRef` work is **absorbed** here as the prompt-composition shape inside
`StageProfile` (nothing from #40 is discarded).
**Date:** 2026-06-07 (designed) / 2026-06-22 (shipped via the
`stage-profile-and-routing-consolidation` plan, t1–t6 all `completed`).

> **Coherence note (2026-06-25, reconciled vs shipped `.agentsrc.json`).** The
> model below shipped as designed, with two reconciliations against the live
> registry: (a) the live `stage_profiles` populates **only** `verifier` (5 slugs)
> and `reviewer` (now **4** slugs — see §3) — `executor`/`orchestrator` remain
> addressable-but-empty per §7 (dispatch deferred); (b) the reviewer set gained a
> **fourth** lens, `cross-harness-adversarial`, after this spec was first written
> (shipped PR #149) — folded into §3 below.

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
  "reviewer":     { "architecture-standards": {…}, "acceptance-invariants": {…}, "adversarial": {…}, "cross-harness-adversarial": {…} },
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
  live `.agentsrc.json` (5 verifier + **4** reviewer profiles, the fourth being
  `cross-harness-adversarial` added post-migration in PR #149) and Go testdata/fixtures migrated.
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

## 7A. Reviewer-set additions folded post-migration (shipped)

After the consolidation migrated the original three lenses, the reviewer stage
gained a fourth, registered in the same `stage_profiles.reviewer` map this spec
owns. These are reconciled against the shipped `.agentsrc.json` registry and
`internal/scaffold/.../prompts/`:

- **`cross-harness-adversarial`** (PR #149) — an adversarial lens that routes the
  pass to a *different* agent harness than the one running, discovered on the host
  by probing PATH (mirrors `internal/platform/cliprobe.go`), excluding the running
  engine, degrading gracefully to a non-blocking `pass`-with-caveat when no
  alternate is installed. The machine-active-platform routing contract is the
  canonical content of `reviewers/cross-harness-adversarial.md` +
  `references/cross-harness-routing.md` (+ the dot-agents overlay); this spec does
  not restate it. It is in the live `go-cli` `lens_set`
  (`[architecture-standards, acceptance-invariants, adversarial, cross-harness-adversarial]`,
  `lens_concurrency: gated`).
- The verifier stage's `cli-runner` (built-binary smoke, PR #146) and
  `citation-check` docs↔code link-integrity enhancement (PR #149) shipped as
  prompt content under the verifier slugs this spec already enumerates; they are
  canonical in their prompt files, not here.

**Open idea (not yet a task): a typed worker-self-gate / `pr-ci`/SAST verifier
profile.** The executor retro (PR #144,
`delegation-lifecycle/references/executor-prompt-retro.md`) records, as a
RECOMMENDED-needs-review item, promoting the worker self-gate (the mechanical
coverage/Sonar/shell-lint/S3776 surface) into a reusable typed verifier stage so
the worker exits at merge-back and the verifier owns the loop (per
`[[verifier-owns-ci-watch-shift-left]]`). That would be a new `stage_profiles.verifier`
slug (`pr-ci`) routed into `topology.verifier_sequence` — additive to this model.
Tracked as a follow-up; **not** in scope of the shipped consolidation.

## 7B. Source-qualified prompt files — the deferred source-resolution point, resolved (2026-08-11)

**Status: RATIFIED + SHIPPED.** D1 carried PR #40's `PromptFileRef` forward "unchanged", including its
`source` field — but *source resolution* was never implemented, only the shape. Every consumer
flattened a `PromptFileRef` to its `path`
(`commands/workflow/profile_prompt.go:promptRefPath`, `bundle.go:flattenBundlePromptPaths`), and
nothing ever fetched a prompt file from a config source. The observable failure: a team layer fetched
via `extends` declaring
`prompt_files: ["po-agents-config:verifiers/verifier.base.md", …]` made
`da workflow resolve-prompt --kind verifier --slug ts-lint` report `matched: true` with every entry
`scope: "unresolved", exists: false` — the source id was treated as part of a literal filename. This
section closes that gap; it is the canonical owner of prompt-source semantics.

### 7B.1 Ref grammar (decision)

- A `prompt_files` entry in the **typed object** form `{source, path, version}` is the canonical
  source-qualified form and is always treated as source-qualified. An undeclared `source` is honored
  as written (it surfaces as an unresolved prompt with a sync hint) rather than silently downgraded
  to a local path — the author was explicit.
- A **bare string** entry is source-qualified **only** when its prefix before the first `:` matches a
  source id declared in the **effective** config. This aligns the prompt tier with the
  `source-id:path[@version]` ref grammar `config-distribution-model` §5 already defines
  (`ParseLayerRef`). Any other string — including a legacy `verifiers/unit.md` and a Windows
  `C:\…` literal — keeps local-path semantics and the historical scope search path
  (absolute → repo-local → `.agents/prompts/` → `<AgentsHome>/prompts/`).
- Source/version provenance is preserved **end to end**: no consumer may flatten a `PromptFileRef` to
  a bare path. The bundle seam renders the canonical `source:path[@version]` string, which round-trips
  back through the same grammar.

### 7B.2 Mechanism (decision)

Three parts, mirroring how a config layer already resolves:

1. **Sync-time fetch.** `LayeredResolver.Resolve` — the one network-touching path, behind
   `da config sync` / `da install` — collects the source-qualified prompt refs the effective config's
   `stage_profiles` declare and fetches each through the SAME per-source-type `Fetcher` an `extends`
   layer uses, caching it content-addressed at
   `~/.agents/cache/config/<source-id>/<prompt-path>/<sha>/`.
2. **Lock pinning.** Each fetched prompt file is recorded in `.agentsrc.lock`'s `units` section as a
   `kind: "prompt"` unit keyed `<source-id>:<prompt-path>[@version]` with its resolved digest. This
   extends the §7A/R5 reproducibility guarantee (same source-set + lock digest ⇒ identical effective
   bundle) from config content to **prompt content**. The kind is new rather than a reuse of
   `artifact`: a prompt is neither merged like a layer nor installed/invoked under a trust posture
   like an artifact — it is content pinned for composition.
3. **Offline probe.** `da workflow resolve-prompt` (and every dispatch surface it feeds) resolves a
   source-qualified entry purely from lock + cache, reporting `scope: "source"`, `exists: true`, and
   the cached path. It remains **offline by contract** (`ResolveLocked`, lock+cache only, never
   network): a never-synced or cache-pruned prompt is `unresolved` plus a `run \`da config sync\`` hint,
   never a fetch trigger — the same skip+sync-hint precedent `LockedRemoteLayerBytes` sets for layers.

Failure policy: a prompt that cannot be fetched is **non-fatal** to the resolve (a warning; the
previous pin is carried forward when one exists). A prompt is composition input, not policy — failing
a whole `da config sync` over one stale prompt ref is a worse trade than surfacing it as unresolved at
resolve-prompt time. Offline resolves never fetch and never drop existing prompt pins.

### 7B.3 Done criteria (for this section)

1. A stage profile pinning `{source, path}` (or a declared-source-prefixed string) resolves to
   `scope: "source"`, `exists: true` with a cached path after `da config sync`, and to `unresolved`
   with a sync hint when the cache is pruned — with zero network calls in either case.
2. A bare string whose prefix names no declared source keeps local-path semantics unchanged.
3. `.agentsrc.lock` carries a `kind: "prompt"` unit per source-qualified prompt file; the lock stays
   **additive** (existing locks remain valid; sibling `layer`/`artifact`/`profile` units are
   untouched; the prompt set is replaced wholesale by pass 1 so a deleted `prompt_files` entry drops
   out).
4. Source/version survives `decodeProfilePromptFiles` and the bundle prompt-file seam; the
   `resolve-prompt` JSON stays backward compatible (`source` and `hint` are additive, `omitempty`).

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
