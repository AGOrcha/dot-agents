# Spec: kg-ideate skill — KG-grounded front-end to the artifact pipeline

**Spec ID:** kg-ideate-skill
**Status:** draft (for review)
**Promoted from:** `.agents/proposals/kg-ideate-skill.yaml` (re-scoped 2026-05-29; see that
proposal for the full SKILL.md draft and per-molecule instruction/template bodies).

**Single-source boundary (read this first).** This spec owns the **skill implementation**
(the four-phase compound skill + its molecule/atom decomposition). It does **not** redefine
the ideation *config profile* (verifiers / reviewers / relevance routing) — that is owned by
`.agents/workflow/specs/ideation-execution-profile/design.md`, which forward-references this
skill in `orchestrate.core`. The two specs are complementary, not duplicative:

| Concern | Owning spec |
|---------|-------------|
| The `kg-ideate` skill itself (phases, molecules, graceful degradation) | **this spec** |
| `ideation` execution_profile: verifiers, reviewers/lenses, relevance routing | `ideation-execution-profile` |
| `contradicting_claims` named query + citation adapter | `graph-backend-adapter-contract` §13.4 |

Do not re-author the ideation pipeline anywhere else. The interactive planning-agent idea
(`.agents/workflow/specs/planning-agent-pipeline-and-interactive/idea.md`) is a *future*
ISP-side interactive forked-session idea, distinct from this batch authoring front-end; it is
not folded here.

---

## 1. Problem statement

The workflow has a cold-start problem across the WHOLE artifact pipeline, not just spec
authoring: ideas become specs, plans, and staged-execution handoffs from scratch without
querying accumulated KG knowledge (prior decisions, research findings, contradictions,
lessons). This produces repeated rediscovery of the same design space, inconsistent
terminology across specs/plans, decisions that contradict prior work, and fanout bundles
whose write-scopes were guessed rather than grounded.

`kg-ideate` is the KG-grounded front-end to the entire artifact pipeline
(idea/proposal → spec → plan → concurrent staged execution), for work planned solo by the
orchestrator OR delegated to subagents.

## 2. Goals

- A KG/research/lessons **briefing** is produced before any spec/plan is written, and that
  briefing is the single shared artifact consumed by all downstream phases and by a spawned
  planner subagent.
- Spec and plan scaffolding are **guided by the briefing**, not generic templates.
- The pipeline **hands off cleanly** to the concurrent staged-execution flow (fanout / ISP)
  without itself implementing.
- The skill is **useful today** in degraded mode and degrades gracefully where one step is
  adapter-gated.

## 3. Decisions

- **D1 — Tiered composition (per `skill-tiering-contract`).** `kg-ideate` is a **T2 compound
  orchestrator** that dispatches in declared order to four **T1 molecule** skills, each
  separately authored and independently invocable. Dispatch depth compound → molecule → atom
  = 2 hops (within the reliable bound). The orchestrator owns only: brief → (spec ⇄ plan, per
  the Phase 3 concurrency fork) → handoff.

  | Phase | Molecule | Owns |
  |-------|----------|------|
  | 1 | `kg-brief` | KG/research/lessons traversal → shared briefing block |
  | 2 | `spec-scaffold` | briefing → decisions/open-questions → `design.md` |
  | 3 | `plan-scaffold` | spec → tasks/write-scopes/dep-order → PLAN/TASKS |
  | 4 | `staged-execution-handoff` | spec+plan → fanout/ISP staged execution |

- **D2 — Phase 1 is CLI-first, MCP fallback.** Prefer `da kg query "<topic>"`; fall back to
  `semantic_search_nodes_tool` / `query_graph_tool` only when the CLI is unavailable or
  empty. `get_impact_radius_tool` runs only when the topic names code, and its output seeds
  Phase 3 write-scopes.

- **D3 — Graceful-degradation contract (the only gated step).** Phase 2 step 7 (structural
  contradiction traversal) is the **only** adapter-gated step. It executes structurally only
  when the active profile's adapter ships a `contradicting_claims` named query (preflight via
  `da graph query --list-queries`). When absent, it **degrades to the competing-decisions
  fallback** (two decision nodes with conflicting rationale presented in step 5). No other
  phase depends on it; shipping early does not reintroduce a silent-no-op trust problem
  because the fallback is documented and explicit.

