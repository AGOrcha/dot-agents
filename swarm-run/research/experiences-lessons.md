# Experiences & Lessons — distilled for an artifact-driven `da` inner-loop swarm

**Scope.** This is the *applicable* subset of the dot-agents lessons/experience corpus,
distilled into RULES for designing an omp "swarm" (a DAG of subagents coordinated via
shared files) that drives the dot-agents inner loop using `da`'s own execution/verification
profiles. Every rule cites its source (`~/proj-docs/dot-agents/.agents/...` unless a
`~/.agents/...` user-dir path is given) and states **why** + **how it constrains the swarm
topology or the agent briefs**.

**Environment note (verified).** Installed CLI is `da version 0.4.2`
(`da --version`). The repo source is ahead of the shipped binary in places
(e.g. `da run` rename landed release 0.5.0 per `stale-dev-binary-vs-shipped-feature/LESSON.md`;
`#332` = `release: 0.5.0` is still an OPEN owner-held PR per the handoff §A). **Verify every
command/flag against the shipped 0.4.2 binary's `--help`, not the repo HEAD**, before a swarm
agent depends on it.

Primary sources read:
- `.agents/lessons/index.md` (index of 68 lesson dirs / 67 entries — drift noted in handoff §F)
- Individual `LESSON.md` files cited inline
- `.agents/active/fold-back/*` (esp. `iteration-close-gate-archived-mergeback-mismatch.yaml`)
- `.agents/proposals/*` + `~/.agents/proposals/*` (esp. `workflow-store-concurrency-safe-writes.md`)
- `.agents/active/handoffs/2026-07-11-dot-agents-state-from-payout-mega-session.md`
- `.github/copilot-instructions.md` (global agent rules — the attribution rule)

---

## RULE 0 (META) — Summaries don't preserve subagent work; artifacts do

**Rule.** Every swarm node's deliverable is a **file written to a known path**, never the
node's return text / chat summary. Wiring between nodes is by **artifact reference (path +
content-hash + a decision/constraint checklist)** — never retold prose. The orchestrator
reads the artifacts; it does not trust the summary.

**Why (grounded).**
- The lossy-relay failure is documented: a hand-back of retold prose *degraded the deliverable
  16→13* — "lossy summary relay drops non-reconstructable detail that reaches the deliverable
  (composition spec evidence sidecar v3 family-2: 16→13)"
  (`.agents/proposals/kg-ideate-phase2-fork-triage.md`; echoed in
  `.agents/proposals/kg-crg-aware-bundle-authoring.md` and
  `.agents/lessons/shipped-content-must-be-self-contained/LESSON.md`). The mandated fix:
  "The hand-back … MUST be **structured/pointer-based — the artifact path(s) + a
  constraint/decision checklist — NOT retold prose.**"
- Distilled analyses are persisted as files with the raw transcript pointer kept separately:
  "The full subagent transcript is available at … this proposal is the distilled,
  decision-shaped version." (`.agents/proposals/crg-dual-read-parity-surface-2026-05.md`).
- This whole investigation exists because the assignment says *"SUMMARIES / RETURN TEXT ARE
  DISCARDED. Your ONLY deliverable is the artifact file."* Same principle: the 2026-07-11
  handoff itself was authored precisely to pull tooling state out of two payout prose handoffs
  into a durable dot-agents artifact (handoff §0 preamble).
- Corollary from `.agents/lessons/worktree-isolation-defeats-status-tracking/LESSON.md`: a
  worker's *status* written into an isolated worktree "never reaches the main repo" — the
  scout re-dispatched one task **5×** because the update was invisible. Coordination state that
  isn't in a shared, orchestrator-readable location does not exist.

**How it constrains the swarm.**
- Topology: the DAG's edges are **files**, not messages. Each node declares `writes:` (exact
  output path) and `reads:` (upstream artifact paths). The orchestrator gates a downstream node
  on the *existence + freshness* of the upstream file, not on a returned summary.
- Agent briefs: every brief ends with "write your full result to `<path>`; your return message
  is discarded." Hand-offs carry `{artifact_path, inputs_digest, decision/constraint checklist}`.
- Freshness: reuse an upstream artifact by hash only when the digest matches AND no
  dependency-manifest entry changed; otherwise re-run the producer (the `inputs_digest` /
  `ComputeInputsDigest` reuse pattern, `kg-ideate-phase2-fork-triage.md`).

---

## RULE 1 — One governing sentinel per lifecycle stage; auto-clear on hand-off (the iteration-close-gate deadlock)

**Rule.** A swarm MUST NOT let two lifecycle owners hold overlapping active sentinels for the
same task. When lifecycle ownership moves (worker closeout → parent archive/cleanup), the
**upstream sentinel MUST be explicitly cleared** as part of the hand-off. Never leave a stale
`iteration-close` sentinel governing a task whose artifacts a later stage already moved.

