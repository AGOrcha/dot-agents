# SDD-Entity KG Schema — Draft (companion to O4)

**Status:** PROPOSAL — IDEATION draft awaiting owner ratification. This is the
concrete node/edge schema the O4 recommendation
([`graph-backend-adapter-contract/open-questions-resolutions.md` §O4](../graph-backend-adapter-contract/open-questions-resolutions.md))
commits to in principle: **typed nodes for SDD structure + state, with a
`KGNote` projection for prose body.** Nothing here is normative until ratified.
**Written:** 2026-06-23
**Author:** Nikash Prakash
**Owning spec:** `work-tracking-storage-abstraction/design.md` (§6 open question
"KG schema for SDD entities + correlation edges"; D1′ tiers; §3A typed views +
correlation).

---

## 0. Design posture (from O4)

D1′ defines three canonical tiers; this schema places each SDD concern in the
tier the spec already ratified:

| Tier | What | Representation in this schema |
|---|---|---|
| 1. Code | functions/types/files | **Existing** `GraphNode`/`GraphEdge` (CRG). Indexed, never authored here. |
| 2. SDD structure + state | Plan/Task/Spec/Proposal entities, their relationships, coordination state | **New typed nodes + typed edges** (this draft). KG-canonical. |
| 3. SDD prose body | the narrative markdown of a spec/plan/lesson | **`KGNote` projection** (existing shape), file-backed + semantic-indexed. The typed node references it. |

**Why typed nodes (not flat `KGNote` rows):** coordination state (status, leases,
PR linkage, `depends_on`, `write_scope`, `verifier_sequence`) and the §3A
correlation edges are *relationships and typed fields*, not free text. A flat
`KGNote.Status` string cannot carry a CAS lease (D5) or answer "which verifier
sequence regresses spec X" (§3A). **Why keep `KGNote` for prose:** a spec/plan
narrative *is* a note-shaped, file-backed, searchable artifact; the existing
semantic-view machinery already indexes it (D1′ tier 3).

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

### 1.1 Working-view nodes (tier 2: structure + coordination state)

```yaml
note_types:
  - name: plan
    fields:
      - { name: plan_id,        type: string, required: true }   # e.g. graph-backend-adapter-contract
      - { name: title,          type: string, required: true }
      - { name: status,         type: enum, values: [draft, active, blocked, archived], required: true }
      - { name: schema_version, type: int,    required: true }
      - { name: prose_note,     type: ref<kg_note>, required: false }   # tier-3 .plan.md body
      - { name: created_at,     type: date }
      - { name: archived_at,    type: date }

  - name: task
    fields:
      - { name: task_id,            type: string, required: true }
      - { name: title,              type: string, required: true }
      # Coordination state — the D5 atomic-transition surface. Backend-canonical.
      - { name: status,             type: enum,
          values: [pending, claimed, in_flight, awaiting_review, done, blocked], required: true }
      - { name: lease_owner,        type: string,  required: false }   # worker/agent id holding the claim
      - { name: lease_expires_at,   type: date,    required: false }   # TTL lease (D5 / §6 open q)
      - { name: pr_url,             type: string,  required: false }   # PR/branch linkage
      - { name: branch,             type: string,  required: false }
      - { name: app_type,           type: enum,
          values: [implementation, release, ideation, analysis, docs], required: false }
      - { name: verification_required, type: bool, required: false }
      - { name: owner,              type: string,  required: false }
      - { name: prose_note,         type: ref<kg_note>, required: false }

  - name: spec
    fields:
      - { name: spec_id,    type: string, required: true }   # e.g. scoped-knowledge-graphs
      - { name: title,      type: string, required: true }
      - { name: status,     type: enum, values: [draft, ratified, superseded], required: true }
      - { name: revision,   type: string, required: false }  # e.g. "v6.1"
      - { name: prose_note, type: ref<kg_note>, required: true }   # the design.md body (tier 3)

  - name: proposal
    fields:
      - { name: proposal_id, type: string, required: true }
      - { name: title,       type: string, required: true }
      - { name: status,      type: enum, values: [open, accepted, rejected, graduated], required: true }
      - { name: prose_note,  type: ref<kg_note>, required: true }
```

