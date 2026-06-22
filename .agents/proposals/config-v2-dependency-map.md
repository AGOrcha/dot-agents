# Config v2 Migration — Dependency Map and Consumer Sequencing

> **⚠️ SUPERSEDED (2026-06-22) by `config-distribution-model` §15.** This map (2026-05-27)
> predates the §15 redesign that collapsed the Tier-1 `config` / Tier-2 `packages` wall into
> one `units` model (D3): one lock `units` section + `inputs_digest`, one CRUD surface via
> `--scope`/`--source` (**no parallel `da packages` command tree**), OCI as one source among
> many (D8), and two-pass resolution surviving only as dependency *order*, not a tier wall.
> The "decided" rows below that assert the two-tier model, the `da packages` subtree, a
> config+packages two-section lock, and pass-2-as-tier-wall are **no longer accurate** — see
> §15 (D1–D14) for the canonical contract. The plan was reconciled accordingly: migration
> task `p6-packages-pass2-cli` is **cancelled** (its surviving artifact-resolution mechanic
> folded into `config-v2-coherence/ce-unified-artifact-sourcing`), `p4d-da-install-repurpose`
> is **deferred** to a separate plan, and `p7`/`release-minor`/`ce` were re-pointed off the
> ghost `p6` onto `p4f` (units-lock wiring). Read this doc only for the consumer-sequencing
> history; do not treat its decision tables as current.

**Status:** SUPERSEDED by config-distribution-model §15 (was: project-local planning artifact)
**Date:** 2026-05-27 (superseded 2026-06-22)
**Author:** drafted to accompany canonical plan `config-v2-migration`
**Pairs with:**
- Spec: `.agents/workflow/specs/config-distribution-model/design.md`
- Spec: `.agents/workflow/specs/org-config-resolution/design.md`
- Canonical plan: `.agents/workflow/plans/config-v2-migration/PLAN.yaml`
- Codex examination: `.agents/proposals/codex-019e6245-examination-and-sequenced-plan.md`

---

## 1. Spec status check — is v2 implementable now?

**Verdict: YES, Phase 0 → Phase 6 are implementable today.** Phase 7 (deprecation) and
some addenda inherit later-track open questions, but none block authoring the canonical
plan or starting Phase 0.

### Decisions already locked

| Decision | Where it lives | Notes |
|---|---|---|
| Two-tier model (config layers vs executable packages) | config-distribution-model §1–§2 | Industry-aligned (Renovate/ESLint vs Helm/Buf) |
| Field surface: `sources` (id+type+url+ref+cache_ttl+auth), `extends`, `packages` | §3 | Concrete JSON shape |
| Tier constraint: extends ⇒ git/http/local; packages ⇒ oci/http | §4 | Schema-time enforcement |
| Reference syntax: `source-id:layer-or-path@version-spec` | §5 | Stable |
| Two-pass resolver (pass 1 layers, pass 2 packages) | §6 | Pass 2 skipped if no packages |
| Lockfile shape (`.agentsrc.lock` with config + packages sections) | §7 | Tier-1 TTL-based; Tier-2 content-addressed immutable |
| Caching paths (`~/.agents/cache/config/...` and `.../packages/<digest>/`) | §8 | Determined |
| Audit event taxonomy (6 config.* events) | §9 | Determined |
| `da config explain` contract (single field + --all + --flags + --value-only + --origin-only + --json) | §10 + proposal config-explain-live-surface.md | Determined |
| Error contract categories (transport, auth, content, schema, not_found) | §11 | Determined |
| Layer model (product → user → org → team → repo-layers → repo-local → plan/task) | org-config-resolution §4 | 7-layer model |
| Merge category rules (scalar, set-union, map-merge, ordered-replace) | org-config-resolution §7.2 | Determined |
| Protected fields (`repo_id`, `project`, repo-owned overrides) | §7.4 | Determined |
| Repo identity model (`repo_id` distinct from `project`; derived from git remote at init) | §5 | Determined |
| `da refresh` is NOT aliased to `da config sync` (kept distinct) | §13.3 + codex Q4 + spec recommendation | Determined |
| `da install` is NOT yet repurposed to wrap config sync + packages install (deferred) | §13.3 | Recommend deferring to a separate plan; do not couple v2 to repurposing existing verbs |
| Signing comes earlier than originally planned | §14 Q3 human addendum | Reflected in Phase 5 stub |
| Workspace lockfile: either-or; standalone repo owns its own, workspace owns aggregate when present | §14 Q5 human addendum | Captured but not implemented in v2 critical path |

