# Spec: meta-loop as operating model — refinement separated from work, orchestrator-driven across all plans

**Spec id:** `meta-loop-operating-model`
**Status:** draft (for review) — design artifact (spec tier). Plan: `workflow/plans/meta-loop-operating-model/` (outline only, §6).
**Source:** the orchestrator/meta-loop run this session — the orchestrator agent-type + dogfood (PR #134), the executor-side retro (PR #144), the cross-harness-adversarial reviewer (PRs #145/#149).
**Grounds in:** the `orchestrator-session-start` / `delegation-lifecycle` / `iteration-close` skills, the `da workflow` task-state machine, the verifier/reviewer `stage_profiles` (`.agentsrc.json`), and the KG-as-SOT self-improvement loop (`work-tracking-storage-abstraction/design.md §3A`, `knowledge-architecture-graph-views`).

---

## 1. Problem & goal

The toolchain already has the pieces of a working agent factory — an orchestrator that fans out
disjoint write-scopes, a verifier/reviewer chain, a task-state machine, a lessons/proposals corpus.
What it does **not** have is a stated **operating model**: a way of working that says *how* those
pieces compose turn over turn, and — critically — that **improving the way we work is itself a
tracked stream of work**, run on its own cadence, not smuggled into feature branches.

The cost of leaving this implicit was measured in the `agent-ops-hardening` reflection: the right
lessons existed but stayed *documentation, not enforcement*, so recognized patterns recurred
(the write_scope/co-commit wall recurred across four tasks **after** a lesson said "promote on 2nd
occurrence"). Refinement happened reactively, lesson-by-lesson, with no loop that schedules it.

**Goal:** encode the meta-loop as the maintainer's operating model —

1. **Separate the REFINEMENT loop from the WORK loop.** Refinement (improving agents / skills /
   prompts / schema / `stage_profiles` — the "how we work") runs as its own dogfood→observe→refine
   cycle, scheduled and tracked, **not** mixed into feature work.
2. **One orchestrator manages task-state across ALL plans/specs** (the cross-plan view) and
   **emits/schedules both work tasks and refinement/ideation tasks** as it manages the general steps.
3. **Cross-harness review is built in** — a different-engine second brain is a first-class stage of
   the chain, not an ad-hoc favor.
4. **Results feed back as data** — every result correlates to the operational + semantic nodes that
   produced it (§3A), so refinement is driven by evidence, not anecdote.

## 2. The principle — two loops, one factory

The system runs **two loops over one substrate**:

| | WORK loop ("the what") | REFINEMENT loop ("the how we work") |
|---|---|---|
| **Object** | features, fixes, docs — shipped artifacts | agents, skills, prompts, schema, `stage_profiles`, lessons, rules, hooks |
| **Trigger** | a plan/spec task becomes eligible | an observation: a recurring defect, a friction tax, a verifier gap, an ideation idea |
| **Shape** | implement → verify → review → merge | **dogfood → observe → refine** (run the way of working, watch it fail, fix the way of working) |
| **Done** | the PR ships and the task completes | the operating mechanism is changed and re-dogfooded |
| **State home** | working/episodic views | operational/semantic views (skills, `stage_profiles`, lessons) |

The principle: **the refinement loop treats the work loop as its subject under test.** You do not
improve the way you work by reasoning about it — you run it, observe where it breaks, and refine the
mechanism. This is exactly what PR #134 did and is the canonical pattern (§4.1).

These are **separated, not isolated.** They share the orchestrator, the task-state machine, the
verifier/reviewer chain, and the KG. The separation is in *budget, cadence, and bookkeeping*: a
refinement task is a tracked task with its own profile, not a side-effect of a feature branch. Mixing
them is the anti-pattern — it lets "while I'm here I'll also tweak the skill" hide an unreviewed
operating-mechanism change inside a feature diff.

## 3. The arc (ideation → orchestrate → execute → review → feed back)

```
                 ┌──────────────── ideation (human + agent) ────────────────┐
                 │  human ideas  +  agent-emitted observations (defects,     │
                 │  friction taxes, verifier gaps, "this recurred again")    │
                 └──────────────────────────┬───────────────────────────────┘
                                            ▼
        ┌───────────────────────────────────────────────────────────────────┐
        │  ORCHESTRATOR  (orchestrator-session-start)                         │
        │  - orient on the active line; reconcile eligible across ALL plans   │
        │  - cross-plan task-state view (da workflow eligible --json)         │
        │  - ingest ideation as tasks (work OR refinement) + classify         │
        │  - SCHEDULE: pick next task, decide fanout vs direct                │
        │  - emit NEW ideation/refinement tasks it discovers while managing   │
        └───────────────┬─────────────────────────────────┬─────────────────┘
                        ▼ (work task)                      ▼ (refinement task)
            ┌───────────────────────┐          ┌────────────────────────────┐
            │ worker (loop-worker)  │          │ refinement worker:          │
            │ delegation-lifecycle  │          │ dogfood → observe → refine  │
            │ implements write_scope│          │ (edits skills/profiles/...) │
            └───────────┬───────────┘          └─────────────┬──────────────┘
                        ▼                                     ▼
        ┌───────────────────────────────────────────────────────────────────┐
        │  VERIFIER → REVIEWER chain   (stage_profiles)                       │
        │  verifier_sequence (per app_type)  →  lens_set:                     │
        │    architecture-standards · acceptance-invariants · adversarial ·   │
        │    cross-harness-adversarial  ← the different-engine second brain   │
        └───────────────────────────────┬───────────────────────────────────┘
                                        ▼
        ┌───────────────────────────────────────────────────────────────────┐
        │  FEED BACK  (iteration-close → §3A correlation, KG-as-SOT)          │
        │  result node ⇄ edges to the stage_profiles / skills / rules /       │
        │  spec/plan/task that produced it  →  lessons + proposals  →         │
        │  become the next ideation inputs to the REFINEMENT loop             │
        └───────────────────────────────────────────────────────────────────┘
```

### 3.1 Ideation (human + agent)
Two sources of ideas feed one queue:
- **Human ideation** — the maintainer's feature/fix/refinement intent, captured as a spec or a plan task.
- **Agent ideation** — observations the orchestrator and workers surface *while running the work loop*:
  a recurring defect, a friction tax (à la the `agent-ops-hardening` transcript taxonomy), a verifier
  gap, a "this pattern recurred a 2nd time → promote it" signal. These are not free-text notes; they are
  **candidate tasks** the orchestrator ingests.

### 3.2 Orchestrator ingests + manages task-state across ALL plans/specs
The orchestrator (PR #134's pure-orchestration agent-type — deliberately **no `Edit`/`Write`**; every
slice is dispatched, every state mutation routes through `da workflow`) is the cross-plan scheduler:
- **Orient + reconcile** on the active line (`orchestrator-session-start` preflight §3: guarded,
  clean-tree-only `git fetch`; cycle-2 F1 added the multi-remote active-line discipline so eligibility
  is never read off the wrong/stale remote).
- **Cross-plan view:** `da workflow eligible --json` over **all** plans, not one — the orchestrator's
  distinguishing job is to see the whole board (work tasks *and* refinement tasks) and reconcile status
  against shipped PRs so an in-flight/done task is never re-dispatched.
- **Classify + schedule:** each ingested idea becomes a task tagged WORK or REFINEMENT, routed to the
  matching `execution_profile` app_type, and scheduled within the budget split (§5, open question).
- **Self-emit:** while managing the general steps the orchestrator emits new ideation/refinement tasks
  it discovers (e.g. a 2nd-occurrence pattern, a gate that should be mechanized) back into the queue.

### 3.3 Workers execute
Work tasks fan out to bounded `loop-worker`s under `delegation-lifecycle` (disjoint `write_scope`,
the canonical §0 pre-fanout gate, the executor self-gate from PR #144). Refinement tasks run the
dogfood→observe→refine shape (§4.1) and their `write_scope` is the operating mechanism itself
(a skill dir, an AGENT.md, a `stage_profiles` entry, a lesson).

### 3.4 Verifier / reviewer chain — with the cross-harness second brain
Both loops pass the same chain, resolved from `stage_profiles`:
- **verifier_sequence** per app_type (e.g. go-cli: `unit → cli-runner`; docs: `schema-check →
  citation-check → cli-runner`) — refinement tasks that touch prose/config route through the non-code
  verifiers (citation-check's docs↔code link-integrity, the `copy_test.go` manifest walk) per PR #149.
- **lens_set** — `architecture-standards · acceptance-invariants · adversarial`, plus
  **`cross-harness-adversarial`** (PRs #145/#149): a read-only adversarial pass dispatched to a
  *different* installed agent harness than the one running — a second pair of eyes from a different
  model family, machine-aware (discovers the host's CLIs, graceful-skips if none ≠ running). This is
  the built-in cross-harness review: the operating model's own changes get reviewed by an engine that
  did not author them.

### 3.5 Feed back — results as queryable data (§3A)
`iteration-close` persists the per-iteration record; under the KG-as-SOT direction a result is an
**episodic node** with edges to the **operational + semantic** nodes that produced it — the
`stage_profiles` it ran under, the skills/rules/agents/hooks in its working set, and the spec/plan/task
it implemented (`work-tracking-storage-abstraction §3A`). Those edges turn the global `CLAUDE.md`
self-improvement loop from memory into data: "which lesson/profile drove this outcome?", "which specs
regress most under which verifier sequence?", "did adopting rule X change result quality?". The
**lessons + proposals** that fall out of this become the next ideation inputs (§3.1) — closing the
loop. Lessons/`stage_profiles` stop being write-only and become nodes results are scored against.

## 4. How it composes with the existing machinery

| Existing mechanism | Role in the operating model |
|---|---|
| **`orchestrator-session-start`** | the orchestrator turn: orient/reconcile across all plans → eligible-orientation → pick → KG readback → fanout-vs-direct decision. The cross-plan scheduler of §3.2. |
| **`delegation-lifecycle`** | fanout + closeout for both loops: canonical §0 pre-fanout gate (status-vs-shipped, write_scope-on-HEAD, coverage-delta forecast, no-overlap), brief-template defaults, merge-back. |
| **`iteration-close`** | the feedback persistence step (§3.5); delegated workers: verify → checkpoint → merge-back; direct: verify → checkpoint → advance. |
| **`da workflow` state machine** | the single authoritative task-state surface — eligible / next / advance / delegation closeout / fold-back. The orchestrator (no Write tool) mutates state *only* here. |
| **verifier/reviewer `stage_profiles`** | the resolvable verify+review chain (§3.4); the registry the cross-harness lens registers into (PR #149). |
| **orchestrator agent-type (PR #134)** | the pure-orchestration role that *is* the scheduler — never becomes a worker. |
| **executor retro (PR #144)** | the worker self-gate that makes the WORK loop right-first-time, reducing the verify/review churn the refinement loop would otherwise have to absorb. |
| **cross-harness-adversarial (PRs #145/#149)** | the different-engine review stage built into the chain. |
| **KG-as-SOT / scoped-KG end-state** | where results → lessons/proposals correlate (§3A); the operating model's feedback substrate, not a side store. |

### 4.1 The canonical refinement cycle (dogfooded, not theoretical)
PR #134 *is* the reference instance of the refinement loop, and it ran exactly the prescribed shape:

1. **Built** the orchestrator agent-type + consolidated the pre-fanout gate.
2. **Dogfooded** it 4/4 (ran the new way of working against real tasks).
3. **Cycle-2 self-refine** — folded **five** refinements *from its own dogfood findings* back into the
   AGENT.md + skills: F1 multi-remote orient, F2 a non-Go branch in the §0 coverage-delta gate, F3
   named closeout verbs + CLI routing (orchestrator has no Write — all mutations via `da workflow`),
   F4 the gate-0d expand-vs-refuse rule (never silently widen a disjoint slice), F5 the deferred-tool
   preload note.

That arc — build → dogfood → observe its own failures → refine the mechanism — is the refinement
loop's unit of work, and the operating model says **run it deliberately on a cadence** rather than only
when a mechanism happens to be under construction.

## 5. Open questions (for the maintainer)

1. **Self-emit aggressiveness.** How aggressively should the orchestrator self-emit ideation/refinement
   tasks (§3.2)? Options: (a) emit silently into a backlog the maintainer triages; (b) emit only on a
   2nd-occurrence rule (matching the existing "promote on 2nd occurrence" lesson); (c) emit and
   auto-schedule within the refinement budget. Risk of (c): the system spends itself refining instead of
   shipping.
2. **Refinement-vs-work budget split.** What is the cadence/budget? E.g. a fixed ratio (1 refinement
   task per N work tasks), a time-boxed refinement window per wave, or event-driven (refine only when a
   recurrence/friction threshold trips). The `agent-ops-hardening` data (~57% of PRs were pure overhead)
   argues for a non-trivial refinement budget — but how much, and is it fixed or adaptive?
3. **When does the inner loop decide to refine?** A worker hitting a recurring friction *could* stop and
   refine the mechanism, or stay in its lane and only emit an observation for the orchestrator. Where is
   the cut — does the work loop ever self-interrupt to refine, or is all refinement scheduled out-of-band
   by the orchestrator?
4. **Refinement task profile.** Refinement tasks edit operating mechanisms (skills, `stage_profiles`,
   AGENT.md). Do they get a dedicated `execution_profile` app_type (e.g. `meta`/`refinement`) with a
   verifier_sequence tuned to dogfood-evidence (the change was re-run, not just edited), or do they reuse
   `docs`/`ideation`? A refinement task arguably is not "done" until it has been re-dogfooded (§4.1).
5. **Cross-harness as gate vs advisory.** Is `cross-harness-adversarial` a blocking gate on
   operating-model changes (a different engine must sign off on a change to *how we work*), or advisory
   like the other lenses? The "second brain reviews the brain" framing argues for blocking on refinement
   tasks specifically.
6. **KG dependency.** §3.5's queryable feedback presumes the KG-as-SOT backend
   (`work-tracking-storage-abstraction`, not yet built). Until then the loop runs on lessons/proposals
   files + `iteration-close` records — is that an acceptable interim, or is the data-driven feedback the
   precondition that gates adopting this operating model wholesale?

## 6. Plan outline (thin — the deliverable is this spec)

Indicative phases for a follow-on `workflow/plans/meta-loop-operating-model/`; not authored here:

- **P0 — Name the two loops in doctrine.** State the WORK/REFINEMENT separation in the operating
  doctrine (global `CLAUDE.md` §Workflow Orchestration + a pointer from the orchestrator AGENT.md), and
  add the WORK|REFINEMENT task tag convention. No code.
- **P1 — Refinement task profile.** Resolve OQ4: define (or reuse) an `execution_profile` app_type for
  refinement tasks, with a verifier_sequence that asserts re-dogfood evidence; register in
  `stage_profiles`.
- **P2 — Orchestrator self-emit.** Resolve OQ1+OQ3: specify the `da workflow` surface for the
  orchestrator to ingest/emit ideation tasks (classify WORK|REFINEMENT, recurrence-threshold trigger).
- **P3 — Budget/cadence.** Resolve OQ2: encode the refinement-vs-work budget into the orchestration
  pass (wave-level window or ratio).
- **P4 — Cross-harness gate policy.** Resolve OQ5: make `cross-harness-adversarial` blocking on
  refinement-tagged tasks (advisory elsewhere).
- **P5 — Feedback wiring.** Resolve OQ6: wire result→operational/semantic correlation (§3A) once the
  KG-as-SOT backend lands; until then, the lessons/proposals→ideation path is the interim loop.
