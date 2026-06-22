---
title: Lessons Learned
description: Seven instructive lessons from the dot-agents corpus, told as short cautionary stories.
sidebar:
  order: 7
---

# dot-agents — Lessons Learned the Hard Way

> **Audience:** anyone asking *"but does this actually work in practice?"*
> **Read time:** 5 minutes. **Full lesson set:**
> [`.agents/lessons/index.md`](../.agents/lessons/index.md) — 34 entries and counting.

The dot-agents `.agents/lessons/` corpus is the project's working memory of
mistakes that wasted real time. Every lesson has the same shape: a pattern, a
root cause, a rule, and cross-references. Below are seven of the most
instructive, presented as short stories. Read them as evidence that the
operating model in [`LOOP_ORCHESTRATION_SPEC.md`](./LOOP_ORCHESTRATION_SPEC.md)
was built from scar tissue, not whiteboards.

---

## 1. The `cd` that swapped two branches

**Lesson:** [`worktree-no-cd`](../.agents/lessons/worktree-no-cd/LESSON.md)

During a session juggling three worktrees, a single
`cd "$SEAM" && go build` left the shell's `pwd` pointing inside the wrong
worktree. The next `git checkout -b crg-dsl-e-f` happened in the seam-di
worktree by accident — Edit/Write tools landed file changes in the *main*
worktree (right filesystem, right files), while the new branch existed in
*seam-di* (wrong git context). Untangling required cross-worktree file copies
and a branch reset.

**Rule:** for any session touching ≥2 worktrees, always
`git -C /abs/path <cmd>`. Build/test that genuinely need cwd inside a worktree
use a subshell: `(cd "$path" && go test ...)`. The parentheses prevent pwd
leak. `gh pr create` takes `--head <branch>` explicitly — always pass it.

**Why it matters for the demo:** the orchestration system can place workers in
isolated worktrees safely *only because* the rule is enforced. Without it,
parallel workers cross-wire.

---

## 2. The prek hook that put a commit on someone else's branch

**Lesson:** [`parallel-worker-branch-drift`](../.agents/lessons/parallel-worker-branch-drift/LESSON.md)

During Wave 7, the `p1b-canonical-when-values` worker's first commit landed
on `feature/t-archival-policy` — a sibling worker's branch. Recoverable via
cherry-pick + reset, no remote impact, but invisible without the right check.

**Root cause:** prek's pre-commit `git stash push` / `pop` cycle drifts onto
whichever branch the underlying shell's HEAD points at. With parallel workers
on the same filesystem, that HEAD migrates.

**Rule:** after every worker push, cheap cross-check the remote ref:
`gh api repos/<owner>/<repo>/git/refs/heads/<branch>`. Workers that report
"branch-switch incident" in closeout get spot-checked via
`gh pr view <n> --json commits`.

**Why it matters for the demo:** parallel agent dispatch is one of the
features. This is the kind of bug it produces when no one's watching, and the
kind of audit-loop the platform now has by default.

---

## 3. The stale TASKS.yaml that almost shipped a revert

**Lesson:** [`verify-task-status-vs-pr-history`](../.agents/lessons/verify-task-status-vs-pr-history/LESSON.md)

`da workflow eligible` reports tasks whose `status: pending|in_progress` in
TASKS.yaml. It does NOT cross-check whether the work already merged. In
session 151d7271, three tasks had merged PRs but stale status; the
orchestrator nearly spawned workers to re-do already-shipped work. The most
careful workers reported "PR already merged, byte-identical to master, no
action" after burning 5–10k tokens. A less careful worker could have pushed a
stale branch and reverted live code.

**Rule (pre-fanout, every task):**
```bash
gh pr list --state merged --search "<task-id>" --json number,title,mergedAt --limit 5
```
And: `delegation closeout --decision accept` auto-advances task status.
`workflow advance` is for direct (non-delegated) work only. Don't double up.

**Why it matters for the demo:** the canonical state is only as fresh as the
last closeout. The platform forces the orchestrator to reconcile against
ground truth (git, GitHub) before each fanout.

---

## 4. The bundle that promised more than it scoped

**Lesson:** [`validate-bundle-against-head`](../.agents/lessons/validate-bundle-against-head/LESSON.md)

After a git history rewrite, three loop-worker spawns were burned because
their delegation bundles referenced file paths and task premises that were
out of date against `HEAD`. The `write_scope` was written from a TASKS.yaml
snapshot that had decayed.

