# SDD-Entity KG Schema — Draft (companion to O4)

**Status:** PROPOSAL — IDEATION draft awaiting owner ratification. This is the
concrete node/edge schema the O4 recommendation
([`graph-backend-adapter-contract/open-questions-resolutions.md` §O4](../graph-backend-adapter-contract/open-questions-resolutions.md))
commits to in principle: **typed nodes for SDD structure + state, with a
`KGNote` projection for prose body.** Nothing here is normative until ratified.
**Written:** 2026-06-23 · **Revised:** 2026-06-24 (field-mirroring pass, open
app_type vocabulary, expanded edge catalog with repo evidence, proposals +
lessons as first-class scoped nodes, rubric-driven view expansion) ·
**Revised:** 2026-06-24 (schema v3 — Codex + Cursor adversarial reconciliation:
composite `task_key`/`result_key` identity + `plan_id` on task/result; app_type
provenance + `app_type_profile` node/join edges; raw-vs-derived outcome split;
operational-knowledge materialization fields; `scope_root` anchor; version/
content-hash on correlation targets; result→plan anchor for waves; see §5A).
**Author:** Nikash Prakash
**Owning spec:** `work-tracking-storage-abstraction/design.md` (§6 open question
"KG schema for SDD entities + correlation edges"; D1′ tiers; §3A typed views +
correlation).

**For adversarial review:** this revision is staged for an adversarial pass —
a **Codex** review and a **Cursor** review. Every "real field," "known
app_type," and edge-cardinality claim below cites the on-disk file or git
evidence it was grounded against (see the inline `[evidence: …]` tags and §6
Research / evidence ledger); reviewers should challenge any claim that lacks
one.

---

## 0. Design posture (from O4)

D1′ defines three canonical tiers; this schema places each SDD concern in the
tier the spec already ratified:

| Tier | What | Representation in this schema |
|---|---|---|
| 1. Code | functions/types/files | **Existing** `GraphNode`/`GraphEdge` (CRG). Indexed, never authored here. |
| 2. SDD structure + state | Plan/Task/Spec/Proposal/Lesson entities, their relationships, coordination state | **New typed nodes + typed edges** (this draft). KG-canonical. |
| 3. SDD prose body | the narrative markdown of a spec/plan/lesson/proposal | **`KGNote` projection** (existing shape), file-backed + semantic-indexed. The typed node references it. |

**Why typed nodes (not flat `KGNote` rows):** coordination state (status, leases,
PR linkage, `depends_on`, `write_scope`, `verifier_sequence`) and the §3A
correlation edges are *relationships and typed fields*, not free text. A flat
`KGNote.Status` string cannot carry a CAS lease (D5) or answer "which verifier
sequence regresses spec X" (§3A). **Why keep `KGNote` for prose:** a spec/plan
narrative *is* a note-shaped, file-backed, searchable artifact; the existing
semantic-view machinery already indexes it (D1′ tier 3).

**Field-fidelity rule (this revision).** Each typed node's field-set **mirrors
the real on-disk artifact** — the canonical JSON Schemas in `schemas/` and the
real artifact frontmatter — rather than inventing a parallel vocabulary. Where
this draft previously invented fields (lease/PR/app_type on `task`, a different
status enum), the fields are now reconciled to what the files actually carry,
and any field that does **not** yet exist on disk is explicitly marked
`PROPOSED (not on disk)` so the future build knows it is adding surface, not
mirroring it.

**Substrate note:** the existing `internal/graphstore` store has `GraphNode`,
`GraphEdge`, `KGNote`, and `NoteSymbolLink` (`store.go`). This draft adds a
*typed-SDD-node* layer beside `KGNote`. Whether these land as a graph-backend
*adapter* (riding the `graph-backend-adapter-contract` seam) or as core typed
tables is an implementation choice for the future work-tracking plan; the
schema below is expressed adapter-shape (per the adapter §4 vocabulary) since
the work-tracking spec rides that seam (D3, §8 relationships).

---

## 1. Node types

Fields use the adapter §4 type vocabulary (`string|int|float|bool|date|enum|
ref<type>`). `prose_note` refs point at the tier-3 `KGNote` projection.
A node field tagged `[mirror: <file>]` reflects a real on-disk field; a field
tagged `[PROPOSED]` does **not** exist on disk yet and is added by this design.

### 1.0 `app_type` is an OPEN vocabulary (not a closed enum)

Several nodes carry an `app_type`. The project **promises custom app-types**,
so `app_type` is modeled as a **free/extensible string** with a *documented
known-starter set*, **not** an exhaustive `enum` constraint. Grounding:

- `internal/config/execution_profile.go:19-22` — `ByAppType map[string]
  AppTypeProfile`; the doc comment states unmatched app_types "fall back to
  caller defaults" (any string is a valid key).
  [evidence: `internal/config/execution_profile.go`]
- `docs/CONFIG_RELEVANCE.md` — "an `app_type` with no entry is **not an error**,
  it just has no overrides… `default_class: situational` guarantees nothing
  unlisted is ever silently dropped." [evidence: `docs/CONFIG_RELEVANCE.md`]
- `app-type-profiles/design.md §7.1` — `app_type` is "a profile ref in the form
  `source-id:profile-name@version-spec` **or bare-name** resolving against the
  default source," and `§2.6` explicitly contrasts the **open** profile surface
  with the **closed** `graph_backend` enum it is replacing — "lets
  community-published backends … participate **without `da` core changes**."
  [evidence: `.agents/workflow/specs/app-type-profiles/design.md` §2.6, §7.1]

**Known starter values** (documented set, *non-exhaustive*; custom values
resolve via the `source-id:name@version` ref form):

| Known `app_type` | Source [evidence] |
|---|---|
| `go-cli` | `.agentsrc.json` `execution_profile.by_app_type` key |
| `ideation` | `.agentsrc.json` `execution_profile.by_app_type` key |
| `go-http-service` | `app-type-profiles/design.md` §8.1 worked baseline |
| `api`, `ui`, `streaming`, `batch` | **COMPOSITE-MEMBER, not independently selectable** (L1, accepted): these are `app-type-profiles/design.md` §3.2 composite *children* (`composes: [api, batch, streaming]`) — a composite component is not the same as a selectable top-level app_type. Marked INFERRED. `ui`/`api` *resemble* the payout `client-ui`/`client-se` split but that repo has no `.agents/` tree to confirm them as selectable app_types [evidence: §3.2 composite children; `NikashPrakash/payout` has `client-ui`/`client-se` dirs only] |
| `po-core-api-se` | `app-type-profiles/design.md` §8.2 composite example |
| `research`, `resume-ideation` | `app-type-profiles/design.md` §8.3, §8.4 non-code profile examples |

Modeled as:

```yaml
# Shared field definition reused by plan.default_app_type and task.app_type
app_type:
  type: string                 # OPEN — NOT enum
  resolves_as: "<source-id>:<profile-name>@<version-spec> | <bare-name>"
  known_starter_values: [go-cli, ideation, go-http-service, api, ui,
                         streaming, batch, po-core-api-se, research,
                         resume-ideation]   # documented, non-exhaustive
  custom_allowed: true         # community profiles resolve via ref form (§7.1)
```

### 1.1 Working-view nodes (tier 2: structure + coordination state)