**Why (grounded).** `.agents/active/fold-back/iteration-close-gate-archived-mergeback-mismatch.yaml`:
- The `iteration-close-gate` R1.1 check verifies `sentinel.expected_artifacts` **by active-path
  existence only**, reading the latest *active* iteration-close sentinel — "not canonical task
  state or delegation-closeout history."
- Reproduced on `t1-kg-brief-molecule`: worker sentinel expected
  `.agents/active/iteration-log/iter-67.yaml` and
  `.agents/active/merge-back/t1-kg-brief-molecule.md`. Then the **parent** ran
  `da workflow delegation closeout --decision accept`, which **archived the merge-back** to
  `.agents/history/kg-ideate-skill/delegate-merge-back-archive/2026-07-07/t1-kg-brief-molecule/`
  and **removed the active file**. The still-active sentinel kept requiring the old active path.
- Result: **recursive stop/pretool deadlock despite canonical task completion**, because
  `stop`/`subagent_stop` only sees the missing active artifact.
- Critical current-state fact: "**`workflow delegation closeout` does not auto-clear the matching
  iteration-close sentinel** … without an explicit clear the stale sentinel remains authoritative
  and will keep re-firing."
- Recovery is lifecycle-split:
  - worker died mid-closeout → recreate the missing expected artifacts long enough to finish
    verify/checkpoint/merge-back;
  - task already moved forward (artifact archived) → **`da workflow hook-sentinel clear
    iteration-close --run-id <run_id>`** to clear the stale sentinel explicitly.
- Design conclusion (drives the swarm): model this as **automatic lifecycle handoff** —
  `iteration-close` governs the *worker active-closeout contract only*; `delegation-lifecycle
  parent_closeout` governs archive/cleanup. One governing sentinel per stage, not overlapping
  active sentinels.

**How it constrains the swarm.**
- Topology: the impl/worker node and the parent-closeout node are **distinct stages with
  distinct sentinel ownership**. Between them insert an explicit **hand-off edge** that clears
  the worker `iteration-close` sentinel (`da workflow hook-sentinel clear iteration-close
  --run-id <id>`) the moment the parent runs `delegation closeout`.