**Rule:** HEAD-validate every `write_scope` path AND the task premise before
`workflow fanout`. Snapshots decay between bundle authoring and worker
spawn, especially after history rewrites or parallel waves.

**Why it matters for the demo:** explicit `write_scope` is the platform's
safety primitive for bounded fanout. Lesson #4 is the discipline that keeps
the primitive truthful.

---

## 5. The const extraction that triggered Sonar CPD

**Lesson:** [`const-extraction-triggers-cpd-on-tables`](../.agents/lessons/const-extraction-triggers-cpd-on-tables/LESSON.md)

Fixing a Sonar `go:S1192` duplicated-literal warning by extracting consts in
`commands/explain.go` made S1192 disappear — and triggered the Copy/Paste
Detector instead. The literal had been in a tabular layout; extracting just
the differing string renamed "duplicated literal" into "duplicated block."

**Rule:** if the literal participates in a ≥3-row tabular layout, don't
extract a const — refactor the whole table to data-driven form (one loop over
a typed slice). Run BOTH the linter and a fresh Sonar PR analysis; the linter
catches S1192, only CPD catches block-level dup.

**Why it matters for the demo:** the `pr-ci` verifier profile catches this
class of regression automatically now. The lesson is what taught the verifier
the pattern.

---

## 6. The verifier that owns CI watch (shift-left)

**Lesson:** [`verifier-owns-ci-watch-shift-left`](../.agents/lessons/verifier-owns-ci-watch-shift-left/LESSON.md)

The earlier pattern, `worker-owns-pr-readiness-loop`, asked impl subagents to
do two things: write code AND babysit CI/Sonar. With 5–10 PRs per wave and
3–7 fix-up cycles per PR, each impl subagent was re-learning the SonarCloud
API and `gh pr checks` schema from scratch. Burnt context, slow waves.

**Rule (dot-agents project overlay only):** register a `pr-ci`
`verifier_profile` in `.agentsrc.json` alongside `unit`/`api`/`batch`. The
existing global `verifier` agent, dispatched with the `pr-ci.project.md`
overlay, owns the CI watch loop. Impl exits cleanly at merge-back. The
verifier auto-fixes mechanical issues (cog, dup, cov, allowlist) and escalates
the rest as a structured fix-up brief.

**Why it matters for the demo:** this is *the* canonical example of the
platform learning. A pattern emerged → a lesson captured it → the lesson
became config + a prompt overlay → next wave runs ~3-5x cheaper.

---

## 7. The starter vs project overlay boundary

**Lesson:** [`starter-vs-project-overlay`](../.agents/lessons/starter-vs-project-overlay/LESSON.md)

Early in the project, dot-agents-specific rules (use `/tmp/dev` binary, Sonar
gates, `gh` for CI checks) were drifting into the generic starter that other
consumers of `da` would receive. Other consumers will have different CI
surfaces (GitLab, Jenkins, none), different gates (no Sonar), different
binaries.

**Rule:** generic starter ships how-to-USE `da` (the product). The dot-agents
dev overlay holds how-to-DEVELOP dot-agents. The two never cross-contaminate.
Demo materials (this file, `DEMO_DIAGRAM.md`, `DEMO_README.md`,
`DEMO_WORKFLOW_WALKTHROUGH.md`) live in `docs/` — they're dot-agents-specific.
Generic operator docs and starter content live elsewhere.

**Why it matters for the demo:** the platform is multi-tenant by design.
Lesson #7 is what keeps that promise honest.

---

## The pattern behind the lessons

Read end-to-end, the corpus tells a consistent story:

- **Bounded operations.** Worktrees, write scopes, fanout slices — the system
  is built around explicit boundaries. Lessons #1, #2, #4 are about what
  happens when a boundary leaks.
- **Reconcile against ground truth.** TASKS.yaml drifts; PRs merge; git
  histories rewrite. Lessons #3 and #4 are about always re-grounding against
  `gh`, `git`, and `HEAD` before acting on stale state.
- **Specialize the agents.** General-purpose impl agents are bad at
  specialized verification loops. Lesson #6 is the canonical case study.
- **Capture the lesson, evolve the system.** Every lesson here became a rule,
  a check, a config, or a skill in the platform. The system gets durably
  smarter session over session.

For the full set, see [`.agents/lessons/index.md`](../.agents/lessons/index.md).
For *how* lessons become platform behavior, see
[`LOOP_ORCHESTRATION_SPEC.md`](./LOOP_ORCHESTRATION_SPEC.md) §"Fold-back +
proposal review".
