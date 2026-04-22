# App Type and Verifier Profiles — Design Spec

**Status:** draft
**Written:** 2026-04-22
**Plan:** (not yet created)

**Related:**
- [config-distribution-model](../config-distribution-model/design.md) — `app_type_verifier_map` field surface (§10), command gating (§13.6). This spec defines the schema and lifecycle behind the `app_type` name.
- [external-agent-sources](../external-agent-sources/design.md) — `verifier_profiles` as future OCI packages (§5), auth/transport for package delivery (§3–§4).
- [org-config-resolution](../org-config-resolution/design.md) — layer precedence (§4) and merge category rules (§7.2) govern how profile refs combine across layers.
- [workflow-parallel-orchestration](../workflow-parallel-orchestration/design.md) — `max_parallel_workers`, eligible fanout set, write-scope conflict detection. Profiles supply the verifier chain each parallel worker runs.
- [scoped-knowledge-graphs](../scoped-knowledge-graphs/design.md) — review kinds may read from scoped KG (repo/team/org). A profile names the graph backend it consults.

---

## Table of contents

1. [Problem statement](#1-problem-statement)
2. [Decisions](#2-decisions)
3. [Profile schema](#3-profile-schema)
4. [Composite profiles and resolution](#4-composite-profiles-and-resolution)
5. [Artifact-scope generalization (non-code)](#5-artifact-scope-generalization-non-code)
6. [Verifier evolution and behavior preservation](#6-verifier-evolution-and-behavior-preservation)
7. [Profile discovery and pipeline integration](#7-profile-discovery-and-pipeline-integration)
8. [Worked examples](#8-worked-examples)
9. [Migration from `app_type_verifier_map`](#9-migration-from-app_type_verifier_map)
10. [Open questions](#10-open-questions)

---

## 1. Problem statement

The workflow pipeline (`orient → eligible → fanout → impl → verifier → review → merge-back`) is hardcoded around one shape of work: software engineering. `write_scope` is a list of file paths, `verifier` runs code tests, `review` runs code review through CRG. This works when the task is code; it fails in three distinct cases:

1. **Non-code domains.** Research, writing, design-ideation, legal review, vendor evaluation. "Verify" means rubric check, citation presence, fact-consistency, editorial pass — not `go test`. "Review" means editorial/rubric critique, not symbol-level code review. These domains already account for 13–15% of observed agent sessions in the authoring account; they are run today by stuffing the pipeline's code-shaped slots with string-ified prose checks.
2. **Multi-profile apps.** A single deployable unit can host multiple runtime modes with divergent verification surfaces. Payout's `po-core-api-se` is the concrete case: the same codebase serves an HTTP API, a batch-job runner, and a streaming/webhook processor. Each mode has a distinct verifier chain (API contract tests vs job-leasing and retry tests vs stream replay + webhook idempotence tests). Forcing one `app_type` per repo collapses this.
3. **Verifier evolution without regressions.** Verifier behavior changes — a new lint rule, a stricter coverage gate, a renamed test suite. Today these changes land by editing a shared script and every consumer gets the new behavior on next run. There is no way to change a verifier without silently changing outcomes for every repo that depends on it.

This spec defines **profiles**: named, versioned, composable bundles that specify what `impl → verify → review` means for a given repo or portion of a repo. It also defines the rules by which profiles evolve without breaking consumers.

This spec does not own the field surface (`app_type`, `app_type_verifier_map`) — that lives in config-distribution-model. It owns the **schema the name refers to** and the **lifecycle** of that schema.

---

## 2. Decisions

### 2.1 A profile is a named bundle, not a flag

A profile is a YAML document with a stable name (`go-http-service`, `research`, `resume-ideation`), a semver version, and fields that pin every pipeline plug-point: `write_scope_kind`, `verifier_chain`, `review_kind`, `review_skill`, `graph_backend`, `impact_radius_kind`.

**Why:** the existing `app_type_verifier_map` treats `app_type` as a flat string key into a flat list of verifier names. That is too narrow — it cannot express review kind, graph backend, or impact radius. It also has no versioning surface, so profile evolution is impossible.

**Rejected alternative — overload `app_type` with comma-separated fields (`go-http-service+api+unit`):** tried conceptually. Breaks at the first composite case (po-core-api-se's three modes) and has no version semantics.

### 2.2 Composite profiles compose; they do not override

A composite profile declares `composes: [profile-ref, ...]`. The resolved verifier chain is the **union** of child chains, de-duplicated by verifier name and ordered by composition order, then within-child declaration order. Review kind and graph backend must either agree across children or be explicitly resolved in the composite.

**Why:** forbidding override keeps the mental model simple. A consumer reading `composes: [api, batch]` knows the resulting chain is `api.verifier_chain ∪ batch.verifier_chain`; there are no hidden subtractions. If a consumer needs to *change* behavior, they fork the profile — which is a visible act with its own version — rather than silently mutate.

**Rejected alternative — full override semantics (`overrides: {verifier_name: new-def}`):** rejected because it makes "what does this profile actually do" unanswerable without executing the full resolution. Hiding behavior changes behind composition is exactly the regression this spec is trying to prevent.

### 2.3 Profiles and verifiers are both versioned; behavior changes gate on version bump

A profile has a semver version. Each verifier entry inside the profile references a verifier by name and semver range. Behavior-changing updates to a verifier or profile require a version bump and go through the behavior-preservation gate in §6.

**Why:** without versioning, "change the coverage threshold from 70 to 80" silently downgrades every repo's green build. With versioning, repos opt in (`^1.2`) or pin (`pinned:sha256:...`) and a major bump requires explicit migration.

### 2.4 Profiles can target non-code `artifact_scope`

The existing `write_scope` (file paths) generalizes to `artifact_scope` — a typed container that can be file paths (code), `(file_path, section_path)` tuples (documents), or `(file_path, artifact_id)` tuples (structured artifacts). The profile declares `write_scope_kind: code | document | artifact`; the orchestrator enforces accordingly.

**Why:** a research task that "edits section 3.2 of the methodology chapter" is a bounded write scope in exactly the same way that a code task that "edits internal/auth/middleware.go" is. Generalizing `write_scope` unlocks the rest of the pipeline (conflict detection, fanout, merge-back) for non-code work without a parallel pipeline.

### 2.5 Profile sources layer: local today, git v1.5, oci v2

Profile files are resolved through the same `sources` / `packages` machinery defined in config-distribution-model. `local` sources are sufficient for v1 (profiles live in the same repo or a sibling `agents-config` repo). v1.5 adds `git` sources for distribution. v2 adds OCI packages per external-agent-sources §5.

**Why:** no new transport is needed. Profile distribution is a consumer of the existing config/package tiers.

### 2.6 Review kind, graph backend, and impact radius are all pluggable

A profile names its review kind (`code-review | rubric-review | citation-review | custom`), its graph backend (`crg | citation-graph | document-cross-ref | none`), and its impact radius kind (`symbol | section | citation | custom`). Each selection binds to a skill reference that implements it. New review/graph kinds are added by publishing new skills, not by patching the pipeline.

**Why:** the pipeline currently assumes code-review + CRG + symbol-radius are the only possibilities. Locking those assumptions into the code path makes the pipeline permanently software-only. Naming them as profile fields makes the software case just one configuration.

---

## 3. Profile schema

```yaml
# profile: research.v1
name: research
version: 1.0.0
description: |-
  Research and knowledge-building tasks: article extraction, source
  synthesis, citation-backed summarization. Writes target document sections,
  not code files.

# Scope kind for write_scope. code = file paths, document = (path, section)
# tuples, artifact = (path, id) tuples.
write_scope_kind: document

# What the impl stage produces. code_change = diff; document_edit = section
# rewrite; artifact_set = any structured output.
impl_output_kind: document_edit

# Ordered verifier chain. Each entry references a verifier by name + version.
# De-duplication key is the name; order matters for stop-on-first-fail.
verifier_chain:
  - name: citation-presence
    version: ^1.0
    on_fail: hard
  - name: source-freshness
    version: ^1.0
    on_fail: soft
  - name: rubric-check
    version: ^2.0
    on_fail: hard

# Review stage kind; must match the capabilities of the named skill.
review_kind: rubric-review
review_skill: rubric-review@^1.0

# Which KG backend review consults. Profiles that use CRG declare it
# explicitly so the pipeline can precondition graph availability.
graph_backend: citation-graph

# Impact-radius kind. Drives how "what changed" is computed for the review
# stage and for cross-task conflict detection.
impact_radius_kind: citation

# Defaults for the impl stage. Orchestrator may override per-task.
impl_defaults:
  allowed_tools: [read, write, web-fetch]
  context_packs: [research-style-guide, source-library-index]
  model_preference: [opus, sonnet]

# Metadata for discovery and explain commands.
metadata:
  domain: non-code
  stability: draft
  maintainer: dot-agents/core
```

### 3.1 Field semantics

| Field | Required | Type | Notes |
|---|---|---|---|
| `name` | yes | string | Unique within a source; resolves the profile ref |
| `version` | yes | semver | Bumps follow the rules in §6 |
| `write_scope_kind` | yes | enum | `code \| document \| artifact` |
| `impl_output_kind` | yes | enum | `code_change \| document_edit \| artifact_set` |
| `verifier_chain` | yes | array | Ordered; each entry has `name`, `version`, `on_fail` |
| `review_kind` | yes | enum | `code-review \| rubric-review \| citation-review \| custom` |
| `review_skill` | yes | skill-ref | Skill that implements the review kind |
| `graph_backend` | yes | enum | `crg \| citation-graph \| document-cross-ref \| none` |
| `impact_radius_kind` | yes | enum | `symbol \| section \| citation \| custom` |
| `impl_defaults` | no | object | Model preference, allowed tools, context packs |
| `composes` | no | array | Composite case — see §4 |
| `metadata` | no | object | Free-form; non-load-bearing |

### 3.2 Composite profile schema addition

```yaml
name: po-core-api-se
version: 1.0.0
composes:
  - api@^1.0
  - batch@^1.0
  - streaming@^1.0

# Composite must resolve any field where children disagree. If all children
# agree on review_kind, the composite may omit it.
review_kind: code-review
graph_backend: crg
impact_radius_kind: symbol

# Composite-local additions (not inherited) — appended after the composed
# chain in declaration order.
additional_verifier_chain:
  - name: webhook-replay
    version: ^1.0
    on_fail: hard
```

---

## 4. Composite profiles and resolution

### 4.1 Verifier chain resolution

```
resolve(composite):
  chain = []
  for child in composite.composes (in declaration order):
    for entry in resolve(child).verifier_chain:
      if entry.name not in chain-by-name:
        chain.append(entry)
      else:
        pick higher version; if incompatible, error
  for entry in composite.additional_verifier_chain:
    if entry.name not in chain-by-name:
      chain.append(entry)
    else:
      error: composite cannot add an entry that duplicates a composed one
  return chain
```

### 4.2 Field agreement rules

| Field | Rule when children disagree |
|---|---|
| `review_kind` | Composite must declare its own; error if omitted |
| `graph_backend` | Composite must declare; error if omitted |
| `impact_radius_kind` | Composite must declare; error if omitted |
| `write_scope_kind` | Must agree across all children; error if disagree |
| `impl_output_kind` | Must agree; error if disagree |

**Why `write_scope_kind` and `impl_output_kind` cannot be resolved at composite level:** they are structural. A profile that composes a `code` child and a `document` child produces tasks whose write_scope is not well-defined. Such compositions are schema errors, not silently-resolved merges.

### 4.3 Version range conflicts inside composition

If two composed children require the same verifier at incompatible version ranges (e.g., `child-a: citation-presence@^1.0` and `child-b: citation-presence@^2.0`), resolution errors. The composite author must either:

- Update one child to align ranges, or
- Fork one of the verifiers into a new named verifier (`citation-presence-strict@^2.0`)

The tool does not silently pick one.

---

## 5. Artifact-scope generalization (non-code)

### 5.1 Scope shapes

| `write_scope_kind` | Entry shape | Example |
|---|---|---|
| `code` | `"path/to/file.go"` | `"internal/auth/middleware.go"` |
| `document` | `{path, section}` | `{path: "research/article.md", section: "H2:Methodology"}` |
| `artifact` | `{path, artifact_id}` | `{path: "design.fig", artifact_id: "frame:checkout-v2"}` |

### 5.2 Conflict detection

The workflow-parallel-orchestration spec's write-scope conflict detection generalizes: two tasks conflict if their `artifact_scope` entries overlap under the scope-kind-specific comparator.

- `code`: file path overlap (existing behavior)
- `document`: same `path` AND section path prefix overlap (e.g., `H2:Methodology` conflicts with `H2:Methodology > H3:Data` but not with `H2:Results`)
- `artifact`: same `path` AND same `artifact_id`

### 5.3 Impact radius generalization

Impact radius under non-code profiles:
- `section`: what other sections cite or are cited by the edited section (document-cross-ref graph)
- `citation`: what downstream documents rely on the modified citation (citation graph)
- `custom`: profile-defined; review skill must implement a matching radius query

### 5.4 Verifier signature contract

Verifiers receive the typed `artifact_scope` and the `impl_output_kind`. A verifier declared with `applies_to: [document]` errors if invoked on a `code` scope. This prevents accidentally running `go test` against a research document.

---

## 6. Verifier evolution and behavior preservation

This is the core mechanism that prevents silent regression when a verifier changes. The rules below apply to both verifier entries and profile bundles.

### 6.1 Version bump semantics

| Change | Required bump | Consumer impact |
|---|---|---|
| Typo / comment / non-behavior | patch | auto-upgrade under `^` |
| Add new optional check (off by default) | patch | no behavior change |
| Add new opt-in check (off by default) | minor | consumer opts in |
| Widen accept set (previously-failing inputs now pass) | minor | consumer may see fewer failures |
| Tighten accept set (previously-passing inputs now fail) | **major** | consumer must migrate or pin |
| Rename / remove a check | **major** | consumer must migrate or pin |
| Change output schema | **major** | consumer must migrate or pin |

**Why widening is minor and tightening is major:** widening can be safely absorbed — consumers that were green stay green, consumers that were red may turn green (never the other way). Tightening is the regression vector — a consumer on `^1.0` sees green builds turn red without changing their code.

### 6.2 The behavior-preservation gate

A major version bump of a verifier or composite profile **must** pass the behavior-preservation gate before publishing:

1. Collect a **corpus** of recent task runs that ran the pre-bump version. The corpus is defined in the profile's `metadata.behavior_corpus_ref` (a git ref, KG query, or explicit list of task ids).
2. Re-run the new version against the same corpus inputs.
3. **Diff outcomes** per task: pre-bump verdict vs. post-bump verdict.
4. Any outcome that changes from pass → fail (regression) blocks the bump unless explicitly justified in the profile's `migration_notes` field with one of the allowed reasons (`intentional_tightening`, `corpus_was_wrong`, `manual_review_approved`).
5. Outcomes that change from fail → pass (relaxation) are reported but do not block.

The gate is enforced by `da packages publish verifier` (once that command lands) and by CI on the profile source repo. It is a contract between maintainer and consumers, not a free-form recommendation.

### 6.3 Deprecation pathway

A verifier entry in a profile can be marked deprecated:

```yaml
verifier_chain:
  - name: legacy-style-check
    version: ^1.0
    on_fail: soft
    deprecated:
      since_version: 2.0.0
      removal_target_version: 3.0.0
      migration_note: "Replaced by style-check@^2.0 which supports TS strict mode"
```

- `deprecated.since_version` emits a warning event (`verifier.deprecated.invoked`) on each run
- `deprecated.removal_target_version` is the profile version in which the entry is removed
- Consumers have the interval between `since_version` and `removal_target_version` to migrate

The profile's CHANGELOG (co-located with the YAML) records the deprecation chain.

### 6.4 Consumer pin ergonomics

Consumers pin profiles the same way they pin packages:

```json
"app_type": "go-http-service@^1.2",
"app_type": "go-http-service@pinned:sha256:abc...",
"app_type": "po-core-api-se@1.0.0"
```

- `^1.2` — auto-upgrade across minor/patch; block at next major
- `pinned:sha256:...` — immutable, requires manual update
- `1.0.0` — exact tag; minor/patch upgrades require explicit edit

Default when the consumer writes just `app_type: go-http-service`: resolves to the latest **non-prerelease** version the source offers, emits a warning that a version pin is recommended.

### 6.5 Why this matters for this codebase

The user's explicit concern — "need to consider how to update verifier while maintaining behavior" — is exactly §6.2. Without the gate, changing the payout go-http-service coverage threshold from 70 to 80 silently fails every Go service that was green at 75. With the gate, the change requires a major bump, the behavior diff surfaces in CI, and consumers either pin to `^1` until they raise coverage or accept the bump in an explicit PR.

---

## 7. Profile discovery and pipeline integration

### 7.1 Resolution pipeline

1. `workflow orient` reads the repo's `.agentsrc.json`, finds the `app_type` (a profile ref in the form `source-id:profile-name@version-spec` or bare-name resolving against the default source).
2. Pass 1 of config resolution (per config-distribution-model §6) fetches the profile YAML from its source, validates against the profile schema, and resolves composition recursively.
3. The resolved profile is cached in the effective config.
4. `workflow eligible` and `workflow fanout` read `verifier_chain` and `write_scope_kind` from the resolved profile.
5. The orchestrator writes the resolved `profile_ref` and `profile_digest` into the delegation bundle so the worker runs against the same resolution.

### 7.2 `da config explain app_type`

```
$ da config explain app_type

Field:   app_type
Value:   go-http-service@^1.2
Resolved: go-http-service@1.2.4 (digest: sha256:abc...)

Layer stack:
  [3] payout:org/base              → not set
  [4] payout:lang/go-service       → go-http-service@^1.0
  [5] payout:app/po-core-api-se    → po-core-api-se@1.0.0   ← active (composite)
  [6] repo-local .agentsrc.json    → not set

Composite expansion:
  po-core-api-se@1.0.0 composes:
    - api@1.1.2
    - batch@1.0.5
    - streaming@1.0.1

Verifier chain (resolved):
  unit (from api@1.1.2)
  contract (from api@1.1.2)
  integration (from batch@1.0.5)
  lease-retry (from batch@1.0.5)
  stream-replay (from streaming@1.0.1)
  webhook-replay (composite additional)
```

### 7.3 Orchestrator precondition check

Before fanout, the orchestrator asserts:

- The resolved profile's `graph_backend` is available in the current environment (e.g., `crg` requires a CRG build present; `citation-graph` requires the citation-graph MCP server)
- Every referenced verifier is installed or resolvable
- Every referenced skill is present

Preconditions failing surface as a single error with the specific missing dependency, not as opaque runtime failures during fanout.

---

## 8. Worked examples

### 8.1 Baseline `go-http-service`

```yaml
name: go-http-service
version: 1.2.4
write_scope_kind: code
impl_output_kind: code_change
verifier_chain:
  - { name: unit, version: ^1.0, on_fail: hard }
  - { name: lint, version: ^1.0, on_fail: hard }
  - { name: coverage, version: ^1.1, on_fail: soft }
review_kind: code-review
review_skill: review-pr@^1.0
graph_backend: crg
impact_radius_kind: symbol
impl_defaults:
  allowed_tools: [read, edit, bash, grep, glob]
  model_preference: [sonnet, opus]
```

### 8.2 Composite `po-core-api-se` (api + batch + streaming)

See §3.2 — the composite declares `composes: [api, batch, streaming]`, resolves `review_kind: code-review` and `graph_backend: crg` (all children agree), and adds `webhook-replay` as a composite-local verifier. The resolved chain is the union of the three children's chains plus `webhook-replay`.

Each child profile is published independently. An update to `streaming@1.0.1 → 1.1.0` (adding a new stream-replay variant) propagates to po-core-api-se on next resolution without requiring po-core-api-se to bump — so long as streaming's minor bump does not break the behavior-preservation gate.

### 8.3 `research` (non-code pressure test)

See §3 — full schema shown there. Key differences from code profiles:

- `write_scope_kind: document` — tasks edit document sections, not files wholesale
- `verifier_chain` uses citation/source/rubric verifiers instead of test/lint/coverage
- `graph_backend: citation-graph` — review consults a citation graph built from the source library, not CRG
- `impact_radius_kind: citation` — "what does my edit affect" is answered in terms of downstream citers, not call-site symbols

This profile drives a research workflow where: orchestrator fans out "rewrite section 3.2" and "expand section 4.1" tasks in parallel; conflict detection enforces no overlap on section paths; verifier chain ensures every cited source exists in the library, is recent enough, and the rewritten section passes the rubric; review consults the citation graph to flag any citation edges the rewrite broke.

### 8.4 `resume-ideation` (second non-code pressure test)

```yaml
name: resume-ideation
version: 0.1.0
write_scope_kind: artifact
impl_output_kind: artifact_set
verifier_chain:
  - { name: evidence-grounding, version: ^1.0, on_fail: hard }
  - { name: star-format-check, version: ^1.0, on_fail: hard }
  - { name: jd-signal-coverage, version: ^1.0, on_fail: soft }
  - { name: llm-judge, version: ^1.0, on_fail: soft }
review_kind: custom
review_skill: resume-review@^0.1
graph_backend: document-cross-ref
impact_radius_kind: custom
impl_defaults:
  allowed_tools: [read, write]
  context_packs: [resume-experience-library, target-jd]
```

This profile drives ResumeAgent's ideation stage specifically: each "generate candidate bullet" task writes into the artifact set (not files); verifiers check that every bullet cites an evidence entry from the experience library, matches STAR format, and covers signals from the target JD; review runs a custom skill that evaluates the draft against the JD using the document-cross-ref graph (library ↔ bullet ↔ JD signal).

Crucially, **ResumeAgent does not need to adopt this profile at spec time**. The worked example validates the abstraction against a real non-code workflow; ResumeAgent adopts once its ideation surface stabilizes.

### 8.5 Pressure-test outcomes (what each example stresses)

| Example | Stresses |
|---|---|
| `go-http-service` | Baseline code path; ensures no regression vs today |
| `po-core-api-se` composite | Composition resolution, field agreement rules, composite-additional entries |
| `research` | Non-code `write_scope_kind: document`; section-path conflict detection; citation-radius review |
| `resume-ideation` | Non-code `write_scope_kind: artifact`; custom review kind; document-cross-ref graph |

If all four resolve correctly through the schema and produce valid delegation bundles, the abstraction holds.

---

## 9. Migration from `app_type_verifier_map`

### 9.1 Today's shape

Today, `config-distribution-model §10` shows:

```yaml
app_type_verifier_map:
  go-http-service: [unit, api, integration]
```

This is a flat name → list-of-verifier-names map, without versions, without review, without graph backend.

### 9.2 Migration steps

1. **Introduce profile files** alongside the existing map. Each key in the map gets a corresponding profile file with `verifier_chain` populated from the list, `review_kind: code-review`, `graph_backend: crg`, `impact_radius_kind: symbol`, `write_scope_kind: code`, `impl_output_kind: code_change`. Version: start at `0.1.0` to signal draft status.
2. **Dual-read phase**: the orchestrator reads the profile if present; falls back to the map entry if not. No consumer change required.
3. **Consumer opt-in**: repos add `app_type: go-http-service@^0.1` to their `.agentsrc.json`. Repos without an explicit `app_type` continue using the map until they migrate.
4. **Deprecation of the map**: once all active repos reference a profile, `app_type_verifier_map` is marked deprecated. `da doctor` warns on its presence. Removal after one release cycle.

### 9.3 Non-regression constraint

Step 1's profile files must produce byte-identical verifier invocations to the pre-migration map. Any divergence is itself a major bump and must pass the §6.2 behavior-preservation gate. This is the highest-risk migration point and should be guarded by a corpus run against recent task history.

---

## 10. Open questions

### Q1: Should profile composition allow override-with-justification?

§2.2 forbids override. This is clean but may be too strict for legitimate cases (e.g., a team wants to tighten a child's coverage threshold without forking). An alternative: allow override only if the overriding profile declares `override_justification:` and the change passes its own behavior-preservation gate against a larger corpus. Leaving this open because the "no override, fork instead" rule is easier to reason about and we can relax later.

### Q2: How are verifier skills themselves distributed?

A profile references verifiers by name + version. Where those verifiers live (local skills, git-sourced skills, OCI packages) and how they are resolved is **not specified here**. It interacts with external-agent-sources §5 (packages) and config-distribution-model §4 (source/tier constraints). A separate document should specify "verifier package contract" once this spec stabilizes — probably an addition to external-agent-sources.

### Q3: Behavior corpus storage and privacy

§6.2's behavior-preservation gate requires a corpus of prior task runs. For research/writing/ideation domains, those runs contain drafts, sources, and possibly proprietary content. Where is the corpus stored? What access does CI need? Suggest: corpus lives in the same source as the profile (git source for git-distributed profiles; OCI for OCI-distributed); access control matches the source's auth. Needs confirmation.

### Q4: Profile discovery across scopes

If a repo's `app_type` names a bare profile (`go-http-service`) and the source is inherited from a team layer, which version resolves? The team layer's pin? A latest-compatible? Config-distribution-model's layer precedence applies but the specific resolution for package refs in inherited layers needs explicit specification. Likely resolution: inherited layer's pin wins; repo-local `app_type` can override to `@pinned:...` for reproducibility.

### Q5: Can a single task span multiple profiles?

A task that touches both code and docs (e.g., "add feature + update docs") could conceptually want both `go-http-service` and `research` verifier chains. Current spec forces one `app_type` per repo. An alternative: tasks can declare `profile_override` to pull in additional verifier chains for that task. Left open — may be solved by adding a `docs-update` composite that includes both chains.

### Q6: Prerelease versions and the behavior gate

Prerelease tags (`1.2.0-rc.1`) should be exempt from the behavior-preservation gate (by definition experimental) but consumed only under explicit opt-in (`app_type: go-http-service@1.2.0-rc.1`). `^1.2` should not resolve to a prerelease. Standard semver semantics suffice but should be called out in the consumer documentation.

---

## Completeness note

This spec is the first of two that ground the payout agent-config and non-software generalization work. The companion spec — [cross-app-dependency-impact](../cross-app-dependency-impact/design.md) — is not yet written; it will describe how changes to one profile or one repo propagate through the dependency graph to affected repos, sharing the profile vocabulary defined here.