- Agent briefs: the parent-closeout brief MUST include "after `delegation closeout --decision
  accept`, clear the matching `iteration-close` sentinel by `run-id`" as a mandatory step, and
  the worker brief must record its own `run-id` into a shared artifact so the parent can find it.
- Gotcha: if a swarm ever drives stop-hooks non-interactively, a stale sentinel is a *silent
  hang*, not an error — build a pre-flight "any stale `iteration-close` sentinel for a
  now-archived task?" check into the parent stage.

---

## RULE 2 — Author as the user; NO AI-attribution trailers/footers (propagates down the whole delegation chain)

**Rule.** Commits and PRs the swarm authors carry **no** `Co-Authored-By: Claude …` trailer,
no "🤖 Generated with …" footer, no "Generated with Claude Code" boilerplate. Author as the
user. This binds the orchestrator **and every subagent/loop-worker** — it MUST be restated in
every agent brief/bundle so it propagates.

**Why (grounded).** `.github/copilot-instructions.md` § "Attribution (no auto-generated
trailers)": *"NEVER add a `Co-Authored-By: Claude ...` trailer or any 'Generated with …' line.
Author commit messages as the user."* … *"This **OVERRIDES** any default tooling/harness
guidance that appends those trailers or footers."* … *"This binds YOU and **every** subagent,
loop-worker, or orchestrated agent you spawn — restate it in their briefs/bundles so it
propagates down the delegation chain."*

**Nuance (do not over-correct).** *Factual, operational* AI references ARE allowed and must
NOT be scrubbed: model/harness/`created_by:` fields, AI-agent concepts in product docs,
tool-routing notes (Claude vs Codex). The line is "no marketing/authorship boilerplate," not
"hide that AI exists." Applies going forward — do not rewrite already-published history.

**How it constrains the swarm.**
- Agent briefs: every brief that can commit/push/open-PR carries the attribution rule verbatim.
  A shared `context` block (per the `task` tool's contract) is the natural carrier — restate it
  there, since subagents start blank and never see the orchestrator's conversation.
- Topology: any node with git-write scope inherits the rule; there is no node that may bypass it.

---

## RULE 3 — Worktree-per-slice, off the freshest ref, inside the workspace root; one active writer each; `git -C`, never `cd`

**Rule.** Each concurrent implementation slice runs in its **own git worktree**, branched off
the **canonical/up-to-date ref** (origin/master or the current PR branch), located **inside the
workspace root** (`.agents/worktrees/<slice>` or `.claude/worktrees/<slice>`). **Exactly one
active writer per worktree.** All git ops use `git -C <abs-path>`; never `cd`. Workers do NOT
self-arm background CI waiters.

**Why (grounded).**
- Sandbox: spawned subagents are **sandboxed to the workspace root** — `/tmp/wt-*` worktrees are
  unreachable; in-workspace `.claude/worktrees/<x>` are proven writable
  (`.agents/lessons/subagent-out-of-workspace-access/LESSON.md`).
- VCS stamping: "Worktrees at sibling paths **outside** the repo root break `go build` VCS
  stamping in the pre-push gate — keep worktrees under the repo (e.g. `.agents/worktrees/`) or
  pass `-buildvcs=false` deliberately" (`.agents/lessons/worktree-sibling-path-buildvcs/LESSON.md`).
- One writer: a worker that armed its own background CI-waiter **resumed and edited the same
  worktree** while a fresh worker edited the same branch — two concurrent mutators, "classic
  half-applied-tangle / lost-edit hazard." Rule: "One active writer per worktree/branch … CI
  verification is the orchestrator's job (main thread polls and relays). A worker that self-arms
  a waiter stays alive and can collide." Next pass → **fresh worktree off the updated branch**,
  never a 2nd concurrent worker into the live one
  (`.agents/lessons/concurrent-workers-one-worktree/LESSON.md`).
- `cd` leaks: "The Bash tool's shell preserves `pwd` between calls. A single `cd <worktree> &&
  <one-shot>` silently moves the session … branches get created in the wrong worktree, commits
  land on the wrong branch." Use `git -C`; build/test that needs cwd uses a `(cd … && …)`
  subshell; `gh pr create` gets explicit `--head <branch>`
  (`.agents/lessons/worktree-no-cd/LESSON.md`).
- Hook drift: prek pre-commit hooks can land a worker's commit on a *sibling* worktree's branch
  via stash/restore — "verify branch HEAD matches worker's claimed commit post-push"
  (`.agents/lessons/parallel-worker-branch-drift/LESSON.md`).
- Freshest ref: assessing readiness / branching from a **stale local ref** caused a 3-way cascade
  (`.agents/lessons/stale-local-master-ref/LESSON.md`) and mass working-tree drift
  (`.agents/lessons/stale-local-checkout-mass-drift/LESSON.md`). `git fetch` and branch off the
  canonical ref.

**How it constrains the swarm.**
- Topology: parallelism unit = one worktree per slice, all under `<workspace>/.agents/worktrees/`.
  The DAG never assigns two live writers to one worktree; a "second pass" is a new node in a
  fresh worktree off the updated branch (or a message to the still-alive same worker).
- Agent briefs: every git command template uses `git -C "$WT"`; the brief forbids `cd` into the
  worktree and forbids arming background CI/monitor waiters. The orchestrator owns CI polling.
- Pre-flight: orchestrator `git fetch` + branch off canonical before spawning; post-push it
  verifies `git ls-remote` shows the worker's claimed SHA on the claimed branch.
- Env gotcha: if venv/coverage gates run in the pre-push hook, symlink the repo `.venv` into
  each new worktree first (`.agents/lessons/venv-symlink-into-worktrees-pre-adapter/LESSON.md`).

---

## RULE 4 — Owner-held merges; workers/verifiers reach READY, the owner merges

**Rule.** No swarm node merges a PR. A worker/verifier drives its PR to a **terminal READY**
state (all checks pass, gate OK, coverage ≥ threshold, 0 issues/hotspots) or to a **FOLD-BACK**
state, and **hands the merge decision to the owner gate**. The orchestrator only orchestrates
terminal states + reconciles status the moment it *observes* a merge.

**Why (grounded).**
- Handoff §0: "**push the coverage fix to green the gate, but leave the merge to the owner
  gate**"; §A: `#332` (release 0.5.0) and `#377` are explicitly **OPEN, owner-held**. There is
  a hard gate: "do NOT auto-merge #377 … autonomous multi-project orchestration stay paused
  until #377 merges + omp restarted."
- Worker terminal-state contract: worker returns "**ONLY** when PR is fully review-ready … OR
  hard-blocked and must fold back … Parent's role shifts to **terminal-only** orchestration"
  (`.agents/lessons/worker-owns-pr-readiness-loop/LESSON.md`; canonical successor for dot-agents
  is `verifier-owns-ci-watch-shift-left` where a `pr-ci` verifier profile owns the CI/Sonar
  loop).
- Merge ≠ auto-advance: "a merged PR does **NOT** auto-advance its task status; run the closeout
  the moment you observe the merge — `delegation closeout --decision accept` (delegated) /
  `advance --status completed` (direct), **NEVER hand-edit**" — and "**No PR-merge automation
  yet**" (`.agents/lessons/reconcile-task-status-on-pr-merge/LESSON.md`; daemon gap tracked in
  `.agents/proposals/pr-merge-auto-reconcile.md`).

