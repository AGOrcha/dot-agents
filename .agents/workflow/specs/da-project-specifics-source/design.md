# Dot-agents Project-Specifics Source — Design Spec

**Status:** draft (2026-06-25)
**Scope:** project (dot-agents) — dogfoods the sources/layers model on the project itself
**Written:** 2026-06-25

**Related (canonical owners — this spec REFERENCES, never re-states):**
- [config-distribution-model §15](../config-distribution-model/design.md#15-coherence-model-v2-scopes-sources-units-and-the-lock) — **canonical** sources / scopes / units / lock / `EnsureResolved` model. This spec consumes §15; it does **not** re-decide the lock, the units model, `inputs_digest`, or scope precedence. PR #157 (`workflow/config-area-coherence`) is reconciling the config-area specs against shipped code — this spec stays downstream of §15 and flags overlap rather than diverging.
- [app-type-profiles §8A/§8B](../app-type-profiles/design.md) — the generic starter profiles and the dot-agents `graph-knowledge` / `da-obs-service` profiles this source *layers in*. That spec owns the profile **shape**; this spec owns *which profiles dot-agents adopts and where they live*.
- [org-config-resolution §4/§7](../org-config-resolution/design.md) — scope precedence and merge categories that govern how the project layer composes with user-local and runtime scopes.
- [external-agent-sources §5](../external-agent-sources/design.md) — Tier-2 artifact (skill/agent/verifier) packaging the project-specific executable units resolve through.

---

## Table of contents

1. [Problem statement](#1-problem-statement)
2. [What "project-specifics" means here](#2-what-project-specifics-means-here)
3. [Decisions](#3-decisions)
4. [Source shape and composition](#4-source-shape-and-composition)
5. [Migration of inline `.agentsrc.json` specifics](#5-migration-of-inline-agentsrcjson-specifics)
6. [Verification and coherence](#6-verification-and-coherence)
7. [Thin plan outline](#7-thin-plan-outline)
8. [Open questions](#8-open-questions)

---

## 1. Problem statement

The dot-agents starter config should ship **generic**: a new project that runs
`da init` should get sensible app-types, starter skills, and starter rules —
nothing dot-agents-internal. But the dot-agents repo's own `.agentsrc.json`
today carries **its own project-specifics inline** — the `execution_profile`
app-types tuned for *this* project (`go-cli`, `ideation`, `docs`), the
`stage_profiles` verifier/reviewer prompt-file bindings *this* project uses,
project-pinned `skills`/`agents`/`rules`, and (per
[app-type-profiles §8A/§8B](../app-type-profiles/design.md)) the forthcoming
`graph-knowledge` and `da-obs-service` profiles that are meaningful only here.

This conflates two things the §15 model deliberately separates: **generic
starter policy** (what ships) and **one project's layered specifics** (what
dot-agents adds on top). Payout already demonstrates the intended pattern — a
generic base plus `payout:lang/go-service` and `payout:app/po-core-api-se`
layers it extends (config-distribution-model §1, app-type-profiles §7.2). The
dot-agents repo should **dogfood the same pattern on itself**: ship generic,
then layer in its specifics from its own config **source**.

The payoff is concrete: (a) the starter no longer leaks dot-agents internals;
(b) a bump to a project profile is a visible, versioned edit in one source, not
a diff buried in the repo-local manifest; (c) dot-agents validates the
sources/extends/units machinery against a real consumer — itself — before
asking other projects to.

## 2. What "project-specifics" means here

The set of things this source gathers, with their current inline home and their
canonical shape owner:

| Project-specific | Current inline home (`.agentsrc.json`) | Shape owner |
|---|---|---|
| Project app-type profiles (`go-cli`, `ideation`, `docs`) | `execution_profile.by_app_type` | app-type-profiles (§3 + §8A view) |
| Dot-agents `graph-knowledge` profile | *not yet present* | app-type-profiles §8B |
| Dot-agents `da-obs-service` composite | *not yet present* | app-type-profiles §8A.6 |
| Verifier/reviewer stage bindings | `stage_profiles.{verifier,reviewer}` | stage-profile-and-routing-consolidation |
| Project-specific (non-starter) skills | `skills` (the project entries beyond the starter set) | external-agent-sources §5 (Tier 2) |
| Project-specific agents | `agents` | external-agent-sources §5 (Tier 2) |
| Project rules / settings overlays | `rules`, `settings` | org-config-resolution merge categories |

**Out of scope (stays where it is):** `repo_id` / `project` (protected fields,
config-distribution-model §6 — never sourced); the `local` operational state and
fetch cache (§15 D13 — never a scope, never projected); secrets/auth (r5 §D5.3
principle — `~/.config/da/`, never AGENTS_HOME, never this source).

## 3. Decisions

Each decision cites the canonical model it consumes; none re-decide it.

### D1 — Dot-agents ships generic; its specifics live in a `da` config source

The starter `.agentsrc.json` that ships with `da init` carries only the
**generic** starter set (the §8A generic profiles, the starter skills/rules).
Dot-agents' own repo-local `.agentsrc.json` `extends` a project source that
layers in this repo's specifics. This is the §15 D1 scope model applied to the
project itself: the project source is a **scope** (`repo-imported`, per §15 D1's
precedence chain product → user-local → org → team → **repo-imported** →
repo-local → project-local overlay → runtime), resolved through a **source**
(D2), carrying **layer**-kind units (D3).

*Why:* the alternative — keep specifics inline and just *document* that they are
project-specific — is the half-old/half-new conflation §15.1 names as the root
incoherence. Dogfooding the source model is the point.

*Rejected:* a fourth top-level "project_overrides" field — that invents a new
slot for what `extends` + a source already expresses (D2 unifies addressing).

### D2 — The project source is `local` first, git-promotable

Per §15 D6, the `local` source (`~/.agents/`, git-backed, auto-bootstrapped) is
sufficient for v1: the dot-agents project layer is authored into the `local`
source working tree and resolves via `local:<path>@<ref>`. When/if the project
profiles should be shared (e.g. published as a `dotagents-builtin` starter
layer per app-type-profiles Q10), the *same* layer is promoted to a `git` (or
`oci`, per §15 D15) source with **no shape change** — only the `source-id` and
`type` change. D2 of §15 makes local-authored and remote-sourced units the same
kind of resolvable unit; this spec relies on that, it does not re-establish it.

*Why local-first:* dot-agents *is* the `local` source repo for its maintainer;
the project specifics are already in this git tree. A git source pointing at the
same repo would be redundant indirection at v1.

*Owner-needed gate:* whether to publish a public `dotagents-builtin` starter
source (app-type-profiles Q10) is an owner decision — see §8 Q1.

### D3 — Composition is `extends` + the §15 merge categories; no new merge rule

The project layer composes via the existing `extends` array and the
org-config-resolution §7.2 merge categories. App-type profiles merge by name
(a project `graph-knowledge` profile adds to the generic set; a project override
of `go-cli` replaces by the category rule for that field). The protected-field
guard (config-distribution-model §6 step 5) drops any attempt by the layer to
set `repo_id`/`project`. This spec adds **zero** new merge semantics — it is a
consumer of §7.2.

*Why:* SoT. The merge model has one owner; re-stating it here would create a
drift surface (the exact failure the governing lesson describes).

### D4 — Units are tracked in the one §15 lock; no separate project lock

The project layer and any project-specific Tier-2 artifacts it declares are
tracked in the single `units` lock section (§15 R1/R7), keyed by
`source:path@version → {kind, digest, ...}`, with the layer's content hashed
into `inputs_digest` (§15 D4/D5). There is **no** project-specific lockfile and
**no** clock-driven staleness — §15 R2 governs. `da config explain` /
`config verify` read this layer's provenance through the same `units` surface
(§15 R3).

### D5 — Project-specific skills/agents are Tier-2 artifacts the layer declares

The non-starter skills (the dot-agents-only ones: `orchestrator-session-start`,
`loop-worker`, `isp`, `plan-wave-picker`, `iteration-close`, `release-cut`, etc.)
and project agents are **artifacts** (§15 D3 `kind: artifact`), declared *by*
the project layer (config-distribution-model §2: "An org or team layer may inject
package declarations that every repo inheriting that layer automatically
receives"). The starter ships only the genuinely-generic skills; the project
layer injects the rest. Provenance is the §15 D7 rule: git-tracked-in-local ⇔
local-authored.

## 4. Source shape and composition

### 4.1 The two layers

```
generic starter (.agentsrc.json shipped by da init)
  └─ generic app-types (§8A: http-service, api, ui, streaming, batch, db)
  └─ starter skills/rules (the cross-project ones)

dot-agents project source layer  (local:da-project/base, git-promotable)
  └─ app-type overrides + additions:
       go-cli, ideation, docs            (this project's tuned shapes)
       graph-knowledge                    (§8B — project-specific)
       da-obs-service                     (§8A.6 composite — R-series host)
  └─ stage_profiles bindings              (this project's verifier/reviewer prompt files)
  └─ project skills (artifacts)           (orchestrator-session-start, isp, loop-worker, …)
  └─ project agents (artifacts)
  └─ project rules / settings overlay
```

### 4.2 How dot-agents' repo-local manifest references it

```json
{
  "version": 2,
  "project": "dot-agents",
  "sources": [
    { "id": "da-project", "type": "local" }
  ],
  "extends": [
    "da-project:da-project/base"
  ]
}
```

Resolution: §15 D10 `EnsureResolved` computes `inputs_digest` over the local
scopes (which now include the project layer), re-resolves if stale, rewrites the
one lock, and projects outputs exact/prune. The repo-local manifest shrinks to
near-empty — its specifics have moved into the layer. (`.agentsrc.json` and
`.agentsrc.lock` stay committed per §15 D14; the layer's own source tracking
follows §15 D7.)

### 4.3 The git-promotion path (forward door, not v1)

If app-type-profiles Q10 resolves to "publish a starter source," the *generic*
profiles graduate to a `dotagents-builtin` git/oci source (§15 D15 allows oci
for `extends`), and the **project** layer stays local (or moves to a private
`git` source). The split — generic published, specifics private — is exactly the
payout topology (`org/base` shared, `app/po-core-api-se` repo-scoped). No shape
change is required to take this door; only `sources[].type` + the ref.

## 5. Migration of inline `.agentsrc.json` specifics

A non-destructive, dual-read-friendly migration mirroring the
config-distribution-model §15 / app-type-profiles §9 ethos (move, don't rewrite;
prove byte-parity).

1. **Author the layer.** Create `da-project/base` in the `local` source carrying
   the project's current `execution_profile.by_app_type` (`go-cli`, `ideation`,
   `docs`), `stage_profiles`, and project `skills`/`agents`/`rules` — copied
   verbatim from the current inline manifest. No behavior change yet.
2. **Add the source + extends.** Add the `da-project` source and the `extends`
   entry to the repo-local `.agentsrc.json` (§4.2). The layer now resolves *and*
   the inline copy still exists — both present.
3. **Verify parity.** `da config explain` must show the **same** effective
   `execution_profile` / `stage_profiles` / skills as before the layer existed
   (the layer and the inline copy are identical, so the merge is a no-op
   superset). This is the parity gate — any diff is a migration bug.
4. **Remove the inline copies.** Delete the now-redundant `execution_profile`,
   `stage_profiles`, project skills/agents/rules from the repo-local manifest.
   `da config explain` must show **byte-identical** effective config to step 3
   (now the layer is the only source of those values). The manifest is reduced
   to §4.2.
5. **Add the new profiles.** Layer in `graph-knowledge` (§8B) and
   `da-obs-service` (§8A.6) — these are *additions*, not migrations, so they
   land after parity is proven.
6. **Lock + gitignore.** `da install` rewrites the single `units` lock (§15
   D10/R1); §15 D14 `.gitignore` auto-fill keeps `.agentsrc.{json,lock}`
   committed and managed outputs ignored. Re-run converges (§15 R5).

The parity gates (steps 3 and 4) are the migration's teeth — they prove the
move changed *location*, not *behavior*, the same discipline app-type-profiles
§9.3 applies to the verifier-map migration.

## 6. Verification and coherence

- **Markdown / schema:** the layer validates against the AgentsRC layer schema
  (config-distribution-model §6 step 3); profiles in it validate against the
  app-type-profiles §3 schema.
- **No concern duplicated-and-divergent (governing lesson):** this spec states
  *which* specifics dot-agents adopts and *where they live*; it points at
  app-type-profiles for profile shape, config-distribution-model §15 for the
  source/units/lock model, org-config-resolution §7 for merge, and
  external-agent-sources §5 for artifact packaging. It restates none of them.
- **Parity gate:** §5 steps 3–4 are the runtime check that the migration is
  behavior-preserving.
- **PR #157 overlap flag:** PR #157 is reconciling the config-area specs against
  shipped code (`app_type_verifier_map`/`verifier_profiles` superseded by
  `execution_profile`/`stage_profiles`). This spec's migration (§5) moves those
  *shipped* fields into a layer — it is **downstream** of #157's reconciliation,
  not in conflict. If #157 changes the field names, §5 follows; the field shapes
  are owned there, not here.

## 7. Thin plan outline

A focused plan (`.agents/workflow/plans/da-project-specifics-source/`), small
because the §15 machinery is already implemented-in-tree (config-distribution-model
§15.8: `inputs_digest` / `units` / `EnsureResolved` built but unwired). This plan
is **wiring + authoring**, not greenfield.

| Task | Write-scope | Gate |
|---|---|---|
| `t1-author-base-layer` | the `da-project/base` layer in the `local` source (verbatim copy of current `execution_profile`/`stage_profiles`/project skills/agents/rules) | layer validates against AgentsRC layer schema |
| `t2-wire-extends` | dot-agents repo-local `.agentsrc.json` (add `da-project` source + `extends`) | `da config explain` resolves the layer; provenance shows it as winning layer |
| `t3-parity-prove` | none (verification) | effective config byte-identical pre/post layer (§5 step 3) |
| `t4-remove-inline` | dot-agents repo-local `.agentsrc.json` (delete the migrated blocks) | effective config byte-identical to t3 (§5 step 4) |
| `t5-add-new-profiles` | the `da-project/base` layer (add `graph-knowledge` §8B + `da-obs-service` §8A.6) | profiles validate against app-type-profiles §3 schema; resolve through composition (§4.1) |
| `t6-lock-gitignore-converge` | run `da install`; verify `.gitignore` auto-fill + lock | re-run yields no diff (§15 R5); `.agentsrc.{json,lock}` committed (§15 D14) |

Dependencies: t1→t2→t3→t4 strictly serial (parity discipline); t5 after t4; t6
last. No code is authored — t1/t5 author config, t2/t4 edit the manifest, t3/t6
verify.

## 8. Open questions

### Q1 — Publish a public `dotagents-builtin` starter source? (owner-needed)

Whether the **generic** starter profiles (§8A) graduate from documented-in-spec
to a published `dotagents-builtin` git/oci source is an owner decision (also
app-type-profiles Q10). It gates whether §4.3's promotion is local-stays-local or
generic-goes-public. The project-specifics source (this spec's subject) is
unaffected either way — it stays local/private. **Owner-needed.**

### Q2 — Which skills are "generic starter" vs "project-specific"? (autonomous, confirm)

§5 step 1 moves the project skills into the layer, but the generic/project split
needs a concrete list. Lean: workflow-orchestration skills tied to *this*
project's loop machinery (`orchestrator-session-start`, `isp`, `loop-worker`,
`iteration-close`, `plan-wave-picker`, `provider-consumer-pair`, `release-cut`)
are project-specific; broadly-useful ones (`agent-handoff`, `agent-start`,
`self-review`, `review-pr`, `skill-architect`, `build-graph`, `review-delta`)
are starter. **Autonomous** with a confirm pass at t1.

### Q3 — Does the project layer carry `stage_profiles` or reference a shared one? (autonomous)

`stage_profiles` prompt-file bindings (e.g. `verifiers/unit.project.md`) are
inherently project-specific (the `.project.md` suffix says so). Lean: the layer
carries them. If the starter ever ships generic `stage_profiles` (the
non-`.project` halves), the layer would override only the `.project` files. Defer
until a generic stage-profile set exists. **Autonomous.**

### Q4 — `local` source `da-project` path layout (autonomous)

Where in the `local` source tree does `da-project/base` live, and how does it
relate to the source's auto-bootstrap (§15 D6)? Lean: a `da-project/` layer
directory in the `~/.agents/` working tree, tracked by the `local` source's git
(§15 D7, local-authored ⇒ git-tracked). **Autonomous**, follows §15 D6/D7
mechanics directly.
