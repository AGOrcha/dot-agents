# Spec: Session-Handoff Journal — crash-survivable cross-session state recovery

Status: spec (design ratified 2026-06-22 — maintainer green-lit). Informed by three research
passes (existing carry-over map, empirical loss survey across dot-agents/payout/ResumeAgent
transcripts, Claude Code hook/compaction surface) + a Codex adversarial design review. Supersedes
the "prose-only" conclusion of `loop-discipline-stop-hooks/p3b` for the handoff case: that was
correct *given no evidence*; the empirical survey is the evidence.

## Problem & why

When an agent session **compacts** (context pressure), **ends**, or is **force-killed** (crash /
tech failure), its live working state evaporates. The next session expensively re-derives it. The
empirical survey (~28 compaction events) is unambiguous: **the auto-summary preserves the *why*
(intent, decisions) but drops the verifiable *live state*** — every compaction is immediately
followed by a burst of `git`/`gh`/`da workflow status` re-grounding. Ranked losses: task↔PR↔merge
state (most frequent; in one session it nearly re-fanned-out already-shipped PRs), branch/remote/HEAD
sync, env gotchas re-learned cold (the `~/Documents` TCC lock, serialize-pushes), in-flight worker
inventory, settled contract shapes, wave/gating intent. Two findings shape everything:

- **Stale artifacts are worse than missing ones** — a crash session's stale status/bundles *actively
  misled* the next session toward duplicate work.
- **Most of the lost state is deterministically queryable** (PR↔task via `gh`+`da`, branch/HEAD via
  `git`), so it can be captured *without agent reasoning*.

## Hard constraints (Claude Code hook surface)

- `PreCompact` fires before manual and auto/context-pressure compaction, receives the full
  uncompacted `transcript_path`, can run shell commands and write files that **survive** compaction —
  but **cannot invoke agent/Claude reasoning** (known, not-planned limitation).
- `Stop`/`SessionEnd` are observability-only and **do not fire on forced/crash termination**. Only
  compaction boundaries are "safe."
- ⇒ The reasoned layer must be **already on disk** when a kill lands. The only lever is **frequency**,
  not a flush-on-exit. The deterministic layer can be hook-driven.

## Decisions (ratified)

- **D1 — The spine is an append-only event log (the "journal"), not discipline-dependent prose.**
  Codex's #1 weakness: a reasoned overlay is missing exactly when it matters (crash mid-reasoning,
  before any trigger). Fix: every state-mutating `da` command **deterministically appends** a
  structured event. Recovery **replays recent events**; it does not trust remembered prose. This is
  crash-survivable (written synchronously per command) and discipline-free for the deterministic part.

- **D2 — Two writers, one log.** (a) **Deterministic** — `da` state-mutating commands auto-append
  typed events; a `PreCompact` hook appends a snapshot of queryable live-state. (b) **Reasoned** — the
  agent appends short deltas (mental model, in-flight decision + *why-now*, next step, pending user
  intent, active blocker) at adaptive cadence. The command-events subsume "write the overlay often"
  for state changes; the reasoned deltas carry only what a command can't.

- **D3 — Explicit command list + per-command typed schemas** (see §Command Surface). Each entry
  separates **`input`** (args/flags invoked) from **`observed`** (the durable delta — new status,
  created id, resulting HEAD, files). A generic blob is a weak journal; the typed `observed` is what
  lets replay reconstruct *what changed*.

- **D4 — Scope: workflow + KG + review; exclude config, hook-plumbing, score.** Config mutation
  (`da refresh` → the `.agentsrc.lock`) is **redundant with the deterministic snapshot** that re-reads
  the lock (which carries `inputs_digest` + per-layer `resolved_sha`/`cache_key`), so config is **not
  journaled** (recovery = "re-run `da refresh`," not "replay link writes"). Hook-sentinel/outcome are
  intra-turn gate plumbing/telemetry (own stores). Score sidecars are recomputable from iter-logs.

- **D5 — `PreCompact` writes the deterministic snapshot** (queryable live-state: task↔PR↔merge,
  branch+remote-identity+HEAD+open-PRs, worktree/worker inventory, active env gotchas). It cannot
  reason, so it captures only the queryable layer; the journal + reasoned deltas carry the rest.

