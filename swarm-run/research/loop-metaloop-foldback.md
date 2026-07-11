# Meta-loop + fold-back digest — how the loop PRODUCES refinement artifacts and folds them BACK into running work

**Scope.** How the profile-driven inner-loop swarm graduates from single-task to **full-loop
orchestration** by USING da's existing meta-loop / fold-back / proposal / review machinery — not
reinventing it. Grounds three specs (`meta-loop-operating-model`,
`loop-discipline-stop-hooks`, `orchestration-companion-stop-hooks`) against the shipped
`da workflow fold-back` + `da review` CLI surface and the scoring/iter-log feedback loop.

**Sibling digests — READ, do not re-derive:**
- `research/da-workflow.md` — the `da workflow` state machine (eligible/next/advance/fanout/
  merge-back/delegation-closeout), slots, N-plan driving, in-flight lifecycle.
- `research/da-run-review.md` — `da review` proposal surface + the two-track proposal/fold-back
  model (PARTS B/C/D). **This digest reuses its findings verbatim** for the review surface.
- `research/da-kg-score.md` — `da score`, the iter-log record, KG-as-SOT feedback substrate.
- `research/experiences-lessons.md` — RULE 1 (iteration-close-gate deadlock), RULE 10/11.

**Binary reality.** Installed `da` = **0.4.2**; repo `VERSION` = 0.4.2 but HEAD source has drifted
ahead (`da-kg-score.md:3`, `da-run-review.md:19-27`). The `fold-back` subtree
(`commands/workflow/cmd.go:950-1003`) and the `da review` proposal surface
(`commands/review.go`) are both **present in 0.4.2** (`da-run-review.md:27`); divergences are
flagged inline. Repo `HEAD` is the authority for source citations below.

---

## 1. The META-LOOP operating model — refinement produced AS the loop runs

### 1.1 Two loops over one substrate

`meta-loop-operating-model/design.md` states the operating model as **two loops over one
factory** (`design.md:35-55`):

| | WORK loop ("the what") | REFINEMENT loop ("the how we work") |
|---|---|---|
| Object | features/fixes/docs — shipped artifacts | agents, skills, prompts, schema, `stage_profiles`, lessons, rules, hooks |
| Trigger | a plan/spec task becomes eligible | an **observation**: a recurring defect, friction tax, verifier gap, ideation idea |
| Shape | implement → verify → review → merge | **dogfood → observe → refine** |
| Done | PR ships, task completes | the operating mechanism is changed and **re-dogfooded** |
| State home | working/episodic views | operational/semantic views (skills, `stage_profiles`, lessons) |

Cited: `design.md:39-45`. The principle: *"the refinement loop treats the work loop as its subject
under test"* (`design.md:47-49`). They are **separated, not isolated** — shared orchestrator, task-
state machine, verifier/reviewer chain, KG; separation is in *budget, cadence, bookkeeping*
(`design.md:51-55`). Mixing them ("while I'm here I'll also tweak the skill") is the named
anti-pattern (`design.md:53-55`).

### 1.2 The arc — how refinement artifacts are EMITTED

The loop's own running produces refinement inputs (`design.md:57-93` diagram). Two ideation
sources feed one queue (`design.md:95-101`):
- **Human ideation** → spec or plan task.
- **Agent ideation** — observations surfaced *while running the work loop*: a recurring defect, a
  friction tax, a verifier gap, a "this recurred a 2nd time → promote it" signal. These are **not
  free-text notes; they are candidate tasks** the orchestrator ingests (`design.md:98-101`).

The **orchestrator** (PR #134 pure-orchestration agent-type — deliberately **no `Edit`/`Write`**;
every state mutation routes through `da workflow`) is the cross-plan scheduler (`design.md:103-116`,
`104-105`). It: orients/reconciles across ALL plans (`da workflow eligible --json` over the whole
board, not one plan — `design.md:109-111`); **classifies** each ingested idea WORK|REFINEMENT and
routes to the matching `execution_profile` app_type (`design.md:112-114`); and **self-emits** new
ideation/refinement tasks it discovers while managing (`design.md:115-116`). Self-emission is a
`da workflow` mutation, keeping the task-state machine the single SOT (`design.md:217-218`).

### 1.3 Proposals (yaml, `da review` track) vs observations/fold-backs (obs-*.md / `fold-back --propose`)

