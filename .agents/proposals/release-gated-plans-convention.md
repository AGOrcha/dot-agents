# Release-gated plans convention

**Created:** 2026-05-28
**Type:** project-local workflow convention (per `[[proposal-routing]]`). Candidate to graduate
into `~/.agents/rules/dot-agents/workflow-artifact-model.md` via `da review` once proven.
**Status:** adopted by maintainer 2026-05-28 (hybrid model).

## Problem

Releases were a separate, easily-forgotten step disconnected from the plan/task flow. A plan
would complete, its work would merge, and the version bump + release would lag (or never
happen) because nothing in the work queue scheduled it. We want releases to be **naturally
scheduled as work flows** — a first-class tail task on plans that merit a version, not an
out-of-band afterthought.

## The release mechanism (dot-agents specifics)

The repo releases via `VERSION` file + `.github/workflows/auto-release.yml`:

- `VERSION` (repo root) holds the current semver (e.g. `0.3.2`).
- On push to `master`, `auto-release.yml` reads `VERSION`, checks whether `v<VERSION>` already
  exists as a GitHub release, and if not: builds, tags `v<VERSION>`, signs (cosign keyless),
  and publishes the release + homebrew dual-cask formula.
- **Therefore: bumping the `VERSION` file on a merge to master IS the release trigger.**

A "release task" is consequently a concrete, bounded unit of work:
> write_scope: `VERSION` + `CHANGELOG.md` + any docs that drift per release (README, `docs/**/*.md`,
> `docs/web/**` site content); bump `VERSION` to the target semver, finalize the CHANGELOG section
> for that version, **run a docs-accuracy pass**; depends_on every other task in the plan.

When that task's PR merges, auto-release fires. No separate manual release step.

### Docs-accuracy pass (required step of EVERY release task — maintainer ruling 2026-05-28)

A release is the checkpoint where documentation must match shipped reality. Every release task
(patch, minor, major) MUST, before bumping VERSION, audit and update:

- `README.md` — command surface, install instructions, version references, feature list
- `docs/**/*.md` — any guide/reference that names commands, flags, file paths, or behavior that
  changed since the last release
- `docs/web/**` — the agorcha.dev site: canonical pages, demo/pitch content, schemas, command
  references, anything that asserts how `da` works
- Generated/embedded version strings, badges, screenshots that show stale output
- Any `*.md` claiming "current" behavior that the release's bundled work changed

The pass is not "regenerate everything" — it's "find what the shipped work made inaccurate and
fix it." The release task's write_scope therefore extends beyond `VERSION` + `CHANGELOG.md` to the
specific docs that drifted. Note in the task which docs were touched and why.

Rationale: docs drift silently between releases; tying the accuracy sweep to the release task means
a version never ships with docs that lie about the product.

## The hybrid model (maintainer ruling 2026-05-28)

Three release paths, chosen by what the work delivers:

### MINOR → plan tail
A **feature plan** (adds user-visible capability, new command, new surface) ends with a
`release-v<X.Y+1>.0` task. The bump is owned by the plan that delivers the feature.

Examples: `config-v2-migration` (new `da config explain` + resolver), `r3-background-worker-service`
(new `da service`), `r2-observability-dashboard`, `worktree-platform`.

### MAJOR → plan tail
A **breaking-change plan** (removes/renames a public surface, changes a contract consumers
depend on, alters default behavior incompatibly) ends with a `release-v<X+1>.0.0` task. The
plan that *delivers the break* owns the major bump. If multiple breaking changes are in flight,
they should be sequenced or bundled into one major-delivering plan to avoid version churn.

### PATCH → train (cadence)
Small fixes, internal refactors, coverage lifts, test hygiene, Sonar cleanups, doc-only
changes — these do NOT each own a version. They accumulate. A standalone, recurring
`release-patch-train` task bumps the patch digit (`0.3.2 → 0.3.3 → 0.3.4`) on a cadence,
bundling whatever merged since the last tag. Not tied to any one plan's completion.

**Infrastructure / distribution / docs work is PATCH, not MINOR (maintainer ruling 2026-05-28).**
New docs site, binary signing, homebrew packaging, CI pipeline changes, refactors — these are
distribution/infrastructure, not new `da` capability, so they ride the PATCH train even though
they're "new." MINOR is reserved for a genuine **feature land** (new `da` command/surface users
invoke). This is why v0.4.0 is NOT the accumulated-infra release — that work ships as a patch
(`0.3.3`), and `0.4.0` stays reserved for the next feature plan's minor tail. The
"next-minor-from-VERSION-at-ready-time" resolution makes this automatic: once `0.3.3` ships,
the next feature plan's `release-minor` tail computes `0.4.0`.

