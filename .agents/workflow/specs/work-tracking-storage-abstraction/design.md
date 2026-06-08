# Spec: work-tracking storage abstraction — decouple coordination state from the git working tree

**Status:** draft (for review). Motivated by the first ultracode wave-engine run (2026-06-06), which
exposed a structural flaw, not just a bug. **Rev 2 (2026-06-07):** incorporates the KG-as-SOT
direction — the `kg` graph store is the source of truth for the SDD artifacts (not just their status),
files become projections, and work results correlate to the artifacts/primitives that produced them
(D1′, D8, §3A); converges this spec with `knowledge-architecture-graph-views`.
**Relationship:** the coordination-plane sibling of
[`agorcha-public-vs-internal-and-obs-deploy.md`](../../proposals/agorcha-public-vs-internal-and-obs-deploy.md)
(that proposal owns the *telemetry/observability* plane; this spec owns the *coordination/work-tracking*
plane — same Cloudflare substrate + daemon + auth model, different data).

---

## 1. Problem

The loop-orchestration + layered-PR model tracks **coordination state** — plan/task/spec status,
in-flight/awaiting-review, dependency readiness, PR/branch linkage — as YAML under
`.agents/workflow/**`, versioned in git, with work executed in isolated **git worktrees** + feature
branches/PRs. **Git's working tree is the wrong substrate for coordination state**, and the first
wave-engine run proved it concretely:

- The engine fanned out loop-workers in isolated worktrees. Each worker, on opening its PR, advanced
  its task status (`merge-back`) — but **inside its own worktree's copy of TASKS.yaml**, which never
  propagates to the main repo.
- The scout reads the **main repo's** TASKS.yaml each wave. It kept seeing the same tasks as
  `pending`, so it **re-dispatched them every wave** → **5 duplicate PRs for `p1c`, 2 for `cj`, and a
  duplicate of an already-merged `cg`**. The engine churned ~5 config-v2 tasks across 6 waves instead
  of progressing.

The duplicate-PR storm is the symptom. The disease: **there is no single shared source of truth for
coordination state that every agent and the orchestrator read atomically.** Status is fragmented
across (a) per-worktree YAML, (b) the lagging canonical TASKS.yaml, (c) GitHub PR state, (d) the
scout's already-stale snapshot. Worktree isolation — which we *want* for parallel code safety —
guarantees the status plane diverges from the content plane.

Secondary gap: **no upstream integration.** Teams on Jira/Linear can't drive the loop from their PM
tool; teams without one have no managed option. The config-v2 scope model already lets org/team/project
choose resources — coordination backend should be one of them.

## 2. Goal

A **storage-agnostic backend for the SDD artifacts and their coordination state**. Agents keep reading
and editing the **same local YAML/markdown** (the working interface is unchanged). A **daemon
subservice watches local changes and syncs them to a configured upstream backend** (and pulls upstream
changes down). The backend is the **shared source of truth** for coordination state, so the
orchestrator/scout sees authoritative in-flight/awaiting-review/done status atomically — regardless of
worktree isolation — and **never re-dispatches in-flight work**.

Pluggable backends, **scope-configured** (org/team/project via config-v2):
- **`local`** — today's git-tracked files; single host; the offline / no-upstream degenerate case.
- **`kg`** — the **knowledge-graph store** is the primary managed backend and the **source of truth
  for the SDD artifacts themselves** (tasks, plans, specs, proposals), not merely their status. The
  `.agents/workflow/**` files become **projections/references** into the graph (see D1′, §3A). This is
  the convergence point of this spec with the `knowledge-architecture-graph-views` end-state (one
  graph store + typed views) and rides the `graph-backend-adapter-contract` swappable-backend seam.
- **`cloudflare-do`** — a managed **deployment** of the `kg` backend (Durable Object + D1/storage),
  not a separate model; the hosted path for teams *without* a PM tool.
- **`jira` / `linear`** — map canonical entities onto their issues/epics/workflow states; for teams
  that already have one.

## 3. Decisions (review these)