**This is the two-track model — the single biggest gotcha** (`da-run-review.md:679-683`). The
meta-loop emits into BOTH tracks; they have different shapes, homes, consumers, and review paths:

| | **Track 1 — config proposals** | **Track 2 — fold-back observations** |
|---|---|---|
| Shape | `config.Proposal` YAML schema, `schema_version:1` (`da-run-review.md:574-601`) | markdown + YAML frontmatter (`foldBackProposalFrontmatter`, `types.go:281-287`; `da-run-review.md:702-713`) |
| Home | `~/.agents/proposals/<id>.yaml` (`da-run-review.md:686`) | `~/.agents/proposals/obs-<ts>.md` or `obs-<slug>.md` (`delegation.go:602-604`; `da-run-review.md:700-702`) |
| Producer | `da workflow prefs set-shared <k> <v>` (queues a pending `rule` proposal) or hand-authored (`da-run-review.md:669-675`) | `da workflow fold-back create … --propose` (`cmd.go:956-974`; `delegation.go:599-625`) |
| Consumer | **`da review` show/approve/reject** — the ONLY track `da review` sees (`da-run-review.md:687-693`) | **human-review** track; NOT in the `da review` queue (`da-run-review.md:716-719`) |
| Apply | approve = write file under `~/.agents/{rules,skills,hooks,settings}/…` + `RunRefresh` all projects + archive (`proposals.go:187-206`; `da-run-review.md:659-665`) | none automatic — a human triages the obs into a spec/plan/lesson or a formal `.yaml` later (`da-run-review.md:717-719`) |

**Human-review vs auto-apply split (grounded):**
- **Track 1 approve is human-gated and NOT dry-run-safe.** `runReviewApprove`/`runReviewReject`
  have no `-n` branch; `-n` is silently accepted but ignored → approve applies + refreshes +
  archives **for real** (`da-run-review.md:511-515,834-837`). A swarm must never fire approvals
  speculatively; gate them behind an explicit human/decision node.
- **Track 2 obs-*.md is inert** until a human graduates it. The `da-shebang-scriptability.md → recipe
  spec` is the canonical "proposal graduates into a spec+plan" example (`da-run-review.md:750-751`).
- **When each is emitted (from `meta-loop` OQ1 resolution, `design.md:209-218`):** default-b — the
  orchestrator self-emits a REFINEMENT task only on the **2nd occurrence** of a pattern.
  **1st occurrence → a fold-back observation into plan notes; 2nd occurrence → an emitted REFINEMENT
  task** (`design.md:214-217`). Auto-schedule (option c) is explicitly rejected for v1 to avoid
  "spends itself refining instead of shipping" (`design.md:216-217`).

### 1.4 `classification` + `routed_to` — the fold-back artifact schema

Every `fold-back create`/`update` writes a staging artifact `foldBackArtifact` at
`.agents/active/fold-back/<id>.yaml` (`types.go:153-163`):

```go
type foldBackArtifact struct {
    SchemaVersion  int    // schema_version
    ID             string // slug, or "fold-<ts>" when no --slug (delegation.go:677-687)
    PlanID         string // plan_id
    TaskID         string // task_id (empty = plan-scoped)
    Observation    string // observation
    Classification string // classification: "small" | "proposal"
    RoutedTo       string // routed_to
    CreatedAt      string // created_at (RFC3339)
}
```

**`classification` ∈ {`small`, `proposal`}** and **`routed_to`** together encode where the
observation landed. The dispatch (`delegation.go:652-665`, `dispatchFoldBackUpsert`):

| Invocation | classification | routed_to | Side effect (repo/home) |
|---|---|---|---|
| `create --task t1` (default) | `small` | `task_note:<plan>/<task>` | appends a bullet / tagged line to `TASKS.yaml` task `Notes` (`createSmallFoldBack`, `delegation.go:627-650`; test `foldback_test.go:52`) |
| `create` (no `--task`) | `small` | `plan_summary:<plan>` | appends to canonical plan `Summary`, bumps `UpdatedAt` (`updatePlanFoldBackSummary`, `delegation.go:552-560`; test `foldback_test.go:70`) |
| `create --propose` | `proposal` | `proposal:obs-<slug\|ts>.md` | writes `~/.agents/proposals/obs-*.md` (`createProposalFoldBack`, `delegation.go:599-625`; test `foldback_test.go:107`) |

