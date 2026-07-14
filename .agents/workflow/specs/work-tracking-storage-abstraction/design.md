# Spec: work-tracking storage abstraction — decouple coordination state from the git working tree

**Status:** draft (for review). Motivated by the first ultracode wave-engine run (2026-06-06), which
exposed a structural flaw, not just a bug. **Rev 2 (2026-06-07):** incorporates the KG-as-SOT
direction — the `kg` graph store is the source of truth for the SDD artifacts (not just their status),
files become projections, and work results correlate to the artifacts/primitives that produced them
(D1′, D8, §3A); converges this spec with `knowledge-architecture-graph-views`. **Rev 3 (2026-06-07):**
adds D9 — Jira/Linear upstream tools map at *milestone* grain (our plan/task/stage decomposition stays
KG-canonical; upstream advances on PR-merge, via subtasks or attribution notes), not per internal step.
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
  behind the same `WorkStore`. Multi-upstream fan-out (Jira *and* CF DO) is deferred (§6). The
  `Plan→epic` / `Task→issue` 1:1 mapping here is the *fine-grained* option; **D9 makes milestone-grain
  the default** (our task/stage churn stays in the KG; upstream advances on PR-merge, not per step).
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
- **D9 — Upstream PM tools (Jira/Linear) map at MILESTONE grain, not internal-task grain.** A
  company's ticket is a **coarse** unit of work — a whole story/feature — and teams want "the ticket's
  work done" as one deliverable, not their board churned by our internal loop. So the `Plan→epic` /
  `Task→issue` 1:1 mapping in D6 is only the *fine-grained* option; the **default is coarser** and
  scope-selectable:
  - **Our decomposition lives in the KG.** The plan + tasks (how *we* break the ticket's work down) and
    all per-stage / in-flight / awaiting-review status are KG-canonical (D1′). They are not pushed as
    upstream workflow-state transitions.
  - **Upstream representation is scope-configurable** to the team's process: either map our tasks → the
    ticket's native **subtasks** (when the team uses them), or keep tasks KG-only and attach a
    **note / attribution link** on the ticket pointing back at the KG plan — no upstream state mutation
    per internal step.
  - **Upstream state is debounced to coarse milestones.** The upstream ticket advances (e.g.
    `In Progress → In Review / Done`) only on a **milestone event — primarily the PR merge** — never on
    every internal in-flight / awaiting-review / per-stage transition. Internal churn stays in the KG;
    only milestone events propagate upstream, keeping the company's board stable while the KG drives the
    loop. (Inbound: an upstream ticket close/reopen is a milestone the daemon reconciles into KG state.)

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

## 3B. Agent interface model — the file-system projection IS the agent's interface (zero new semantics)

**This is the foundational principle of the whole storage model, stated once here as the
single source. Every tier (§3 D1/D1′), the daemon (D4), the `WorkStore` facade (D3), and
the R-series service surfaces are subordinate to it.**

> **The file-system projection is the agent's primary interface — zero new semantics.**

Agents live on the server and have direct file-system access. They operate by **reading and
writing the projected files directly** — the `.agents/workflow/**` YAML/markdown, specs,
plans, tasks, lessons, results. Those files are *just there and available*. An agent does
**not** need to learn any `da` command, RPC, HTTP/UDS call, or event-bus API to do its work:
its default and only required path is to read and edit the files. Editing a file is the act;
nothing new must be invoked.

**The system reconciles file changes after the fact.** When an agent writes a file, the
**system** — the `da service` daemon (D4) ingesting + reconciling local edits into the KG /
state / event log — picks up the change afterward and propagates it (status transition,
graph node update, episodic event). The agent does not perform that propagation and does not
wait on it; reconciliation is the system's job, downstream of the file write. This is exactly
what D2 ("agents never block on the backend") and Requirement 2 ("agents read/edit the same
local YAML/markdown — no agent-facing change") already assert; §3B names *why* it must hold:
**the projection is the interface, so the interface must carry zero semantics beyond "it's a
file."**

### The boundary: agent-facing vs system-side

| Plane | What it is | Who uses it | Required for the agent? |
|---|---|---|---|
| **Agent-facing** | The **FS projection** — `.agents/workflow/**` and the other projected files. Read/write a file; zero new semantics. | **Agents** (on the server, with FS access) | **Yes — this is the agent's interface.** |
| **System-side** | `da` commands, UDS / HTTP (R3 §2A), the `EventBus` / transport (R3 §D4), and external integrations (Jira/Linear, CF DO). Propagation, control plane, external integration. | The **daemon / system / operators / external tools** | **No.** Optional sugar at most. |

- **`da` commands are optional, not required.** A human or script *may* use `da workflow …`
  for convenience (it writes through `WorkStore`, D7/D3), but an agent reaching the same
  outcome by editing the projected file is the **default, fully-supported path** — the
  daemon reconciles that edit identically. `da` is sugar over the file, never a gate in
  front of it.
- **UDS / HTTP / EventBus are system-side propagation, not the agent's interface.** R3 §2A's
  local transport and R3 §D4's `EventBus` move bytes *between system components and to
  external consumers*; they are the control plane and external-integration plane. An agent
  never has to speak UDS, HTTP, or the bus to get its work recorded — it writes a file and
  the system carries it onward.

**Net rule for any design that touches this model:** if a change would require an agent to
call a command, an RPC, or a transport to do ordinary work — instead of just reading/writing
a projected file — it **violates §3B** and must be reworked so the file path remains the
agent's interface. New semantics belong in the *system's* reconciliation, never on the
agent's side of the projection.

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
- **Mapping fidelity** — *granularity resolved by D9* (milestone grain; tasks → subtasks OR KG-only +
  attribution; upstream debounced to PR-merge). Remaining: for the fine-grained option, canonical
  `Task` ↔ Jira/Linear issue field mapping (write_scope, verifier_sequence, cross-plan `depends_on` as
  custom fields vs links; round-trip without loss); and for the milestone option, the exact
  milestone→workflow-state map per team + handling **multiple PRs per ticket** (which merge advances
  the ticket?) and an **upstream reopen** racing in-flight KG work.
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
  Its §2A local transport (UDS/HTTP) and §D4 `EventBus` are the **system-side** propagation/control
  plane of §3B, not the agent's interface; R3 §2A/§D4 point back here for that boundary.
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

## 9. Amendment (2026-07-11): the `git-ref` backend + read-from-master shim

**Provenance.** kg-ideate run on proposal `.agents/proposals/read-task-state-from-master-source.md`
(owner ask: "read task state from a master source"). Phase-1 briefing: KG has no SDD
decision nodes for this topic yet (code-graph only), so this grounds in §1–§8 above +
lessons `worktree-isolation-defeats-status-tracking`, `stale-local-master-ref`,
`stale-local-checkout-mass-drift`, `single-source-of-truth-across-specs-and-plans`.
This section **adds** a backend; it does not revise D1–D8.

The §2 backend ladder jumps from `local` (per-worktree files, **no** shared SOT) straight
to `kg` (needs the `da service` daemon + graph store). That gap is the common case: a team
wants cross-worktree **atomic status** (the D2/D5 re-dispatch fix) without standing up the
KG/DO daemon. A **git-native shared SOT** fills it.

- **D9 — `git-ref` WorkStore backend (git-native shared SOT).** Coordination state lives on
  a dedicated ref — `refs/agents/state` (configurable) — **orthogonal to the code branch**:
  worktrees on `feature/x`, `feature/y`, or detached HEAD all resolve status against the one
  ref. **Read:** via git (`git cat-file`/`show <ref>:<path>`), or a single shared linked
  worktree of the ref that all agents read — this is D2 ("status reads resolve against the
  backend, not the per-worktree YAML") in pure git. **Write:** a transition commits the
  changed state file(s) to the ref via atomic compare-and-swap (`git update-ref <ref> <new>
  <old>`, retry-on-mismatch) — the interprocess-safe RMW that today's `agentslock` only
  half-covers (only `plan_task.go` locks; `delegation.go`/`contract.go`/`eligible_accounting.go`
  don't), now serialized at the ref. **Conflict granularity:** split status into **per-task
  state files** under the ref so two workers transitioning *different* tasks never hit a
  line-level `TASKS.yaml` conflict (this also realizes D5's per-task lease/claim); the
  ref-level CAS-retry loop is the fallback. Rides the same `WorkStore` interface (D3) and the
  same D8 scope ladder — the backend value becomes
  `work_tracking.backend = local | git-ref | kg | cloudflare-do | jira | linear`. **This is
  the sane default upgrade from `local`:** no daemon, no external service, works offline (local
  ref is the degenerate SOT), and a team graduates `local → git-ref → kg` without changing the
  agent-facing file interface.

- **D10 — the state ref is NOT merged into the default (code) branch (answers the sync question).**
  It is a **parallel lineage** (like `refs/notes/*`), never an ancestor/descendant of `main`.
  Merging it into `main` would re-entangle the two planes D1 separates — pollute code history
  with status churn and force merges between two unrelated trees. What *does* sync:
  - **ref ↔ remote:** push/fetch `refs/agents/state` to/from `origin` so every clone/host shares
    one authority (`git push origin refs/agents/state`, a configured refspec). This is
    "replicate the ref," not "merge the ref into a branch."
  - **cross-worktree on one host:** nothing to sync — linked worktrees share the same object
    store + refs, so all worktrees see the ref natively (one ref, many worktrees).
  - **optional one-way audit snapshot:** if a human-readable copy in the code tree is wanted
    (D1′: "committed YAML is a periodic snapshot for audit, never the authority"), project the
    ref → a periodic snapshot commit/export on `main` — **one-way, never a merge back**; the ref
    stays the authority. This resolves §6's "Git vs backend double-tracking" for `git-ref`.

- **§3B compliance.** The git-ref backend still **projects into a readable file path** (a shared
  linked worktree of the state ref, or a read-through checkout into `.agents/workflow/`), so an
  agent keeps reading/editing a plain file with zero new semantics. The ref, the CAS write, and
  the remote sync are **system-side** (D4-style reconciliation, but git instead of a daemon) —
  the projection remains the agent's interface, honoring §3B's net rule.

- **Near-term read-from-master shim (ship first).** Before the full `WorkStore`/git-ref backend
  lands, add `work_tracking.read_from = worktree|master`: when `master`, `loadCanonicalTasks`
  (`commands/workflow/plan_task.go`) and the scout's eligibility/next read resolve `TASKS.yaml`
  from the canonical ref / `origin/<default-branch>` instead of the per-worktree working copy;
  writes land as today. Read-side-only, but it kills the re-dispatch storm (done-criterion #2's
  motivating 5×p1c failure) at the cost of one `git show`-backed read path — worth doing
  regardless of the full backend.

- **Resolves/re-scopes the commit-scope thread.** With coordination state on its own ref, task
  state is no longer committed into **code** branches at all — the whole-store-vs-task-scoped
  commit problem (`obs-da-workflow-commit-scope-safety.md`; payout `worker-bundle-authoring`
  tasks `commit-1-task-pathset`/`commit-2-cli-scoped-mode`) largely evaporates. Those tasks
  should be re-scoped as "write coordination state to the state ref," not "scope the code-branch
  commit." Recorded so the two planes get separate lineages.

**Added open question (§6):** CAS contention granularity under high fan-out — per-task state
files (preferred) vs ref-level CAS-retry — and whether the shared linked-worktree projection or
a read-through checkout is the cleaner §3B-compliant read path. Not blocking the read-from-master
shim.

**Execution:** plan `git-ref-work-backend` (dot-agents) carries the tasks; see its `.plan.md`.
