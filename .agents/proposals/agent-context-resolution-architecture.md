# Agent Context Resolution — Architecture Note

**Status:** project-local architecture note (precursor to a synthesis spec)
**Written:** 2026-05-03
**Revised:** 2026-05-03 (added §1.5 resource graduation matrix, §1.6 execution telemetry pillar, §6.5 audit-confirmed pipeline state)
**Author:** drafted with agent assist
**Scope:** orientation document for four converging specs; not yet a design contract.

---

## 0. Why this note exists

Four specs in `.agents/workflow/specs/` are evolving in parallel and their
boundaries are beginning to overlap. Each draft is internally coherent, but
none of them owns the question that running agents actually need answered:
**at the moment a task is dispatched, how does the agent get the right
knowledge, the right tools, and the right bounds?**

This note frames the four specs against that question, names the synthesis
spec they all need (provisionally `agent-context-resolution`), and maps every
new behavior onto either an existing command extension or a small set of new
commands. It is not a design spec — it is the orientation that lets the
existing drafts stop drifting from each other.

---

## 1. The four anchor specs

### 1.1 `scoped-knowledge-graphs` — data model and storage

- **Role:** the substrate. Defines what a knowledge fact is, where it lives,
  how staleness is detected, how derivations propagate, how contradictions
  are surfaced.
- **Owns:** scope chain (`repo → user → team → org`), driver-event staleness
  model (§2.5), derivation propagation (§2.6), contradiction field (§3.2),
  source-hash and content-addressed notes (gap-adopt from
  multi-agent-memory-dkg).
- **Status:** draft v3 (canonical, post-Codex review).
- **Currently missing:** explicit Read API. The spec defines the model but
  not how a running agent queries it.

### 1.2 `skill-tiering-contract` — composition vocabulary and runtime invariants

- **Role:** the bounds. Defines what level of agent autonomy each artifact
  type carries, what each tier may compose into, and what verification each
  tier requires.
- **Owns:** atom / molecule / compound / cell / organism vocabulary;
  per-tier invariants (determinism, allowed downstream calls,
  verification posture, attendance model); declarative tier metadata.
- **Status:** draft (D1 proposed; D2–D5 open).
- **Currently missing:** runtime enforcement protocol. D1 proposes a
  vocabulary; the contract has no point of consumption yet.

### 1.3 `app-type-profiles` — per-app verifier and backend selection

- **Role:** the projection. Names which verifier chain runs for which app
  type, and which graph backend a profile reads from.
- **Owns:** profile schema, command gating, app-type-to-verifier-map,
  profile-to-graph-backend mapping.
- **Status:** draft (no plan yet).
- **Currently missing:** a contract for what `graph_backend` actually answers.
  Without scoped-KG's Read API the field is a name pointing at nothing.

### 1.4 `agent-context-resolution` — the dispatch contract (NEW)

- **Role:** the connecting tissue. Defines what gets injected into a worker
  at task dispatch time and what gates a knowledge update before it
  propagates.
- **Owns:** Read API surface for scoped-KG, promotion protocol with
  code-driven thresholds and async peer review, tier-aware dispatch
  including system-prompt injection and escape-hatch routing,
  execution-telemetry schema (see §1.6).
- **Status:** not yet drafted. This note is the precursor.

---

## 1.5 Resource graduation matrix

Managed compounding is not a lessons-only mechanism. Every dot-agents
config resource type follows the same lifecycle shape: it is born,
used, signals room for improvement, and either graduates to a higher
tier/scope or retires. Today only the *birth* event is well instrumented
for most rows; *use signals* are mostly missing; *graduation* gates
exist only for hand-authored YAML proposals.

