# Recommendation — fold-back-resolution-lifecycle

**TARGET decision: MERGE-into-`agent-ops-hardening`** (active plan).

Do NOT create a competing spec+plan. The `agent-ops-hardening` plan already OWNS this
concern and has already absorbed its sibling:

- `.agents/workflow/plans/agent-ops-hardening/PLAN.yaml` summary carries
  `(fb:plan-archive-foldback-sweep) MERGED 2026-07-13 into proposal obs-fold-back-resolution-lifecycle`
  — the lifecycle is the PRIMARY path; the sweep is retained only as a bad-state safety net.
- The routed fold-back already lives under this plan:
  `.agents/history/agent-ops-hardening/fold-backs/fold-back-resolution-lifecycle.yaml`
  (`classification: proposal`, `routed_to: proposal:obs-fold-back-resolution-lifecycle.md`,
  `resolution: CONSUMED 2026-07-13`).

The gap is that the plan tracks the *routing* of this idea but has no TASKS.yaml task for the
actual `da workflow fold-back resolve|reject` verb. Merge the drafted tasks below into
`.agents/workflow/plans/agent-ops-hardening/TASKS.yaml` (parent serializes the canonical write;
do NOT run `da workflow` mutations from this ideation pass).

---

## Phase 1 — KG Briefing (grounding)

### KG Briefing: fold-back resolve/reject lifecycle
Generated: 2026-07-22

**Prior Decisions (2 found)**
- `PLAN.yaml (fb:plan-archive-foldback-sweep)` — the resolve/reject *lifecycle* is the PRIMARY
  archiver; `plan archive` is demoted to a bad-state reconciliation safety net; deferred
  fold-backs are NEVER swept.
- Archived fold-back YAML `path_note` — normalize history path to **plural** `fold-backs/`
  (routing convention + all existing archives use plural), not the singular `fold-back/` the raw
  observation prescribes. Implement the CLI against the plural path.

**Research Findings (0 relevant)** — [none] (mechanical CLI concern, no outside evidence needed).

**Contradictions (1 found)**
- Path spelling: raw observation says `.agents/history/<plan>/fold-back/<id>.yaml` (singular);
  the merge note + live archives use plural `fold-backs/`. RESOLVED in favor of plural; the
  archive path must READ both spellings for orphan detection and do a one-time rename of the
  singular dirs. `.agents/history/` currently holds BOTH (singular under `config-v2-coherence`,
  `kg-command-surface-readiness`, `plan-archive-command`, `loop-agent-pipeline`; plural elsewhere).

