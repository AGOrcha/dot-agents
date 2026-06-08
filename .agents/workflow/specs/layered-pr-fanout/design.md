# Spec: Layered PR Fanout with `awaiting_review` Task Status

**Status:** accepted (graduated from project-local proposal
`.agents/proposals/layered-pr-fanout-with-pr-open-status.md`; all §5
decisions locked and §6.1–§6.4 + §6.6(a)(b)(c) resolved 2026-05-28).
This document is the canonical contract; the proposal is preserved for
historical context only.

**Companion artifact:**
`.agents/proposals/lens-evidence-policy-renderings.md` — locked §6.6(c)
via three project renderings (dot-agents / payout / ResumeAgent).

**Related specs:**
- `graph-backend-adapter-contract` — adapter-contract is the canonical
  surface for the lineage queries this spec uses to drive layered
  base-branch resolution and cross-plan conflict checks.
- `config-distribution-model` — `verifier_profiles` registry hosts the
  per-stage verifier sequence this spec references for the
  verifier-vs-lens split.
- `agent-run-scoring-observability-platform` — downstream consumer of
  the state-transition event stream this spec emits.

---

> **Consolidation update (2026-06-07) — `stage-profile-and-routing-consolidation`:** `verifier_profiles` + `reviewer_profiles` are now unified into one **typed** `stage_profiles` map (stage `executor`/`verifier`/`reviewer`/`orchestrator` → slug → `{label, prompt_files}`), and `app_type_verifier_map` is **retired** into `execution_profile.by_app_type.<type>.topology.verifier_sequence`. Legacy keys still load (folded, deprecated). Mentions of those keys below describe the pre-consolidation surface — read them as the new model.


## 1. Problem & Goals

### 1.1 Problem

The current workflow eligibility model gates downstream tasks on dep
tasks reaching `completed` (PR merged to master). With the maintainer
as the sole reviewer + merger, every wave serializes through that
review queue. Cross-plan in-flight tasks cannot pre-stack because their
base branches do not exist yet. Parallel velocity collapses to the
maintainer's review cadence even when downstream implementation work is
otherwise ready.