Route format constants: `delegationTaskNoteRouteFmt = "task_note:%s/%s"`,
`delegationPlanSummaryRteFmt = "plan_summary:%s"`, `delegationProposalRoutePfx = "proposal:"`
(`delegation.go:110,113-114`). `proposalAbsPathFromRoutedTo` (`delegation.go:376-385`) resolves a
`proposal:` route back under `AgentsHome()/proposals/` **with `..` / slash traversal guards**.

So `classification` = *how big / which track* (inline note vs proposal-file); `routed_to` = *the
exact durable target*. `task_note`/`plan_summary` stay **in-repo** (canonical plan triad); `proposal`
crosses into the **global `~/.agents/`** human-review track (Track 2). Note: `task_note`/
`plan_summary` are always `classification: small`; only `--propose` yields `classification: proposal`.

### 1.5 `--slug` = idempotent create-or-update; refinement of an existing observation

- `fold-back create --slug <s>` writes a **tagged** note `- (fb:<slug>) <obs>` and re-running the
  same slug **replaces the tagged line in place** (dedupe — `setFoldBackTaggedNote`,
  `delegation.go:312`; test `delegation_fanout_test.go:1176-1195`). Untagged prior notes are
  preserved.
- `fold-back update --plan <p> --slug <s> --observation "…"` refines an existing slug-scoped
  artifact (`cmd.go:976-990`; `runWorkflowFoldBackUpdate` → `runWorkflowFoldBackUpsert(cmd, true)`,
  `delegation.go:435-437`). Guards (`validateFoldBackPriorAgreement`, `delegation.go:491-514`):
  plan must match; `--propose` rejected on update (`"--propose is not valid when updating an
  existing slug-scoped fold-back"`); task-scoped artifacts require the matching `--task`.
- A `small` artifact **cannot be upgraded to `--propose`** via update
  (`validatePriorFoldBack`, `delegation.go:667-674`): *"cannot use --propose for slug %q: existing
  artifact is inline (small)"*. Track choice is fixed at create time.

### 1.6 `fold-back list` — the read surface

`da workflow fold-back list [--plan <id>]` (`cmd.go:993-1002`; `runWorkflowFoldBackList`) reads
`.agents/active/fold-back/*.yaml` (`readFoldBackArtifacts`, `delegation.go:689-716`), optional plan
filter. Empty → `No fold-back observations` (text) / `[]` (`--json`)
(`delegation_test.go:17-32`). Journaling: create/update emit `journal.CmdFoldBackCreate` /
`CmdFoldBackUpdate` with a `FoldBackObserved` payload (`journal_emit_test.go:375-399`).

---

## 2. FOLD-BACK into RUNNING tasks/plans — non-terminal re-entry, not a sink

### 2.1 A fold-back created mid-run routes INTO existing work

The whole point of `routed_to` is that a mid-run observation **mutates a live plan/task the loop is
still driving**, rather than parking in a dead-letter:
- **`task_note:<plan>/<task>`** — `updateTaskFoldBackNote` (`delegation.go:533-540`) loads the
  canonical `TASKS.yaml`, finds the task by ID (`findTaskInPlan`, `delegation.go:520-531`), mutates
  its `Notes`, and re-saves. It **requires the task to exist** — a fold-back to a missing task is a
  hard error (`errTaskNotFoundInPlanShort`, `delegation.go:112`), and the dry-run path mirrors that
  precondition (`assertFoldBackTaskExists`, `delegation.go:547-550`). So a fold-back lands on a
  **real, in-flight row** of a running plan.
- **`plan_summary:<plan>`** — `updatePlanFoldBackSummary` (`delegation.go:552-560`) appends to the
  canonical plan `Summary` and bumps `UpdatedAt`, folding a plan-level note into the running plan.
- **`proposal:obs-*.md`** — escalates cross-cutting/`~/.agents`-scoped observations out of the plan
  into the human-review track (Track 2, §1.3).

Because the target is the **canonical plan triad** (`PLAN.yaml`/`TASKS.yaml`/`.plan.md`), the note
re-enters the active board the orchestrator reads via `da workflow eligible`/`orient` — it is picked
up on the next orchestration pass, **not** filed away. Per the operating model (OQ3 resolution,
`design.md:233-242`), the **work loop does not self-interrupt to refine**: a worker that hits a
recurring friction stays in its lane and **emits a fold-back**; the orchestrator — the only agent
seeing the cross-plan board — owns scheduling any resulting refinement out-of-band. Worker refining
mid-task = the forbidden "while I'm here" anti-pattern (`design.md:239-242`).