| Resource | Birth | Use signal | Improvement signal | Graduation path |
|---|---|---|---|---|
| **Lesson** | After a correction (manual `LESSON.md` write) | Re-read at session-start | Repeat mistakes despite the lesson; high citation count | Promote to a rule (proposal of `type: rule`, `action: add`) |
| **Skill** | Repetitive action observed; designed via `/skill-architect` | Skill invocation trace (see §1.6) | Post-skill actions the skill should own; redundant or stale instructions | Tier upgrade (T0→T1→T2) or split into atoms; promote to global via `da skills promote` |
| **Subagent** | Ad-hoc `Agent` invocation pattern stabilizes | Spawn outcome + merge-back quality + retry/replacement count | Follow-up work after merge-back; frequent override-the-default-prompt | Ad-hoc spawn → named role (T0/T1) → registered global subagent type (T2) → bundled into a plugin (T3). **Tier and role compose**: role names what the subagent does; tier sets autonomy at dispatch. |
| **Hook** | Repeated guard or context-injection pattern | Fire rate, block correctness, **output utility** (does the agent reference what was injected?), **token cost vs benefit**, workaround frequency | False positives; wasted-token injections the agent ignores; agents pathing around the guard | Promote a guard hook to a declarative rule; promote a context-injection hook to a skill; downgrade to advisory; retire if injection cost exceeds utility |
| **Rule** | Promoted from lesson/proposal | Read count at session-start; violation count | Rule fires but doesn't change behavior; rule contradicts another rule | Refactor, narrow scope, or retire |
| **Plugin** | Coherent set of resources distributed together (subagents + skills + hooks + rules) | **Bundle-level** invocation rate (commands run, MCP tools called, subagents spawned); component co-use frequency | Internal drift (one component evolves while others lag); interface instability; partial-use patterns (consumers using 2/5 components) | Project-local plugin → distributed plugin → standard offering, with versioning and behavior-preservation gates |

The plugin row is **bundle-shaped, not single-resource-shaped.** A
plugin's audit asks about interface stability and internal coherence;
its score weighs co-invocation patterns, not raw call counts. Treat
plugin graduation as its own §X in the synthesis spec, distinct from
single-resource graduation.

---

## 1.6 Execution telemetry — the missing data layer

The dispatch contract has three pillars (Read API, Promotion gate,
Tier-aware dispatch). The promotion gate's score axes — consumers,
recency-weighted access, contradiction history, derivation depth —
all assume **measurement we do not have**. Without per-resource
execution traces the impact score is a guess.

**Fourth pillar — execution telemetry.** Every resource use produces
a structured trace. The `iter-N.yaml` v2 schema already has the bones
of this for *iterations*; generalizing it to *per-resource-use* is the
move.

**Seeded by** the `review-decision.yaml` envelope from ADR-0002
(see [`docs/adr/0004-execution-telemetry-schema-seed.md`](../../docs/adr/0004-execution-telemetry-schema-seed.md)).
The on-disk authoring guide is
`~/.agents/skills/dot-agents/self-review/instructions/output-format.md`.
Future hook/subagent/rule traces reuse this envelope shape.

Schema sketch (per skill invocation; analogous shapes for hook fire,
subagent spawn, rule read):

```yaml
schema_version: 1
resource_type: skill | hook | subagent | rule | plugin
resource_id: <name>
invoked_at: <RFC3339>
invoked_by: <agent_role>
plan_id: <if applicable>
task_id: <if applicable>
outcome:
  declared: success | failure | partial
  agent_self_assessment: <free text>
post_invocation:
  agent_actions_after_skill_returned: [...]
  user_corrections: [...]
  retries_in_loop: <int>
improvement_signals:
  missing_in_skill: [...]   # things the agent did that the skill should own
  redundant_in_skill: [...] # parts of the skill that aren't load-bearing
  tooling_gap: { present: bool, note: <text> }
  script_gap: { present: bool, note: <text> }
  instruction_gap: { present: bool, note: <text> }
```

Three things fall out:

- The **score formula** in §3 becomes computable: recency-weighted
  access = trace rate; consumers = unique invokers; contradiction
  history = retries-in-loop pattern.