**How it constrains the swarm.**
- Topology: the DAG terminates each slice at **READY** (a leaf artifact:
  `PR #<n> · ✅ READY · N/N checks · gate OK · new_cov X% · dup Y% · 0 issues`). A human/owner
  edge sits between READY and merge. A separate **post-merge reconcile** node fires *after the
  owner merges*, running `delegation closeout --decision accept` (delegated) — never
  `workflow advance` after delegated work (`verify-task-status-vs-pr-history/LESSON.md`).
- Agent briefs: workers/verifiers get "drive to READY or FOLD-BACK; never merge; report the
  terminal line." The reconcile node's brief: "on observed merge, closeout — never hand-edit
  status," and batch-check `gh pr list --state merged` vs task statuses to catch stale siblings.

---

## RULE 5 — Multi-target families need a shared core, not N parallel copies

**Rule.** When a plan produces a **family** of near-identical implementations across targets
(per-language generators/verifiers, per-platform adapters, per-backend drivers), the swarm MUST
scope it as **(1) build the shared core + adapter seam, then (2..N) add thin per-target
adapters** — never N independent tasks each reimplementing the shared shape.

**Why (grounded).** `.agents/lessons/multi-target-family-needs-shared-core/LESSON.md`: the r4
eval generator family (`generator-go`/`typescript`/`python` as **3 independent tasks**) copied
~15 scaffolding functions verbatim; the ts/py PRs hit
`new_duplicated_lines_density = 18.6%/18.7%` (threshold ≤3%; the ts file was 57.9% duplicated
vs the merged go file) and **could not merge**. The Coverage gate measures dup **against the
whole project**, so the FIRST target merges fine and *every subsequent target is blocked because
it duplicates the merged first one* — "N parallel copies" is literally un-mergeable past the
first. Owner ruling: "the duplication is a sign of bad design and impl." Fix = extract a shared
`gencore` engine parameterized by a `LanguageProfile`.

**How it constrains the swarm.**
- Topology: a target-parameterized fan-out is a **sequenced diamond**: core node (owns the
  `Profile`/`Adapter` seam) → then parallel thin-adapter nodes that `depends_on` the core. The
  adapter nodes may only start once the core lands (this is a genuine
  "B strictly requires A's output" sequence, not premature serialization).
- Agent briefs: the core node's *deliverable is the seam interface*; each adapter brief supplies
  only per-target specifics and is told "reuse `gencore`; do not re-scaffold." At PLAN/topology
  time the orchestrator counts target-parameterized tasks — if ≥2 share >~30% structure,
  restructure into core + adapters before fanout.
- Verification: adapter nodes must check `new_duplicated_lines_density` via the SonarCloud PR API
  (`/api/qualitygates/project_status?pullRequest=<n>`), not just per-OS Test jobs.

---

## RULE 6 — Classify generated/untracked files before any cleanup (corrected: managed-gitignore autofill supersedes manual gitignore)

**Rule.** Before any `git clean` / `reset --hard` on a "sync clean to master," the swarm MUST
**classify each untracked path by what it is**, not "not on master → junk." Snapshot first
(`git diff --binary > patch` + tar untracked). **CORRECTION for the current codebase:** the
lesson's "gitignore the live per-machine wiring by hand" step is superseded by the config-v2
**managed `.gitignore` auto-fill** — managed outputs are pruned/ignored by the resolver, not
hand-edited.

**Why (grounded).** `.agents/lessons/classify-generated-files-before-cleanup/LESSON.md`: three
kinds with opposite handling —
1. now-on-master duplicate → delete (reappears on checkout);
2. **durable authored artifact** in a master-tracked category (lessons, iteration-logs,
   **fold-back**, **research**, verification results) → **commit, never delete** — "losing a
   lesson breaks the self-improvement loop";
3. **live per-machine generated wiring** (hook manifests with absolute `$HOME` paths, lock
   files) → **neither delete nor archive**; `git clean` strips live wiring until the next
   `da refresh`. Root cause of the miss: `.github/hooks/*.json` were waved off as "junk" but were
   live `da refresh` hook wiring.

**Correction (per managed-gitignore auto-fill).** The lesson's remedy for kind 3 is "gitignore
it by hand." The current config-v2 model instead makes the resolver **own** the `.gitignore`:
`config-v2-coherence` success criteria include *"outputs exact/prune projection + `.gitignore`
auto-fill (§6.1)"* and *"the source of truth is the lock + commands, never leaked git state;
managed artifacts cannot accidentally be committed … Re-runs converge (the block is regenerated,
not appended)."* (`.agents/proposals/config-v2-coherence-scopes-sources-lock.md` §6/§7;
`.agents/history/config-v2-coherence/PLAN.yaml`). So: **let `da refresh`/`EnsureResolved`
regenerate managed wiring and manage its own `.gitignore`; do not hand-gitignore or hand-clean
managed outputs** — and `da run …`/managed flows are the intended path
(`verify-managed-file-target/LESSON.md`, `refresh-import-before-relink/LESSON.md`).