### 2.2 Routing selection by scope — the proposal-routing rule

`proposal-routing-rule.yaml` (`rules/dot-agents/proposal-routing.md`; `da-run-review.md:735-751`)
governs which target a fold-back should pick:
- **Loop observations → `active/fold-back/` then `workflow fold-back create`** — explicitly the
  fold-back path, distinct from specs/plans/lessons (`da-run-review.md:746-747`).
- Global (affects shared `~/.agents/`) → `--propose` (Track 2) → later a formal `.yaml` (Track 1).
- Project-local → inline `task_note`/`plan_summary` (stays in the repo).
- Decision heuristic: *"if this repo were removed from dot-agents management, would this proposal
  still matter?"* Yes → global; No → project-local (`da-run-review.md:743-744`).
- OQ4 scope expansion (`design.md:260-267`): scoped **proposals and lessons** run under the same
  non-code `docs`-composed refinement profile — the meta-loop can emit/refine scoped proposals and
  lessons on the same gate as plan/skill refinements.

### 2.3 Archival lifecycle

Fold-back artifacts are **durable authored artifacts** in a master-tracked category —
`experiences-lessons.md:266-268` RULE: *"commit, never delete — losing a lesson breaks the
self-improvement loop."* The `active/fold-back/*` set is one of the swarm's own memory categories
(`experiences-lessons.md:20,289-291`). `isDMAFile` treats `fold-back/` as a delegate-merge-back-
archive category (`fs.go:65-67`; `fs_test.go:58-60`).

Two archival paths:
- **Plan closeout** — `da workflow plan archive --plan <id>` bundles PLAN/TASKS/`.plan.md` +
  merge-backs into `history/<plan>/` (`experiences-lessons.md:496-498`). ⚠ It auto-commits
  **repo-wide** (sweeps unrelated dirty store paths) and archive moves currently don't persist
  reliably ("ARCHIVE-BLOCKED", `experiences-lessons.md:497-498`) — a swarm must not depend on it as a
  clean checkpoint.
- **Orphan sweep** — `archive_orphans.go` leaves a truly-orphaned task's active file in place and
  writes a fold-back (`classification:"small"`, `routed_to:"active/fold-back"`) so an operator can
  adjudicate (`archive_orphans.go:485-510,481-484`). `workflow drift`/`sweep` also flag stale pending
  Track-1 proposals (>30d, `flag_stale_proposals`, read-only, never deletes;
  `da-run-review.md:760-764`).

### 2.4 CONSTRAINT (already known) — the iteration-close-gate archived-mergeback deadlock

Recorded fold-back `.agents/active/fold-back/iteration-close-gate-archived-mergeback-mismatch.yaml`
(`experiences-lessons.md:67-110`, RULE 1). Sequence: a worker's `iteration-close` sentinel expects
active-path artifacts (`iter-N.yaml`, `active/merge-back/<task>.md`); the **parent** runs
`da workflow delegation closeout --decision accept`, which **archives the merge-back** to
`.agents/history/<plan>/delegate-merge-back-archive/<date>/<task>/` and **removes the active file**;
the still-active sentinel keeps requiring the old active path → **recursive stop/pretool deadlock
despite canonical task completion** (`experiences-lessons.md:80-85`). Critical fact:
**`workflow delegation closeout` does NOT auto-clear the matching `iteration-close` sentinel**
(`experiences-lessons.md:86-88,491`). Escape:
`da workflow hook-sentinel clear iteration-close --run-id <run_id>` (`experiences-lessons.md:92-93,
501-502`).

**Constraint on the pipeline (not to re-solve here):** when the gate stage archives a merge-back
via `delegation closeout`, it MUST clear the worker's stale `iteration-close` sentinel in the same
step (model as **automatic lifecycle handoff** — `iteration-close` governs the *worker active-
closeout contract only*; the parent stage owns cleanup, `experiences-lessons.md:94-97`). A fold-back
mid-run does not itself trip this — but any fold-back stage that co-occurs with parent closeout
inherits the constraint.

---

## 3. Meta-loop ↔ iteration record, lessons, and `da score` feedback

