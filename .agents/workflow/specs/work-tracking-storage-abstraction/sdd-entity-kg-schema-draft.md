# SDD-Entity KG Schema — Draft (companion to O4)

**Status:** PROPOSAL — IDEATION draft awaiting owner ratification. This is the
concrete node/edge schema the O4 recommendation
([`graph-backend-adapter-contract/open-questions-resolutions.md` §O4](../graph-backend-adapter-contract/open-questions-resolutions.md))
commits to in principle: **typed nodes for SDD structure + state, with a
`KGNote` projection for prose body.** Nothing here is normative until ratified.
**Written:** 2026-06-23 · **Revised:** 2026-06-24 (field-mirroring pass, open
app_type vocabulary, expanded edge catalog with repo evidence, proposals +
lessons as first-class scoped nodes, rubric-driven view expansion).
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
| `api`, `ui`, `streaming`, `batch` | `app-type-profiles/design.md` §3.2 composite children (`composes: [api, batch, streaming]`); `ui`/`api` mirror the payout `client-ui`/`client-se` split [evidence: `NikashPrakash/payout` repo root: `client-ui`, `client-se`] |
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
    fields:
      - { name: task_id,            type: string, required: true }   # [mirror: schema `id`]
      - { name: title,              type: string, required: true }   # [mirror: `title`]
      - { name: status,             type: enum,
          values: [pending, in_progress, blocked, completed, cancelled], required: true }  # [mirror: schema `status` enum — was [pending,claimed,in_flight,awaiting_review,done,blocked]; corrected]
      - { name: depends_on,         type: list<string>, required: true }   # [mirror: `depends_on`] task ids
      - { name: blocks,             type: list<string>, required: true }   # [mirror: `blocks`] task ids
      - { name: write_scope,        type: list<string>, required: true }   # [mirror: `write_scope`] path globs
      - { name: verification_required, type: bool, required: true }        # [mirror: `verification_required`]
      - { name: owner,              type: string, required: false }        # [mirror: `owner`]
      - { name: notes,              type: string, required: false }        # [mirror: `notes`]
      # --- coordination state the on-disk TASKS.yaml does NOT yet carry ---
      # These are the D5 atomic-transition surface the future WorkStore adds.
      - { name: app_type,           type: string, required: false }   # [PROPOSED] OPEN vocab (§1.0); on disk app_type lives only at plan.default_app_type today
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
      - { name: status,     type: enum, values: [draft, ratified, superseded], required: true }  # [mirror: **Status:** line, e.g. "draft v5 (canonical)"]
      - { name: revision,   type: string, required: false }  # [mirror: **Status:**/**Written:** revision tail, e.g. "v6.1"]
      - { name: prose_note, type: ref<kg_note>, required: true }   # the design.md body (tier 3)
      # Note: Supersedes/Related/Plan frontmatter lines are modeled as EDGES (§2.1), not fields.

  - name: proposal
    # Mirrors the canonical .yaml proposal (schemas + .agents/proposals/kg-ideate-skill.yaml)
    # and the prose .md proposal frontmatter (Status / Written / Author / Scope).
    fields:
      - { name: proposal_id, type: string, required: true }   # [mirror: yaml `id` / md filename]
      - { name: title,       type: string, required: true }   # [mirror: H1 / yaml-derived]
      - { name: status,      type: enum,
          values: [draft, open, accepted, promoted, graduated, rejected, deferred], required: true }  # [mirror: yaml `status: draft`; md "**Status:** promoted (2026-05-25)" / "deferred"]
      - { name: type,        type: string, required: false }  # [mirror: yaml `type: skill|spec`]
      - { name: action,      type: enum, values: [create, modify, retire], required: false }  # [mirror: yaml `action: create`]
      - { name: target,      type: string, required: false }  # [mirror: yaml `target.installed_at`/`starter_source`]
      - { name: prose_note,  type: ref<kg_note>, required: true }   # the proposal body / rationale (tier 3)

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
      - { name: prose_note,  type: ref<kg_note>, required: true }   # the LESSON.md body (tier 3)
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
  - name: stage_profile
    # Mirrors .agentsrc.json stage_profiles.{verifier,reviewer,...}.<slug>.
    fields:
      - { name: slug, type: string, required: true }   # [mirror: stage_profiles.<role>.<slug> key, e.g. citation-check]
      - { name: role, type: enum, values: [executor, verifier, reviewer, orchestrator], required: true }  # [mirror: stage_profiles.<role>]
      - { name: label, type: string, required: false }  # [mirror: stage_profiles.<role>.<slug>.label]
      - { name: prose_note, type: ref<kg_note>, required: false }   # [mirror: prompt_files]

  - name: skill
    fields:
      - { name: name,       type: string, required: true }          # [mirror: .agentsrc.json `skills[]` entry / SKILL.md dir]
      - { name: scope,      type: enum, values: [repo, project, global], required: false }  # [PROPOSED] scoped skills
      - { name: prose_note, type: ref<kg_note>, required: false }   # SKILL.md

  - name: rule
    fields:
      - { name: rule_id,    type: string, required: true }          # [mirror: .agentsrc.json `rules[]` entry: global|project]
      - { name: scope,      type: enum, values: [global, project], required: false }  # [mirror: rules[] values]
      - { name: prose_note, type: ref<kg_note>, required: false }

  - name: agent
    fields:
      - { name: name, type: string, required: true }                # [mirror: .agentsrc.json `agents[]`, e.g. loop-worker]

  - name: hook
    fields:
      - { name: event, type: string, required: true }               # [mirror: schemas/hook.schema.json event]
      - { name: name,  type: string, required: true }