```yaml
note_types:
  - name: plan
    # Mirrors schemas/workflow-plan.schema.json (PLAN.yaml).
    fields:
      - { name: plan_id,            type: string, required: true }   # [mirror: schema `id`] e.g. graph-backend-adapter-contract
      - { name: title,              type: string, required: true }   # [mirror: `title`]
      - { name: status,             type: enum,
          values: [draft, active, paused, completed, archived], required: true }  # [mirror: schema `status` enum — was [draft,active,blocked,archived]; corrected]
      - { name: schema_version,     type: int,    required: true }   # [mirror: `schema_version`]
      - { name: summary,            type: string, required: true }   # [mirror: `summary`]
      - { name: owner,              type: string, required: false }  # [mirror: `owner`]
      - { name: default_app_type,   type: string, required: false }  # [mirror: `default_app_type`] OPEN vocab (§1.0)
      - { name: current_focus_task, type: string, required: false }  # [mirror: `current_focus_task`]
      - { name: created_at,         type: date,   required: true }   # [mirror: `created_at`]
      - { name: updated_at,         type: date,   required: true }   # [mirror: `updated_at`]
      - { name: prose_note,         type: ref<kg_note>, required: false }   # tier-3 .plan.md body [PROPOSED projection link]

  - name: task
    # Mirrors schemas/workflow-tasks.schema.json tasks[] item.
    # IDENTITY: task ids are PLAN-LOCAL (`t1`/`release-minor` collide across plans),
    # so the graph node key is COMPOSITE: task_key = plan_id + "/" + task_id. This is
    # the same `<plan-id>/<task-id>` form code already enforces for cross-plan deps —
    # `commands/workflow/base_resolution.go:223`: "DependsOn is the dep id set (may
    # include cross-plan `<plan>/<task>` ids)" with a dedicated
    # TestSelectAllEligibleTasks_CrossPlanDepMissingPlan test. The on-disk
    # `plan_id` lives in the TASKS.yaml file header (schemas/workflow-tasks.schema.json
    # top-level), NOT per task, so the projected node materializes it explicitly.
    fields:
      - { name: task_key,           type: string, required: true }   # COMPOSITE node key = plan_id + "/" + task_id (globally unique). [grounded: base_resolution.go:223 `<plan>/<task>` form]
      - { name: plan_id,            type: string, required: true }   # [mirror: TASKS.yaml file-header `plan_id`] — promoted onto the task node so the key is unique and result_for_task→belongs_to_plan is unambiguous
      - { name: task_id,            type: string, required: true }   # [mirror: schema `id`] PLAN-LOCAL; not unique on its own
      - { name: title,              type: string, required: true }   # [mirror: `title`]
      - { name: status,             type: enum,
          values: [pending, in_progress, blocked, completed, cancelled], required: true }  # [mirror: schema `status` enum — was [pending,claimed,in_flight,awaiting_review,done,blocked]; corrected]
      - { name: depends_on,         type: list<string>, required: true }   # [mirror: `depends_on`] task ids
      - { name: blocks,             type: list<string>, required: true }   # [mirror: `blocks`] task ids
      - { name: write_scope,        type: list<string>, required: true }   # [mirror: `write_scope`] path globs
      - { name: verification_required, type: bool, required: true }        # [mirror: `verification_required`]
      - { name: owner,              type: string, required: false }        # [mirror: `owner`]
      - { name: notes,              type: string, required: false }        # [mirror: `notes`]
      # --- app_type IS on disk (correction, Codex#5) ---
      # `CanonicalTask.AppType` exists in commands/workflow/types.go (`app_type,omitempty`)
      # and live TASKS.yaml files carry it — e.g. docs-starlight-migration/TASKS.yaml
      # tasks carry `app_type: docs` / `app_type: go-cli`. So task.app_type is a
      # [mirror], NOT [PROPOSED]. (The earlier "not on disk" claim was wrong and is
      # corrected in §5 too.) The REAL gap is schema drift — see the §5 follow-up:
      # schemas/workflow-tasks.schema.json has additionalProperties:false and omits
      # `app_type`, so it REJECTS the field code + artifacts already use.
      - { name: app_type,           type: string, required: false }   # [mirror: CanonicalTask.AppType / live TASKS.yaml] OPEN vocab (§1.0)
      # --- app_type RESOLUTION PROVENANCE (Codex#5 / docs/CONFIG_RELEVANCE.md) ---
      # The raw string is not enough: a historical result must record WHICH profile
      # actually drove the run after profiles move or an unmatched custom type falls
      # back. Grounded in docs/CONFIG_RELEVANCE.md §"How app_type is selected"
      # (app_type_source precedence: task|plan-default|flag|none; `matched` sibling)
      # and app-type-profiles §7.1/§2.3 (versioned `source-id:name@version` refs).
      - { name: app_type_source,    type: enum, values: [task, plan-default, flag, none], required: false }  # [mirror: CONFIG_RELEVANCE `app_type_source`] which selector won
      - { name: matched,            type: bool,   required: false }   # [mirror: CONFIG_RELEVANCE `matched`] did the resolved app_type hit a profile entry, or fall back?
      - { name: resolved_profile_ref, type: string, required: false } # [mirror: app-type-profiles §7.1 `source-id:profile-name@version-spec`] the profile the run actually resolved to
      - { name: source,             type: string, required: false }   # [mirror: app-type-profiles §2.5/§7.1 config source-id the profile resolved from]
      - { name: version,            type: string, required: false }   # [mirror: app-type-profiles §2.3 resolved profile semver] which version actually fired
      # --- coordination state the on-disk TASKS.yaml does NOT yet carry ---
      # These are the D5 atomic-transition surface the future WorkStore adds.
      - { name: lease_owner,        type: string, required: false }   # [PROPOSED] worker/agent id holding the claim (D5)
      - { name: lease_expires_at,   type: date,   required: false }   # [PROPOSED] TTL lease (D5 / §6 open q)
      - { name: pr_url,             type: string, required: false }   # [PROPOSED] PR/branch linkage
      - { name: branch,             type: string, required: false }   # [PROPOSED]
      - { name: prose_note,         type: ref<kg_note>, required: false }  # [PROPOSED]

  - name: spec
    # Mirrors design.md frontmatter: Status / Written(+revision history) /
    # Supersedes / Related / Plan. [evidence: graph-backend-adapter-contract/design.md
    # lines 1-14; scoped-knowledge-graphs/design.md line 6]
    fields:
      - { name: spec_id,    type: string, required: true }   # [mirror: dir name] e.g. graph-backend-adapter-contract
      - { name: title,      type: string, required: true }   # [mirror: H1]
      - { name: status,     type: enum, values: [draft, ratified, superseded], required: true }  # PARTIAL mirror (M2): `draft` is attested in **Status:** frontmatter ("draft v5 (canonical)"), but there is NO spec JSON Schema and `ratified`/`superseded` are INVENTED enum values (spec frontmatter is freeform). Treat `ratified`/`superseded` as [PROPOSED] normalized values, not on-disk mirrors.
      - { name: revision,   type: string, required: false }  # [mirror: **Status:**/**Written:** revision tail, e.g. "v6.1"]
      - { name: prose_note, type: ref<kg_note>, required: false }   # the design.md body (tier 3). OPTIONAL until the KGNote projection link is built (M3).
      # Note: Supersedes/Related/Plan frontmatter lines are modeled as EDGES (§2.1), not fields.

  - name: proposal
    # Mirrors the canonical .yaml proposal (schemas + .agents/proposals/kg-ideate-skill.yaml)
    # and the prose .md proposal frontmatter (Status / Written / Author / Scope).
    fields:
      - { name: proposal_id, type: string, required: true }   # [mirror: yaml `id` / md filename]
      - { name: title,       type: string, required: true }   # [mirror: H1 / yaml-derived]
      - { name: status,      type: enum,
          values: [draft, open, accepted, promoted, graduated, rejected, deferred], required: true }  # PARTIAL mirror (M2): only `draft`/`promoted`/`deferred` are ATTESTED on disk (yaml `status: draft`; md "**Status:** promoted"/"deferred"). `open`/`accepted`/`graduated`/`rejected` are [PROPOSED] lifecycle values, not yet observed in any artifact.
      - { name: type,        type: string, required: false }  # [mirror: yaml `type: skill|spec`]
      - { name: action,      type: enum, values: [create, modify, retire], required: false }  # [mirror: yaml `action: create`]
      - { name: target,      type: string, required: false }  # [mirror: yaml `target.installed_at`/`starter_source`]
      - { name: scope,       type: enum, values: [repo, project, global], required: false }   # [mirror: prose `.md` `**Scope:**` frontmatter; PROPOSED for the canonical `.yaml`] enables SCOPED proposals — the OQ4 goal the §4.3 view-mapping + §1.1 note already claim; was MISSING here (Codex#1)
      - { name: prose_note,  type: ref<kg_note>, required: false }   # the proposal body / rationale (tier 3). OPTIONAL until the projection link is built (M3).

  - name: lesson
    # First-class SCOPED node (maintainer ask). Mirrors LESSON.md YAML frontmatter
    # (name / description / type) + the one-line index entry in .agents/lessons/index.md.
    # [evidence: .agents/lessons/additive-state-fields/LESSON.md frontmatter;
    #  .agents/lessons/index.md]
    fields:
      - { name: lesson_id,   type: string, required: true }   # [mirror: frontmatter `name` / dir name]
      - { name: description, type: string, required: true }   # [mirror: frontmatter `description` == index one-liner]
      - { name: type,        type: enum, values: [feedback, pattern, rule], required: false }  # [mirror: frontmatter `type: feedback`]
      - { name: scope,       type: enum, values: [repo, project, global], required: false }   # [PROPOSED] enables SCOPED lessons (rules: [global, project])
      # --- operational-knowledge materialization (rubric §4.1 made queryable, Codex#6) ---
      - { name: rubric_dimensions, type: list<enum>, required: false }   # [PROPOSED] §4.1 dimensions this lesson evidences: [source, lifecycle, scope, provenance, what_only_doing_reveals, surface_vs_path]
      - { name: evidence_kind,     type: enum, values: [discovered_by_doing, declared_surface, correction, pattern], required: false }  # [PROPOSED] is this knowable from the file, or only by running the work? (the §4.1 what-only-doing-reveals split)
      - { name: surface_vs_path,   type: enum, values: [declared_surface, exercised_path], required: false }  # [PROPOSED] §4.1 surface-vs-path axis
      - { name: tags,              type: list<string>, required: false }  # [PROPOSED] free tags for cross-cutting query
      - { name: prose_note,  type: ref<kg_note>, required: false }   # the LESSON.md body (tier 3). OPTIONAL until the projection link is built (M3).
```