**Applicable Lessons (3 found)**
- `foldback-update-clobbers-proposal-body` — `fold-back update` on a proposal-routed fold-back
  REWRITES the destination `~/.agents/proposals/obs-<slug>.md` body. The resolve verb MUST NOT
  touch the proposal body; it only stamps status/resolution on the staging artifact and archives
  it. (Recover a clobbered proposal from the staging artifact's add-commit.)
- `workflow-task-update-replaces-notes` — replace-by-default clobber family; a resolution stamp
  must be additive, never overwrite existing observation/notes.
- `cli-update-verbs-destructive-replace` (sibling backlog lead) — same replace footgun; align the
  resolve verb's write semantics with the additive fix.

**Gaps (2 identified)**
- The `foldBackArtifact` struct has no `status`/`resolution` field, so the sweep hand-edited the
  archived YAML — the model must gain these before a verb can set them.
- No lifecycle state for "deferred" (open, waiting on a named trigger) fold-backs, which the sweep
  must be able to distinguish from terminal ones.

**Prior Spec / Plan Overlap**
- `agent-ops-hardening` (active) — OWNS this. `plan-archive-foldback-sweep` (archived prior art) —
  already merged into this lead.

**Impact Radius (seeds write-scopes)**
- `commands/workflow/types.go` — `foldBackArtifact` struct (add `status`, `resolution`, optional
  `resolved_at`/`trigger`).
- `commands/workflow/delegation.go` — `newWorkflowFoldBackCmd`, `foldBackDir`,
  `foldBackArtifactFile`, `readFoldBackArtifacts`, `renderFoldBackList`, `loadFoldBackArtifactByID`.
- `commands/workflow/cmd.go` — subcommand registration (`foldBackCmd.AddCommand`).
- `commands/workflow/fs.go` — `isDMAFile` dead `"fold-back"` path segment (remove).
- `commands/workflow/plan_task.go` — `archiveSinglePlan` (bad-state reconciliation pass).
- `commands/workflow/drift*.go` — drift/health checks (flag terminal-but-active + orphaned-terminal).

---

## Current CLI ground truth (verified 2026-07-22)

- `da workflow fold-back` subcommands: `create`, `update`, `list` only. No `resolve`/`reject`.
- `foldBackArtifact` (`commands/workflow/types.go`): `schema_version, id, plan_id, task_id,
  observation, classification, routed_to, created_at`. No `status`, no `resolution` — the archived
  YAML's `resolution:`/`path_note:` were added by hand during the sweep (the exact defect).
- Classifications & routes (`createProposalFoldBack`/`createSmallFoldBack`,
  `planFoldBackRouting`): `proposal` → `~/.agents/proposals/obs-<slug|ts>.md`
  (`routed_to: proposal:...`); `small` → task note (`routed_to: note:<plan>/<task>`) or plan
  summary (`routed_to: plan-summary:<plan>`).
- Active fold-backs live at `.agents/active/fold-back/<id>.yaml` (`foldBackDir`,
  `foldBackArtifactFile`); terminal archives at `.agents/history/<plan>/fold-backs/[<task>/]<id>.yaml`.
- `isDMAFile` (`commands/workflow/fs.go`) still lists `"fold-back"` in its path-segment switch —
  dead/unreachable leftover to remove.

---

## Drafted tasks to MERGE into `agent-ops-hardening` TASKS.yaml

Dependency order: `p6a` → (`p6b`, `p6c`) ; `p6d` depends on `p6a`+`p6b`. Suggested `app_type: go-cli`.

### p6a — Add status + resolution to the fold-back artifact model
- **write_scope:** `commands/workflow/types.go`, `commands/workflow/delegation.go`
- **change:** Extend `foldBackArtifact` with `status` (`pending` | `deferred` | `consumed` |
  `rejected`) and `resolution` (free-text evidence), plus `resolved_at` and an optional `trigger`
  (for deferred). Absent `status` on load normalizes to `pending` (v1 on-disk artifacts keep
  historical semantics — additive-state-fields lesson). `readFoldBackArtifacts`/`renderFoldBackList`
  surface a STATUS column. Terminal = `consumed`|`rejected`; active = `pending`|`deferred`.
- **acceptance:** Round-trip test proves a legacy artifact (no `status:`) loads as `pending` and a
  written artifact re-reads with its status/resolution intact; `fold-back list` renders the new
  column. No existing create/update/list behavior changes.

### p6b — `da workflow fold-back resolve` / `reject` verb
- **write_scope:** `commands/workflow/delegation.go`, `commands/workflow/cmd.go`
- **change:** Add `resolve` and `reject` subcommands under `newWorkflowFoldBackCmd` taking
  `--plan`, `--id` (fold-back id/slug), `--resolution <text>`, and honoring global `-n/--dry-run`
  (mirror `foldBackDryRun`). Behavior: (1) load the artifact by id (`loadFoldBackArtifactByID`);
  (2) VERIFY the durable route actually landed — for `classification: proposal`, the
  `~/.agents/proposals/obs-*.md` target from `routed_to` EXISTS; for `small`, the tagged
  observation is present in the task note / plan summary; (3) set `status=consumed`
  (resolve) or `status=rejected` (reject) + `resolution` + `resolved_at`; (4) ARCHIVE by MOVING
  the artifact from `.agents/active/fold-back/<id>.yaml` to
  `.agents/history/<plan>/fold-backs/[<task>/]<id>.yaml` (plural), creating the plan history dir if
  needed; (5) append a typed journal event. HARD CONSTRAINT (lesson
  `foldback-update-clobbers-proposal-body`): resolve/reject NEVER rewrites the proposal file body —
  it only stamps the staging artifact and moves it. The write is ADDITIVE (never clobber
  observation).
- **acceptance:** A test drives `resolve` on a proposal-routed fold-back: artifact moves to
  `history/<plan>/fold-backs/<id>.yaml` with `status: consumed` + resolution, the ACTIVE copy is
  gone, and the proposal body is byte-identical (unchanged). A `reject` test lands `status:
  rejected`. Resolving a fold-back whose route did NOT land errors out (non-zero) without moving.
  `--dry-run` previews the move + status without writing.

### p6c — Drift/health flags terminal-but-active and orphaned-terminal fold-backs
- **write_scope:** `commands/workflow/` (the drift/health command file, e.g. `drift.go`)
- **change:** `da workflow drift` (health) flags (a) a fold-back with terminal status
  (`consumed`/`rejected`) still sitting in `.agents/active/fold-back/`, and (b) an orphaned
  terminal record on an already-archived plan. `deferred` fold-backs carrying a live trigger are
  reported HEALTHY, never flagged.
- **acceptance:** Test fixtures: a terminal-but-active fold-back is flagged; a `deferred`-with-
  trigger fold-back is NOT flagged; a clean `pending` fold-back is NOT flagged. (If the repo tracks
  a lint/drift `ChecksRun` count assertion, bump it — lesson `lint-check-count-assertion`.)

### p6d — Plan-archive bad-state reconciliation + remove dead isDMAFile fold-back path
- **write_scope:** `commands/workflow/plan_task.go`, `commands/workflow/fs.go`
- **change:** (1) In `archiveSinglePlan`, run a bad-state RECONCILIATION pass (NOT a blanket
  archive): move only TERMINAL (`consumed`/`rejected`) fold-backs left unarchived into
  `history/<plan>/fold-backs/`. It MUST NOT touch `deferred` (open, waiting-on-trigger) fold-backs
  — surface those for re-homing/carry-forward instead. Read BOTH `fold-back/` (singular) and
  `fold-backs/` (plural) spellings for orphan detection, and do a one-time rename of singular
  history dirs to plural. (2) Remove the dead `"fold-back"` case from `isDMAFile`'s path-segment
  switch in `fs.go` (unreachable leftover).
- **acceptance:** Test proves plan archive moves a terminal-unarchived fold-back but leaves a
  `deferred` one in place (surfaced, not archived); a singular `fold-back/` history dir is renamed
  to plural. An `isDMAFile` test confirms the `"fold-back"` segment removal doesn't regress the
  other DMA path skips (`delegation`, `merge-back`, `verification`,
  `delegate-merge-back-archive`).

---

## Notes for the parent
- The routed proposal `obs-fold-back-resolution-lifecycle.md` remains the durable design record;
  these tasks operationalize it. Once merged, mark `(fb:fold-back-resolution-lifecycle)` in the
  plan summary as scheduled.
- No AI-attribution trailers in any commit/PR authored from these tasks.
