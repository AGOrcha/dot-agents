# release-docs-refresh as a mandatory auto-included release predecessor

**Created:** 2026-07-04
**Type:** project-local workflow convention amendment (per `[[proposal-routing]]`). Amends
`[[release-gated-plans-convention]]` (§"Docs-accuracy pass"). Project-local because it is specific
to this repo's release machinery (`VERSION` file + `auto-release.yml` + release-gated plan tails)
and to the in-repo `release-docs-refresh` skill — it would not matter if this repo were removed from
dot-agents management.
**Status:** proposed 2026-07-04.

## Problem

The `release-gated-plans-convention` already mandates a "Docs-accuracy pass (required step of EVERY
release task)" — audit README / `docs/**` / `docs/web/**` / `docs/PLATFORM_DIRS_DOCS.md` against the
shipped code before bumping `VERSION`. But that mandate lived only as prose *inside* the release
task's responsibilities. Nothing structurally forced it to run: an agent (or human) cutting a
release could bump `VERSION` and finalize the `CHANGELOG` without ever running the docs pass, and the
convention would be silently violated. The `release-docs-refresh` skill mechanizes the pass, but it
still had to be remembered and invoked by hand each cut.

Owner's ask: the docs refresh should be **"auto added whenever we have a release cut"** — a
structural predecessor of the version bump, for **every** cut (patch AND minor), never added
manually.

## Decision

`release-docs-refresh` is a **required predecessor task** of every version-bump task. It is its own
task (never folded into the bump), it declares `write_scope: docs, README.md, docs/web` (it NEVER
touches `VERSION` or `CHANGELOG.md`), and the bump task `depends_on` it — so the bump cannot run
until the docs pass completes.

1. **PATCH path.** The standing `release-patch-train` plan now carries a `release-docs-refresh` task
   with `blocks: [release-patch]`, and `release-patch` declares
   `depends_on: [release-docs-refresh]`. Both are recurring: reset to pending in lockstep after each
   cadence tick, so every patch cut re-runs the docs pass before the next bump.

2. **MINOR / MAJOR path.** Every plan-tail `release-v<target>` task MUST add a `release-docs-refresh`
   task and list it in `depends_on` alongside the plan's other tasks. The convention's "How to wire a
   plan-tail release task" recipe now includes this step, so newly authored feature-plan release
   tails inherit the predecessor automatically — minor cuts get the docs pass too, not just the patch
   train.

3. **Discipline.** Where the docs pass finds that the CODE breaks a documented contract/promise (not
   merely lagging docs), the `release-docs-refresh` skill classifies it and routes a tracked
   code-fix/proposal via the `promise-gap-analyst` (and platform-dir drift via the
   `platform-dirs-change-analyst`) — it does NOT paper over the gap by editing the doc.

## Where this is wired (canonical locations)

- `.agents/workflow/plans/release-patch-train/TASKS.yaml` — new `release-docs-refresh` task;
  `release-patch.depends_on: [release-docs-refresh]`.
- `.agents/workflow/plans/release-patch-train/PLAN.yaml` — `current_focus_task` repointed to
  `release-docs-refresh`; success_criteria / verification_strategy reference the pre-cut gate.
- `docs/RELEASE_VERIFICATION.md` — "Before cutting a release (maintainers)" section makes the
  `release-docs-refresh` step a REQUIRED pre-cut item and documents the minor-tail convention.
- `.agents/proposals/release-gated-plans-convention.md` — §"Docs-accuracy pass" mechanization
  amendment + §"How to wire a plan-tail release task" now requires the predecessor.

## Rationale

Tying the docs pass to a structural `depends_on` (rather than prose) means a version can never ship
with docs that lie about the product, and the safeguard cannot be forgotten — it is scheduled by the
work graph, the same way the release itself is scheduled by the `VERSION` bump firing
`auto-release.yml`.

## Relationship to other artifacts

- `[[release-gated-plans-convention]]` — this amends its §"Docs-accuracy pass" from a prose mandate
  into a structural predecessor task.
- `[[proposal-routing]]` — project-local markdown proposal (release mechanics are repo-specific).
- Candidate to graduate, together with the parent convention, into
  `~/.agents/rules/dot-agents/workflow-artifact-model.md` via `da review` once proven.