The orchestrator already runs a wave-fanout pattern in its head ("after
one wave is done you can start the next layer"), but that pattern is
not encoded in the workflow CLI. Encoding it explicitly lets the system
self-correct as PRs land, makes the in-flight topology inspectable,
and decouples implementation velocity from review latency.

### 1.2 Goals

1. **Parallel velocity** — downstream tasks become eligible the moment
   an upstream task's PR is open + green, not when it is merged.
2. **Layered fanout** — `da workflow fanout` understands that a new
   task may need to branch off an in-flight PR rather than `master`,
   and records that decision in the delegation bundle.
3. **Slot reclamation** — `awaiting_owner_review` (human-review) tasks
   free their worker slot so the next layer of work can start; the
   maintainer's review queue is no longer the rate limit.
4. **Self-correcting topology** — merges fast-forward downstream
   branches automatically; rejections cascade-block downstream tasks
   visibly; force-rebases trigger rebase-only downstream iterations.
5. **Verifier/lens separation** — objective signals (CI, coverage,
   issue counts) unblock downstream eligibility automatically;
   subjective signals (lens reviews) gate the merge transition, not the
   eligibility transition.

---

## 2. Decisions

Every decision below is locked. Rationale + alternatives-rejected are
captured in line.

### 2.1 Status name: `awaiting_review`

The new status is named `awaiting_review`, not `pr_open`.

- **Rationale:** generalizes beyond coding contexts (any artifact
  waiting on a review gate fits the same pattern: spec graduation, KG
  query review, lesson promotion).
- **Rejected:** `pr_open` (too coding-centric; locks future
  non-PR-shaped workflows out of the same machinery).

### 2.2 Sub-statuses: `awaiting_agent_review` and `awaiting_owner_review`

`awaiting_review` carries a sub-state field with exactly two values:

- `awaiting_agent_review` — lens reviewers (architecture-standards,
  acceptance-invariants, adversarial, optionally thermo-nuclear) are
  dispatched or running.
- `awaiting_owner_review` — lens verdicts accepted; human maintainer
  has the merge decision.

- **Rationale:** single status with a sub-state field keeps eligibility
  logic simple ("is dep in `awaiting_review`-or-better?") while still
  letting concurrency accounting and slot-reclamation behavior split on
  the sub-state.
- **Rejected:** two top-level statuses (`awaiting_agent_review`,
  `awaiting_owner_review`); rejected because it doubles every status
  predicate that already checks `awaiting_review`.

### 2.3 Precondition for entering `awaiting_review`

Transition `in_progress → awaiting_agent_review` is allowed only when
**all** of the following hold:

- Branch pushed.
- PR opened on GitHub.
- Primary verifier chain (e.g. `pr-ci`) terminal green.
- SonarCloud quality gate OK.
- Zero OPEN new-code issues.

No partial-green shortcut. The verifier owns this transition (see §6).

- **Rationale:** half-green tasks polluting downstream eligibility
  would force downstream rework when the upstream finally fails its
  remaining checks; the cost of waiting for full green is paid once,
  not amortized into cascade-rework.
- **Rejected:** allow `pr-mark-open` before all CI complete (proposal
  §6.1) — silent downstream-rework risk too high.

### 2.4 Lens reviewers gate the merge transition, not the eligibility transition

Lens reviewers (architecture-standards, acceptance-invariants,
adversarial, optionally thermo-nuclear) run inside `awaiting_review`
on the path from `awaiting_agent_review → awaiting_owner_review`.
They do **not** gate entry into `awaiting_review` itself.

Sequence:

```
in_progress
  → (verifier sequence green)
  → awaiting_agent_review
  → (lens dispatch terminal: accept)
  → awaiting_owner_review
  → (maintainer merges)
  → completed
```

- **Rationale:** verifier checks are objective (CI / coverage / issues)
  and can fire automatically; lens checks are subjective and may force
  scope rework. Objective signals unblock downstream eligibility cheaply;
  subjective signals are merge-gate so a lens failure does not silently
  invalidate downstream work that has already branched off.
- **Rejected:** run lenses pre-`awaiting_review` (would block
  eligibility on subjective signals, defeating the parallelism goal).

### 2.5 Multi-dep base resolution: v1 manual, v2 lineage-aware

When a new task has multiple dependencies all in `awaiting_review` on
different branches:

- **v1 behavior:** `da workflow fanout` refuses, surfaces a sequencing
  prompt, and requires the orchestrator/user to pass `--base-branch`
  explicitly.
- **v2 direction (not in initial scope):** auto-sequence using
  contract + symbol + blocker-list signals queried from the
  graph-backend adapter (see §4 and §5).

- **Rationale:** v1 honesty over v1 guesswork; auto-merge of two
  in-flight branches without a lineage signal risks silent conflict
  introduction. v2 only becomes safe once the adapter-contract surface
  exposes the lineage queries the algorithm needs.
- **Rejected:** auto-merge-base creation in v1 (the
  `wave-stack/<N>` branch proposed in the original proposal §3.2) —
  cannot reliably detect conflict-free merge without lineage data.

### 2.6 Cascade on rejection: hard-block downstream

When a maintainer closes a PR without merging (the upstream task moves
to `blocked`), every downstream task currently in `awaiting_review` or
`in_progress` that branched off that upstream's PR cascade-blocks.

Manual unblock paths:

- Replan the downstream work against a new base, OR
- Re-open the upstream PR (no cascade automatically lifts; explicit
  user action required).

The orchestrator MAY issue replacement planning tasks (per the v2
direction in §2.5) when cascade-block fires, but the workflow does not
require this.

- **Rationale:** silent drift is worse than visible block; making the
  cascade explicit forces a re-planning step rather than letting
  downstream workers continue against a dead base.
- **Rejected:** auto-rebase downstream onto `master` (would silently
  drop the upstream contract changes downstream depended on).

### 2.7 Force-rebase detection: SHA1 set difference

A force-rebase is detected by computing the SHA1 set difference of the
PR's commit list between two `eligible` polls. Any drop of previously
observed commits (with replacement) is treated as a force-rebase event.

- **Rationale:** does not rely on `--force-with-lease` event hooks;
  works regardless of which client (maintainer, worker, GitHub web UI)
  performed the rebase.
- **Rejected:** GitHub webhook listening (adds an out-of-band runtime
  dependency; SHA1 diff is cheap and offline).

### 2.8 Concurrency accounting

Slot accounting is per task, gated on the sub-status of
`awaiting_review`:

| Status                       | Counts against `max_parallel_tasks` |
| ---                          | ---                                 |
| `pending`                    | No                                  |
| `in_progress`                | Yes                                 |
| `awaiting_agent_review`      | **Yes** (lens dispatch can bounce work back) |
| `awaiting_owner_review`      | **No** (human-review timeline is unbounded; slot freed) |
| `blocked-on:<ref>`           | **No** (slot freed — but tracked in a separate `blocked` bucket per §3.4.3 so pathological "all blocked" state is visible) |
| `completed` / `blocked` / `cancelled` | No                         |

When a task in `awaiting_owner_review` bounces back (maintainer
requests changes), it re-acquires a slot (or queues if at capacity)
and re-enters `in_progress`.

- **Rationale:** `awaiting_agent_review` holds the slot because the
  lens dispatch budget is bounded (see §5) and the task may bounce
  back to `in_progress` within minutes; `awaiting_owner_review` frees
  the slot because human-review latency is unbounded and the slot
  must be reclaimable for the next layer.
- **Rejected:** free the slot on `awaiting_agent_review` (would let
  the worker pool oversubscribe whenever lens dispatch is slow).

### 2.9 `max_parallel_tasks` default

`max_parallel_tasks` (renamed from `max_parallel_workers`; see §5) is
**capacity-derived** when unset: `clamp(NumCPU - 2, 2, 16)` — reserve two
cores for the orchestrator + OS, floor at 2 so small machines still
parallelize, ceiling at 16 so large machines do not oversubscribe. An
explicit preference override may exceed the auto ceiling up to **32** (a
typo guard, not a capacity policy). This supersedes the former fixed
default of 7.

- **Rationale:** a fixed `7` is arbitrary — it under-uses large machines
  and can overrun small ones. Each slot may run a worker that compiles and
  tests (CPU + IO heavy), so the budget should scale with cores.
- **Rejected:** a fixed default (does not adapt to the host); an unbounded
  override (a typo like `999` would thrash — hence the 32 guard).

### 2.10 `base` and `ready` are domain-agnostic seams (git is adapter #1)

The two predicates the layered model runs on — **"is this work's input
available to branch from?"** (`base`) and **"can a downstream consumer
start before the upstream fully merges?"** (`ready`) — are abstract
seams, not git concepts. Git branches + PRs are the first *adapter* that
satisfies them, not the model itself.

- `ready` is already abstracted by the `internal/events` PR producer:
  the `awaiting_review` transition is surfaced as an `event.pr.*` signal
  (§6 emits on every edge), consumed by base-resolution through the
  `prSourceLister` seam. A non-VCS domain plugs in a different producer
  (`event.doc.section_approved`, `event.dataset.partition_ready`) and the
  eligibility/fanout machinery is unchanged. The events producer is
  "ready" adapter #1; PRs are not privileged.
- `base` is an **addressable output reference** (`OutputRef`), not a
  branch name. The git adapter resolves a ready dependency's `OutputRef`
  to its PR head branch; other adapters resolve it to a content hash, an
  artifact/object id, or a versioned blob handle. `--base-branch` is the
  git adapter's concrete spelling of `--base-ref`; it is one adapter's
  surface, not the contract.

- **Rationale:** the §4 algorithm and §4.2 bundle schema are written in
  git terms (`base_branch`, `pr_branch`, `master`) because git is the
  only adapter v1 ships. Naming the seams explicitly keeps that from
  ossifying into a git-only assumption: the algorithm depends on
  `OutputRef` + a readiness signal, and git is injected as the default
  adapter (`prBaseResolver`). The `config-distribution-model` content-hash
  auto-sync already gives a VCS-free way to say "this input is at version
  X" — exactly what a branch ref provides for code — so a
  content-addressed adapter is a near-term, not hypothetical, second
  adapter.
- **Rejected:** keeping `base`/`ready` defined in terms of git PRs (locks
  every non-code domain — spec graduation, dataset partitioning,
  doc-section review — out of the layering machinery the rest of this
  spec builds, contradicting the §2.1 generalization that named the
  status `awaiting_review` rather than `pr_open` for exactly this reason).

---

## 3. Lifecycle State Machine

### 3.1 States

```
pending
  └─→ in_progress
        ├─→ blocked            (external block; manual recovery)
        ├─→ cancelled          (manual)
        └─→ awaiting_review
              ├── awaiting_agent_review
              │     ├─→ in_progress          (lens reject / verify retry needed)
              │     └─→ awaiting_owner_review (lens accept)
              └── awaiting_owner_review
                    ├─→ in_progress          (maintainer requests changes; slot re-acquired)
                    ├─→ blocked              (maintainer closes PR; cascade)
                    └─→ completed            (maintainer merges)
```

### 3.2 Edge specifications

| From → To | Trigger | Actor | Precondition |
| --- | --- | --- | --- |
| `pending → in_progress` | task picked | worker | dependencies satisfied per §4 |
| `in_progress → awaiting_agent_review` | verifier sequence terminal green + PR open + Sonar OK + 0 OPEN issues | verifier (auto) | §2.3 fully met |
| `in_progress → blocked` | external dependency missed / unrecoverable verify failure | worker/orchestrator | manual or after `primary_chain_max` exhaustion |
| `in_progress → cancelled` | manual | user | — |
| `awaiting_agent_review → awaiting_owner_review` | all lens verdicts accept (per `lens_concurrency` policy) | lens-gate dispatcher | §5.3 met |
| `awaiting_agent_review → in_progress` | any lens rejects OR verifier-bounce after lens findings | lens-gate dispatcher | task re-acquires slot (already holds it) |
| `awaiting_owner_review → completed` | PR merged on GitHub | poll detector / `delegation closeout` | merge SHA confirmed |
| `awaiting_owner_review → in_progress` | maintainer leaves "request changes" review | poll detector | re-acquires slot or queues |
| `awaiting_owner_review → blocked` | maintainer closes PR without merge | poll detector | cascade-block downstream per §2.6 |
| `awaiting_review (any) → in_progress` | force-rebase detected (SHA1 set diff) | poll detector | downstream rebase tasks enqueued |
| `* → blocked` (cascade) | upstream rejection (§2.6) | poll detector | downstream was branched off the rejected PR |

### 3.3 Eligibility decay (telemetry, not blocking)

A task in `awaiting_owner_review` for more than **24 hours** without
merge is surfaced in `da workflow eligible` output with a
`needs_review_since` annotation. This is a maintainer-visibility
signal; it does not change eligibility computation.

### 3.4 `blocked-on:<ref>` parameterized state

A task may be paused on an external blocker — another in-flight task,
a missing secret, a pending maintainer decision, an external service
outage. The state is `blocked-on:<ref>`, where `<ref>` is part of the
state, not a separate field:

- `blocked-on:task:<plan>/<task>` — waiting for another task to reach
  `completed` (or any other named status)
- `blocked-on:secret:<NAME>` — waiting for a GH/repo secret to exist
  (detected via `gh secret list`)
- `blocked-on:decision:<id>` — waiting for an explicit maintainer
  decision (consumed via `da workflow unblock`)
- `blocked-on:condition:<predicate>` — generic predicate the runtime
  can re-evaluate on every event tick (e.g.
  `condition:gh-checks(<pr>)=green`)

#### 3.4.1 Entry edges

| From | Trigger |
|---|---|
| `in_progress` | Worker fold-back artifact declares a transient blocker; runtime parses the fold-back's `blocked_on:` field and applies the parameterized state instead of forcing re-spawn |
| `awaiting_review` | Lens reviewer or maintainer comment requests change that depends on external resolution; status regresses to `blocked-on:<ref>` (not all the way to `in_progress`) |
| Direct CLI | `da workflow block <task> --on <ref> --note <reason>` for explicit maintainer-driven pause |

#### 3.4.2 Exit edges

| To | Trigger |
|---|---|
| `in_progress` | Auto-resume when the blocker's predicate evaluates true (the runtime owns the watch); or `da workflow unblock <task> --resume-as in_progress` for manual override |
| `pending` | Manual override `da workflow unblock <task> --resume-as pending` (e.g., scope changed during the pause) |
| `cancelled` | Maintainer cancels a long-stuck blocker via `da workflow advance --status cancelled` |

#### 3.4.3 Slot semantics

`blocked-on:<ref>` **frees the slot** (same as `awaiting_owner_review`
per §2.8) — no active compute, just a held place in the DAG. This is
load-bearing: lets downstream work consume the slot while the blocked
task waits for its blocker to resolve.

A new slot-ledger bucket — `blocked` — is tracked separately from
`code-impl` / `design` / `hygiene` so the "all N slots blocked,
nothing actually running" pathology is visible to the orchestrator
(and the productionized
[[workflow-orchestrator-daemon]]).

#### 3.4.4 Eligibility decay for blocked tasks

A task `blocked-on:<ref>` for more than **N days** (default N=7,
configurable per project via `.agentsrc.json`) without the blocker's
predicate evaluating true is surfaced in `da workflow eligible` output
with a `blocker_stale_since` annotation — a gentle nudge before
silent-zombie territory. Maintainer can then `da workflow unblock` or
`--status cancelled` explicitly.

#### 3.4.5 Auto-resume contract

The runtime polls each `blocked-on:<ref>` predicate on every event
tick (worker terminal, PR merge, gh-secret event, scheduled wake).
Predicate evaluation is:

- `task:<plan>/<task>` → status of the named task is `completed`
- `secret:<NAME>` → `gh secret list` includes the name
- `decision:<id>` → an explicit `da workflow resolve-decision <id>`
  has landed
- `condition:<predicate>` → predicate-specific evaluator (CI green,
  external HTTP probe, etc.); evaluator registry pluggable per project

On predicate true, transition to `in_progress` (the implicit default;
overridable in the original `block` call via `--resume-as`).

#### 3.4.6 Cascade semantics with §2.6 (hard-block downstream)

If task A is `blocked-on:<ref>` and task B `depends_on: [A]`:
- B's eligibility computation already requires A to be in
  `{completed, awaiting_owner_review}` per §3.1
- A in `blocked-on:*` does NOT satisfy B's dep (unlike
  `awaiting_owner_review` which does)