**How it constrains the swarm.**
- Topology: any "clean/sync workspace" node is a **guarded stage**: snapshot → classify → route
  (delete / commit / regenerate-via-`da refresh`) — never a blind `git clean` node.
- Agent briefs: a cleanup brief carries the 3-way classification checklist AND the correction:
  "for managed outputs, re-run `da refresh` rather than deleting or hand-gitignoring." It MUST
  commit any authored fold-back/lesson/research/iteration-log it finds (these are the swarm's own
  durable artifacts, per Rule 0).
- Gotcha: the swarm's *own* coordination artifacts (Rule 0 files) live in these tracked
  categories — a careless clean node would delete the swarm's memory. Protect
  `.agents/active/**`, `.agents/lessons/**`, `.agents/history/**`, `research/`.

---

## RULE 7 — Tests must drive the production path (green ≠ verified); cross-harness review is the reliable catch

**Rule.** A swarm node's "done" requires that its tests **drive the same entry point a real
caller uses** and assert the observable end state, then are **mutation-verified**. A green test
that hand-drives an intermediate step, compares a value to itself, or exercises a
callable-but-uncalled gate **certifies nothing** and MUST be rejected.

**Why (grounded).** `.agents/lessons/tests-must-drive-the-production-path/LESSON.md`: three forms
caught in ONE wave, all by cross-harness review, **none** by the implementer's own tests or
same-model self-review — (1) tautological comparison (input compared to itself via the same
`Snapshot()`), (2) lifecycle papered over (test manually called `ApplyDerivation` that the
production trigger never invoked — a real caller got zero rows), (3) green helper nothing calls
(`ValidateReadsFrom` well-tested but no load path invoked it). Rule: drive the public path;
compare independent readbacks; integration-test the gate at the real call site; **mutation-verify**
("If I deleted the production implementation and kept the test, would it fail?"). "Cross-harness
adversarial review is the reliable catch … Treat it as a **blocking** gate on substantive work."
The fidelity generalization: `.agents/lessons/prototype-experiment-fidelity-gate/LESSON.md`
("faithful inputs + real negative control + real execution + independent cross-harness audit").

**How it constrains the swarm.**
- Topology: the DAG places a **verifier node** downstream of every impl node, and for substantive
  changes a **cross-harness adversarial reviewer** as a *blocking* gate (a different model than
  the implementer — same-model self-review rubber-stamps this class). This maps directly onto the
  go-cli execution profile's `lens_set = [architecture-standards, acceptance-invariants,
  adversarial, cross-harness-adversarial]` with `lens_concurrency = gated` (handoff §B.2).
- Agent briefs: the verifier brief includes the mutation-verify check and the three anti-patterns;
  the impl brief must add coverage for behavior it introduces before the slice is "complete"
  (`tests-for-each-slice/LESSON.md`), and coverage must be *meaningful*, not line-executing.
- Gotcha: never allowlist past a coverage gate to make a slice "pass"
  (`no-lazy-allowlist-tech-debt/LESSON.md`); the auto-fix path is "add real tests," not edit
  `coverage-exceptions.txt`.

---

## RULE 8 — Single source of truth across specs/plans; siblings point, never restate

**Rule.** Each concern lives in **ONE canonical doc**; every sibling spec/plan that overlaps
**points to it** ("see `<canonical>#section`") and never re-states it. Tasks **reference** the
canonical spec — they never embed a drift-prone copy of a spec detail.

**Why (grounded).** `.agents/lessons/single-source-of-truth-across-specs-and-plans/LESSON.md`:
`config-v2-migration` and `config-v2-coherence` both described the same concern; one side was
refreshed, the other left stale; a task **embedded the stale copy**, a worker "implemented
faithfully against the stale copy, and burned a cycle building to a design that had already been
replaced." A stale embedded spec-detail "passes every schema-level eligibility check … invisible
until a worker has already implemented against it."

**How it constrains the swarm.**
- Topology: a **design/reconciliation node** runs before any fan-out to (a) verify each detail
  against actual code, (b) single-source duplicated concerns into pointers, (c) fix stale
  spec-details embedded in TASKS, (d) fold new ideas into the one canonical doc.
- Agent briefs: worker bundles carry a **pointer** to the canonical spec section, not an inline
  restatement. When updating notes, remember `da workflow task update --notes` **REPLACES** the
  field (`workflow-task-update-replaces-notes/LESSON.md`) — read current notes, keep valid
  context, prefix the dated rescope, edit only what changed.