- The **L5 hard test** ("what did the system notice without a human
  asking?") becomes detectable — telemetry surfaces "this skill
  produces post-invocation corrections at rate R; propose refactor."
- The **review packet** (§3) carries real evidence: "graduate this
  lesson because it has been cited in 8 plans, has 0 contradictions,
  and reduced retry-rate in skill-X by 40%."

Trace storage decision is open (see §6).

---

## 2. The dispatch decision

When the orchestrator dispatches work, three inputs converge:

```
                       ┌─ scope chain ──────► WHO is the work for
   plan / task ────────┼─ tier (declared) ──► HOW autonomous is it
                       └─ app_type ─────────► WHAT verifier chain runs

   ──────► dispatch contract resolves ──────►

   worker prompt ◄──── tier guidelines (system-instruction injection)
   worker tools  ◄──── scope-aware skill set
   worker bounds ◄──── tier invariants (timeout, write-scope, depth)
   worker reads  ◄──── scoped-KG Read API at requested scope
   worker writes ◄──── promotion gate on every kg update
```

The dispatch contract is the function:

```
dispatch(plan, task) →
    {prompt_injection, allowed_skills, allowed_tools,
     execution_bounds, kg_read_handle, kg_write_gate}
```

Every existing piece feeds into one of these outputs. Nothing new is
spawned out of nowhere — the contract just makes the wiring explicit.

---

## 3. Data flow for knowledge updates

```
   agent or human writes a kg note
              │
              ▼
   driver event recorded (scoped-KG §2.5)
              │
              ▼
   impact score computed
       axes: downstream consumers · upstream effect · cross-scope reach ·
             provenance tier · contradiction history · derivation depth ·
             reversibility cost · author trust · recency-weighted access
              │
        ┌─────┴─────┐
        ▼           ▼
   below threshold   above threshold
        │           │
        ▼           ▼
   auto-promote   queue as proposal (existing `da review` system)
                  ┌─ review packet: update + evidence chain +
                  │  upstream/downstream neighborhoods + why-not list +
                  │  historical contradictions
                  └─ async peer review (PR-shaped):
                     reviewers per scope-standing register
                     N approvals → promote
                     veto in rollback window → revert
                     no derivations built yet → revert is cheap
```

The review packet is the artifact reviewers actually inspect. The score
just decides whether a reviewer is needed.

---

## 4. Behavior-to-command mapping

| Behavior | Command | Status |
|---|---|---|
| Build/refresh code graph | `kg build` / `kg update` / `kg code-status` | exists |
| Read code graph at scope | `kg serve` MCP tools | exists; **extend** with scope-aware variants |
| Read note layer at `(scope, topic, staleness)` | new MCP tool: `query_notes_by_scope_topic_staleness` | **new** |
| CRUD a note | `kg note add` / `update` / `view` / `search` | **new** (warm-store has notes, no full CRUD surface) |
| Compute impact score | callable function inside `kg update` post-write hook | **new** (internal) |
| View evidence chain for a note | `kg evidence <note-id>` | **new** |
| Generate review packet | `kg review-packet <note-id>` | **new** |
| Trigger promotion evaluation | `kg promote <note-id>` (manual; auto via hook) | **new** |
| Async peer review of a promotion | `da review approve` / `reject` with `type: kg-promotion` | **extend** existing proposal system |
| Validate plan tier consistency | `workflow plan validate --tier-check` | **extend** `workflow plan create/validate` |
| Inject tier guidance into worker prompt | orchestrator (`isp`, `loop-worker`) — tier from plan → system instructions | **extend** orchestrator pipeline |
| Worker escape-hatch routing | `iteration-close` skill → flag escalation to human review | **extend** existing skill |
| Upstream impact traversal | `kg impact --upstream` (or default to bidirectional) | **extend** existing command (audit already flagged this) |
| Configure thresholds and tier defaults | `.agentsrc.json` new sections: `promotion_gates`, `tier_defaults` | **extend** schema (atomic four-place update per `schema-usage.md`) |
| Auto-archive stale obs proposal when referenced plan/task closes | hook on `workflow plan archive` + `workflow advance --status completed`; new `kg observations sweep` | **new** (closes the orphan loop the audit identified) |
| Stale-proposal detection in drift surface | extend `workflow drift`: "obs older than N days with no `reviewed_at` and no matching plan/spec" | **extend** existing command |
| Cross-tool observation import | `workflow propose import --from codex` (reads `~/.codex/ambient-suggestions/<hash>/`); `--from cursor` (reads `~/.cursor/plans/`); `--from claude-memory` | **new** (single review queue, three feeds; honors §5 proposal-system reuse) |
| Lessons → rules graduation | `kg promote lesson <name>` evaluates score; auto-creates a `type: rule` proposal under `~/.agents/proposals/` for review | **new** (the first concrete instance of managed compounding) |
| Per-resource execution trace write | hook on skill/hook/subagent/rule invocation → append to per-scope trace store | **new** (the §1.6 telemetry pillar) |
| Per-resource trace query for review packets | `kg trace query --resource-type=skill --resource-id=<name>` | **new** |
| Wire self-review into iteration-close | extend `iteration-close` skill to call `self-review`, capture as `review-decision.yaml`, then `workflow checkpoint --log-to-iter <N> --role review` | **extend** existing skill (closes the dead-coded review block; see §6.5) |

---

## 5. The proposal-system reuse — load-bearing

`da review` already implements **async peer review with rollback**.
It has a YAML schema (schema_version, id, status, type, action, target,
rationale, content, created_at, created_by), a lifecycle (pending → applied
or rejected), and a routing rule between global (`~/.agents/proposals/`) and
project-local (`.agents/proposals/`).

A knowledge-promotion proposal is the same shape:

```yaml
schema_version: 1
id: promote-note-abc123-team-to-org
status: pending
type: kg-promotion
action: promote
target:
  note_id: abc123
  from_scope: team
  to_scope: org
rationale: |-
  Driver event 2026-05-03T14:22Z exceeded threshold 0.65 (consumers=18,
  cross_scope=2, contradiction_history=0, provenance_tier=verified).
content:
  evidence_chain: [...]
  upstream_neighborhood: [...]
  downstream_consumers: [...]
  why_not: [...]
created_at: "2026-05-03T14:22Z"
created_by: kg.update.hook
```

This means we do **not** build a parallel `kg review` queue. We extend the
existing review handler to recognize `type: kg-promotion`, render the review
packet on `da review show`, and apply the promotion via the existing
approval flow.

The architectural payoff: one review pipeline, one mental model, one
auditable history — for both global rule changes and knowledge crystallization.

---

## 6. Decisions still needed before the synthesis spec

These belong in `agent-context-resolution/design.md`. Listed here so the
spec author has a punch list.

1. **Impact score weights.** Proposed: `score = (downstream + upstream·W_up
   + cross_scope·W_cross) × scope_weight × (1 + contradiction_penalty)
   × (1 / provenance_tier)`. Initial weights need calibration against
   real graph data; the formula itself needs sign-off.
2. **Rollback window definition.** Time-based, read-count based, or
   "no derivations built yet"? Each has different staleness implications.
3. **Reviewer standing.** Who has authority to review at team vs org scope?
   Probably scope-keyed: an org-scope promotion needs an org-scope reviewer.
4. **Concurrent update conflicts.** When two pending updates touch the same
   fact, do we serialize per-fact, attempt automatic merge, or escalate?
5. **Tier inference fallback.** Tier is declared in plans, but plans
   sometimes get malformed. Does the validator hard-reject, soft-warn, or
   use a heuristic default with a warning?
6. **Why-not list construction.** What signals does the system use to know
   a fact *should not* be updated alongside the current update? This
   matters for evidence-chain completeness.
7. **Trace storage location.** Per-scope warm sqlite (joins cleanly with
   the scoped-KG model, queryable from review packets), append-only event
   log (cheaper writes, harder cross-referencing), or hybrid (events log
   first, periodic compaction into warm). Affects retention and privacy.
8. **Trace retention policy.** Traces include task context which may
   contain user prompts. Default retention window? Eviction trigger?
   Summarize-then-delete after N days?
9. **Privacy boundary on traces.** Some traces will include private user
   prompts; some scopes (team, org) cannot see them. Define a redaction
   pass at scope-promotion boundary or limit trace publication to scopes
   where the originating data is visible.
10. **Scheduled triage cadence.** How often does a job sweep `~/.agents/proposals/`
    and `.agents/active/fold-back/` for stale entries, and where do its
    findings emerge (drift surface? ambient suggestion? scheduled agent)?

---

## 6.5 Audit-confirmed pipeline state (2026-05-03)

A read-only audit of the existing review/self-improvement pipeline
established baseline measurements and identified concrete gaps the
synthesis spec must address. Findings:

**Conversion baseline.** Of 12 traceable auto-emissions sampled across
`~/.agents/proposals/obs-*.md` (3), `.agents/active/fold-back/*.yaml` (4),
and `~/.cursor/plans/*.md` (5 sampled), exactly **1 closed the full
chain** (`obs-1776215397478320000.md` → `agent-resource-lifecycle` plan
→ archived). Conversion rate ~1/12 ≈ 8%. The synthesis spec is
accountable to *measuring improvement against this baseline*, not just
"feels better."

**impl-results.md presence: 24/42 history dirs (57%)**, not 6/41 as
prior surface-mining suggested. The 18 dirs without it are the *newer*
plans that ran through the `workflow plan archive` + delegation pipeline
(`agent-resource-lifecycle`, `loop-runtime-refactor`, `plan-archive-command`,
`workflow-parallel-orchestration`, etc.) where `merge-back.md` already
captures per-task narrative. The rule was authored for the pre-delegation
era; CLAUDE.md was rewritten 2026-05-03 to reflect the current toolchain.

**Self-review skill is functionally orphaned.** It runs in chat, returns
a summary, persists nothing. `iteration-close` never invokes it. Wiring
self-review → `review-decision.yaml` → iter-log review block closes
two gaps with one change (see §4 mapping addition).

**The v2 review block is dead-coded.** Schema requires it
(`schemas/workflow-iter-log.schema.json` lines 8-17). Writer exists
(`commands/workflow/iter_log.go:397-426 mergeReviewIterLog`). But no
skill or CLI path ever calls `workflow checkpoint --log-to-iter --role
review`, and no path writes the `review-decision.yaml` input the writer
reads from. All 22 v2 logs have `overall_decision: ""`. v1 logs (39 of
them) lack the field entirely; partial backfill is possible but
fabricates an explicit-accept gate that never ran — recommend leaving
v1 out of scope.

**Skill rework lineage: 5/5 spec-driven, 0/5 observation-driven** in
the sample (orchestrator-session-start, iteration-close, loop-worker,
agent-start). All reworks landed inside scoped plan tasks. The
observation→plan compounding chain exists structurally but is rarely
the trigger today.

**Lessons → rules has zero plumbing.** 15 lessons exist; `grep "lesson"
~/.agents/rules/**` finds zero rules that cite a specific lesson. No
skill loads a lesson via instructions/. No CLI surface promotes a
lesson into a rule. This is the cleanest first concrete instance for
managed compounding (see §1.5 row 1, and §C.10/C.11 of the research
evaluation index).

**Implication for the synthesis spec.** The architecture must specify:

- A mechanical resolver that auto-archives `obs-*.md` when the
  referenced plan/task closes (gap: orphan accumulation).
- Self-review wiring into iteration-close as a near-term plan
  (closes the dead-coded review block in one move).
- A lessons-to-rules graduation surface with code-driven thresholds
  (the missing managed-compounding instance).
- Cross-tool observation importers (Codex ambient-suggestions and
  Cursor plans are siloed today; same shape, never crossed).
- A scheduled triage cadence over proposals + fold-backs so stale
  signals don't accumulate.

---

## 7. What this note unblocks

- **`scoped-knowledge-graphs`** can add §6 "Read API" with concrete query
  signatures. The Read API is what backs every other spec's
  `graph_backend` field.
- **`skill-tiering-contract`** can resolve D2 (declaration site), D3
  (validation point), D4 (enforcement runtime), D5 (escape-hatch) by
  pointing at the dispatch contract as the consumer.
- **`app-type-profiles`** can define `graph_backend` concretely as "a
  named handle to a scoped-KG Read API instance" rather than a free-form
  string.
- **`agent-context-resolution`** itself becomes drafteable. The four open
  decisions above are the first sections.

---

## 8. What this note explicitly does not do

- It does not specify the score formula's final weights — that needs data.
- It does not redesign the existing proposal system — only extends its type
  vocabulary.
- It does not commit to a specific MCP tool naming scheme — that lives in
  the synthesis spec.
- It does not address cross-org KG federation (the multi-agent-memory-dkg
  evaluation flagged that as `[GAP-ADOPT — conceptual only]` for now).
- It does not address skill-graph dispatch beyond a single hop — the
  current research evidence (`shivsakhuja`, `the_smart_ape`) supports
  1–2 hops reliably, and that is the bound the dispatch contract enforces.

---

## 9. Suggested next moves

1. **Land this note** as the orientation reference. Link from each of the
   four anchor specs.
2. **Wire self-review into iteration-close as a near-term plan** (audit
   §6.5 identified this as the single highest-leverage fix; closes the
   dead-coded review block AND gives self-review an output surface).
   Scope: 6 tasks. Verifiable end-to-end against a fresh iter-log. Plan
   landed 2026-05-03 at
   [`.agents/workflow/plans/self-review-iteration-close-wiring/`](../workflow/plans/self-review-iteration-close-wiring/).
   Produces ADR-0002 through ADR-0005, seeds the §1.6 execution-telemetry
   schema via the chosen `review-decision.yaml` envelope, and is the
   first plan to use the new ADR convention adopted in
   [`docs/adr/0001-adopt-architecture-decision-records.md`](../../docs/adr/0001-adopt-architecture-decision-records.md).
3. **Draft `agent-context-resolution/design.md`** — start with §1 Read API
   (smallest blast radius, most concrete). Circle back to promotion gate,
   tier dispatch, and execution telemetry (§1.6).
4. **Sharpen the existing drafts** in parallel: scoped-KG §6, skill-tiering
   D2–D5, app-type-profiles `graph_backend` definition. These are
   mechanical edits once the synthesis spec exists.
5. **Lessons → rules graduation as the first concrete demo** of managed
   compounding (§1.5 row 1). 15 inputs, 0 outputs today; closed loop we
   control; reuses the existing proposal/review infrastructure. This is
   the wedge for everything else.
6. **Canonical plans for the synthesis spec come last.** No canonical
   plan until the synthesis spec and one of the existing drafts are
   stable enough that implementation has a contract to be accountable to.