> **Why `proposal` and `lesson` are first-class scoped working/semantic nodes
> (OQ4 ask):** a key project goal is **scoped proposals and lessons** — a
> proposal or lesson that belongs to a project/scope, not just a global pile.
> Promoting them from "prose `KGNote` rows" to typed nodes with a `scope` field
> (and the proposal→spec / lesson→result edges in §2) is what makes "which
> lessons are in scope here?" and "which proposal graduated this spec?"
> queryable. The `.agentsrc.json` `rules: [global, project]` field
> [evidence: `.agentsrc.json`] is the existing scope axis these nodes inherit.

### 1.2 Operational-view nodes (tier 2: what produced a result)

These are the §3A "operational" view — the primitives a result is scored
against. Lightweight identity nodes; their prose/config lives in files. See the
§4.1 **rubric** for what distinguishes this view (operational = *what doing the
work reveals + the paths actually exercised*, not the full declared surface).

```yaml
  # NOTE (M1, accepted): the correlation edges in §2.2 (exercised_rule /
  # applied_lesson / produced_under_profile) point at these nodes, and §3A-Q3
  # ("did adopting rule/lesson X change downstream quality?") compares result
  # outcomes across time. If the rule/lesson/profile CONTENT changed between two
  # results, the edge must distinguish "ran under X v1" vs "v2" or the before/after
  # comparison is unsound. So rule/lesson/stage_profile carry a `content_hash` (and
  # where it exists, a `version`), consistent with O5's content-hash mutation
  # primitive. The loop scores against the version that actually fired.

  - name: stage_profile
    # Mirrors .agentsrc.json stage_profiles.{verifier,reviewer,...}.<slug>.
    fields:
      - { name: slug, type: string, required: true }   # [mirror: stage_profiles.<role>.<slug> key, e.g. citation-check]
      - { name: role, type: enum, values: [executor, verifier, reviewer, orchestrator], required: true }  # [mirror: stage_profiles.<role>]
      - { name: label, type: string, required: false }  # [mirror: stage_profiles.<role>.<slug>.label]
      - { name: version,      type: string, required: false }   # [mirror: app-type-profiles §2.3 profile semver — the version that fired]
      - { name: content_hash, type: string, required: false }   # [PROPOSED] M1 — pins which profile content a result ran under (O5 content-hash primitive)
      - { name: prose_note, type: ref<kg_note>, required: false }   # [mirror: prompt_files]

  - name: skill
    fields:
      - { name: name,       type: string, required: true }          # [mirror: .agentsrc.json `skills[]` entry / SKILL.md dir]
      - { name: scope,      type: enum, values: [repo, project, global], required: false }  # [PROPOSED] scoped skills
      - { name: content_hash, type: string, required: false }       # [PROPOSED] M1 — which skill content a result ran under
      - { name: prose_note, type: ref<kg_note>, required: false }   # SKILL.md

  - name: rule
    fields:
      - { name: rule_id,    type: string, required: true }          # [mirror: .agentsrc.json `rules[]` entry: global|project]
      - { name: scope,      type: enum, values: [global, project], required: false }  # [mirror: rules[] values]
      - { name: content_hash, type: string, required: false }       # [PROPOSED] M1 — pins the rule content version a result fired under (Q3 soundness)
      - { name: prose_note, type: ref<kg_note>, required: false }

  - name: app_type_profile
    # H3 (accepted): app_type as a bare string is provenance metadata, not a JOIN.
    # The ref form `source-id:profile-name@version-spec` (app-type-profiles §7.1)
    # needs a REFERENT so "which tasks ran under app_type X", "which community
    # profile backed this result", and the O3/t7 graph_backend selection are
    # joinable. This node IS that referent; the bare `app_type` string on
    # plan/task/result remains as the raw label + provenance.
    fields:
      - { name: profile_ref, type: string, required: true }   # [mirror: app-type-profiles §7.1 `source-id:profile-name@version-spec`] the resolvable ref (node key)
      - { name: name,        type: string, required: true }   # [mirror: profile name, e.g. go-http-service]
      - { name: source_id,   type: string, required: false }  # [mirror: §2.5/§7.1 config source-id]
      - { name: version,     type: string, required: false }  # [mirror: §2.3 profile semver]
      - { name: scope,       type: enum, values: [repo, project, global], required: false }  # [PROPOSED] profiles are scoped via sources/extends
      - { name: composes,    type: list<string>, required: false }  # [mirror: §3.2 `composes: [api, batch, streaming]`] composite children (see L1)

  - name: agent
    fields:
      - { name: agent_id, type: string, required: true }            # [PROPOSED] L2 — composite id (scope + name); bare name collides across scopes
      - { name: name, type: string, required: true }                # [mirror: .agentsrc.json `agents[]`, e.g. loop-worker]
      - { name: scope, type: enum, values: [repo, project, global], required: false }  # [PROPOSED] L2 — agents are as scoped as skills/rules

  - name: hook
    fields:
      - { name: hook_id, type: string, required: true }             # [PROPOSED] L2 — composite id (scope + event + name); event+name alone collides across scopes
      - { name: event, type: string, required: true }               # [mirror: schemas/hook.schema.json event]
      - { name: name,  type: string, required: true }
      - { name: scope, type: enum, values: [repo, project, global], required: false }  # [PROPOSED] L2 — scope axis for hooks

  - name: scope_root
    # H4 (accepted): `scope: project` resolves against WHICH project? Without a
    # scope-root entity the query "which lessons are in scope here?" has no graph
    # answer. proposal-routing.md draws the concrete boundary: global (`~/.agents/`)
    # vs project-local (`.agents/`). This node is the anchor a scoped node points at.
    fields:
      - { name: root_id, type: string, required: true }   # e.g. "global:~/.agents" or "project:<repo>/.agents"
      - { name: kind,    type: enum, values: [global, project, repo], required: true }   # [mirror: proposal-routing.md global vs project-local boundary]
      - { name: path,    type: string, required: false }  # the `.agents/` root this scope resolves against

  # SCOPE-ENUM reconciliation (H4, accepted): the scope axis is INCONSISTENT across
  # nodes — `rule.scope = [global, project]` (mirrors `.agentsrc.json rules[]` which
  # only carries those two literal values) vs `lesson/skill/proposal/app_type_profile/
  # agent/hook.scope = [repo, project, global]`. Rationale for keeping them distinct:
  # `rule` MIRRORS an on-disk two-value field, so widening it would invent surface;
  # the others are [PROPOSED] and adopt the full three-value axis. The canonical axis
  # is [repo, project, global]; `rule` is the documented two-value subset until
  # `.agentsrc.json` gains a `repo` rule scope. `spec`/`plan`/`task` are flagged as
  # ALSO scopable (specs/plans are equally global-vs-project per proposal-routing.md)
  # — adding their scope is deferred to the work-tracking build, not invented here.