### 3.1 The iteration record (`iter-N.yaml`) is the feedback persistence step

`meta-loop` §3.5 (`design.md:136-144`): `iteration-close` persists the per-iteration record; under
KG-as-SOT a result is an **episodic node** with edges to the **operational + semantic** nodes that
produced it (the `stage_profiles` it ran under, the skills/rules/agents/hooks in its working set,
the spec/plan/task it implemented). Those edges turn the global `CLAUDE.md` self-improvement loop
"from memory into data" (`design.md:139-144`).

Grounded record shape (`da-kg-score.md:361-365`): `.agents/active/iteration-log/iter-N.yaml`,
written by **`da workflow checkpoint --log-to-iter N`** — the canonical per-iteration record
(`IterationRecord`, `iterlog.go:71-90`): iteration, wave, task_id, commit, files-changed, impl
block, verifiers[], review block. Loader matches strictly `^iter-\d+\.yaml$` so score sidecars are
not re-parsed (`da-kg-score.md:365`).

### 3.2 `da score` — outcome feedback the meta-loop is driven by

`da score` scores every **iteration** against the versioned rubric
(`docs/OUTCOME_SCORING_RUBRIC.md`; `da-kg-score.md:320-322`). Signals assembled from the iter-log +
git + transcripts (`da-kg-score.md:348-359`): `landed`, `verifier` (from iter-log), `tests`
(self-reported ⚠), `scope`, `correction_pressure`, `hook_outcomes`
(`iter-N.hook-outcomes.yaml`), `token_efficiency`, and (HEAD/3.0.0) `human_label`
(`iter-N.labels.yaml`). Combination = `weighted_mean_renormalized` over present signals; bands
excellent ≥0.85 / good ≥0.70 / fair ≥0.50 / poor (`da-kg-score.md:331-333`).

**The automatic feed from the loop:** `da workflow close-task` =
`checkpoint --log-to-iter N → score iteration N → advance → focus → commit`
(`close_task.go`; `da-kg-score.md:389,451`), emitting `score_value`/`score_band`. Sidecars:
`iter-N.score.yaml`, `session-<id>.score.yaml` (`da-kg-score.md:376-380`).

**Hook-outcome scoring is the hard "went-off-scope" signal** (`da-kg-score.md:367-374`): a gate that
had to **`remediate`** drives the `hook_outcomes` sub-score to **0.0**; `advise` → 0.6; all `allow`
→ 1.0. Only `intervention_class ∈ {prevent_before_action, remediate_at_stop}` vote. So the meta-loop
reads a low/remediated score as evidence a mechanism needs refining — closing the loop from
outcome → observation → refinement task (`design.md:141-144`; `da-kg-score.md:454`).

### 3.3 Lessons — the interim self-improvement substrate

Until KG-as-SOT lands (OQ6, `design.md:282-291`), the loop runs on the **interim** substrate:
`iteration-close` records + `.agents/lessons/<name>/LESSON.md` + `.agents/proposals/` files, with the
lessons/proposals → ideation path closing the loop manually (`design.md:284-289`). This matches the
global rule: *"After ANY correction… update `.agents/lessons/{name}/LESSON.md`"* (context
`copilot-instructions.md` §Self-Improvement Loop). Lessons are **explicitly not proposals** — a
distinct category from `fold-back` (`da-run-review.md:745-747`). The meta-loop's promotion trigger
("promote on 2nd occurrence") is exactly the lesson the `agent-ops-hardening` reflection cited
(`design.md:20-21,209-218`). KG upgrade turns these write-only files into queryable nodes — a strict
**enhancement**, not a precondition (`design.md:286-291`).

### 3.4 Stop-hook gates that PROVE the loop upheld its contract (why scores are trustworthy)

`loop-discipline-stop-hooks/design.md` and `orchestration-companion-stop-hooks/design.md` supply the
sentinel-backed gates whose outcomes feed `hook_outcomes` scoring:
- **Sentinel protocol** (D5, `loop-discipline…:120-134`): each skill writes
  `.agents/active/hook-sentinels/<skill>-<run-id>.json` declaring plan/task/agent_type/expected_
  artifacts; the Stop/SubagentStop gate validates before allowing stop; **no sentinel = no
  enforcement**. CLI: `da workflow hook-sentinel write|read|clear` (R4.1-R4.4,
  `loop-discipline…:289-302`).