- **D1 — Two planes, cleanly separated.** *Artifact content* (code, docs, designs) stays in git.
  *Coordination state* (status, assignment, in-flight, PR/branch linkage, dep readiness, leases) lives
  in the backend. The `.agents/workflow/**` YAML remains the agent-facing **projection/working copy**
  of coordination state + the editable home of design content; the daemon reconciles it with the
  backend.
- **D1′ — Three canonical tiers, not two (the KG-as-SOT reconciliation).** D1's "content git-canonical,
  state backend-canonical" split is too coarse once the backend is the `kg`. Resolve it by tier
  (consistent with `knowledge-architecture-graph-views`):
  1. **Code** — always **git-canonical**. The graph only *indexes* it (impact/affected-flows); it is
     never authored in the KG.
  2. **SDD artifact structure + state** — **KG-canonical**: the plan/task/spec/proposal *entities*,
     their relationships (depends_on, write_scope, verifier_sequence, spec↔plan↔task trace), and all
     coordination state (status, leases, PR linkage). This is the *graph*, and it is the SOT.
  3. **SDD artifact prose body** — the narrative markdown of a spec/plan/lesson is **authored in files
     (git-canonical as text)** and **indexed into the KG semantic view**; the KG node references the
     file as its text source. The structured `.agents/workflow/**` YAML (PLAN/TASKS) is a **generated
     projection** of tier 2 + an offline-editable working copy the daemon reconciles back.
  Net rule: *prose stays a file; structure and state become the graph; the committed YAML is a periodic
  snapshot for git history/audit, never the authority.* This supersedes the §6 "git vs backend
  double-tracking" open question.