### Open questions that do NOT block implementation

| Q | Status | Why non-blocking |
|---|---|---|
| §14 Q1 (publish-time lint of config repos) | Belongs to a follow-on plan for the central config repo; v2 client always validates on fetch | Phase 4c `da config lint` covers client side |
| §14 Q2 (team layers declaring new sources, with allowlist) | Belongs to a later policy phase (post v2); allowlist primitive does not exist | Recommend deferring; document non-support in Phase 1b |
| §14 Q4 (CI-oriented JSON output for explain) | Phase 4 ships `--json`; CI-stable JSON schema decisions can iterate post-v1.5 | Document the v1 `--json` schema in `commands/config/explain.go` godoc |
| org-config-resolution §16 Q1 (source packaging boundary) | Resolved in config-distribution-model §3 (separate tier) | Already settled |
| org-config-resolution §16 Q2 (repo registry authority) | Org-level concern; v2 client just consumes `repo_id` | Out of scope for client v2 |
| org-config-resolution §16 Q3 (protected field enforcement granularity) | Phase 0 covers `repo_id` + `project`; finer grain is iterative | Captured in Phase 0 notes |
| org-config-resolution §16 Q4 (verifier_profile schema growth) | Phase 1c (optional) handles source-aware prompt_files via additive union type | Decision recorded below in §6 |
| org-config-resolution §16 Q5 (orchestration-repo artifact shape) | Org concern; doesn't bind v2 client | Out of scope |

### Open questions that DO need a human ruling before Phase 7

| Q | Required ruling |
|---|---|
| Deprecation soak window length | Recommendation: 2 minor releases between Phase 0 ship and Phase 7 (warnings) |
| When can v1 loading be REMOVED (post-Phase 7) | Recommendation: at least 6 months after Phase 7 warnings ship; deferred to a future "v1 sunset" plan |

---

## 2. Phase decomposition — rationale

Authored as 13 phase tasks in `config-v2-migration`. The split optimizes for:

- **smallest reviewable PR per phase** (no phase touches more than ~6 source files outside tests)
- **independent verification** (each phase has its own test surface; no "wait for phase N+3 to test phase N")
- **forward compatibility** (v1 keeps working until Phase 7, allowing soak)
- **value delivery at minimum** (Phases 0 + 1 + 4 already enable `da config explain` on flat config; layered fetching and packages are additive)

| Phase | Critical-path? | Value at end of phase | Why it's standalone |
|---|---|---|---|
| **p0** schema-additive-extension | YES | v2 fields parseable + validated; no behavior change | Pure schema/struct; v1 unaffected |
| **p0b** repo-id-bootstrap | YES | `repo_id` auto-derived at `da init`; identity available to later phases | Independent; pairs with p0 but no resolver dep |
| **p1** resolver-core-flat | YES | Snapshot API exists; flat (user+repo) layered provenance | Enables p4 without waiting on network fetching |
| **p1b** resolver-extends-tier1 | YES | Real layer fetching (git/http/local) + lockfile config section | Heaviest single phase; testable via fake fetcher |
| **p1c** verifier-profile-source-aware | OPTIONAL | `verifier_profiles.prompt_files` supports source-aware refs (additive union type) | Can defer to follow-on plan; recommend Option A backward-compat — see §6 |
| **p2** lockfile-doctor-status | nice-to-have | Drift visible in `da doctor`/`da status` | Consumer of p1b's lock; can ship later |
| **p3** audit-events | YES (compliance) | 6 config.* events emitted to a structured sink | Pluggable sink; r2-observability-dashboard becomes a consumer |
| **p4** config-explain-cli | YES | `da config explain` ships against flat OR layered resolver | Depends only on p1 (the snapshot API); ships value before p1b |
| **p4b** app-types-snapshot-refactor | YES | `workflow app-types` consumes shared snapshot | No user-facing contract change; preserves existing tests |
| **p4c** config-sync-lint | nice-to-have | `da config sync` / `lint` / `verify` round out the subtree | After p1b for real `sync` semantics |
| **p5** source-types-http-oci | YES (for packages) | http+oci fetchers (read-only); signing stub recorded in audit | Mocked transport in tests |
| **p6** packages-pass2-cli | YES | `da packages install/update/list/publish`; pass 2 resolution + lockfile packages section | The full distribution flow |
| **p7** v1-deprecation-warnings | gate | `da config migrate`; v1 stays loadable with warning | Defer per soak window |