- **Gates:** `iteration-close-gate` (verify→checkpoint→merge-back, R1), `isp-gate` (staged runtime,
  R2), `loop-worker-gate` (write_scope confinement, R3) (`loop-discipline…:200-286`). Two-tier:
  **hard** = platform-native remediation; **soft** = stderr advisory exit 0 (D1,
  `loop-discipline…:63-76`).
- **Companion gates** (`orchestration-companion…:119-155`): orchestrator handoff gate (fanout/
  existing-bundle handoff) and delegation-closeout gate (parent `accept`/`reject` before
  `workflow delegation closeout`). Wave-picker gets **no v1 hook** — a conversational recommendation
  cannot be truthfully blocked/scored (D3, `orchestration-companion…:156-166`).
- Outcomes emit the R1.5 vocabulary `allow|advise|remediate` (`orchestration-companion…:181-187`),
  written by **`da workflow hook-outcome write`** → `iter-N.hook-outcomes.yaml`
  (`da-kg-score.md:369`), which is exactly what `da score` reads (§3.2).

---

## 4. Pipeline integration

Target: extend `swarm-run/design/profile-driven.swarm.yaml` from a single-task pipeline
(`mode: pipeline`, `target_count: 3` bounded fold-back iterations) to full-loop orchestration by
**invoking da's machinery**, not reinventing it. The single-task DAG (profile_resolve → executor →
verify_1..3 → review_1..4 → gate) becomes one inner unit driven over N tasks/plans by an
orchestrator layer.

### 4.1 Which pipeline stage emits proposals/observations

- **`gate` stage** (currently the ready-gate + fold-back recorder + ref committer,
  `profile-driven.swarm.yaml:60-72`) is the natural emitter. It already writes `COORD/GATE.md
  FOLD-BACK + per-blocker reasons` on reject. Extend it so, on a confirmed cross-cutting/recurring
  signal, it **also durably records** the observation via CLI:
  - **In-plan / task-scoped (1st occurrence, default):**
    `da workflow fold-back create --plan <P> --task <T> --observation "<blocker>" --slug <stable-slug>`
    → `classification:small`, `routed_to:task_note:<P>/<T>` (mutates the running `TASKS.yaml` row;
    §1.4, §2.1). Re-runs with the same `--slug` dedupe in place (§1.5).
  - **Plan-level:** omit `--task` → `routed_to:plan_summary:<P>`.
  - **Cross-cutting / `~/.agents`-scoped (2nd occurrence per OQ1, `design.md:214-217`):**
    `da workflow fold-back create --plan <P> [--task <T>] --observation "…" --propose --slug <s>`
    → `classification:proposal`, `routed_to:proposal:obs-<s>.md` in `~/.agents/proposals/`
    (Track 2, human-review).
  - **Shared config change (a preference):** `da workflow prefs set-shared <k> <v>` → queues a
    pending Track-1 `.yaml` proposal for `da review` (`da-run-review.md:669-673,830-831`).