- **D2 — Backend is the source of truth for STATUS; local is the working copy.** Agents never block on
  the backend (offline-tolerant). The daemon keeps a fresh local cache. **Status reads by the
  orchestrator/scout resolve against the backend (or the daemon's backend-synced view), not the
  per-worktree YAML** — this is what kills re-dispatch. Conflict rule: backend wins for *status*
  transitions; local wins for *content* edits.
- **D3 — A `WorkStore` interface.** A Go interface over the canonical entities (`Plan`, `Task`,
  `Spec`, `Proposal`, `WorkItem`) with read / list-eligible / **transition** / **claim(lease)** /
  watch ops. Implementations: `localfs` (behaviour-preserving wrapper over today's files — the
  migration bridge), `kg` (the graph store; the structural target, sharing the
  `graph-backend-adapter-contract` seam so a `cloudflare-do` deployment is one backend behind it),
  `jira`, `linear`. Selected per scope via config-v2. `WorkStore` is the **working-view** facade over
  the same graph the typed views (§3A) read.
- **D4 — One daemon owns the sync loop.** The `da service` / workflow-orchestrator-daemon (the same
  long-lived host process the obs proposal §5.5 and r3-background-worker-service already define) watches
  local artifacts, pushes changes to the backend, pulls backend changes down, and serves the
  orchestrator's status/eligibility queries from a backend-fresh view. It already owns iter-log
  ingestion, the rescore loop, the worker event stream, and the credential proxy — coordination sync
  consolidates here, not a new process.
- **D5 — Status transitions are atomic through the backend (the structural re-dispatch fix).** A
  worker opening a PR records `pending → awaiting_review` in the **backend** (via the daemon or a CLI
  call that hits `WorkStore`), making it immediately visible to the scout — independent of which
  worktree the worker ran in. The scout's eligibility is a backend query, and admitting a task takes a
  **lease/claim** so two waves can't both grab it before the first transition lands.
- **D6 — Two integration layers (the two ideas).** (a) A **generic PM-upstream adapter** (Jira/Linear)
  mapping `Plan→epic`, `Task→issue`, status→workflow-state, with custom fields for write_scope /
  verifier_sequence / depends_on. (b) The **native CF DO backend** for teams without a PM tool. Both
  behind the same `WorkStore`. Multi-upstream fan-out (Jira *and* CF DO) is deferred (§6).
- **D7 — CLI surface.** `da workflow …` reads/writes through `WorkStore` instead of raw YAML;
  `da config … work_tracking.backend=<local|kg|cloudflare-do|jira|linear>` selects it per scope (a new
  config-v2 layer, org→team→project overridable); `da service run [-d]` runs the daemon. Backend
  endpoint + auth reuse the **external-agent-sources** credential model (credential-ref by id; secrets
  in `~/.config/da/credentials.json` 0600) — no parallel auth path. This fills the gap the obs proposal
  named ("focuses Cloudflare options but doesn't specify CLI interaction").
- **D8 — Scope must declare the SOT (no implicit default beyond `local`).** A project's stable source
  of truth is the `(work_tracking.backend, kg store id)` pair resolved at its scope; it must be
  **explicitly specified** at org/team/project (config-distribution-model §15 scope ladder, same as
  `execution_profile`). Absent any declaration the backend is `local` (today's behaviour); any *shared*
  SOT — the thing that makes cross-worktree status atomic and cross-artifact correlation possible — is
  opt-in by scope, so two projects never silently share or split a graph.

## 3A. Knowledge-graph SOT: typed views + cross-artifact correlation

The `kg` backend is not just a status store; it is the single graph the
`knowledge-architecture-graph-views` end-state describes, with the SDD tiers mapping onto the four
human-memory views. `WorkStore` (D3) is the read/write facade for the **working** view; the others are
read-oriented services over the same store.

| View | Holds (node types) | Today's home |
|---|---|---|
| **working** | tasks, plans, loop/wave state, leases, in-flight/awaiting-review | TASKS.yaml / PLAN.yaml / orchestrator state |
| **semantic** | specs, proposals, contracts, invariants, **`stage_profiles`** | `workflow/specs/**`, `proposals/**`, `*_CONTRACT`/`*_SPEC`, `.agentsrc.json` |
| **operational** | skills, rules, agents, hooks, workflows, execution_profile routing | `.agents/skills/**`, `~/.agents/rules/**`, agent/hook configs |
| **episodic** | history, fold-backs, iteration logs, **work results**, git events | `history/**`, `active/fold-back/**`, `iteration-log/**` |

**Correlation = edges, and that is the feedback loop.** A wave/iteration result is an **episodic node**
with edges to the **operational + semantic** nodes that produced it: the `stage_profiles` (executor/
verifier/reviewer/orchestrator) it ran under, the skills/rules/agents/hooks in its working set, and the
spec/plan/task it implemented. Those edges make the self-improvement loop *queryable* rather than
anecdotal:
- "Which lesson/rule/profile drove this outcome?" (result → operational)
- "Which specs' tasks regress most, under which verifier sequence?" (result → semantic + operational)
- "Did adopting rule X / a new `stage_profiles.verifier` slug change downstream result quality?"

This is what closes `CLAUDE.md`'s self-improvement loop on data instead of memory: lessons, rules,
skills, and `stage_profiles` stop being write-only and become nodes results are scored against.

## 4. Requirements (behavioral)

1. The orchestrator/scout's eligibility reflects **authoritative** status — an in-flight /
   awaiting-review / done task is never re-dispatched — independent of git worktree isolation.
2. Agents read/edit the **same** local YAML/markdown interface as today (no agent-facing change).
3. The daemon syncs local ↔ backend bidirectionally; offline edits reconcile on reconnect.
4. Backend is pluggable and **scope-configured** (`local` / `kg` / `cloudflare-do` / `jira` /
   `linear`); the active SOT is the scope-resolved `(backend, kg store id)` pair (D8).
5. A status transition (esp. *claimed* / *awaiting-review*) by any worker is visible to all others
   atomically (claim) or within the sync interval (lesser transitions).
6. `local` backend is behaviour-identical to today (the no-regression migration bridge).
7. Under the `kg` backend, a completed work result is queryable with edges to the `stage_profiles`,
   skills/rules/agents/hooks, and spec/plan/task that produced it (§3A correlation).

## 5. Done criteria

1. `WorkStore` interface + `localfs` implementation (wraps current files; existing `da workflow`
   behaviour unchanged) + one remote implementation (`cloudflare-do`) selectable by scope config.
2. The wave engine resolves eligibility + claims through `WorkStore`; **demonstrated: no re-dispatch
   under worktree isolation** (the failure that motivated this spec).
3. The daemon syncs local ↔ backend; a status transition made in one worktree is visible to the scout
   without a main-repo file edit.
4. `da config work_tracking.backend` switches backends; `da service run -d` runs the sync daemon;
   auth via the external-agent-sources credential store.
5. With the `kg` backend, the SDD artifacts are graph-canonical and the `.agents/workflow/**` files are
   regenerable projections (D1′); a result node carries the correlation edges of requirement #7 and at
   least one feedback-loop query (result → operational/semantic) returns them.

## 6. Open questions

- **Atomic claim/lease semantics** — how the scout leases a task so concurrent waves don't double-pick
  before the transition lands (TTL lease? compare-and-set in the backend?).
- **Conflict resolution per field** — status (backend-wins) vs content (local-wins) is the starting
  rule; needs precise field ownership + a merge story for concurrent edits.
- **Mapping fidelity** — canonical `Task` ↔ Jira/Linear issue: write_scope, verifier_sequence,
  cross-plan `depends_on` as custom fields vs links; round-trip without loss.
- **Multi-upstream** — sync to >1 backend (e.g. Jira for humans + CF DO for the engine)? Or one
  primary + read-only mirrors?
- **Git vs backend double-tracking** — *resolved by D1′*: prose git-canonical, structure+state
  KG-canonical, committed YAML a periodic snapshot. Remaining detail: snapshot cadence + what (if
  anything) of the graph is committed for offline/audit replay.
- **Projection fidelity & regeneration** — the `.agents/workflow/**` YAML must regenerate losslessly
  from the graph (and re-ingest a hand-edited file) without churn or ordering noise in git diffs.
- **KG schema for SDD entities + correlation edges** — node/edge types for Plan/Task/Spec/Proposal and
  the result→`stage_profiles`/skill/rule/agent/hook edges; how much reuses the existing `KGNote`
  shape vs new typed nodes (see schema-usage `KGNote` field caveats).
- **Daemon-down / CI fallback** — mirror the obs proposal's bootstrap fallback (call sites read the
  backend/credentials directly when the daemon isn't running).
- **Migration path** — `localfs` backend wraps today's files so nothing breaks day one; the `kg`
  backend opts in per scope and ingests existing artifacts on first sync. Sequencing of the cutover,
  gated on `graph-backend-adapter-contract`.

## 7. Out of scope / deferred

- The telemetry/observability plane (iter-logs, scores, dashboard) — owned by the agorcha obs proposal;
  this spec is coordination state only (shared substrate, different data).
- Jira/Linear adapters land *after* the `WorkStore` interface + CF DO backend prove out.
- The Cloudflare deploy topology, CF Access, public/internal split — owned by the obs proposal.

## 8. Relationships

- **agorcha-public-vs-internal-and-obs-deploy** — shares the CF DO/D1 substrate, the `da service`
  daemon, and the external-agent-sources auth model. That = telemetry; this = coordination.
- **config-distribution-model §15 (scope ladder)** — `work_tracking.backend` is a scope-mergeable
  layer (org→team→project), exactly like `execution_profile`.
- **external-agent-sources** — credential model + the credential-proxy daemon mode the backend auth
  reuses.
- **r3-background-worker-service / workflow-orchestrator-daemon** — the daemon that runs the sync loop.
- **layered-pr-fanout** — its `awaiting_review` status only actually gates the scout once a shared
  backend exists; today worktree isolation defeats it (this spec is the missing substrate).
- **knowledge-architecture-graph-views** — the end-state this spec's `kg` backend converges on: one
  graph store + typed working/operational/semantic/episodic views (§3A). This spec is the
  coordination/working-view slice of that vision made concrete.
- **graph-backend-adapter-contract** — the swappable-backend seam (one store, pluggable adapters) the
  `kg` `WorkStore` rides; the `kg`/`cloudflare-do`/`localfs` split is its adapter set. The KG-as-SOT
  cutover is gated on this contract landing.
- **stage-profile-and-routing-consolidation** — produces the `stage_profiles` operational/semantic
  node type that result nodes correlate against (§3A); cleaner primitives ⇒ a cleaner feedback graph.
- **Motivating failure** — the wave-engine re-dispatch storm (5×p1c) is the canonical regression test
  for done-criterion #2.
