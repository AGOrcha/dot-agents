# Proposal: Unify history archive locations

**Status:** DECIDED + IMPLEMENTED 2026-06-08 — history unified at `.agents/history/` (single location). Was: draft (decision artifact).
**Created:** 2026-05-28
**Owner:** pr10-branch-split / history-archive-location-unify
**Cross-refs:** [[workflow-archive-orphan-sweep]], [[delegation-bundle-contract-divergence]], [[resource-model-current-state]]

---

## 1. Problem — two archive locations coexist

Two distinct directory shapes carry "archived delegation" artifacts under `.agents/history/`:

| Format | Path | What lives there | Schema |
|---|---|---|---|
| **Legacy / orchestration-sweep** | `.agents/history/archived-delegations/<date-bucket>/...` | Loose `<task>.md` and/or `del-<id>-<ts>.yaml` files at the date root; sometimes a per-task subdir; sometimes a `MANIFEST.md` | Ad hoc — added by hand during orchestrator commits that batch-archive a "wave" of completed delegations |
| **Canonical (current)** | `.agents/history/<plan>/delegate-merge-back-archive/<YYYY-MM-DD>/<task>/{delegation.yaml,merge-back.md,closeout.yaml}` | One subdir per task, three canonical files | Written by `da workflow delegation closeout` and `da workflow plan archive` (see `commands/workflow/delegation.go:1621` and `commands/workflow/fs.go`) |

The legacy location is **not produced by any current Go code path**:
`grep -rn 'archived-delegations' commands internal` returns zero matches.
It is purely a manual orchestrator convention from the period before
`delegation closeout` and `plan archive` were implemented.

Result: a reader landing in `.agents/history/` sees two parallel archive
hierarchies and must guess which one is authoritative for any given task.
The fold-back `backlog-hygiene-archived-plan-orphans.md` already shows downstream
agents getting confused about where merge-back artifacts belong.

---

## 2. Audit — counts and age range

Both formats were measured on 2026-05-28 against `origin/master` (HEAD `85374a79`).

### 2a. Legacy `.agents/history/archived-delegations/`

- **10** date-bucket directories: `2026-05-20`, `2026-05-23`, `2026-05-24`,
  `2026-05-25`, `2026-05-26`, `2026-05-26-w5`, `2026-05-26-w6`,
  `2026-05-26-w7w8`, `2026-05-26-w9`, `2026-05-26-w10`.
- Date range: **2026-05-20 → 2026-05-26** (7-day window).
- **101 leaf entries** at depth 2:
  - **64 loose files** (mixed `.md` merge-back narratives and `.yaml`
    delegation contracts, no consistent pairing).
  - **37 per-task subdirectories** (later dates started moving toward the
    `<task>/<files>` shape that the canonical format already uses).
- Plan attribution: implicit — derived from the delegation YAML's
  `plan_id` field; the on-disk path does not encode the parent plan.
- Originating commits (sample): `66669d17 chore(agents): archive 20 stale
  pre-#37 delegation contracts`, `cc605ea7 workflow(orchestration): archive
  Wave 9 delegations`, `47776c68 ... archive Wave 10 delegations` — all
  hand-curated batch commits, not CLI-driven.

### 2b. Canonical `.agents/history/<plan>/delegate-merge-back-archive/`

- **31 plans** carry a `delegate-merge-back-archive/` subtree.
- Date range: **2026-04-12 → 2026-05-28** (the format has been in
  continuous use the whole time the legacy bucket also accumulated).
- **137 per-task subdirectories** across those 31 plans.
- Each per-task subdir reliably contains `delegation.yaml`, `merge-back.md`,
  and (when closeout ran) `closeout.yaml`.
- Plan attribution: explicit in the path (`history/<plan>/...`).
- Writer: `commands/workflow/delegation.go` (closeout) and
  `commands/workflow/fs.go` (plan archive move).

### 2c. Key observation

The legacy bucket holds **artifacts from many distinct plans** intermixed by
date. The same plan's tasks often appear in **both** locations — e.g. tasks
from `seam-interface-di-migration`, `loop-agent-pipeline`, and
`r1-5-hook-enforcement-telemetry` show up under `archived-delegations/`
date buckets *and* under `history/<plan>/delegate-merge-back-archive/`. The
split is not "old plans vs new plans"; it is "what the orchestrator hand-swept
that day vs what the CLI archived."

---

## 3. Options

### Option A — Document the transition; keep both locations

- Add a note to `~/.agents/rules/dot-agents/workflow-artifact-model.md`
  (and the project mirror at `.agents/rules/...`) explaining that
  `archived-delegations/` is a legacy hand-curated bucket frozen at
  2026-05-26 and that all current archival flows through
  `history/<plan>/delegate-merge-back-archive/`.
- Add a `README.md` at `.agents/history/archived-delegations/` pointing
  forward to the canonical layout.
- Zero code changes. Zero file moves. Pure documentation.
- **Cost:** future readers still need to consult two places. Tooling that
  walks `history/<plan>/delegate-merge-back-archive/` (e.g. KG indexers,
  plan-archive reports) silently misses the legacy bucket forever.

### Option B — Migrate legacy entries into the canonical layout

For each leaf under `archived-delegations/<date>/`:

1. Read the delegation YAML to extract `plan_id` and `task_id`.
2. Move the entry to
   `.agents/history/<plan_id>/delegate-merge-back-archive/<date>/<task_id>/`,
   preserving the original `<date>` (so the historical clock is honoured).
