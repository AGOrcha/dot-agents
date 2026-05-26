# Proj Mega Lineage Salvage Audit

**Date:** 2026-05-25
**Compared:** `master`, `proj-mega-branch`, and
`feature/windows-da-init-fixes-for-demo-from-proj-mega-branch`

## Result

The named `proj-mega-branch` no longer contains branch-only `.agents/` content
relative to current `master`; it is an ancestor of the current branch history.
The recoverable omissions live on its later feature lineage.

Restored now:

- `.agents/active/active.loop.md`
- `.agents/active/orchestrator.loop.md`

These are runtime inputs still referenced by the loop scripts and the
orchestration specification. They are stable overlay definitions, not a
snapshot of current task progress. The restored overlay text was minimally
adapted so absent `loop-state.md` is optional and iteration history reads the
existing `iteration-log/iter-*.yaml` artifacts. Closeout examples were also
aligned to the current `--kind test` and `workflow advance --task --status`
command surface.

## Deliberately Not Restored

- `.agents/active/loop-state.md`, active handoffs, fold-backs, and historical
  active delegation files: these contain obsolete current-position state and
  would conflict with newer work on `master`.
- Branch-only active plan directories: `master` has newer canonical plans,
  including later hook, scoring, decomposition, and interface-DI work.
- Symlinked `.agents/skills/*` materialization: these are locally generated
  through refresh/install, not durable source files to cherry-pick.

## Recoverable Candidates For A Separate Pass

The descendant branch contains durable artifacts not present on `master` that
should be reviewed individually before import:

- `.agents/proposals/agent-context-resolution-architecture.md`: directly
  relevant to profile injection and task-dispatch context resolution.
- `.agents/proposals/config-explain-live-surface.md`
- `.agents/proposals/graph-backend-adapter-contract.md`
- `.agents/proposals/plan-archive-command.md`
- `.agents/proposals/scope-routed-da-review.md`
- `.agents/proposals/verify-record-review-direct-iteration.md`
- `.agents/proposals/workflow-app-types-discovery.md`
- `.agents/rules/dot-agents/proposal-routing.md`
- `.agents/workflow/graph-bridge.yaml`
- `.agents/sandbox/ttrpg-adapter/` as an optional example domain, not runtime
  state.

## Prompt/Profile Finding

The current specification already defines three layers: global worker profile,
project overlay, and per-delegation prompt. The staged script runtime does not
fully implement that contract: the `impl`, `verifier`, and `review` paths in
`bin/tests/ralph-worker` load role prompt files but omit the common profile and
project overlay that legacy mode includes.

Inspection of `~/.agents/profiles/loop-worker.md` shows it should be treated as
a donor for transformation rather than loaded unchanged into every staged
worker. Its bounded-work discipline and evidence metadata belong in a shared
stage base. Its architecture, acceptance, and adversarial review lenses
belong in separate named reviewer agent definitions. Its
`verify` / `checkpoint` / `merge-back` completion sequence is full-slice
worker behavior and conflicts with staged implementation and verifier workers,
which must write typed artifacts and stop.

There is also an unresolved parent-gate contract mismatch: the global profile
describes canonical advancement before delegation closeout, while the staged
ISP prompt specifies delegation closeout before advancement. That ordering
must be normalized before the transformed agent definitions are generated.

The corresponding implementation decision is tracked in
`.agents/active/proj-mega-salvage-and-profile-layering.plan.md`.

During validation, the installed `iteration-close` skill under
`~/.agents/skills/dot-agents/iteration-close/` was also found to retain
positional `workflow advance <plan> <task> completed` examples. That managed
resource is not changed in this salvage pass; it is tracked as follow-up work
in the active plan.