Internal-only plans (`root-command-decomposition`, `seam-interface-di-migration`,
`production-code-helper-extraction`, `coverage-*`, `sonarqube-pr10`,
`cross-platform-test-skips-audit`, etc.) feed the patch train; they get NO plan-tail release
task.

## How to wire a plan-tail release task

When creating or amending a version-worthy plan:

1. Add a final task `release-v<target>` with:
   - `app_type: release` (or `go-cli` if no release app-type exists yet)
   - `write_scope: VERSION, CHANGELOG.md`
   - `depends_on:` **every other task id in the plan** (the release is the last thing)
   - `verification_required: true`
   - notes: the semver rationale (why minor vs major) + the CHANGELOG section to finalize
2. The task's PR bumps `VERSION` and finalizes the CHANGELOG `## [X.Y.Z]` section.
3. On merge, `auto-release.yml` tags + publishes.

## Decision rule (which path?)

Ask of the plan's delivered work:

1. **Does it remove/rename/incompatibly-change a public surface or contract?** → MAJOR plan tail.
2. **Else, does it add a user-visible capability/command/surface?** → MINOR plan tail.
3. **Else (fix, refactor, internal, test, docs, coverage, lint)** → no plan tail; feeds the
   PATCH train.

"Public surface" = `da` CLI commands/flags, `.agentsrc.json` schema, generated config shapes,
skill/agent/hook contracts other projects consume.

**Under-the-hood swaps are NOT major (maintainer ruling 2026-05-28).** A plan that replaces an
internal implementation without changing observable behavior or any public surface is MINOR
(or patch), not MAJOR — even if it removes a whole subsystem. Example: `graph-backend-adapter-contract`
decommissions the Python CRG bridge in favor of a native backend; because `da` consumers see no
behavioral or contract change, this is a MINOR, not a MAJOR. MAJOR is reserved for changes that
*semantically* break what consumers depend on, not for large internal refactors.

## Version collision handling

Multiple feature plans may complete near the same time. To avoid two plans both claiming
`0.5.0`:

- The release task's actual target version is resolved **at task-ready time**, not at
  plan-creation time — read current `VERSION`, compute the next minor/major from it.
- If two feature plans' release tasks become ready simultaneously, they serialize at the
  VERSION bump (one merges `0.5.0`, the second rebases and becomes `0.6.0`). This is the
  same conflict-sequencing the orchestrator already applies to shared-file PRs.
- The release task notes should say "next minor from current VERSION" rather than hardcode
  a number, so a rebase recomputes cleanly.

## Relationship to other artifacts

- `[[workflow-artifact-model]]` — release tasks live in TASKS.yaml like any task; the
  permanent record lands in `history/<plan>/` on archive. This convention is the missing
  "and then ship it" tier between implementation and history.
- `[[dep-routing-on-partial-start-signals]]` — release tasks have the widest depends_on in
  the plan; dep-routing must hold them until all siblings reach awaiting_owner_review/merged.
- `CHANGELOG.md` — finalized per-version by the release task; the running "Unreleased" section
  accumulates entries as feature PRs merge (each feature PR adds its line).

## Immediate application (2026-05-28, revised)

- **v0.3.3 (patch)** is the imminent release — the accumulated infra/distribution/docs work
  (docs site, cosign signing, homebrew dual-cask, agorcha deploy, CI dedupe, refactors,
  platform-diagnostics). This is PATCH-grade per the ruling above, not a feature land. PR #145
  is re-versioned `0.4.0 → 0.3.3`: bump `VERSION` `0.3.2 → 0.3.3`, CHANGELOG section `## [0.3.3]`,
  + the required docs-accuracy pass. Its merge fires the 0.3.3 release.
- **v0.4.0 is RESERVED** for the next genuine feature land (config-v2 completing → `da config explain`
  + resolver; or r3 `da service`). The first feature plan to complete claims `0.4.0` via its
  `release-minor` tail, computed automatically from VERSION (`0.3.3` → next minor `0.4.0`).
- `da config explain` (#162) therefore ships in `0.4.0` with the config-v2 plan tail, NOT in 0.3.3.
- Active feature plans needing a plan-tail release task added: see the classification sweep
  (delegated) — `config-v2-migration`, `r3-background-worker-service`,
  `r2-observability-dashboard`, `r1-outcome-scoring`, `r4-code-task-generation-eval`,
  `r5-review-labeling-access`, `worktree-platform`, `graph-backend-adapter-contract`,
  `platform-driven-diagnostics` (folds into 0.4.0).
- Patch-train task to be created as a standalone recurring item.