3. Normalise file names: `del-*-<ts>.yaml` → `delegation.yaml`;
   `<task>.md` → `merge-back.md`. No `closeout.yaml` exists for these —
   leave that file absent (it's already optional in the canonical schema).
4. Where the legacy entry is a loose `.md` with no paired YAML (e.g.
   `MANIFEST.md`, audit notes), move it to the parent plan's
   `delegate-merge-back-archive/<date>/_orchestration-notes/` so it stays
   reachable but doesn't pollute the per-task layout.
5. After all entries are migrated, delete `.agents/history/archived-delegations/`.

- **Cost:** ~100 files to move with `git mv`, plus rename normalisation.
  Some legacy entries lack a YAML and the `plan_id` has to be inferred from
  the `.md` body — manual disambiguation for ~10-20 files.
- **Benefit:** one canonical archive shape. KG indexers, plan-archive
  tooling, and `da workflow merge-back archive` (the proposed command in
  [[workflow-archive-orphan-sweep]]) only ever have to know one path.

### Option C — Hybrid (rejected up front)

Document AND partially migrate (e.g. only the per-task subdir entries).
Rejected because it produces a third state — "some legacy entries moved,
some not" — which is strictly worse than either A or B.

---

## 4. Recommendation

**Option B — migrate legacy entries into the canonical layout, then remove
`archived-delegations/`.**

Rationale:

1. **No active code path writes to the legacy location.** Migration is a
   one-time `git mv` sweep; there is nothing to refactor and no risk of
   regression from a writer that would re-create the old shape.
2. **The cross-referenced [[workflow-archive-orphan-sweep]] task is about
   to introduce a new CLI command (`da workflow merge-back archive`) that
   resolves where to put orphaned merge-backs.** That command will key off
   the canonical `history/<plan>/delegate-merge-back-archive/<date>/<task>/`
   path. Leaving the legacy bucket in place forces the orphan-sweep design
   to either ignore it (creating a permanent blind spot) or carry a
   compatibility branch (carrying tech debt forward into new code).
3. **Plan attribution is path-encoded in the canonical layout but only
   YAML-encoded in the legacy layout.** Consumers like the
   `completed-plan-audit-analysis` spec already link directly into
   `history/<plan>/delegate-merge-back-archive/` paths — they cannot easily
   walk the legacy bucket without parsing every YAML.
4. **The volume is small enough to do manually with confidence** (101 leaf
   entries, ~7-day window, all in git history so any move can be inspected
   and rolled back).
5. **Option A's "freeze and document" is a deferral, not a decision.** It
   would still leave the next agent who lands in `.agents/history/`
   wondering which directory is canonical.

---

## 5. Migration plan (if B is approved)

This proposal does not perform the migration itself — its scope is
`.agents/proposals/history-archive-location-unify.md` only. The following
follow-up tasks should be filed against `pr10-branch-split` (or a successor
plan) once the recommendation is locked.

### Follow-up tasks to file

1. **`history-archive-migrate-legacy-entries`** — execute the `git mv` sweep
   for the 101 leaf entries. Write-scope: `.agents/history/`. Verification:
   `find .agents/history/archived-delegations -type f` returns empty;
   `find .agents/history -path '*/delegate-merge-back-archive/2026-05-*'`
   shows the migrated entries under their parent plans.

2. **`history-archive-normalize-filenames`** — within the migrated entries,
   rename `del-*-<ts>.yaml` → `delegation.yaml` and `<task>.md` →
   `merge-back.md` so they match the canonical schema. May be folded into
   task 1 if the worker is comfortable doing both in one pass.

3. **`history-archive-disambiguate-orphan-md`** — manually triage the
   loose `.md` files in legacy buckets that have no paired YAML
   (`MANIFEST.md`, audit notes, no-op closeouts). Each needs a parent-plan
   decision: route to `<plan>/delegate-merge-back-archive/<date>/_orchestration-notes/`,
   or — if truly orphaned with no plan attribution — to
   `.agents/history/_legacy-orchestration-notes/<date>/`.

4. **`history-archive-remove-archived-delegations-dir`** — after tasks 1-3
   leave the directory empty, `rmdir` it and grep-verify zero remaining
   references in `commands/`, `internal/`, `.agents/rules/`, and
   `.agents/skills/`. The hand-curated `MANIFEST.md` files for each date
   bucket should be subsumed by task 3 first.

5. **`workflow-artifact-model-doc-canonical-archive`** — append a short
   section to `~/.agents/rules/dot-agents/workflow-artifact-model.md`
   stating that `history/<plan>/delegate-merge-back-archive/<date>/<task>/`
   is the only valid archive location for delegated-task artifacts, and
   that `archived-delegations/` no longer exists. This makes the
   decision discoverable by future agents reading the rule set at
   session start.

### Sequencing

Tasks 1 and 2 can run as one delegation (single write scope, no
dependencies). Task 3 depends on task 1 (entries already moved). Task 4
depends on 1-3. Task 5 is independent of 1-4 but should land in the same
PR or immediately after so docs match disk reality. The full sequence is
expected to fit in a single small follow-up PR.

### Risk and rollback

- All operations are `git mv` / `git rm` of files already committed. Full
  rollback is `git revert <PR-merge-commit>`.
- No production code is touched. CI impact is limited to any test that
  globs `.agents/history/...` — none exist today (`grep -rn
  'archived-delegations' commands internal` is empty).
- The legacy bucket has been read by zero CLI commands during its 7-day
  lifetime, so no consumer breaks.

### Out of scope for this proposal

- Building `da workflow merge-back archive` (covered by
  [[workflow-archive-orphan-sweep]]).
- Re-validating the contents of each archived merge-back (covered by
  `completed-plan-audit-analysis`).
- Backfilling missing `closeout.yaml` files for legacy entries — they were
  never produced and reconstructing them adds no signal.