- **D6 — Adaptive cadence for the reasoned layer.** Ranked preemptors override a dumb backstop:
  (1) **before risky/irreversible ops** (push, merge, `--force`, destructive, migrate — a `PreToolUse`
  hook-nudge forces the agent to refresh first), (2) **context-pressure**, (3) **after a decision /
  user-instruction / PR-open / task-advance** (self-discipline + folded into `advance`/`iteration-close`
  as `da` defaults), (4) **task/iteration boundaries**. Backstop: **work-OR-time** (~5–8 consequential
  tool-calls OR ~10 min) with a **hard ceiling** (never >1 un-recorded decision/intent), a
  **dirty-check no-op guard** (no write if nothing material changed), and **cheap append-only deltas**
  with rare full-compaction at boundaries/`PreCompact`. Young-Daly intuition (interval ≈ √(2·cost·MTBF)):
  catastrophic kills are rare ⇒ **moderate, not paranoid**; cheap deltas are what *let* us write often.

- **D7 — Read-back is a *verified recovery view*, not a raw handoff.** (Codex #2/#8.) On
  `agent-start`/orient, load the candidate journal+snapshot, **run each item's cheap re-verify
  command**, and inject only facts tagged `verified` / `changed` / `missing`, plus explicit deltas
  ("snapshot said PR#12 open; GitHub says merged"). **Never inject stale claims as facts.**
  "Distrust-then-verify" as a behavioral instruction is insufficient — verification happens
  mechanically at read time.

- **D8 — Composite-identity keying.** (Codex #3 — "wrong handoff is worse than none.") Key
  handoffs by `{repo-fingerprint, remote-url, default-branch, current-branch, HEAD, worktree-path,
  PR#}`. On read-back, **quarantine** identity mismatches; if several remain, choose newest-verified
  and surface older ones only as stale references.

- **D9 — Atomic writes, non-git-tracked path.** Every append/compaction is temp-then-rename (Codex
  #5: a kill can tear a mid-write file → partial writes become false authority). The log lives off the
  git-tracked tree (Codex #6: frequent writes else pollute the working tree / risk accidental commits).

- **D10 — Freshness-as-heartbeat unifies with stale-artifact hygiene.** The reasoned layer carries
  `last_reasoned_write`; recency **relative to the deterministic snapshot's timestamp** is the trust
  signal — the gap *is* the un-hooked loss window. One timestamp-delta drives both the trust label
  (fresh→trust / stale→hypothesis / orphaned→**quarantine the crash-bundle**) and the hygiene
  decision. No separate heartbeat plumbing for the handoff layer.

- **D11 — Honest limit (stated, not hidden).** Reasoning formed between the last durable append and a
  mid-turn SIGKILL is unrecoverable — the only thing that survives is file state already written. The
  per-command journal *minimizes* that window; that is why a journal beats chasing pure frequency.

- **D12 — The in_progress-task heartbeat is deferred to R2 observability (0.4.1).** It is a *different*
  concern: cross-session, fleet-level, consumed by a dashboard/broker (`r2-observability-dashboard`
  `t-push-broker`), lifecycle = monitor-a-running-fleet. The handoff freshness signal (D10) is
  per-session, local, consumed by next-orient, lifecycle = survive-a-death. They rhyme ("recency =
  aliveness") but must stay two timestamps with two owners. **MVP must not depend on the R2 heartbeat.**

- **D13 — The journal is the episodic-memory seam.** This append-only event stream *is* the "episodic"
  typed view from `[[knowledge-architecture-graph-views]]` (events/history/decisions). Building it now
  lays that seam — and ties into the obs/KG-projection direction (internal state as KG projections).

## Requirements

- **R1.** Every command in the journaled set (§Command Surface, Tiers 1–2 + KG + review) appends one
  typed event with the common envelope, `input`, and `observed`, written atomically, synchronously, to
  the non-git-tracked journal — on success only (a failed command appends a `failed` event or nothing,
  never a false `observed`).
- **R2.** A `PreCompact` hook appends a deterministic live-state snapshot (the queryable set) +
  timestamps it. Files survive compaction; a `SessionStart(matcher: compact)` hook re-injects it.
- **R3.** The reasoned overlay is append-only deltas at the D6 cadence, with the dirty-check guard and
  the hard ceiling; never written inside the git-tracked tree.
- **R4.** `agent-start`/orient produces a **verified recovery view** (D7): runs re-verify commands,
  injects only verified/changed/missing facts + deltas, applies the D10 trust gradient, and quarantines
  identity-mismatched or orphaned candidates (D8).
- **R5.** Config is **not** journaled (D4); the snapshot's lock re-read is the SoT for resolved config.
- **R6.** No new divergent source of truth: the journal references plan/task ids, PR#s, and the lock
  rather than re-storing their content (Codex #7). Tier-2 commands journal `{changed_fields}` only.

## Command Surface (the spine)

### Common envelope (every entry)
```yaml
ts:         <RFC3339 UTC ns>     # replay ordering key
seq:        <monotonic int>      # tiebreaker within a ts
actor:      <main|loop-worker|orchestrator>
command:    <canonical, e.g. "workflow advance">
cwd_repo:   <repo identity / path>   # the journal may span repos (sweep/drift)
event_type: <durable_delta|input_only|failed>
input:      { ...typed flags invoked... }
observed:   { ...typed durable delta... }
```

### Tier 1 — journal unconditionally (canonical transitions + irreversible FS moves)
`workflow advance`, `start-task`, `close-task`, `plan create`, `plan archive`, `merge-back`,
`delegation closeout`, `fanout`, `fold-back create/update`, `verify record`, `checkpoint`, `commit`,
`archive-orphans`, `sweep --apply`, `review approve/reject`.

### Tier 2 — journal the delta only (snapshot covers the full content)
`task add`, `task update`, `plan update`, `prefs set-local/set-shared` → `observed: {changed_fields}` +
intent; the full row is re-readable from TASKS.yaml/PLAN.yaml/prefs.

### KG — in scope, delta/decision framing (the warm store + code-graph are themselves snapshots)
- content delta: `kg ingest`, `kg link add/remove`, `kg maintain reweave|mark-stale|compact`, `kg warm`
  → counts + ids, not bodies.
- decision events: `kg build`, `kg update`, `kg postprocess` → "rebuilt at base X" + outcome, not
  node/edge dumps.
- `kg sync` → journal fully (moves a git remote; not locally snapshot-recoverable). Skip `kg setup`
  after first run.

### Excluded
- **config** (`config explain/verify/relevance` are read-only; `da refresh` → lock = snapshot-redundant
  per D4; optional belt-and-suspenders is a single thin `refresh {inputs_digest_before, after}` event,
  never the resolved layers).
- **hook-sentinel/outcome** (intra-turn gate plumbing / telemetry), **score** (recomputable).

### Representative per-command schemas
```yaml
advance:            input {plan,task,status,commit_state}; observed {from_status,to_status,committed,head_sha?}
start-task:         input {plan,task,seed_symbols[],seed_paths[]}; observed {to_status:in_progress,sidecar_path?,committed,head_sha?}
close-task:         input {plan,task,next_focus?}; observed {to_status:completed,next_focus_set,committed,head_sha?}
task add:           input {plan,task_id,title,depends_on[],write_scope[],app_type}; observed {appended:true}   # Tier 2
task update:        input {plan,task,changed_fields{}}; observed {fields_replaced[]}                            # Tier 2
plan create:        input {plan,title,owner}; observed {plan_dir,files_created[]}
plan archive:       input {plans[],force}; observed {archive_paths[],active_dirs_removed[]}
fanout:             input {plan,task,write_scope[],delegate_profile,base_branch?,verifier_sequence?}; observed {delegation_path,bundle_path,resolved_base_branch,resolved_write_scope[]}
merge-back:         input {task,summary,verification_status,commit_state}; observed {artifact_path,files_changed[],verdict,committed,head_sha?}
fold-back create:   input {plan,task?,observation,slug?,propose}; observed {artifact_id,routed_to[],action}
delegation closeout:input {plan,task,decision,note?}; observed {archived_paths[],reconciled_task_status}
verify record:      input {kind,status,scope,summary,task?}; observed {verification_log_id,result_artifact_path?}
checkpoint:         input {message,verification_status,log_to_iter?}; observed {checkpoint_id,iter_stub_path?}
commit:             input {includes[],dry_run}; observed {staged_paths[],head_sha,noop}
archive-orphans:    input {dry_run}; observed {actions:[{artifact,class,resolution,dest_path?}]}
sweep --apply:      input {stale_days,proposal_days}; observed {fixes_applied:[{project,action}]}
review approve/reject: input {proposal_id,reason?}; observed {decision,applied,archived_path,refresh_triggered}
kg ingest:          input {file?,all,type?}; observed {notes_created,notes_updated,note_ids[]}
kg build/update:    input {repo?,base?}; observed {outcome,nodes?,edges?,files?}
kg sync:            input {push}; observed {pull_status,push_status,head_sha?}
```

## Done criteria

1. A session that compacts and resumes shows **no `git`/`gh`/`da status` re-grounding burst** for
   state already in the verified recovery view (the §Problem failure is gone).
2. `agent-start` after a forced-kill loads the journal, **runs the re-verify commands**, and presents
   verified/changed/missing facts + the latest reasoned deltas — never a raw stale claim.
3. A stale/wrong-identity crash-bundle is **quarantined**, not auto-resumed (the near-re-fan-out
   failure is mechanically prevented).
4. Every Tier-1/Tier-2/KG/review command appends a typed event; config appends nothing; partial-write
   and concurrent-write are safe (atomic + interprocess-safe — see Relationship to `p4h`).
5. The journal write adds negligible latency/tokens (dirty-check guard + append-only deltas).

## Canonical orient & persist mechanics

The verified recovery view (D7/R4) reads the journal + snapshot; the broader
session-start **orient** surface and the boundary-written **checkpoint/session-log**
it complements (§Relationship) carry their own fixed data models, defined here so the
journal references them rather than re-storing their content (R6). Consolidated from the
retired docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md.

### Orient data model

`da workflow orient` and the session-start hook share one data source (also backing
`da workflow status`):

- `project`: `name`, `path`.
- `git`: `branch`, `sha`, `dirty_file_count`, `recent_commits` (up to 5 one-line summaries).
- `active_plans`: list of `path`, `title`, and up to 3 pending checklist items / leading summary lines.
- `checkpoint`: latest fields from `checkpoint.yaml`, or `null` if absent.
- `handoffs`: list of `path` + title.
- `lessons`: up to 10 recent entries from the first existing of (1) `.agents/lessons/index.md`, (2) `.agents/lessons.md`.
- `proposals`: `pending_count`.
- `next_action`: first non-empty of (1) `checkpoint.next_action`, (2) first pending checklist item of the first active plan, (3) `"Review active plan"`.
- `warnings`: non-fatal issues such as a missing lessons index, missing git repo, or unreadable checkpoint.

**Output formats.** The hook prints human-readable Markdown to stdout with sections for
Project, Active Plans, Last Checkpoint, Pending Handoffs, Recent Lessons, Pending
Proposals, and Next Action. `da workflow orient` prints the same Markdown by default;
`da workflow orient --json` emits the same canonical model as JSON.

**Behavior.** Missing optional artifacts render as empty sections, not errors; orient
never blocks session start; the MVP persists no separate `orient.yaml`.

### Checkpoint schema

`da workflow checkpoint` (session-end hook or manual) writes
`~/.agents/context/<project>/checkpoint.yaml`, where `<project>` is
`.agentsrc.json.project` when present, else the repo directory basename:

```yaml
schema_version: 1
timestamp: "2026-04-09T23:30:00Z"
project:
  name: "dot-agents"
  path: "/Users/nikashp/Documents/dot-agents"
git:
  branch: "feature/workflow-automation"
  sha: "abc1234"
  dirty_file_count: 2
files:
  modified:
    - "internal/platform/hooks.go"
    - "internal/platform/hooks_test.go"
message: "phase 3 complete"
verification:
  status: "pass"
  summary: "go test ./... passed"
next_action: "Implement proposal review command"
blockers: []
```

Field rules: `schema_version` is required and set to `1`; `timestamp` required, UTC
RFC3339; `project.name`/`project.path` required; `git.branch`/`git.sha`/`git.dirty_file_count`
required when git data is available, else `unknown`/`0`; `files.modified` required, may be
empty; `message` optional, defaults to `""`; `verification.status` required, one of
`pass|fail|partial|unknown`; `verification.summary` required, may be empty; `next_action`
required and concrete; `blockers` required, may be empty.

### Session-log format

Each checkpoint append writes one Markdown entry to
`~/.agents/context/<project>/session-log.md`:

```md
## 2026-04-09T23:30:00Z
branch: feature/workflow-automation
sha: abc1234
files: 2
verification: pass
message: phase 3 complete
next_action: Implement proposal review command
```

### Persist behavior

Session capture never blocks session end; if the checkpoint write fails the hook prints a
warning and exits 0. `da workflow checkpoint` may expose `--message` and `--verification`
flags, but the stored schema stays exactly as above. This artifact is distinct from the
journal's `checkpoint` event (§Command Surface), which records the command invocation and
its `observed` delta — not the stored file.

## Deferred / out of scope

- The cross-session **in_progress-task heartbeat** + push-broker → R2 / 0.4.1 (D12).
- A Playwright/visual layer over the verifier — unrelated.
- GitHub-OIDC-style non-interactive provisioning — unrelated (that's the dm6 minting thread).

## Relationship to other artifacts

- **Complements, does not duplicate:** plans/tasks (referenced by id), `checkpoint`/iter-log
  (boundary-written outcomes; the journal is per-command + reasoned-delta), memory (durable facts vs
  ephemeral live-state). No new divergent SoT (R6).
- **Depends on `config-v2-migration/p4h-agentslock-interprocess-lock`** for safe concurrent appends
  when the `PreCompact` hook runs `da` alongside the agent's `da` on shared state.
- **Episodic seam** of `[[knowledge-architecture-graph-views]]`; ties to the obs/KG-projection direction.
- Builds on the `agent-handoff` starter skill (completed in `loop-discipline-stop-hooks/p3b`), which
  becomes the home for the cadence rules + the verified-recovery-view read logic.

## Sequencing

Build **after the config-v2 0.4.0 cut** (it depends on `p4h` and benefits from the settled lock model).
A plan (`workflow/plans/session-handoff-journal/`) follows this spec when scheduled.

## Refinements from manual validation (2026-06-22)

Manually dogfooded the read side against a live coach-death recovery (a Codex session hit
its limit mid-finish-line; its p4g/p4h work was stranded in worktrees; the `~/Documents`
checkout was TCC-locked). The snapshot + overlay + verified-recovery-view worked and would
have eliminated the re-grounding storm. Three refinements fall out:

- **R7 — Re-verify sources MUST be robust to a locked/missing working tree — the criterion is
  the SOURCE, not the tool.** Refines D7. Recovery must work when the local checkout is
  unavailable — in the validation the TCC lock blocked *all* local filesystem access to the
  repo, so any recovery that read local `git` or on-disk files would itself fail. Prefer state
  queries backed by a **store/service** that is available independent of the working tree.
  Today only the remote/API qualifies (`gh pr list`, `gh api .../commits/master`,
  `gh api .../contents/<file>`); local `git` and `da` *reading the on-disk files* do NOT (they
  depend on the tree). This is **not** a permanent `gh`-over-`da` rule: by design, once the
  KG-projection end-state lands (`[[knowledge-architecture-graph-views]]` — the scoped KG store
  is the SoT and the git files are projections of it), `da` querying the KG store is
  store-backed and equally robust — a first-class, preferred re-verify source (it hits the SoT
  directly, not the projected files). The handoff's recovery-robustness and the KG-as-SoT
  architecture reinforce each other; until then, `gh`/remote is primary and local `git`/`da`
  is fallback.
- **R8 — Snapshot/journal MUST distinguish canonical state from in-flight-PR state.** Refines
  D1/D3. A task can be "done in an open PR, not yet merged" (the validation had p4g/p4h
  `pending` on master but reconciled in unmerged PR #90). Each tracked item carries its locus —
  `{canonical: master@sha}` vs `{in_open_pr: #N, status}` — so recovery doesn't treat
  in-PR work as either done-on-master or fresh-eligible.
- **R9 — The per-command journal (write side) is the non-optional, must-build piece.** The
  manual replica covered only the READ side (snapshot + overlay + recovery-view, point-in-time).
  The continuous, crash-survivable per-command append cannot be faked by hand; it is precisely
  what survives a mid-turn kill. Manual handoffs validate the recovery UX; they do not deliver
  the crash-survivability — that requires building the journal.