- **`executor` / `verify_*` / `review_*` stages** surface observations to `gate` via `COORD` signal
  files (they do **not** self-mutate the board — CONVENTIONS: "no board mutation by workers; Main
  reconciles", `profile-driven.swarm.yaml:11-13`). Refinement is scheduled out-of-band by the
  orchestrator (OQ3 no-self-interrupt, `design.md:233-242`) — a `review_*` lens that spots a
  recurring defect writes a reject reason; only `gate`/Main emits the durable fold-back.
- **New orchestrator layer (full-loop):** a Main/orchestrator node runs `orchestrator-session-start`
  discipline — `da workflow eligible --json` across ALL plans, classify WORK|REFINEMENT, drive the
  inner pipeline per eligible task (N-plan driving — see `research/da-workflow.md` /
  `SlotsNplanDoc`'s digest), and **self-emit** a REFINEMENT task on 2nd-occurrence via
  `da workflow` (no Write). It honors the in-flight cap (≤2 refinement tasks/wave default, OQ2,
  `design.md:220-231`).

### 4.2 How fold-backs created during a run are routed into the running plan set

- A `gate`-stage `fold-back create --task <T>` writes into the **canonical `TASKS.yaml` row** of a
  task the orchestrator is still driving (`task_note` route, §2.1). Because it mutates the canonical
  triad, the next orchestration pass sees it via `da workflow eligible`/`orient` — the observation
  **re-enters active work, it is not terminal** (§2.1).
- Board writes are **serialized through Main** (single writer per checkout; `experiences-lessons.md`
  RULE 10, :433-437): the pipeline emits the fold-back only from `gate`/Main while quiescent, and
  Main **re-reads `eligible` after every write** to confirm the transition landed. Workers never
  write the canonical store directly.
- `--propose` fold-backs and `prefs set-shared` proposals route OUT of the plan into
  `~/.agents/proposals/`; a **dedicated human-gated review node** (never speculative) runs
  `da review` / `da review show <id>` / `approve|reject <id> --reason "…"` — remembering approve is
  **not `-n`-safe** and triggers a global `RunRefresh` (§1.3; `da-run-review.md:834-837`).
- `da workflow orient` (`# Pending Proposals` count), workflow `health` (`N pending proposal(s)`),
  and `drift`/`sweep` (stale >30d) are the signals that tell the DAG **when** a review gate should
  run (`da-run-review.md:753-764,838-841`).

### 4.3 Exact `da workflow fold-back` / `review` commands + state files

**Emit (gate/Main, write-scoped):**
```
da workflow fold-back create --plan <P> --task <T> --observation "<obs>" [--slug <s>]      # small → task_note:<P>/<T>
da workflow fold-back create --plan <P> --observation "<obs>" [--slug <s>]                 # small → plan_summary:<P>
da workflow fold-back create --plan <P> [--task <T>] --observation "<obs>" --propose [--slug <s>]  # proposal → proposal:obs-<s|ts>.md
da workflow fold-back update --plan <P> --slug <s> [--task <T>] --observation "<refined>"  # refine existing (no --propose)
da workflow fold-back list [--plan <P>] [--json]                                            # read staging
da workflow prefs set-shared <key> <value>                                                  # queue Track-1 .yaml proposal
```
**Review (human-gated node only):**
```
da review                         # list pending Track-1 .yaml proposals (--json NOT honored — parse text)
da review show <id>               # print proposal YAML
da review approve <id>            # apply-to-~/.agents + RunRefresh + archive  (NOT dry-run-safe)
da review reject <id> --reason "" # archive as rejected
```
**Record + score outcomes (close of each inner iteration):**
```
da workflow checkpoint --log-to-iter N    # writes .agents/active/iteration-log/iter-N.yaml
da workflow hook-outcome write …          # writes iter-N.hook-outcomes.yaml (gate outcomes)
da workflow close-task …                  # checkpoint → score iteration N → advance → commit
da score iteration N --recompute --json   # read/refresh iter-N.score.yaml (parse PascalCase nested keys)
```
**Escape the known deadlock (§2.4), at parent closeout:**
```
da workflow hook-sentinel clear iteration-close --run-id <run_id>
```

**State files touched:**
- `.agents/active/fold-back/<id>.yaml` — fold-back staging artifact (`classification` + `routed_to`;
  `types.go:153-163`). **Commit, never delete** (`experiences-lessons.md:266-268`).
- `PLAN.yaml` / `TASKS.yaml` / `<plan-id>.plan.md` — canonical plan triad mutated by `task_note` /
  `plan_summary` routes.
- `~/.agents/proposals/<id>.yaml` (Track 1) and `~/.agents/proposals/obs-*.md` (Track 2);
  `archived/` on review.
- `.agents/active/iteration-log/iter-N.yaml` + `iter-N.hook-outcomes.yaml` + `iter-N.score.yaml` +
  `session-<id>.score.yaml` — the feedback record/scoring set.
- `.agents/active/hook-sentinels/<skill>-<run-id>.json` — gate sentinels (archived to
  `.agents/history/<plan>/hook-sentinels/<date>/`).
- `.agents/lessons/<name>/LESSON.md` — interim self-improvement corpus.

**Binary caveat for the swarm:** build from source (`go build -o <bin> ./cmd/da`) and point every
node at it — installed 0.4.2 lacks `da run` and the review-admin surface (`da-run-review.md:774-777`),
and several `da workflow` commands (incl. `fold-back create`) are recorded as **NOT `--dry-run`
side-effect-free in 0.4.2** (`experiences-lessons.md:424-426,474-475`; `da-run-review.md:729-733`).
Do not treat `-n` as a safe preview for fold-back or review on 0.4.2.