```

### 1.3 Episodic-view nodes (tier 2: results + history)

```yaml
  - name: result
    # Mirrors the three real result artifacts:
    #  - iter-log: schemas/workflow-iter-log.schema.json (iteration / wave / task_id / impl / verifiers[] / review / outcome)
    #  - merge-back.md frontmatter (schema_version / task_id / parent_plan_id)
    #  - history/<plan>/impl-results.md (Plan / Spec references)
    fields:
      - { name: result_id,   type: string, required: true }   # [mirror: iter-log `iteration` (iter-N) / wave-M]
      - { name: kind,        type: enum, values: [iteration, wave, fold_back, review, merge_back], required: true }
      - { name: outcome,     type: enum, values: [pass, fail, partial, regressed], required: false }  # [mirror: iter-log verifiers[].status]
      - { name: score,       type: float, required: false }    # rescore-loop signal
      - { name: occurred_at, type: date,  required: true }      # [mirror: iter-log `date`]
      - { name: wave,        type: string, required: false }    # [mirror: iter-log `wave` == active plan id]
      - { name: prose_note,  type: ref<kg_note>, required: false }   # merge-back.md / impl-results.md / iter-log
```

### 1.4 Tier-3 prose projection (existing `KGNote` shape)

`kg_note` is the existing `KGNote` record (`store.go:104`). It is **not
re-defined** here — typed nodes reference it via `prose_note: ref<kg_note>`.
The `KGNote.FilePath` field points at the git-canonical markdown body
(D1′ tier 3), and `IndexedAt`/`Summary` feed the semantic view. This is the
"projection" half of the O4 recommendation: prose stays a file, indexed as a
note, referenced by the typed structural node.

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
  - { name: plan_for_spec,   from: plan, to: spec, cardinality: many-to-one }
    # spec→plan is ONE-TO-MANY (one spec ⇒ many plans), so plan→spec is many-to-one.
    # [evidence: spec `config-distribution-model` ⇒ TWO plans: config-v2-migration
    #  AND config-v2-coherence both name it as owning spec in PLAN.yaml]
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
`result → task` **YES**, `result → plan` **NO**.

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
    # [evidence: iter-log `task_id`; merge-back.md `task_id`]
  # result -> plan: INTENTIONALLY OMITTED (reachable via result_for_task→belongs_to_plan)

  # result -> operational (what it ran under)
  - { name: produced_under_profile, from: result, to: stage_profile, cardinality: many-to-many }
    # [evidence: iter-log verifiers[].type ↔ .agentsrc.json verifier_sequence slugs]
  - { name: exercised_skill,        from: result, to: skill,         cardinality: many-to-many }
  - { name: exercised_rule,         from: result, to: rule,          cardinality: many-to-many }
  - { name: ran_agent,              from: result, to: agent,         cardinality: many-to-many }
  - { name: fired_hook,             from: result, to: hook,          cardinality: many-to-many }

  # result -> lesson (the operational-knowledge feedback loop, §4.2)
  - { name: produced_lesson,        from: result, to: lesson,        cardinality: one-to-many }
    # a failure/win during work yields a lesson; closes the "info discoverable only BY DOING" loop
  - { name: applied_lesson,         from: result, to: lesson,        cardinality: many-to-many }
    # a result that consumed an in-scope lesson — lets us score "did applying lesson X change outcome?"
```

### 2.3 Code-index edges (tier 1 ↔ tier 2 bridge)

A task's `write_scope` and a result's touched files connect tier-2 nodes to the
existing tier-1 code graph. Reuses the existing `NoteSymbolLink` mechanism where
the endpoint is a code symbol; a file-level edge where the endpoint is a path.

```yaml
  - { name: write_scope_touches, from: task,   to: code_node, cardinality: many-to-many }  # code_node = existing GraphNode [mirror: tasks[].write_scope]
  - { name: result_changed,      from: result, to: code_node, cardinality: many-to-many }  # [mirror: iter-log files_changed / git diff]
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
WHERE res.outcome = 'regressed' AND v.role = 'verifier'
RETURN s.spec_id, v.slug, count(*)
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

- **`task` coordination-state fields are PROPOSED, not on-disk.** `app_type`,
  `lease_owner`, `lease_expires_at`, `pr_url`, `branch` (§1.1) do **not** exist
  in `schemas/workflow-tasks.schema.json` today (the real task carries only
  `id/title/status/depends_on/blocks/owner/write_scope/verification_required/
  notes`). Whether the WorkStore adds them to TASKS.yaml or holds them
  graph-only is a build decision; the schema marks them `[PROPOSED]` so the
  build knows it is *adding* surface.
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
