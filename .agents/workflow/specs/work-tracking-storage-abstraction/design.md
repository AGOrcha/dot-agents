# Spec: work-tracking storage abstraction — decouple coordination state from the git working tree

**Status:** draft (for review). Motivated by the first ultracode wave-engine run (2026-06-06), which
exposed a structural flaw, not just a bug.
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
- **`cloudflare-do`** — our managed backend (Durable Object + D1/storage), abstracted so other
  storage backends slot in; the path for teams *without* a PM tool.
- **`jira` / `linear`** — map canonical entities onto their issues/epics/workflow states; for teams
  that already have one.

## 3. Decisions (review these)

- **D1 — Two planes, cleanly separated.** *Artifact content* (code, docs, designs) stays in git.
  *Coordination state* (status, assignment, in-flight, PR/branch linkage, dep readiness, leases) lives
  in the backend. The `.agents/workflow/**` YAML remains the agent-facing **projection/working copy**
  of coordination state + the editable home of design content; the daemon reconciles it with the
  backend. (Design *content* — spec/plan markdown — stays git-canonical; only the *state fields* are
  backend-canonical.)
- **D2 — Backend is the source of truth for STATUS; local is the working copy.** Agents never block on
  the backend (offline-tolerant). The daemon keeps a fresh local cache. **Status reads by the
  orchestrator/scout resolve against the backend (or the daemon's backend-synced view), not the
  per-worktree YAML** — this is what kills re-dispatch. Conflict rule: backend wins for *status*
  transitions; local wins for *content* edits.
- **D3 — A `WorkStore` interface.** A Go interface over the canonical entities (`Plan`, `Task`,
  `Spec`, `WorkItem`) with read / list-eligible / **transition** / **claim(lease)** / watch ops.
  Implementations: `localfs` (behaviour-preserving wrapper over today's files — the migration bridge),
  `cloudflare-do`, `jira`, `linear`. Selected per scope via config-v2.
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
  `da config … work_tracking.backend=<local|cloudflare-do|jira|linear>` selects it per scope (a new
  config-v2 layer, org→team→project overridable); `da service run [-d]` runs the daemon. Backend
  endpoint + auth reuse the **external-agent-sources** credential model (credential-ref by id; secrets
  in `~/.config/da/credentials.json` 0600) — no parallel auth path. This fills the gap the obs proposal
  named ("focuses Cloudflare options but doesn't specify CLI interaction").

## 4. Requirements (behavioral)

1. The orchestrator/scout's eligibility reflects **authoritative** status — an in-flight /
   awaiting-review / done task is never re-dispatched — independent of git worktree isolation.
2. Agents read/edit the **same** local YAML/markdown interface as today (no agent-facing change).
3. The daemon syncs local ↔ backend bidirectionally; offline edits reconcile on reconnect.
4. Backend is pluggable and **scope-configured** (`local` / `cloudflare-do` / `jira` / `linear`).
5. A status transition (esp. *claimed* / *awaiting-review*) by any worker is visible to all others
   atomically (claim) or within the sync interval (lesser transitions).
6. `local` backend is behaviour-identical to today (the no-regression migration bridge).

## 5. Done criteria

1. `WorkStore` interface + `localfs` implementation (wraps current files; existing `da workflow`
   behaviour unchanged) + one remote implementation (`cloudflare-do`) selectable by scope config.
2. The wave engine resolves eligibility + claims through `WorkStore`; **demonstrated: no re-dispatch
   under worktree isolation** (the failure that motivated this spec).
3. The daemon syncs local ↔ backend; a status transition made in one worktree is visible to the scout
   without a main-repo file edit.
4. `da config work_tracking.backend` switches backends; `da service run -d` runs the sync daemon;
   auth via the external-agent-sources credential store.

## 6. Open questions

- **Atomic claim/lease semantics** — how the scout leases a task so concurrent waves don't double-pick
  before the transition lands (TTL lease? compare-and-set in the backend?).
- **Conflict resolution per field** — status (backend-wins) vs content (local-wins) is the starting
  rule; needs precise field ownership + a merge story for concurrent edits.
- **Mapping fidelity** — canonical `Task` ↔ Jira/Linear issue: write_scope, verifier_sequence,
  cross-plan `depends_on` as custom fields vs links; round-trip without loss.
- **Multi-upstream** — sync to >1 backend (e.g. Jira for humans + CF DO for the engine)? Or one
  primary + read-only mirrors?
- **Git vs backend double-tracking** — do the YAML files stay git-committed (history/audit) *and*
  backend-synced (live coordination)? Likely: content git-canonical, state backend-canonical, with the
  committed YAML a periodic snapshot — needs a clean, non-confusing rule.
- **Daemon-down / CI fallback** — mirror the obs proposal's bootstrap fallback (call sites read the
  backend/credentials directly when the daemon isn't running).
- **Migration path** — `localfs` backend wraps today's files so nothing breaks day one; remote
  backends opt-in per scope. Sequencing of the cutover.

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
- **Motivating failure** — the wave-engine re-dispatch storm (5×p1c) is the canonical regression test
  for done-criterion #2.