- Reference discipline: artifacts reference code by **symbol/relational anchor, never line
  number** — line numbers drift the instant any sibling edit lands and a cold-start worker
  follows the stale reference (`symbol-only-references-in-artifacts/LESSON.md`).

---

## RULE 9 — Verify plan/task readiness against the CANONICAL ref before fanout (specs move; task notes don't)

**Rule.** Before the swarm answers "ready to implement?" / activates a wave / fans out, it MUST
(1) `git fetch` and read the spec on the **canonical ref** (origin/master + any merged
coherence/redesign PRs), and (2) **diff each task's notes against the spec sections they claim to
implement**, reconciling stale notes FIRST (dated `RESCOPED <date> (<section>)` prefix).

**Why (grounded).** `.agents/lessons/verify-plan-readiness-against-canonical-ref/LESSON.md`: a
merged redesign (PR #162 §2A/EventBus) **silently invalidated** the r3 task notes; a full wave was
fanned out against the stale notes, "every worker implemented faithfully — against a design that a
merged PR had already superseded," and the cross-brain gate **rejected the wave wholesale**.
"Worker fidelity *guarantees* wasted waves … The only cheap interception point is the readiness
check, and it is only valid against the canonical ref." Cheap mechanical check:
`git log -1 --format=%h -- <spec design.md>` vs `<plan TASKS.yaml>`; if the spec is newer,
reconcile. Sibling per-task check: HEAD-validate `write_scope` paths + task premise before fanout
(`validate-bundle-against-head/LESSON.md`); pre-flight `write_scope` via the code graph
(`bundle-scope-via-code-graph/LESSON.md`, `mcp__code-review-graph__file_summary`); cross-check
`gh pr list --state merged --search <task-id>` + dependent-task statuses
(`verify-task-status-vs-pr-history/LESSON.md`). Also: a plan's `status`/prose is a *claim*, not
ground truth — diff stated artifacts vs repo, check task-level vs header status, `git log
--grep=<plan-id>` for post-dated reconciliation (`stale-plan-status-vs-reality/LESSON.md`).

**How it constrains the swarm.**
- Topology: a **readiness-gate node** is mandatory upstream of every fan-out; the DAG does not
  spawn implementation nodes until it passes. When a coherence/redesign PR merges, every plan
  referencing that spec is "suspect until reconciled" — reconciliation is part of landing the
  redesign, not a follow-up.
- Agent briefs: the readiness-gate agent's brief is the freshness checklist above; it emits a
  reconciliation artifact (per Rule 0) that the fanout node reads.

---

## RULE 10 — `da` 0.4.2 shared-store writes are NOT concurrency-safe; serialize all board writes (dep/eligibility gating is unreliable under concurrency)

**Rule.** In `da` 0.4.2 the workflow shared-store writers are **not** mutually serialized, so
concurrent `da workflow advance` / `task update` / `delegation closeout` / `fanout` **silently
lose updates**. The swarm MUST funnel **all** canonical-store writes (TASKS.yaml / PLAN.yaml /
delegation contracts / bundles) through a **single serialized writer** (one orchestrator, or a
mutex node). Never run two `da workflow` writes concurrently in the same checkout; treat
`da workflow eligible` output as **stale/advisory** if any writer ran concurrently.

**Why (grounded).**
- Handoff §C: "**RMW lost-update race — FIX IS INCOMPLETE.** Confirmed: only
  `commands/workflow/plan_task.go` takes `agentslock.AcquireFileLock` … The other TASKS.yaml/
  PLAN.yaml writers **`delegation.go`, `contract.go`, `eligible_accounting.go` have zero lock
  usage** → concurrent `da workflow advance`/`task update` **still silently lose updates** there.
  **This is why board reconciles must be serialized.**"
- `~/.agents/proposals/workflow-store-concurrency-safe-writes.md`: "`da workflow` has **multiple
  independent write paths** … do unsynchronized read-modify-write, so one writer silently clobbers
  another's update (confirmed lost-update in board reconcile). PR #377 added `withTasksLock`
  **only** around `plan_task.go` … a lock on one of N writers does not serialize the file."
  Unguarded writers: `delegation.go` (`saveCanonicalPlan`, `saveDelegationContract`),
  `contract_core.go`, `eligible_accounting.go`, `saveDelegationBundle*`. Mitigation until fixed:
  "avoid running `da workflow` writes concurrently with an active worker in the same checkout —
  that is this very race."
- Compounding: worktree isolation defeats status tracking (Rule 0 /
  `worktree-isolation-defeats-status-tracking`) — an in-worktree status write is invisible to the
  main-repo reader anyway; so eligibility computed from a per-worktree TASKS.yaml is doubly
  unreliable.
- Dep-routing intent: "start X since Y …" means *make X eligible per dep routing*, not
  force-spawn; verify Y ≥ `awaiting_owner_review` before fanout, else queue `blocked-on:Y`
  (`dep-routing-on-partial-start-signals/LESSON.md`). The gating *policy* is sound, but the
  *store it reads* is racy in 0.4.2, so the swarm cannot trust eligibility under concurrent writes.
- UX gotchas (0.4.x): `eligible`/`next` **silently skip draft plans** (no auto-activate);
  `plan create` needs `--title` and the "next" hint prints a wrong flag shape
  (`.agents/active/fold-back/plan-activation-ux-friction.yaml`). Adjacent dry-run bugs:
  `plan update --dry-run` mutates PLAN.yaml; `start-task`/`foldback create` dry-run has
  side-effects (handoff §C/§E) — **do not trust `--dry-run` as side-effect-free** in 0.4.2.

**How it constrains the swarm.**
- Topology: there is **exactly one board-writer** in the DAG — the orchestrator (or a dedicated
  serialized "board" node). Worker/verifier nodes **request** status transitions via artifacts
  (Rule 0); they do **not** call `da workflow advance/closeout` concurrently. Fan-out waves are
  gated so no two `da workflow` writes overlap in one checkout.
- Agent briefs: workers are forbidden from writing the canonical store directly; they write their
  merge-back/verification artifacts and signal the orchestrator. The orchestrator reads
  `da workflow eligible --json` **only when quiescent** (no in-flight writer in the same checkout),
  and re-reads after every serialized write.
- Verification: after any board write, the orchestrator re-reads to confirm the transition landed
  (lost-update means "the write may have been clobbered"); batch-reconcile against
  `gh pr list --state merged`.

---

## RULE 11 — Merge-back artifacts require full `schema_version=1` frontmatter (silent closeout rejection otherwise)

**Rule.** Any merge-back artifact a swarm worker writes to
`.agents/active/merge-back/<task-id>.md` MUST carry the **full** `schema_version=1` frontmatter;
`files_changed:` MUST be `[]` (empty inline list) OR a block list. Otherwise
`da workflow delegation closeout` rejects it — **silently on first hit**.

**Why (grounded).** `.agents/lessons/merge-back-frontmatter-required/LESSON.md`: partial/absent
frontmatter → closeout "rejects the artifact with a parse error or a missing-field error, and the
failure mode is silent enough to be confusing on first hit (worker thinks closeout succeeded; it
didn't)." Required fields shown in the lesson: `schema_version`, `task_id`, `title`,
`summary`, `files_changed` (`[]` or block list), a result block (`status` + `summary`),
`integration_notes`, `created_at` (quoted RFC3339). Colon-space hazard → use block scalars
(`|-`) for text containing `: `.

**How it constrains the swarm.**
- Agent briefs: the worker's closeout brief carries the **exact frontmatter template**; the
  worker must not consider its slice done until the merge-back file validates.
- Topology: the parent-closeout node (Rule 1/Rule 4) reads the merge-back file; if closeout
  "succeeds" but the task didn't advance, suspect silent frontmatter rejection — verify the task
  status actually flipped (ties into Rule 10's re-read-after-write).
- Note: a delegation **contract** is a *separate* file from the **bundle**; nothing
  auto-materializes the contract from the bundle in 0.4.x — the worker/orchestrator may need to
  author it (`.agents/proposals/delegation-bundle-contract-divergence.md`).

---

## Swarm-relevant hooks — commands / flags / state-files a swarm agent invokes non-interactively

**Global non-interactive flags (all `da` commands, verified on 0.4.2 `--help`):**
`--json` (machine-readable output), `-y/--yes` (auto-confirm prompts), `-n/--dry-run`
(⚠️ **NOT side-effect-free in 0.4.2** — `plan update --dry-run` mutates PLAN.yaml;
`start-task`/`foldback create` dry-run have side-effects — handoff §C/§E),
`-f/--force`, `-v/--verbose`.

**Read/gate the board (advisory under concurrency — Rule 10):**
- `da --json workflow eligible [--plan <ids>] [--limit N]` — unblocked eligible tasks with
  conflict detection. Silently **skips draft plans** — activate first
  (`da workflow plan update --status active --plan <id>`, positional/flag shape is buggy per
  `plan-activation-ux-friction.yaml`). Read **only when the checkout is quiescent**.
- `da workflow next` — remediation-hinted next task (same draft-skip caveat; hint prints a wrong
  flag shape in 0.4.x).

**Serialized board writes (funnel through ONE writer — Rule 10):**
- `da workflow plan create <id> --title <t>` (`--title` is REQUIRED or it errors without naming it).
- `da workflow advance --status completed` — for **direct** (non-delegated) work only.
- `da workflow delegation closeout --plan <P> --task <T> --decision accept` — **delegated** work;
  auto-advances status; reads the `<task>.md` merge-back (Rule 11) + review-decision + contract.
  ⚠️ **Does NOT auto-clear the matching `iteration-close` sentinel** (Rule 1).
- `da workflow merge-back --plan <P> --task <T>` — writes `<taskID>.md` / flips contract to
  `completed` (one-file-per-task_id; lens accumulation not yet shipped — handoff §B.3).
- `da workflow fanout --task <T>` — materializes bundle; HEAD-validate write_scope + premise first
  (Rule 9); resolves `verifier_sequence` onto the contract.
- `da workflow plan archive --plan <id>` — bundles PLAN/TASKS/.plan.md + merge-backs into
  `history/<plan>/`. ⚠️ auto-commits **repo-wide** (sweeps unrelated dirty store paths — handoff
  §C); archive moves currently don't persist reliably (handoff §H "ARCHIVE-BLOCKED").

**Lifecycle sentinels (Rule 1):**
- `da workflow hook-sentinel clear iteration-close --run-id <run_id>` — **the** escape from the
  archived-merge-back deadlock; clear the stale worker sentinel after parent closeout.

**Execution/verification profiles the swarm drives (handoff §B):**
- `execution_profile.by_app_type.<appType>` (`internal/config/execution_profile.go`): `relevance`
  (per-stage core/situational/noise), `topology` (`executors`, `verifiers_per_executor`,
  `reviewers`, `verifier_sequence []slug`), `lenses` (`lens_set []slug`,
  `lens_concurrency parallel|gated|tiered`), `graph_backend`. Live go-cli profile:
  `lens_set = [architecture-standards, acceptance-invariants, adversarial,
  cross-harness-adversarial]`, `lens_concurrency = gated`.
- `stage_profiles.<stage>.<slug>` (`internal/config/agentsrc.go`): exactly `label`,
  `prompt_files []PromptFileRef`, `precondition_policy` — no `command`/`on_fail`/`verdict`.
- Verifier precondition gate: `config.ResolvePreconditionPolicy(projectPath, appType)`
  (`internal/config/precondition_resolve.go`) — resolves `verifier_sequence` slug →
  `stage_profiles.verifier.<slug>.precondition_policy` → top-level `precondition_policies`.
- Prompt resolution: `da workflow resolve-prompt --kind <executor|verifier|reviewer> --slug <slug>`.

**Config resolution / managed outputs (Rule 6):**
- `da refresh` (re-links managed outputs for the active platform; regenerates hook wiring +
  managed `.gitignore` — do not hand-clean/gitignore managed outputs).
- `da config explain [field-path] [--value-only|--origin-only] [--json]` (config-v2 surface).

**Coordination state-files the swarm reads/writes (edges, per Rule 0):**
- `.agents/active/delegation-bundles/<id>.yaml` (worker input) + `.agents/active/delegation/<id>.yaml`
  (contract/state — not auto-materialized from the bundle in 0.4.x).
- `.agents/active/merge-back/<task-id>.md` (Rule 11 frontmatter).
- `.agents/active/verification/<task_id>/review-decision.yaml`,
  `.agents/active/review/<task_id>-<lens>.md` (lens-scoped by filename; typed CLI artifacts are
  NOT lens-scoped in 0.4.x → N-lens collision — handoff §B.3).
- `.agents/active/iteration-log/iter-N.yaml` (canonical per-iteration record — do not duplicate
  as prose).
- `.agents/active/fold-back/*.yaml` (deferred observations; `classification` + `routed_to`).
- `PLAN.yaml` / `TASKS.yaml` / `<plan-id>.plan.md` (canonical plan triad).

**Top gotchas for a non-interactive swarm:**
1. `--dry-run` is NOT side-effect-free (0.4.2) — never rely on it to preview safely.
2. Shared-store writes race (Rule 10) — one serialized board writer; re-read after every write.
3. Worktree-isolated status writes are invisible to the main-repo reader (Rule 0/10) — coordinate
   via shared artifacts + open-PR query + an in-orchestrator dispatched-set, not per-worktree files.
4. `delegation closeout` does not clear the `iteration-close` sentinel → silent hang (Rule 1).
5. Silent merge-back frontmatter rejection (Rule 11).
6. Never auto-merge; owner holds the merge gate (Rule 4).
7. Draft plans are silently skipped by `eligible`/`next`; activate first.
8. Verify features against the shipped 0.4.2 binary, not repo HEAD (`da run` rename shipped 0.5.0;
   `release: 0.5.0` PR #332 still open).
9. No AI-attribution trailers/footers on any commit/PR any node authors (Rule 2).