```

### 1.3 Episodic-view nodes (tier 2: results + history)

```yaml
  - name: result
    # Mirrors the three real result artifacts:
    #  - iter-log: schemas/workflow-iter-log.schema.json (iteration / wave / task_id / impl / verifiers[] / review / outcome)
    #  - merge-back.md frontmatter (schema_version / task_id / parent_plan_id)
    #  - history/<plan>/impl-results.md (Plan / Spec references)
    # IDENTITY: like task_id, the iter-log `iteration` (iter-N) and `wave-M` are
    # PLAN-LOCAL (iter-1 collides across every plan), so the result node key is
    # COMPOSITE: result_key = wave (== active plan id) + "/" + result_id. The
    # plan/task this result belongs to is carried explicitly (see plan_id /
    # parent_task_key below) so result_for_task and the transitive plan lookup
    # cannot point at the wrong plan.
    fields:
      - { name: result_key,  type: string, required: true }   # COMPOSITE node key = wave (plan id) + "/" + result_id (globally unique)
      - { name: result_id,   type: string, required: true }   # [mirror: iter-log `iteration` (iter-N) / wave-M] PLAN-LOCAL; not unique on its own
      - { name: kind,        type: enum, values: [iteration, wave, fold_back, review, merge_back], required: true }
      - { name: plan_id,         type: string, required: false }   # [mirror: iter-log `wave` / merge-back `parent_plan_id` / impl-results `Plan:`] provenance metadata (NOT a traversable result→plan edge, §2.2)
      - { name: parent_task_key, type: string, required: false }   # COMPOSITE FK to the producing task (plan_id + "/" + task_id); empty for wave/fold_back results that span many tasks (see §2.2 H2 handling)
      # --- outcome: STORE the raw verifier status; DERIVE rollups separately ---
      - { name: outcome,     type: enum, values: [pass, fail, partial, unknown], required: false }  # [mirror: iter-log verifiers[].status EXACTLY — schema enum is pass|fail|partial|unknown]
      - { name: outcome_rollup, type: enum, values: [pass, fail, partial, regressed, unknown], required: false }  # [DERIVED — not on disk] scoring rollup; `regressed` is computed by comparing this result's outcome/score against the prior result for the same task/spec window, kept SEPARATE from the stored raw status
      - { name: score,       type: float, required: false }    # [PROPOSED] rescore-loop signal — iter-log has NO `score` field today; this is added by the future WorkStore scoring layer, not mirrored
      - { name: occurred_at, type: date,  required: true }      # [mirror: iter-log `date`]
      - { name: wave,        type: string, required: false }    # [mirror: iter-log `wave` == active plan id]
      # --- operational-knowledge materialization (rubric §4.1 made queryable) ---
      - { name: rubric_dimensions, type: list<enum>, required: false }   # [PROPOSED] which §4.1 dimensions this result evidences: [source, lifecycle, scope, provenance, what_only_doing_reveals, surface_vs_path]
      - { name: evidence_kind,     type: enum, values: [failure, win, regression, flake, surface_path_gap], required: false }  # [PROPOSED] what KIND of operational evidence this is (§4.2)
      - { name: surface_vs_path,   type: enum, values: [declared_surface, exercised_path], required: false }  # [PROPOSED] §4.1 surface-vs-path axis — was this the full declared surface or the path actually run?
      - { name: tags,              type: list<string>, required: false }  # [PROPOSED] free tags for cross-cutting query (e.g. "re-dispatch-storm", "buildvcs")
      - { name: prose_note,  type: ref<kg_note>, required: false }   # merge-back.md / impl-results.md / iter-log