### Dependency graph

```
p0 ── p0b
 │
 ├──── p1 ──── p4 ──── p4b
 │      │       │
 │      └── p1b ──── p2
 │            │  └── p4c ─┐
 │            └── p3      │
 │            │           │
 │            └── p1c     │
 │            │           │
 │            └── p5 ──── p6 ─── p7
 │                              ▲
 │                              │
 └── p4b ──────────────────────-┘
```

(p7 depends on p4b + p6 only; soak gate enforced manually before opening that PR.)

---

## 3. Codex deliverable → v2 dependency map

This table is the integration with `codex-019e6245-examination-and-sequenced-plan.md`. The
recommended PR sequence (PR-A → PR-B → PR-C → onward) is **preserved**; v2 phases insert
INTO that sequence rather than replacing it.

| Codex deliverable | v2 milestone required | Can ship against v1? | Recommended sequencing | Notes |
|---|---|---|---|---|
| **PR-A** (`lens-agents-starter` + `closeout-advance-contract-fix` + `isp-lens-count-reconcile`) | none — pure starter/scaffold edits | YES | Ship now, independent of v2 | Already merged as PR #122 per codex §1.5 and `verifier-owns-ci-watch-shift-left/LESSON.md` |
| **PR-B** (`direct-contract-create` — `da workflow contract create` + auto-materialize) | none | YES | Ship independently | Touches `commands/workflow/*` not `commands/config/*`; orthogonal to v2 |
| **PR-C** (`config-snapshot-api` + `app-types-uses-snapshot` from codex Phase 4) | **Phase 1 (p1-resolver-core-flat) AND Phase 4 (p4-config-explain-cli) AND Phase 4b (p4b-app-types-snapshot-refactor)** | NO (depends on snapshot API) | **PR-C IS Phase 1 + 4 + 4b of this plan** — re-sequence: do not ship PR-C as a standalone branch; ship it as the natural milestone of `config-v2-migration` p1→p4→p4b | Codex correctly identified the work; canonical plan owns the implementation |
| `pr-ci-verifier` integration (PR #123 lesson; new `pr-ci-watch` subagent per pr-ci-verifier-integration-audit.md) | NONE for the watcher itself; verifier_profile.prompt_files source-awareness is **p1c (optional)** | YES — watcher uses flat `verifier_type: pr-readiness` against existing `verification-result.schema.json` (no schema change per audit §4.4) | Ship watcher pre-v2; soak `prompt_files` as flat strings under v1 first; p1c only required if a later need emerges to ship verifier prompts via OCI packages | Low coupling. Watcher's terminal artifact is one new verifier_type id, not a schema change. |
| `agent-context-resolution-architecture` (planned synthesis spec) | **Phase 1 + Phase 5 (source types) minimum** | NO — dispatch contract needs source-aware skill+agent refs | Defer canonical spec authoring until p5 ships; then write the spec; THEN open its implementation plan | Doc-level work (spec authoring) can begin in parallel; CLI work waits for v2 substrate |
| `scope-routed-da-review` (proposal-routing aware `da review`) | **Phase 1 minimum** (snapshot API for scope-routed read); benefits from Phase 4 (`da config explain --scope`) | PARTIAL — project-scope-only MVP can read `.agents/proposals/*.md` without v2; full team/org needs v2 layered sources | Author canonical spec at `.agents/workflow/specs/scope-routed-da-review/design.md` (codex Phase 5 task) anytime; MVP can ship on v1; full team/org feature waits on v2 + external-agent-sources auth | Phase-1 partially-overlapping; recommend MVP first per codex |
| `verify-record-review-direct-iteration` (direct iter contract auto-materialize) | NONE | YES | Ship independently (PR-B) | Orthogonal to v2 |
| `staged-profile-dispatch-and-return-gate` (named per-stage agents + return gate) | NONE for starter agent extraction; **Phase 1 + Phase 5 (packages)** for runtime resolution of stage agent refs from packages | PARTIAL — starter overlay ships now (PR-A); runtime package-resolved refs wait for v2 | Phase-A starter work landed; pkg-resolved per-stage agents become a follow-on plan after p6 | Codex elaboration; cleanly separable |
| platform-driven-diagnostics P1–P6 (active plan) | NONE | YES | Unaffected | No `agentsrc`/`config.Load`/`app_type` deps in TASKS.yaml |
| loop-discipline-stop-hooks (active plan) | NONE for code; one `.agentsrc.json` migration mention in `tests/p6-payout-backfill.contract.md` is for payout (downstream repo) | YES | Unaffected; payout's `.agentsrc.json` migration is a separate user-scope concern | grep showed only `app_type: research` (TASKS.yaml metadata field) + a payout-side path string — neither implies v2 dep |
| r1-5-hook-enforcement-telemetry (active plan) | NONE | YES | Unaffected | Same as above; `app_type` is the TASKS.yaml dispatch field, not the v2 config field |
| graph-backend-adapter-contract (draft plan) | NONE | YES | Mentions "oci packages for ttrpg-campaign-bootstrap skill" in TASKS.yaml notes but this is sandbox/research, not a code dep | Unaffected |
| All other active plans (per `da workflow plan list`) | NONE | YES | Unaffected | grep -ln 'verifier_profile\|extends\|packages\|repo_id' against active TASKS.yaml found only metadata mentions |

### Re-sequenced codex bottom-line

Codex's "Bottom-line recommendation" (codex doc §9) becomes:

- **PR-A** — ship now (codex Phase 1 + 2 + reconcile). Independent of v2.
- **PR-B** — ship now (codex Phase 3 = `direct-contract-create`). Independent of v2.
- **PR-C** — **DO NOT ship as a standalone branch.** Instead implement via `config-v2-migration`
  phases p1 → p4 → p4b. The codex Phase 4 (`config-snapshot-api`) is exactly what p1 owns;
  codex `app-types-uses-snapshot` is exactly what p4b owns.
- **Codex Phase 5** (`scope-routed-da-review` spec) — author canonical spec anytime; project-scope
  MVP can land independently of v2; team/org scope waits for v2.
- **Codex Phase 6** (`tier-1 config-distribution`) — **NOW OWNED by `config-v2-migration` p0 + p1b + p2**.
  Codex's task table rows for `source-tier1-extension` and `repo-id-field` map to p0 + p0b.
- **Codex Phase 7+** (per-stage native agents, KG note CRUD, telemetry, OCI) — these were left
  as "follow-on plans"; OCI moves into `config-v2-migration` p5+p6, the rest become their own
  plans on their own cadence.

---

## 4. Re-sequencing recommendations for existing plans

Grep across active TASKS.yaml found **zero** tasks with a hard v1 config-API dependency that
v2 would break. Specifically:

- `app_type: research` / `app_type: go-cli` in TASKS.yaml are TASKS.yaml *task-metadata* fields
  used by `workflow fanout` for verifier dispatch; they consume `app_type_verifier_map` which is
  a v1 field that **remains valid under v2** (the field is preserved through Phase 7).
- The "oci packages" mention in `graph-backend-adapter-contract/TASKS.yaml` is a *sandbox* note,
  not a code dep on v2's packages tier.
- The payout `.agentsrc.json` migration mentioned in `loop-discipline-stop-hooks/TASKS.yaml`
  is a downstream-repo file update, not a code-level v2 dep.

**Recommendation: no active plan needs re-sequencing.** The `config-v2-migration` plan slots
in alongside existing active work without conflicts.

The one soft coupling: any active plan that adds NEW `AgentsRC` typed fields should land
**before** Phase 0 (p0-schema-additive-extension), or **after** Phase 7, to avoid two
concurrent schema-shape branches. No such plan is currently active per the audit.

---

## 5. Backward-compatibility policy

### v1 surface lifetime

- **Phase 0 → Phase 6**: v1 `.agentsrc.json` continues to load with zero changes required. No
  warning, no friction. `version: 1` is a first-class supported value.
- **Phase 7 (post-soak)**: v1 still loads, now with a one-time deprecation notice to stderr.
  `da config migrate` becomes the supported path to v2. Recommended soak: **2 minor releases**
  between p0 ship and p7 ship.
- **Final removal of v1 loader**: NOT in this plan. A separate "v1 sunset" plan should land
  at least **6 months** after Phase 7 ships. That plan removes v1 parsing, removes the
  backward-compat shim, and bumps schema constants.

### Field-level deprecation cadence

| v1 field | v2 fate |
|---|---|
| `version: 1` | accepted through Phase 7; `version: 2` is the v2 default; migrate writes 2 |
| `sources: [{type, path, url, ref}]` (no id) | accepted; resolver synthesizes anonymous ids; migrate adds explicit ids |
| `app_type_verifier_map` | unchanged; remains valid under v2; verifier_profiles graduates per p1c (optional) |
| `verifier_profiles.<id>.prompt_files: [string]` | accepted; per p1c may also accept `[{source, path, version}]` (additive union) |
| `project` | unchanged; gains sibling `repo_id`; `repo_id` is preferred for resolution but `project` remains for display per org-config-resolution §5.4 |
| `kg`, `refresh`, `agents`, `skills`, `rules`, `hooks`, `mcp`, `settings` | unchanged; merge under v2 layer rules |
| `extends`, `packages`, `repo_id`, `features` | NEW; absent in v1; absence is valid in v2 too (empty/omitempty) |

### Tooling backward-compat

| Tool | v1 behavior | v2 behavior | Notes |
|---|---|---|---|
| `da init` | writes v1 | writes v2 starting at p0 ship | v1 init removed at p7 |
| `da refresh` | projection refresh | unchanged | DO NOT alias to `config sync` per codex Q4 + spec §13.3 recommendation kept |
| `da install` | combined setup | unchanged in this plan | Spec §13.3 proposes repurposing in a later phase; do NOT couple to v2 |
| `da explain` | static human docs | unchanged | New `da config explain` is the v2 surface; `da explain` keeps its semantics |
| `workflow app-types` | reads .agentsrc.json directly | consumes snapshot API after p4b | identical user-facing output |
| `da doctor` | health checks | adds lockfile/TTL checks after p2 | v1 (no lock file) is a no-op branch |

---

## 6. Open questions for the user

### Q1 — verifier_profile.prompt_files migration option

Two paths under p1c:

- **Option A (recommended)**: Additive union type. `prompt_files` accepts BOTH `[string]` and
  `[{source, path, version}]`. Zero migration cost; resolver coerces flat strings to local.
- **Option B**: Force all entries to typed objects; requires bulk migration of every existing
  `verifier_profiles` entry across all repos and dot-agents starter.

**Recommend Option A.** Confirm before p1c starts; if Option B is chosen, p1c becomes a
critical-path task (not optional) and the scope expands to cover starter migration.

### Q2 — deprecation soak window

How long between Phase 0 (v2 schema available) and Phase 7 (v1 deprecation warnings)?

**Recommend 2 minor releases.** Confirm before scheduling p7.

### Q3 — v1 final removal timing

After Phase 7 ships warnings, how long until v1 loading is removed entirely?

**Recommend at least 6 months.** This becomes a separate "v1 sunset" plan, not part of
`config-v2-migration`.

### Q4 — `da install` repurposing

Spec §13.3 proposes that `da install` should run `da config sync && da packages install` as
a combined setup step. This is a user-contract change.

**Recommend deferring.** Keep `da install` as-is in v2; introduce the combined verb later if
needed. Confirm before any phase touches `commands/install.go`.

### Q5 — `da config explain` JSON schema stability commitment

Phase 4 ships `--json`. The CI-stable JSON schema decision (spec §14 Q4) needs a ruling:

- Document the v1 JSON schema in `commands/config/explain.go` godoc and treat it as stable
  going forward?
- Or label the JSON output as "experimental" through some soak period?

**Recommend documenting stable from p4 ship.** CI integrations need a contract.

### Q6 — Phase 5 signing posture default

Phase 5 ships an opt-in `Verifier` interface with a permissive (warn-only) default impl. Should
the warn-only default apply only to config layers, only to packages, or both?

**Recommend both.** Signature checking becomes ERROR-by-default in a later plan after key
infrastructure (external-agent-sources) lands.

### Q7 — Single config repo or multi-repo authoring?

Spec org-config-resolution §16 Q2 asks where `repo_id → team/system/ownership` metadata lives.
v2 client just *consumes* `repo_id`. But the central config repo layout (§12) matters when
authoring layers.

**Out of scope for `config-v2-migration`.** Defer to a separate "central config repo bootstrap"
plan once v2 client lands.

---

## 7. Recommended FIRST task

Start with **`p0-schema-additive-extension`**. Rationale:

- It is the foundation for every other phase (5 other phases `depends_on` it directly).
- It is the smallest reviewable change in the plan (~3 files: struct, schema, tests).
- It is fully testable in isolation (v1 fixture + v2 fixture round-trip).
- It has no network surface, no CLI surface, no behavior change for existing users.
- It enables independent parallel work on p0b (repo_id bootstrap) immediately after.

Per `[[schema-usage]]` four-place atomic-change rule, p0 must update:

1. Struct definition (`AgentsRC` in `internal/config/agentsrc.go`)
2. `agentsRCCore` mirror struct (same file)
3. `UnmarshalJSON` field copies
4. `MarshalJSON` field copies
5. `agentsRCKnown` map (so unknown-field guard doesn't swallow new fields)
6. `schemas/agentsrc.schema.json` (additive properties + `additionalProperties: false` on
   nested objects)

Plus testdata fixtures under `internal/config/testdata/v1/` and `internal/config/testdata/v2/`
to exercise both load paths.

---

## 8. Cross-references

- `[[schema-usage]]` — applies to p0, p0b, p1c (any struct field change is four-place atomic)
- `[[additive-state-fields]]` — applies to p1 snapshot API (use `[]T{}`, pointer for absent)
- `[[validate-bundle-against-head]]` — applies to every phase's write_scope before authoring
  tests that import from earlier phases
- `[[worktree-no-cd]]` — applies to any subagent spawned to implement these tasks
- `[[no-lazy-allowlist-tech-debt]]` — no allowlist additions proposed in any phase
- `config-distribution-model/design.md` §1–§14 — spec authority for every phase
- `org-config-resolution/design.md` §1–§17 — layer model + repo identity + merge rules
- `external-agent-sources` — referenced by p5 (signing posture, OCI wire protocol); transport
  details deferred to that spec
- `codex-019e6245-examination-and-sequenced-plan.md` — codex's 7-phase sequenced plan that
  this plan integrates with
- `pr-ci-verifier-integration-audit.md` — surfaces `prompt_files` source-awareness as p1c
  (optional)