- **D4 — Briefing is self-contained cold-start context.** The Phase 1 briefing block is the
  full context a spawned planner subagent needs to run Phase 2/3 from briefing + topic alone
  (solo and delegated paths share one artifact shape).

- **D5 — Spec↔plan concurrency is a deliberate fork (Phase 3 step 10).** Sequential is the
  default; concurrent/folded is chosen only when there are no open questions whose answers
  change task ordering or write-scopes. The chosen mode + rationale are recorded in plan
  notes. Direct application of the `workflow-artifact-model` rule "spec before plan… unless
  the work is purely mechanical."

- **D6 — Tier boundaries are respected.** Phase 2 writes the spec tier only (no file paths /
  function names / task lists). Phase 3 owns the plan tier and authors via
  `da workflow plan create <id>`; never hand-edits task status.

## 4. Requirements (behavioral)

1. Running `kg-ideate <topic>` with no prior KG nodes produces a briefing that states "KG has
   no prior decisions on this topic" rather than fabricating.
2. With an adapter that lacks `contradicting_claims`, the skill completes all four phases and
   the briefing's Contradictions section shows `[adapter-absent]`; the spec still resolves
   competing decisions explicitly.
3. With `dotagents-builtin:graph/citation@^1.0` active, Phase 2 step 7 runs
   `da graph query contradicting_claims` and emits each contradiction as an explicit spec
   decision point.
4. Phase 3 derives each task's `write_scope` from the Phase 1 impact radius when the topic
   named code; a task with an unknowable write-scope is flagged as a residual spec open
   question.
5. Phase 4 produces no code; it emits the direct-vs-fanout decision and hands briefing + spec
   + plan into `[[orchestrator-session-start]]` / `[[isp]]`.

## 5. Done criteria

1. Four molecule `SKILL.md` files authored (`kg-brief`, `spec-scaffold`, `plan-scaffold`,
   `staged-execution-handoff`) plus the `kg-ideate` compound orchestrator, each self-declaring
   `tier` + `calls:` + `verifier: batch`.
2. Instruction files (`kg-queries`, `corpus-scan`, `context-scan`, `decision-review`,
   `gap-conversion`, `contradiction-framing`, `done-criteria`, `plan-scaffolding`,
   `execution-handoff`) and templates (`briefing-output`, `spec-output`, `plan-output`)
   authored per the proposal bodies.
3. Degraded-mode run (no citation adapter) completes end-to-end and the contradiction step
   no-ops cleanly into the competing-decisions fallback (Requirement 2 demonstrated).
4. `ideation-execution-profile` relevance routing resolves `kg-ideate` as `orchestrate.core`
   (no change needed here — that spec already forward-references it).
5. Structural-contradiction traversal (Requirement 3) demonstrated once §13.4's
   `contradicting_claims` ships end-to-end — gated, scheduled separately (see plan).

## 6. Deferred / gated

- **Phase 2 step 7 structural traversal** — gated on `dotagents-builtin:graph/citation@^1.0`
  shipping `contradicting_claims` end-to-end (build → query → MCP) per
  `graph-backend-adapter-contract` §13.4. Until then the competing-decisions fallback stands.
- **`da kg query` CLI shape normativity** — not yet normative in the adapter contract; if this
  skill drives strong opinions about that surface, route them as input to the contract work.
- **Starter promotion** — destined for
  `internal/scaffold/home/starter/skills/global/kg-ideate/` (ships via `da init`); repo-local
  for now, pending proposal-routing v2. Promotion of the non-citation phases can happen on its
  own schedule ahead of the adapter-gated step.

## 7. Related

- `.agents/workflow/specs/ideation-execution-profile/design.md` — the ideation **profile**
  (verifiers/reviewers/relevance) that routes to this skill. Single-source counterpart.
- `.agents/workflow/specs/graph-backend-adapter-contract/design.md` §13.4 — citation adapter +
  `contradicting_claims`; source of the one gate.
- `.agents/workflow/specs/layered-pr-fanout/design.md` — the staged-PR target Phase 4 feeds.
- `[[orchestrator-session-start]]`, `[[isp]]`, `[[delegation-lifecycle]]` — the staged runtime.
- `.agents/proposals/kg-crg-aware-bundle-authoring.md` — sibling (delegation-bundle side of the
  same capability bet).
- `.agents/proposals/kg-ideate-skill.yaml` — promoted source proposal (full bodies).