```

### 1.4 Tier-3 prose projection (existing `KGNote` shape)

`kg_note` is the existing `KGNote` record (`store.go:104`). It is **not
re-defined** here — typed nodes reference it via `prose_note: ref<kg_note>`.
The `KGNote.FilePath` field points at the git-canonical markdown body
(D1′ tier 3), and `IndexedAt`/`Summary` feed the semantic view. This is the
"projection" half of the O4 recommendation: prose stays a file, indexed as a
note, referenced by the typed structural node.

**Projection-boundary reconciliation (M3, accepted — verified against
`internal/graphstore/store.go:104-114`: `KGNote{Status string; Version int;
ArchivedAt string /*RFC3339*/; IndexedAt float64}`):**

- **Status/version authority rule.** The typed `spec.status` + `spec.revision`
  duplicate `KGNote.Status` + `KGNote.Version`. **Authority: the typed node is
  canonical for structure/state (tier 2); `KGNote.Status`/`Version` are the
  projection's cached copy and MUST be derived from the typed node, never the
  reverse.** When they diverge, the typed node wins and the projection is
  re-synced. This removes the coherence hazard O4's open question flags.
- **Date-type mapping.** Typed nodes use `type: date` (e.g. `result.occurred_at`,
  `plan.updated_at`), but the existing store uses heterogeneous representations:
  `KGNote.IndexedAt` is `float64` (unix secs) and `KGNote.ArchivedAt` is an RFC3339
  string. The adapter `date` vocabulary therefore does **not** map 1:1 to the
  store; the projection layer is responsible for the conversion (typed `date` ↔
  store's float/RFC3339), and the build must pick ONE canonical wire form for
  `date` so round-trips are lossless. Flagged for the work-tracking build.
- **`prose_note` is OPTIONAL, not required.** The projection link itself is
  `[PROPOSED]` (the typed-node↔KGNote wiring is not yet built), so requiring a ref
  to a shape that does not exist on disk is incoherent. `prose_note` is therefore
  `required: false` on `spec`/`proposal`/`lesson` until the projection lands; the
  build promotes it to required once the KGNote link is real.

---

## 2. Edge types

Each edge's cardinality is grounded in a **real repo case** (cited inline;
full ledger in §6).

### 2.1 Structural / trace edges (proposal → spec → plan → task)

```yaml
edge_types:
  # --- Plan/task structure ---
  - { name: contains_task,   from: plan, to: task, cardinality: one-to-many }
    # plan-has-many-tasks. [evidence: TASKS.yaml header `plan_id` FK + tasks[] array;
    #  loop-discipline-stop-hooks/TASKS.yaml has 17 tasks under one plan_id]
  - { name: belongs_to_plan, from: task, to: plan, cardinality: many-to-one }
    # EXPLICIT FK edge (the inverse of contains_task) — was MISSING; added per feedback.
    # [evidence: TASKS.yaml `plan_id` field is the literal FK]
  - { name: depends_on,      from: task, to: task, cardinality: many-to-many }   # incl. cross-plan [mirror: tasks[].depends_on]
  - { name: blocks,          from: task, to: task, cardinality: many-to-many }   # [mirror: tasks[].blocks]

  # --- SDD trace (the proposal↔spec↔plan↔task chain D1′ requires queryable) ---
  - { name: plan_for_spec,   from: plan, to: spec, cardinality: many-to-many }
    # spec→plan is ONE-TO-MANY (one spec ⇒ many plans). plan→spec was modeled
    # many-to-one, but L4 (accepted) is correct: a plan CAN span >1 spec — THIS PR is
    # the live case (the dossier's owning specs list FIVE: graph-backend-adapter-contract,
    # scoped-knowledge-graphs, work-tracking-storage-abstraction, graphstore-concurrency-contract,
    # kg-command-surface-readiness, and the companion draft's header names a sibling
    # KG-schema spec). So plan→spec is MANY-TO-MANY.
    # [evidence: spec `config-distribution-model` ⇒ TWO plans: config-v2-migration
    #  AND config-v2-coherence both name it as owning spec in PLAN.yaml; this PR's own
    #  dossier spans 5 owning specs]
  - { name: derives_plans,   from: spec, to: plan, cardinality: one-to-many }
    # The forward spec→plan one-to-many, named explicitly. Same evidence as above.
    # [evidence: also agent-run-scoring-observability-platform proposal records its
    #  spec was "promoted to a workflow spec + FOUR plans"]
  - { name: implements_spec, from: task, to: spec, cardinality: many-to-many }   # a task can touch >1 spec; a spec is implemented by many tasks

  # --- proposal → spec (a proposal yields one or more specs) ---
  - { name: graduated_from,  from: spec, to: proposal, cardinality: many-to-one }
    # ONE proposal ⇒ one OR MORE specs (split-off / derived). spec→proposal is many-to-one.
    # [evidence: graph-backend-adapter-contract/design.md "**Supersedes:**
    #  .agents/proposals/graph-backend-adapter-contract.md (proposal accepted; this spec
    #  is the canonical artifact)"; agent-run-scoring proposal "promoted to a workflow
    #  spec + four plans"]
  - { name: yields_spec,     from: proposal, to: spec, cardinality: one-to-many }
    # Forward proposal→spec, named explicitly to capture the split-off/derived case.
  - { name: supersedes_proposal, from: proposal, to: proposal, cardinality: many-to-one,
      relation: supersedes }
    # L3 (accepted): proposals get revised, so they deserve the same supersedes
    # family the spec↔spec edges (below) have. [parallel to supersedes_spec]

  # --- scope anchoring (H4) ---
  - { name: in_scope_of,     from: proposal, to: scope_root, cardinality: many-to-one }
    # also applies from lesson/skill/rule/agent/hook/app_type_profile → scope_root.
    # This is the edge that answers "which lessons/proposals are in scope HERE?"
    # against a concrete global (`~/.agents/`) or project (`.agents/`) root.

  # --- spec → spec (was MISSING — added per feedback) ---
  - { name: supersedes_spec, from: spec, to: spec, cardinality: many-to-one,
      relation: supersedes }
    # [evidence: scoped-knowledge-graphs/design.md "**Supersedes:** spec.1.md, spec.2.md";
    #  graph-backend §11 "go-native-code-graph-analysis — superseded-in-part by §11"]
  - { name: derived_from_spec, from: spec, to: spec, cardinality: many-to-many,
      relation: derived-from }
    # [evidence: work-tracking-storage-abstraction owns the KG schema sibling of
    #  graph-backend-adapter-contract (this very draft's "Owning spec" header)]
  - { name: related_spec,    from: spec, to: spec, cardinality: many-to-many,
      relation: related-to }
    # The design.md "**Related:**" frontmatter list, typed.
    # [evidence: graph-backend-adapter-contract/design.md "**Related:**" lists
    #  app-type-profiles, scoped-knowledge-graphs, config-distribution-model,
    #  external-agent-sources, go-native-code-graph-analysis (5 related specs);
    #  skill-tiering-contract & workflow-parallel-orchestration also carry "**Related:**"]

  # Prose projection link (typed structural node -> its tier-3 note) is the
  # prose_note ref<kg_note> field on each node (§1), not a separate edge, to keep
  # the one-note-per-artifact relationship single-valued.
```

### 2.2 Correlation edges (the §3A feedback loop — result → what produced it)

These are the edges §3A calls "the feedback loop." A `result` node fans out to
every operational + semantic node in its working set, making the
self-improvement loop *queryable* instead of anecdotal.

**Result-edge policy (per maintainer):** `result → spec` **YES**,
`result → task` **YES**, `result → plan` **conditional** (see H2/M4 below).

> **H2 / M4 reconciliation (Cursor, accepted).** The original "drop `result→plan`,
> reach it transitively via `result_for_task→belongs_to_plan`" policy has two holes:
> (1) a **`wave`/`fold_back` result spans MANY tasks**, so a many-to-one
> `result_for_task` cannot represent it and the transitive path has no single task
> to walk — leaving a plan-scoped result with **no structural anchor at all** (H2).
> (2) The dedup rationale is **inconsistent** (M4): `result→plan` was dropped to
> avoid "double-tracking the FK," yet §2.1 keeps BOTH `contains_task` (plan→task)
> and `belongs_to_plan` (task→plan) — the same FK in both directions. Resolution:
> bidirectional FK edges are accepted as the project's chosen convention (they make
> traversal symmetric and were added per maintainer feedback), so the same standard
> applies to results: add an explicit **`result_for_plan`** anchor for `wave`/
> `fold_back` results (which have no single task), keep `result_for_task` for
> task-scoped results, and retain `plan_id` on the result node as provenance. This
> removes the contradiction and gives wave-level results a real anchor.
>
> **On-disk caveat (flagged for the build, not contradicted here):** the
> `merge-back.md` frontmatter literally carries `parent_plan_id`
> [evidence: `…/delegate-merge-back-archive/2026-04-21/smoke-and-precedence-tests/merge-back.md`:
> `task_id: …`, `parent_plan_id: …`], and `impl-results.md` names a `Plan:`
> [evidence: `.agents/history/graphstore-concurrency-contract/impl-results.md`].
> The schema **deliberately does not** materialize a `result → plan` edge: the
> plan is reachable transitively via `result --result_for_task--> task
> --belongs_to_plan--> plan`, so a direct edge would double-track the FK and
> let result-quality be scored against a plan rather than the task/spec that
> actually produced it. The `parent_plan_id` on disk is retained as *provenance
> metadata on the result node*, not promoted to a traversable edge.

```yaml
  # result -> semantic (which spec it implemented) — result→spec YES
  - { name: result_implements,      from: result, to: spec, cardinality: many-to-many }
    # [evidence: impl-results.md names `Spec: .agents/workflow/specs/graphstore-concurrency-contract/design.md`]
  # result -> working (which task produced it) — result→task YES
  - { name: result_for_task,        from: result, to: task, cardinality: many-to-one }
    # task-scoped results (iteration/review/merge_back). [evidence: iter-log `task_id`; merge-back.md `task_id`]
  # result -> plan: anchor for WAVE/FOLD_BACK results that span many tasks (H2/M4 fix)
  - { name: result_for_plan,        from: result, to: plan, cardinality: many-to-one }
    # ONLY for kind ∈ {wave, fold_back} which have no single producing task.
    # [evidence: iter-log `wave` == plan id; merge-back `parent_plan_id`]. Task-scoped
    # results use result_for_task instead; the two are mutually exclusive per result.

  # result -> operational (what it ran under)
  - { name: produced_under_profile, from: result, to: stage_profile, cardinality: many-to-many }
    # [evidence: iter-log verifiers[].type ↔ .agentsrc.json verifier_sequence slugs]
  - { name: exercised_skill,        from: result, to: skill,         cardinality: many-to-many }
  - { name: exercised_rule,         from: result, to: rule,          cardinality: many-to-many }
  - { name: ran_agent,              from: result, to: agent,         cardinality: many-to-many }
  - { name: fired_hook,             from: result, to: hook,          cardinality: many-to-many }

  # result/plan/task -> app_type_profile (H3 fix: the app_type JOIN, not a dead string)
  - { name: produced_under_profile_ref, from: result, to: app_type_profile, cardinality: many-to-one }
    # which app_type_profile actually drove this run (answers "which community profile backed this result")
  # plan/task -> app_type_profile: the app_type ref resolves to its referent profile
  - { name: resolves_to,            from: task, to: app_type_profile, cardinality: many-to-one }   # task.app_type ref → profile (also from plan.default_app_type)
  - { name: composes,               from: app_type_profile, to: app_type_profile, cardinality: many-to-many }   # [mirror: §3.2 composite children] — distinguishes composite-member from selectable (L1)

  # result -> lesson (the operational-knowledge feedback loop, §4.2)
  - { name: produced_lesson,        from: result, to: lesson,        cardinality: many-to-many }
    # a failure/win during work yields/REINFORCES a lesson; closes the "info discoverable only BY DOING" loop.
    # MANY-TO-MANY (was one-to-many, M5 fix): CLAUDE.md says "update an EXISTING LESSON.md after
    # corrections," so a lesson is reinforced across MANY results over time — one-to-many discarded that history.
  - { name: applied_lesson,         from: result, to: lesson,        cardinality: many-to-many }
    # a result that consumed an in-scope lesson — lets us score "did applying lesson X change outcome?"