### 1.2 Operational-view nodes (tier 2: what produced a result)

These are the §3A "operational" view — the primitives a result is scored
against. Lightweight identity nodes; their prose/config lives in files.

```yaml
  - name: stage_profile
    fields:
      - { name: slug, type: string, required: true }   # executor/verifier/reviewer/orchestrator profile slug
      - { name: role, type: enum, values: [executor, verifier, reviewer, orchestrator], required: true }
      - { name: prose_note, type: ref<kg_note>, required: false }

  - name: skill
    fields:
      - { name: name,       type: string, required: true }
      - { name: prose_note, type: ref<kg_note>, required: false }   # SKILL.md

  - name: rule
    fields:
      - { name: rule_id,    type: string, required: true }
      - { name: prose_note, type: ref<kg_note>, required: false }

  - name: agent
    fields:
      - { name: name, type: string, required: true }

  - name: hook
    fields:
      - { name: event, type: string, required: true }
      - { name: name,  type: string, required: true }
```

### 1.3 Episodic-view nodes (tier 2: results + history)

```yaml
  - name: result
    fields:
      - { name: result_id,  type: string, required: true }   # iter-N / wave-M result id
      - { name: kind,       type: enum, values: [iteration, wave, fold_back, review], required: true }
      - { name: outcome,    type: enum, values: [pass, fail, partial, regressed], required: true }
      - { name: score,      type: float, required: false }    # rescore-loop signal
      - { name: occurred_at, type: date, required: true }
      - { name: prose_note, type: ref<kg_note>, required: false }   # merge-back.md / iter-log
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

### 2.1 Structural / trace edges (spec ↔ plan ↔ task)

```yaml
edge_types:
  # Plan/task structure
  - { name: contains_task,   from: plan,     to: task,     cardinality: one-to-many }
  - { name: depends_on,      from: task,     to: task,     cardinality: many-to-many }   # incl. cross-plan
  - { name: blocks,          from: task,     to: task,     cardinality: many-to-many }   # inverse of depends_on

  # SDD trace (the spec↔plan↔task chain D1′ requires queryable)
  - { name: plan_for_spec,   from: plan,     to: spec,     cardinality: many-to-many }
  - { name: implements_spec, from: task,     to: spec,     cardinality: many-to-many }
  - { name: graduated_from,  from: spec,     to: proposal, cardinality: one-to-one }     # proposal -> spec
  - { name: supersedes_spec, from: spec,     to: spec,     cardinality: many-to-one }

  # Prose projection link (typed structural node -> its tier-3 note)
  # Expressed as the prose_note ref<kg_note> field on each node (§1), not a
  # separate edge, to keep the one-note-per-artifact relationship single-valued.
```

### 2.2 Correlation edges (the §3A feedback loop — result → what produced it)

These are the edges §3A calls "the feedback loop." A `result` node fans out to
every operational + semantic node in its working set, making the
self-improvement loop *queryable* instead of anecdotal.

```yaml
  # result -> semantic (which spec/plan/task it implemented)
  - { name: result_implements,      from: result, to: spec, cardinality: many-to-many }
  - { name: result_for_task,        from: result, to: task, cardinality: many-to-one }

  # result -> operational (what it ran under)
  - { name: produced_under_profile, from: result, to: stage_profile, cardinality: many-to-many }
  - { name: exercised_skill,        from: result, to: skill,         cardinality: many-to-many }
  - { name: exercised_rule,         from: result, to: rule,          cardinality: many-to-many }
  - { name: ran_agent,              from: result, to: agent,         cardinality: many-to-many }
  - { name: fired_hook,             from: result, to: hook,          cardinality: many-to-many }