- Therefore B stays `pending`, no implicit block-on-block cascade
- Maintainer may explicitly `block` B on A if they want decay
  signals to surface the wait

#### 3.4.7 Concrete in-flight cases (illustrative)

- `pr10-branch-split/signing-native-mac-windows` (folded back today
  awaiting Apple Dev ID secret) — would be
  `blocked-on:secret:APPLE_DEVELOPER_ID_CERT_P12_BASE64` with
  auto-resume when `gh secret list` shows it
- `pr10-branch-split/pr8b-org-migration` (HOLD pending maintainer
  ratification on module path canonical) — would be
  `blocked-on:decision:agorcha-module-path-cut`

These two cases motivated the state addition (review comment on
PR #149, 2026-05-28).

---

## 4. Layered Base-Resolution Algorithm

`da workflow fanout` computes the recommended `base_branch` for a new
delegation bundle from the union of two inputs:

1. The task's declared `depends_on` set (TASKS.yaml).
2. A lineage query against the graph-backend adapter for symbols /
   types / helpers / decisions the task's `write_scope` will
   transitively touch.

### 4.1 Algorithm

```
inputs:
  task_id            : the new task being fanned out
  depends_on         : set of dep task ids (may include cross-plan ids "<plan>/<task>")
  write_scope        : the new task's intended write paths/symbols
  in_flight_tasks    : { task_id → status, pr_branch, pr_number }

steps:
  1. status_of_deps := { t → in_flight_tasks[t].status for t in depends_on }
  2. if all status_of_deps ∈ { completed, awaiting_owner_review } AND
        all deps' branches have merged to master → base = "master"
  3. else if exactly one dep is in { awaiting_review (any sub-status) }
        → base = that dep's pr_branch
  4. else if multiple deps in awaiting_review on distinct branches:
        a. query graph-backend adapter for lineage:
             reach(task_id.write_scope) ∩ reach(each_dep.write_scope)
        b. v1: refuse, surface the conflict set, require --base-branch
        c. v2 (future): if lineage queries show a clean transitive
             merge-base, propose it; else refuse
  5. emit base_recommendation = { base_branch, base_pr, base_task,
        lineage_certificate? }
```

### 4.2 Bundle schema additions

The delegation bundle YAML gains three fields under `scope` (additive,
backward-compatible — older workers ignore unknown fields):

```yaml
scope:
  base_branch: <ref>          # the branch to check out from
  base_pr: <pr-number>        # the PR backing the base, if any
  base_task: <plan>/<task>    # the upstream task whose PR backs the base
```

When `base_branch` is omitted, the worker defaults to `master`
(today's behavior).

### 4.3 Lineage-certificate (optional v1, required v2)

A `lineage_certificate` is the serialized output of the graph-backend
adapter query used to justify the chosen base. In v1 it is optional
metadata for observability; in v2 the auto-sequencing path produces it
as a hard precondition (the certificate IS the justification for
selecting a layered base over `master`).

### 4.4 Output-addressability precondition & resolver adapters

Layering is an **optimization that requires composable, addressable
outputs**, and the algorithm degrades safely when a domain lacks them.

#### 4.4.1 The two requirements layering needs

A downstream stage can branch off an upstream's *in-flight* (un-merged)
output only when both hold:

1. **Addressable** — the upstream output has a stable identifier a
   consumer can name before it is finalized (git: the PR head branch;
   content-addressed: the digest; artifact store: the object id).
2. **Composable** — a consumer can build on that partial output and the
   eventual finalize (merge) reconciles cleanly (git: branch off an
   unmerged branch; the merge fast-forwards downstream per §7.1).

When either fails — a single opaque binary doc whose edits are not
mergeable, a proprietary blob with no partial identity — the output is
**not layerable**. The honest behavior is to **completion-gate**: the
downstream `base` resolves to the domain trunk (git: `master`) and the
dependent task stays gated on the upstream reaching a terminal/merged
state, exactly the pre-layering model (§1.1). This is not a degradation
bug; it is the correct floor for non-composable outputs.

#### 4.4.2 Resolver adapter seam

`resolveBase` (§4.1) depends on a `baseResolver` adapter, not on git
directly:

```
baseResolver:
  Trunk() -> ref                             # the domain "no-layer" base (git: master)
  LayerableBase(dep) -> (OutputRef, layerable bool)
                                             # the ready dep's addressable output,
                                             # or (_, false) -> completion-gate to Trunk()
```

- **git adapter (v1, default — `prBaseResolver`):** `Trunk()` =
  `master`; `LayerableBase` returns the dep's PR head branch when its
  status is an `awaiting_review` value (the `ready` signal, itself
  produced by the events producer) and a PR branch exists; otherwise
  `(_, false)`.
- **content-addressed adapter (near-term):** `Trunk()` = the latest
  published digest; `LayerableBase` returns the upstream output's content
  hash when the producer has emitted a `ready` event for it. Demonstrated
  end-to-end in the resolver tests (a digest `OutputRef`, no PR number).
- **non-composable adapter (floor):** `LayerableBase` always returns
  `(_, false)` → every dependent completion-gates. The domain still gets
  the workflow's status model, slot accounting, and decay signals — it
  just forgoes the parallel-velocity optimization.

The §4.2 bundle fields (`base_branch`, `base_pr`, `base_task`) are the
**git adapter's projection** of the generic `{ base_ref, base_output,
base_task }`. They remain the canonical on-disk names in v1
(backward-compatible); a future multi-adapter pass introduces neutral
aliases without breaking existing bundles.

#### 4.4.3 AI-driven non-code domains nudge toward addressable outputs

For AI-generated non-code work (drafts, datasets, designs), the
user-facing benefit runs the other way: putting outputs under **content
addressing** — even when the domain has no native VCS — is what *unlocks*
both layering and rollback/lineage. So the system's stance is to
**encourage versioned/addressable outputs** rather than treat their
absence as a hard blocker. Absence → completion-gate (§4.4.1), presence →
full layered velocity. The choice stays the domain's; the model works
either way.

---

## 5. Stage Concurrency Model

### 5.1 Per-task lifecycle stages

Each task moves through these stages, each potentially running
concurrently across different tasks at the same time:

```
impl → verify → review-agent → review-owner → done
```

A "slot" is occupied from impl-start until terminal (completed /
blocked / cancelled), with the exception in §2.8 that
`awaiting_owner_review` frees the slot.

### 5.2 Concurrency knobs

Two top-level knobs in `.agentsrc.json`:

| Field                      | Meaning                                                     | Default |
| ---                        | ---                                                         | ---     |
| `max_parallel_tasks`       | maximum tasks holding a slot concurrently (see §2.8)        | `clamp(NumCPU-2, 2, 16)` (§2.9) |
| `max_parallel_within_task` | maximum stages running concurrently inside one task         | unset → derive from `evidence_policy` |

When `max_parallel_within_task` is unset, the dispatcher derives it
from the bundle's `evidence_policy` (verifier sequencing already
follows evidence_policy; lens sequencing reuses that surface).

The `max_parallel_workers` field is renamed to `max_parallel_tasks`.
A migration path MAY accept the old name with a deprecation warning;
the spec requires only that the new name is the canonical one.

### 5.3 Lens dispatch concurrency

Default: **parallel** dispatch. All lenses in the bundle's `lens_set`
run concurrently inside `awaiting_agent_review`. Sequencing is opt-in,
project-configurable, via four additive fields under
`verification.evidence_policy`:

```yaml
evidence_policy:
  lens_set:         [<lens-name>, ...]
  lens_concurrency: parallel | sequential | gated   # default: parallel
  lens_chain:                                       # honored only when lens_concurrency = gated
    - lens: <lens-name>
      on_fail: short_circuit | record
      on_pass: continue
  lens_tier_gate:                                   # cheap-tier → expensive-tier gate
    tier1: [<lens-name>, ...]
    tier2: [<lens-name>, ...]
    tier3: [<lens-name>, ...]
    tier_promotion: any_finding_above_LOW | tier1_all_pass | <other policy>
  lens_routing:                                     # per-app-type lens-set override
    by_app_type:
      <app-type>: [<lens-name>, ...]
```

All four fields are optional. Projects that do not configure them get
parallel dispatch with the default lens set
(`architecture-standards`, `acceptance-invariants`, `adversarial`).

- **Rationale:** wall-clock minimization is the user-visible win this
  spec is built around; sequencing lenses by default would partially
  defeat it. Project shape varies sharply (dot-agents wants
  architecture-first; payout wants adversarial-first; ResumeAgent
  wants a 2-lens set excluding thermo-nuclear) — no single global
  default fits all.
- **Rejected:** forced sequential dispatch (defeats the parallelism
  goal); forced parallel without opt-out (forces compute spend on
  projects whose lenses are expensive or coupled).

### 5.4 Slot semantics by sub-status

Per §2.8: `awaiting_agent_review` holds the slot, `awaiting_owner_review`
frees it. Bounce-back from `awaiting_owner_review` re-acquires a slot
(or queues if at `max_parallel_tasks` capacity).

The slot reclamation in `awaiting_owner_review` is what enables the
layered-fanout v2 model to start the next layer: as soon as the
upstream task moves to owner-review, the next layer's tasks become
eligible AND a slot is available for them to start.

---

## 6. Verifier vs Lens-Gate Split

The two transitions on the path from `in_progress` to `completed` are
owned by distinct dispatchers:

### 6.1 Verifier owns `in_progress → awaiting_agent_review`

- Triggered by: worker exit at merge-back per existing
  `worker-owns-pr-readiness-loop` pattern.
- Runs: the verifier sequence declared in the bundle's
  `verifier_sequence`, terminating on either green-all or
  retry-budget exhaustion (`primary_chain_max` / future
  `verifier_chain_max` — see §10 F2).
- On terminal green: calls the equivalent of `pr-mark-open` (verifies
  §2.3 preconditions, transitions task to `awaiting_agent_review`).
- On exhaustion: transitions to `blocked` with a fold-back artifact.

### 6.2 Lens-gate owns `awaiting_agent_review → awaiting_owner_review`

- Triggered by: entry into `awaiting_agent_review` (or scheduled per
  policy on tasks already in the state).
- Runs: lens dispatch per §5.3 (`lens_concurrency` policy +
  `lens_set` / `lens_routing`).
- On lens accept (all lenses): transition to
  `awaiting_owner_review`.
- On lens reject (any lens with `on_fail: short_circuit`, or
  policy-driven aggregation): transition back to `in_progress`;
  worker re-engages with the lens findings as fresh context.
- Does NOT cascade-block downstream `awaiting_review` tasks on lens
  rejection unless the rework changes the public contract (see §7).

### 6.3 User/maintainer owns `awaiting_owner_review → completed`

- Triggered by: maintainer merging the PR on GitHub.
- Detected by: poll detector observing the merge SHA, OR explicit
  `da workflow delegation closeout` call.
- On merge: cascade fast-forwards downstream branches per §7.

---

## 7. Force-Rebase & Reject Cascade Semantics

### 7.1 Maintainer merges upstream (happy path)

- Upstream task auto-completes on next `eligible` / poll iteration.
- Downstream branches (already containing the upstream's commits)
  fast-forward on next push or auto-rebase. No downstream rework.

### 7.2 Maintainer rejects upstream (closes without merge)

- Upstream → `blocked` with a "base rejected" fold-back artifact.
- Every downstream task in `awaiting_review` or `in_progress` that
  branched off the rejected PR cascade-blocks (§2.6).
- Unblock requires explicit user/orchestrator action: replan onto a
  new base OR re-open the upstream PR.

### 7.3 Force-rebase detected (SHA1 set diff per §2.7)

- The PR-holding task regresses to `in_progress` (worker re-pushes the
  new tip).
- Downstream tasks that branched off the rebased PR are auto-enqueued
  with a `rebase-only` flag; their workers run a rebase-only iteration
  (not full re-impl) — the workflow distinguishes "your base moved"
  from "your impl is wrong."

### 7.4 Maintainer "request changes" review

- Task regresses to `in_progress`; worker prompt sees the
  change-request context fresh.
- Slot is re-acquired (or queues at capacity); no cascade fires
  unless the change request is later escalated to PR close.

---

## 8. Implementation Findings (References Only)

The following findings from the renderings work (§6.6 of the parent
proposal) are tracked separately in the `evidence-policy-schema-cleanup`
plan/task. They are NOT spec content — the spec records that they must
be addressed before the lens-sequencing surface lands, but the work
itself is implementation:

- **F1.** `delegationEvidencePolicy.classification_required` exists in
  code with no CLI flag and no documentation. Must be surfaced
  (flag + docs) or removed before evidence_policy is locked.
- **F2.** `primary_chain_max` is documented as the verifier retry
  budget; reusing it as a bundle-wide chain budget shared across
  verifier and lens dispatch would silently shift semantics. Split
  into `verifier_chain_max` + `lens_chain_max` before any
  sequencing-driven lens retry behavior lands.
- **F3.** `sandbox_mutations` today only steers verifier behavior; the
  adversarial lens path can read the same field to enable
  active-probing review (mutation-based probes inside the sandbox)
  rather than read-only review. Extend semantics, do not add a new
  field.

See `.agents/workflow/plans/evidence-policy-schema-cleanup/` for the
implementation tracking of F1/F2/F3.

---

## 9. Done Criteria (Verifiable)

A planner working from this spec MUST satisfy each of these to
consider the layered-fanout work complete:

1. **Eligibility unblocks on `awaiting_review`.**
   `da workflow eligible --json` returns a task whose only unmet dep
   was just transitioned from `in_progress` to `awaiting_review` as
   eligible-now (test: simulate the transition, re-run `eligible`,
   assert the dependent task appears).

2. **Fanout records the base.**
   `da workflow fanout --base-task <plan>/<task>` produces a bundle
   whose `scope.base_branch`, `scope.base_pr`, and `scope.base_task`
   are populated from the upstream task's PR metadata.

3. **Single-dep auto-base.**
   When a task has exactly one dep in `awaiting_review` and no
   `--base-branch` flag, `da workflow fanout` selects that dep's PR
   branch automatically (no human input).

4. **Multi-dep refusal.**
   When a task has multiple deps in `awaiting_review` on distinct
   branches and no `--base-branch` flag, fanout refuses with a
   non-zero exit code and a sequencing prompt naming the conflict set.

5. **Slot reclamation on owner-review.**
   When a task transitions to `awaiting_owner_review`, the coach
   ledger frees its slot (test: at `max_parallel_tasks - 1` slots
   occupied, transitioning the last in-progress task to
   `awaiting_owner_review` causes a queued task to start).

6. **Cascade-block on rejection.**
   Closing the upstream PR (without merge) moves every downstream
   `awaiting_review` task with `base_task` pointing to it into
   `blocked` on the next poll iteration.

7. **Force-rebase triggers downstream rebase-only iterations.**
   SHA1 set-difference detection on the upstream PR's commit list
   regresses the upstream task to `in_progress`, transitions
   downstream tasks back to `in_progress` with a `rebase-only` flag,
   and the worker prompt for those iterations reflects the flag.

8. **Verifier-vs-lens ownership respected.**
   Verifier dispatcher is the sole code path that performs the
   `in_progress → awaiting_agent_review` transition; lens-gate
   dispatcher is the sole code path that performs the
   `awaiting_agent_review → awaiting_owner_review` transition.

9. **Lens parallel default.**
   A bundle that omits `evidence_policy.lens_concurrency` dispatches
   all lenses in `lens_set` concurrently.

10. **Lens opt-in sequencing honored.**
    A bundle with `lens_concurrency: gated` + `lens_chain` runs lenses
    in declared order, short-circuiting on `on_fail: short_circuit`.

11. **Decay annotation surfaces.**
    `da workflow eligible --json` annotates `awaiting_owner_review`
    tasks older than 24h with `needs_review_since: <ISO-8601>`.

12. **`max_parallel_tasks` schema documented.**
    `.agentsrc.json` schema declares `max_parallel_tasks` with default
    7 and (optionally) accepts `max_parallel_workers` with a
    deprecation warning.

---

## 10. Deferred / Out of Scope

The following are explicitly NOT in scope for the initial layered-PR
fanout implementation. They are noted here so future work can pick
them up without re-litigating the boundary:

- **Auto-merge by the system.** The maintainer remains the sole
  merger; this spec only reduces the wait between merges.
- **AI-assisted conflict resolution** on downstream rebase. Workers
  perform the rebase; conflict resolution remains human.
- **GitHub draft PR auto-promotion** when CI turns green (e.g.
  auto-mark `Ready for review`).
- **Cross-repo layering** (e.g. a dot-agents PR that depends on a
  homebrew-tap PR). Single-repo layering only in v1.
- **Multi-adapter base resolution.** v1 ships only the git `baseResolver`
  adapter (§4.4.2). Content-addressed and non-composable adapters are
  specified (§4.4) and exercised by the resolver tests, but no production
  non-git adapter is wired, and the neutral `base_ref` / `base_output`
  bundle aliases (§4.4.2) land with the first one, not before.

---

## 11. Open Questions

### 11.1 Lineage-aware base resolution (§2.5 v2)

The v2 auto-sequencing algorithm in §4 depends on a `base_branch_lineage`
query against the graph-backend adapter. The query surface (symbol /
type / decision lineage traversal) is partially defined in the
`graph-backend-adapter-contract` spec but the specific traversal
shape — "given task B's `write_scope`, return the set of in-flight
tasks whose `write_scope` defines symbols B transitively reaches" — is
not yet enumerated.

**Resolution path:** the v2 base-resolution algorithm must be deferred
until the adapter-contract spec adds (or names) the traversal-shaped
query. This spec does not block on that resolution; v1 manual
sequencing is sufficient for initial implementation.

**Decision needed in v2:** which adapter (CRG, gopls-shim, or a future
adapter) is the authoritative lineage source for Go code; this spec
notes that CRG's Go analysis is currently weak and a thin gopls /
Go AST shim may be required before adapter-contract uniformity ships.

---

## 12. Relationship to Other Specs

- **`graph-backend-adapter-contract`** — provides the query surface
  the v2 layered-base auto-sequencing depends on (§4, §11.1).
  Implementing v2 requires either adapter-contract graduating with the
  traversal queries, or an interim CRG / gopls shim.
- **`config-distribution-model`** — hosts the `verifier_profiles`
  registry that this spec's verifier-vs-lens split (§6) consumes; its
  content-hash auto-sync is the VCS-free versioning a content-addressed
  `baseResolver` adapter builds on (§2.10, §4.4.2).
- **`internal/events` PR producer** — the `ready` signal adapter (§2.10,
  §4.4.2). The producer emits the `awaiting_review` transition as
  `event.pr.*`, consumed by base-resolution via the `prSourceLister`
  seam; a non-VCS domain swaps in a different producer without touching
  eligibility or fanout. This spec depends on the producer for the
  readiness signal but does not own its contract.
- **`agent-run-scoring-observability-platform`** — downstream
  consumer of the state-transition events this spec emits
  (entry/exit edges in §3.2). The event stream contract is owned by
  the observability spec; this spec only commits to emitting events
  on every edge.
- **`workflow-artifact-model`** rule — adding `awaiting_review` is a
  Tier-3 (TASKS.yaml) refinement; it does not perturb the
  Spec/Plan/Tasks/History tier model.
- **`thermo-nuclear-lens-evaluation`** — informs the lens-set
  enumeration in §5.3; thermo-nuclear is optional per that spec.