```

### 2.3 Code-index edges (tier 1 ↔ tier 2 bridge)

A task's `write_scope` and a result's touched files connect tier-2 nodes to the
existing tier-1 code graph. Reuses the existing `NoteSymbolLink` mechanism where
the endpoint is a code symbol; a file-level edge where the endpoint is a path.

```yaml
  - { name: write_scope_touches, from: task,   to: code_node, cardinality: many-to-many }  # code_node = existing GraphNode [mirror: tasks[].write_scope]
  - { name: result_changed,      from: result, to: code_node, cardinality: many-to-many }  # [mirror: merge-back `FilesChanged []string` paths (commands/workflow/delegation.go:217) / git diff] — NOT iter-log `files_changed`, which is an INTEGER count (schemas/workflow-iter-log.schema.json:47-51), not paths, so the file-level edge cannot be sourced from it (Codex#2)
```

(`code_node` is the existing CRG `GraphNode`; these edges are the
`NoteSymbolLink`-style bridge, not new node types.)

---

## 3. The three feedback-loop queries §3A names (expressed against this schema)

§3A lists three questions the correlation edges must answer. Each is a §5-DSL-
shaped query against the schema above (illustrative; not yet conformance-tested):

**Q1 — "Which lesson/rule/profile drove this outcome?" (result → operational)**

```
MATCH (res:result)-[:produced_under_profile]->(sp:stage_profile)
WHERE res.result_id = $result_id
RETURN sp.slug, sp.role
-- union with exercised_rule / exercised_skill / applied_lesson for the full working set
```

**Q2 — "Which specs' tasks regress most, under which verifier sequence?"
(result → semantic + operational)**

```
MATCH (res:result)-[:result_implements]->(s:spec)
MATCH (res)-[:produced_under_profile]->(v:stage_profile)
WHERE res.outcome_rollup = 'regressed' AND v.role = 'verifier'
RETURN s.spec_id, v.slug, count(*)
-- NOTE: `regressed` is the DERIVED outcome_rollup (§1.3), not the raw stored
-- outcome. The stored `outcome` mirrors iter-log verifiers[].status =
-- [pass,fail,partial,unknown] EXACTLY; `regressed` is computed by the scoring
-- layer (comparing against the prior result for the same task/spec). Filtering on
-- raw `outcome = 'regressed'` would never match mirrored iter-log data (Codex#4).
```

**Q3 — "Did adopting rule/lesson X change downstream result quality?"
(rule|lesson → results before/after)**

```
MATCH (res:result)-[:applied_lesson]->(l:lesson)
WHERE l.lesson_id = $lesson_id
RETURN res.result_id, res.outcome, res.score, res.occurred_at
-- caller compares score distribution across occurred_at windows
```

These are exactly the queries that "close `CLAUDE.md`'s self-improvement loop on
data instead of memory" (§3A) — lessons/rules/skills/stage_profiles stop being
write-only and become nodes results are scored against.

---

## 4. The four typed views (§3A) — rubric first, then mapping

### 4.1 View rubric (built BEFORE expanding the categories)

Before deciding what each view captures, this rubric fixes the **dimensions**
each KG view-category is scored on. A node/edge belongs to a view when it scores
on that view's *defining* dimensions:

| Dimension | Question it answers |
|---|---|
| **Source** | Where does this knowledge originate — a declared file, a run, a correction? |
| **Lifecycle** | Is it mutable in-flight (working), append-only history (episodic), durable definition (semantic), or routing config (operational)? |
| **Scope** | repo / project / global — who does it apply to? (the `scope` field axis) |
| **Provenance** | Can you trace *what produced it* (a result, a proposal, a correction)? |
| **What-only-doing-reveals** | Is this discoverable by reading the declared surface, or **only by running the work** (a failure, a flaky path, a real-vs-declared gap)? |
| **Surface-vs-path** | Does it describe the *full declared surface* (an API/MCP/CLI could offer) or the *path actually exercised*? |

The decisive split the rubric forces: **semantic** = durable declared
definitions (read the file and you know it); **operational** = the routing +
the *paths actually used* and *what doing the work revealed* (you only know it
by running). That distinction is what the previous draft under-modeled.

### 4.2 Operational-knowledge — EXPANDED (the maintainer's main ask)

The previous draft scoped "operational" to bare routing identities
(`stage_profile`/`skill`/`rule`/`agent`/`hook`). Per the rubric's
*what-only-doing-reveals* and *surface-vs-path* dimensions, operational
knowledge is **much broader**. It now includes:

- **Failures and wins** — `result` nodes with `outcome ∈ {fail, regressed}`
  (failures) / `{pass}` with high score (wins), edged to the spec/task and the
  profile/rule they ran under (§2.2). The wave-engine re-dispatch storm (D5) is
  the canonical *failure* this loop must make queryable.
- **Lessons** — `lesson` nodes (now first-class, §1.1), edged from the result
  that `produced_lesson` and to the results that `applied_lesson`. This is the
  literal "after ANY correction… update a LESSON.md" loop from `CLAUDE.md`,
  made structural. [evidence: `.agents/lessons/index.md`; `LESSON.md` frontmatter]
- **Info discoverable only BY DOING** — captured as `lesson`/`result.prose_note`
  content tagged via the rubric's *what-only-doing-reveals* dimension (e.g. the
  "worktree-sibling-path-buildvcs" lesson — a build-stamping failure invisible
  until a worktree at a sibling path was actually run). [evidence:
  `.agents/lessons/worktree-sibling-path-buildvcs`, `hermetic-env-for-cli-probe-tests`]
- **Specific operational PATHS (used, not the full surface)** — the rubric's
  *surface-vs-path* dimension. A `stage_profile`/`skill`/`agent` defines a
  *surface* (everything it *could* do); the operational view records the
  **path actually exercised** via the `produced_under_profile` / `exercised_*`
  edges from real `result` nodes. "Which of the verifier_sequence slugs actually
  fired and gated?" is a path question, not a surface question.
- **Patterns / notes found while working** — `lesson` of `type: pattern`, plus
  `result.prose_note` (merge-back.md narrative is precisely "notes found while
  working"). [evidence: merge-back.md per-task write-up; `CLAUDE.md` "Per-task
  Record … merge-back.md"]

### 4.3 View mapping (post-expansion)

| View (§3A) | Defining rubric dimensions | Node/edge types |
|---|---|---|
| **working** | mutable in-flight; source=declared YAML; scope=plan | `task`, `plan` (+ `status`/lease/PR/`write_scope`), `proposal` (open/draft), in-flight result state |
| **semantic** | durable declared definition; read-the-file knowability | `spec`, `proposal` (accepted/graduated), `lesson` (durable rule), `stage_profile` *definition*, rule/skill *definition* |
| **operational** | routing + path-actually-used + what-doing-reveals | `stage_profile` (routing), `skill`, `rule`, `agent`, `hook`, **`lesson` (operational know-how)**, **failure/win `result`s + the `exercised_*`/`produced_lesson`/`applied_lesson` edges (the paths actually run)** |
| **episodic** | append-only history; provenance | `result`, the correlation edges (§2.2), `result_changed` (§2.3) |

`WorkStore` (D3) is the read/write facade over the **working** view; the other
three are read-oriented services over the same store (§3A). `proposal`,
`lesson`, `skill`, and `rule` carry a `scope` field (§1) so each view can be
queried **scoped** (repo / project / global) — the OQ4 "scoped proposals and
lessons" goal.

---

## 5. Open schema details (for the future work-tracking plan, not resolved here)

These are deliberately left for the `work-tracking-storage-abstraction` plan to
pin (they depend on the `WorkStore`/daemon build, gated on
`graph-backend-adapter-contract` landing):

- **`task` coordination-state fields are PROPOSED, not on-disk** — EXCEPT
  `app_type`. `lease_owner`, `lease_expires_at`, `pr_url`, `branch` (§1.1) do
  **not** exist in `schemas/workflow-tasks.schema.json` today (the real task
  carries only `id/title/status/depends_on/blocks/owner/write_scope/
  verification_required/notes`). Whether the WorkStore adds them to TASKS.yaml or
  holds them graph-only is a build decision; the schema marks them `[PROPOSED]`.
  **Correction (Codex#5):** `app_type` is **NOT** in that PROPOSED set — it is
  already on disk. `CanonicalTask.AppType` exists in
  `commands/workflow/types.go` (`app_type,omitempty`) and live TASKS.yaml carry
  it (e.g. `docs-starlight-migration/TASKS.yaml` tasks have `app_type: docs` /
  `app_type: go-cli`). The earlier "task app_type is `[PROPOSED]`/not on disk"
  claim was wrong and is corrected in §1.1.

- **SCHEMA-DRIFT FOLLOW-UP (flagged, NOT fixed here) — `workflow-tasks.schema.json`
  rejects the `app_type` field the code + artifacts already use.** This is a real
  bug, not a doc issue: `schemas/workflow-tasks.schema.json` sets
  `additionalProperties: false` on each task item and does **not** list
  `app_type` among the properties, so a TASKS.yaml that carries `app_type` (which
  `CanonicalTask` marshals and live plans like `docs-starlight-migration` already
  write) fails schema validation. **Recommended fix-task (route via `da workflow`,
  do not hand-edit here):** add an `app_type` property (open string, optional) to
  the task-item `properties` in `schemas/workflow-tasks.schema.json` so the schema
  matches `CanonicalTask` and the live artifacts. Scope: `schemas/workflow-tasks.schema.json`
  only; verification: a TASKS.yaml carrying `app_type` validates. This write-scope
  is intentionally OUT of this docs-only PR.
- **Lease semantics** — `lease_owner` + `lease_expires_at` as a TTL lease vs a
  separate compare-and-set primitive (§6 open question "Atomic claim/lease
  semantics"). Drafted as fields; the CAS mechanism is implementation.
- **Projection regeneration** — how `task`/`plan` typed nodes regenerate the
  `.agents/workflow/**` YAML losslessly (§6 "Projection fidelity"). The schema
  supports it (typed fields round-trip to YAML keys); the diff-stability
  guarantee is build work.
- **Status-enum reconciliation** — this draft mirrors the *on-disk* enums
  (`task: pending|in_progress|blocked|completed|cancelled`;
  `plan: draft|active|paused|completed|archived`). If the WorkStore needs the
  richer lifecycle (claimed/in_flight/awaiting_review for D5 leasing), that is a
  schema *extension* of the on-disk enum, applied via the workflow CLI — not a
  silent redefinition.
- **Snapshot cadence** — what (if any) of the graph is committed to git for
  offline/audit replay (§6 "Git vs backend double-tracking", resolved-by-D1′
  remaining detail).
- **Adapter vs core tables** — whether these typed nodes ship as a graph-backend
  adapter (riding the §4 adapter vocabulary used above) or as core typed tables.
- **`stage_profile` dual-residence** — it appears in both semantic (definition)
  and operational (routing) views; whether that is one node with two view
  memberships or two nodes is a view-layer decision.

---

## 5A. Reconciliation — adversarial pass (Codex + Cursor, 2026-06-24)

This revision folds the validated findings from the Codex and Cursor adversarial
reviews on PR #136. Each finding was re-verified against the cited on-disk
code/schema before acceptance (a second brain can also be wrong). Disposition:

| Finding | Verdict | Verification evidence | Where folded |
|---|---|---|---|
| **Codex#3 / Cursor H1** — task/result ids are plan-local; cross-plan deps are code-enforced | **ACCEPT** | `base_resolution.go:223` ("`may include cross-plan <plan>/<task> ids`") + `TestSelectAllEligibleTasks_CrossPlanDepMissingPlan`; `plan_id` is a TASKS.yaml file-header (`workflow-tasks.schema.json`), not per-task; `CanonicalTask` has no PlanID field | §1.1 `task_key`+`plan_id`; §1.3 `result_key`+`plan_id`+`parent_task_key` |
| **Codex#1** — `proposal` node missing `scope` | **ACCEPT** | proposal fields stopped at `prose_note`; §4.3 + §1.1 note already claim scoped proposals | §1.1 `proposal.scope` |
| **Codex#2** — result→code edge sourced from wrong artifact | **ACCEPT** | iter-log `files_changed` = INTEGER (`workflow-iter-log.schema.json:47`); merge-back `FilesChanged []string` (`delegation.go:217`) | §2.3 `result_changed` citation |
| **Codex#4** — `outcome` enum not a mirror | **ACCEPT** | schema enum = `[pass,fail,partial,unknown]` (`:221`); draft had `regressed` | §1.3 raw `outcome` + derived `outcome_rollup`; §3-Q2 query |
| **Codex#5** — app_type provenance missing + live data misclassified as PROPOSED | **ACCEPT** | `CanonicalTask.AppType` exists; `docs-starlight-migration/TASKS.yaml` carries `app_type: docs`; `CONFIG_RELEVANCE.md` defines `app_type_source`/`matched`; schema rejects the field (`additionalProperties:false`, no `app_type`) | §1.1 provenance fields + correction; §5 schema-drift follow-up |
| **Codex#6 / rubric** — operational knowledge prose-only, not queryable | **ACCEPT** | result/lesson had no tags/dimensions | §1.1 lesson + §1.3 result materialization fields |
| **Cursor H2** — `result_for_task` many-to-one can't represent wave/fold_back; breaks transitive plan path | **ACCEPT** | `kind` includes `wave`/`fold_back` which span many tasks | §2.2 `result_for_plan` anchor |
| **Cursor H3** — app_type is a dead string; no profile node/edge | **ACCEPT** | ref form `source-id:name@version` (`app-type-profiles §7.1`) needs a referent | §1.2 `app_type_profile` node; §2.2 `resolves_to`/`produced_under_profile_ref`/`composes` edges |
| **Cursor H4** — `scope` has no anchor entity; inconsistent enums | **ACCEPT** | `proposal-routing.md` global vs project boundary; `rule.scope=[global,project]` vs others `[repo,project,global]` | §1.2 `scope_root` node + enum-reconciliation note; §2.1 `in_scope_of` edge |
| **Cursor M1** — correlation edges carry no version/hash; defeats Q3 | **ACCEPT** | edges point at versionless nodes; O5 makes content-hash the mutation primitive | §1.2 `content_hash`/`version` on rule/lesson/skill/stage_profile |
| **Cursor M2** — invented fields not `[PROPOSED]` (score, spec.status, proposal.status) | **ACCEPT** | iter-log has no `score`; no spec JSON Schema; only `draft/promoted/deferred` attested | §1.1 status notes; §1.3 `score [PROPOSED]` |
| **Cursor M3** — KGNote projection double-track + date mismatch + required-on-nonexistent | **ACCEPT** | `store.go:104-114` `KGNote{Status;Version;ArchivedAt RFC3339;IndexedAt float64}` | §1.4 authority rule + date mapping + `prose_note` relaxed to optional |
| **Cursor M4** — dedup applied inconsistently (drop result→plan but keep both task/plan FK edges) | **ACCEPT** | §2.1 has both `contains_task` + `belongs_to_plan` | §2.2 reconciliation note (bidirectional FK accepted; result→plan restored for waves) |
| **Cursor M5** — `produced_lesson` one-to-many loses reinforcement history | **ACCEPT** | `CLAUDE.md` "update an EXISTING LESSON.md after corrections" | §2.2 made many-to-many |
| **Cursor L1** — composite-children app_types conflated with selectable | **ACCEPT** | §3.2 `composes: [api,batch,streaming]` | §1.0 table marked composite-member/inferred |
| **Cursor L2** — hook/agent no id, no scope | **ACCEPT** | bare event+name / name collide across scopes | §1.2 `hook_id`/`agent_id` + `scope` |
| **Cursor L3** — no proposal→proposal supersedes edge | **ACCEPT** | spec↔spec family exists; proposals are revised | §2.1 `supersedes_proposal` |
| **Cursor L4** — plan→spec modeled many-to-one but may be many-to-many | **ACCEPT** | this PR's dossier spans 5 owning specs | §2.1 `plan_for_spec` → many-to-many |

### Reconciliation — rejected / deferred

No finding was rejected outright — every cited contradiction verified against the
on-disk source. The following are **partially deferred to the build** (accepted in
principle, not fully materialized in this draft, with one-line rationale):

- **`spec`/`plan`/`task` scope fields (part of H4):** *Deferred to the work-tracking
  build* — accepted that specs/plans are global-vs-project scopable, but adding the
  field is build surface (no on-disk scope field exists for them yet); flagged in
  the §1.2 enum-reconciliation note rather than invented here.
- **`workflow-tasks.schema.json` app_type drift FIX (part of Codex#5):** *Deferred —
  out of this PR's docs-only write_scope.* Recorded as an explicit fix-task
  recommendation in §5 (route via `da workflow`); the schema is intentionally not
  edited here.
- **Canonical `date` wire form (part of M3):** *Deferred to the build* — accepted
  that typed `date` does not map 1:1 to the store's float64/RFC3339; the build must
  pick one canonical form. Flagged in §1.4, not pinned here (it is a storage-layer
  decision, not a schema-design one).

---

## 6. Research / evidence ledger (grounding for the adversarial pass)

Every cardinality and "real field" claim above is grounded in one of these
checked artifacts. Reviewers: challenge any inline claim that is not in this
ledger.

**Node-field mirrors (on-disk schemas / frontmatter):**

- `schemas/workflow-plan.schema.json` — plan required = `schema_version,id,
  title,status,summary,created_at,updated_at`; status enum =
  `draft|active|paused|completed|archived`; also `owner,success_criteria,
  verification_strategy,current_focus_task,default_app_type`.
- `schemas/workflow-tasks.schema.json` — top `schema_version,plan_id,tasks`;
  task item required = `id,title,status,depends_on,blocks,write_scope,
  verification_required`; optional `owner,notes`; status enum =
  `pending|in_progress|blocked|completed|cancelled`. **No** app_type/lease/
  pr_url on disk.
- `schemas/workflow-iter-log.schema.json` — result fields: `schema_version,
  iteration,date,wave,task_id,impl,verifiers[],review` (+ `commit,
  files_changed,agent,session_tokens`); verifiers[].status =
  `pass|fail|partial|unknown`.
- `internal/config/execution_profile.go:19-22` — `ByAppType map[string]
  AppTypeProfile` → app_type is an open string key.
- spec frontmatter: `graph-backend-adapter-contract/design.md` lines 1-14
  (`**Status:** draft v5`, `**Supersedes:**`, `**Related:**` list, `**Plan:**`);
  `scoped-knowledge-graphs/design.md:6` (`**Supersedes:** spec.1.md, spec.2.md`).
- proposal shapes: `.agents/proposals/kg-ideate-skill.yaml`
  (`schema_version,id,status,type,action,target,rationale`);
  `.agents/proposals/agent-context-resolution-architecture.md` frontmatter
  (`**Status:**/**Written:**/**Author:**/**Scope:**`).
- lesson shape: `.agents/lessons/additive-state-fields/LESSON.md` frontmatter
  (`name,description,type: feedback`); `.agents/lessons/index.md` one-liner index.

**Edge-cardinality evidence (real cases found):**

- **spec→plan ONE-TO-MANY:** `config-distribution-model` spec ⇒ **two** plans —
  `.agents/workflow/plans/config-v2-migration/PLAN.yaml` and
  `.agents/workflow/plans/config-v2-coherence/PLAN.yaml` both name it as owning
  spec (grep-confirmed). Also `.agents/proposals/agent-run-scoring-observability-platform.md`
  records its spec was "promoted to a workflow spec + **four** plans."
- **proposal→spec (one ⇒ one-or-more):** `graph-backend-adapter-contract/design.md`
  `**Supersedes:** .agents/proposals/graph-backend-adapter-contract.md (proposal
  accepted; this spec is the canonical artifact)`; `agent-run-scoring` proposal
  promoted to spec + four plans.
- **spec→spec:** `scoped-knowledge-graphs/design.md` `**Supersedes:** spec.1.md,
  spec.2.md`; `graph-backend §11` `go-native-code-graph-analysis — superseded-in-part`;
  `**Related:**` lists in `graph-backend-adapter-contract`, `skill-tiering-contract`,
  `workflow-parallel-orchestration`, `shared-target-projection-wiring`.
- **task→plan FK:** `TASKS.yaml` `plan_id` header + `tasks[]` array;
  `.agents/workflow/plans/loop-discipline-stop-hooks/TASKS.yaml` = 17 tasks under
  one `plan_id`.
- **result→{spec,task}, NOT plan:** `impl-results.md`
  (`.agents/history/graphstore-concurrency-contract/impl-results.md`) names
  `Plan:` + `Spec:`; `merge-back.md`
  (`…/delegate-merge-back-archive/2026-04-21/smoke-and-precedence-tests/merge-back.md`)
  carries `task_id` + `parent_plan_id`; iter-log carries `task_id`. The
  `parent_plan_id`/`Plan:` are kept as provenance metadata on the result node,
  **not** a traversable `result→plan` edge (§2.2 policy).

**Could NOT ground (reported, not invented):**

- **payout repo:** `AGOrcha/payout` does not exist; `NikashPrakash/payout` is
  reachable but is an **older app-only snapshot** (`client-ui`, `client-se`,
  `stack`, `swarm`, `payout-prd.yaml`) with **no `.agents/workflow/` tree** — so
  no payout-side edge-case evidence was available. The only signal carried over
  is that its `client-ui`/`client-se` split corroborates the `ui`/`api`
  known-starter app_types named in `app-type-profiles` §3.2/§8.2.
- The local `~/Documents/payout` is TCC-locked (per the brief) and was not read.

---

*This schema is a PROPOSAL drafted to give O4 a concrete artifact for review,
now field-mirrored and evidence-grounded for an adversarial (Codex + Cursor)
pass. It is the design foundation for the future `work-tracking-storage-abstraction`
plan's `kg` `WorkStore` and typed-view services; it lands no code until that plan
is created (gated on `graph-backend-adapter-contract` completing). Ratify the
design now; defer the build.*