```

### 2.3 Code-index edges (tier 1 ↔ tier 2 bridge)

A task's `write_scope` and a result's touched files connect tier-2 nodes to the
existing tier-1 code graph. Reuses the existing `NoteSymbolLink` mechanism where
the endpoint is a code symbol; a file-level edge where the endpoint is a path.

```yaml
  - { name: write_scope_touches, from: task,   to: code_node, cardinality: many-to-many }  # code_node = existing GraphNode
  - { name: result_changed,      from: result, to: code_node, cardinality: many-to-many }
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
-- union with exercised_rule / exercised_skill for the full working set
```

**Q2 — "Which specs' tasks regress most, under which verifier sequence?"
(result → semantic + operational)**

```
MATCH (res:result)-[:result_implements]->(s:spec)
MATCH (res)-[:produced_under_profile]->(v:stage_profile)
WHERE res.outcome = 'regressed' AND v.role = 'verifier'
RETURN s.spec_id, v.slug, count(*)
```

**Q3 — "Did adopting rule X change downstream result quality?"
(rule → results before/after)**

```
MATCH (res:result)-[:exercised_rule]->(r:rule)
WHERE r.rule_id = $rule_id
RETURN res.result_id, res.outcome, res.score, res.occurred_at
-- caller compares score distribution across occurred_at windows
```

These are exactly the queries that "close `CLAUDE.md`'s self-improvement loop on
data instead of memory" (§3A) — lessons/rules/skills/stage_profiles stop being
write-only and become nodes results are scored against.

---

## 4. Mapping to the four typed views (§3A)

| View (§3A) | Node types in this draft |
|---|---|
| **working** | `task`, `plan` (+ lease/status/PR fields), result-in-flight state |
| **semantic** | `spec`, `proposal`, `stage_profile` (the profile *definition*) |
| **operational** | `stage_profile` (routing), `skill`, `rule`, `agent`, `hook` |
| **episodic** | `result`, the correlation edges (§2.2), `result_changed` (§2.3) |

`WorkStore` (D3) is the read/write facade over the **working** view; the other
three are read-oriented services over the same store (§3A).

---

## 5. Open schema details (for the future work-tracking plan, not resolved here)

These are deliberately left for the `work-tracking-storage-abstraction` plan to
pin (they depend on the `WorkStore`/daemon build, gated on
`graph-backend-adapter-contract` landing):

- **Lease semantics** — `lease_owner` + `lease_expires_at` as a TTL lease vs a
  separate compare-and-set primitive (§6 open question "Atomic claim/lease
  semantics"). Drafted as fields; the CAS mechanism is implementation.
- **Projection regeneration** — how `task`/`plan` typed nodes regenerate the
  `.agents/workflow/**` YAML losslessly (§6 "Projection fidelity"). The schema
  supports it (typed fields round-trip to YAML keys); the diff-stability
  guarantee is build work.
- **Snapshot cadence** — what (if any) of the graph is committed to git for
  offline/audit replay (§6 "Git vs backend double-tracking", resolved-by-D1′
  remaining detail).
- **Adapter vs core tables** — whether these typed nodes ship as a graph-backend
  adapter (riding the §4 adapter vocabulary used above) or as core typed tables.
  Expressed adapter-shape here for forward-compatibility with the D3/§8 seam
  relationship; the choice is the plan's.
- **`stage_profile` dual-residence** — it appears in both semantic (definition)
  and operational (routing) views; whether that is one node with two view
  memberships or two nodes is a view-layer decision.

---

*This schema is a PROPOSAL drafted to give O4 a concrete artifact for review. It
is the design foundation for the future `work-tracking-storage-abstraction`
plan's `kg` `WorkStore` and typed-view services; it lands no code until that plan
is created (gated on `graph-backend-adapter-contract` completing). Ratify the
design now; defer the build.*
