# Delegation: Codex hooks + agents-linking gap — audit & #29 doc-review

- type: AUDIT/ANALYSIS + REVIEW. **Read-only. Make NO code or doc
  changes.** Two deliverables; do not fix anything.
- workspace: main checkout `/Users/nikashp/Documents/dot-agents`
  (read internal/platform, commands, the #29 diff). Use
  `cd /Users/nikashp/Documents/dot-agents`.
- status: delegated

## Context

The maintainer, reading `docs/PLATFORM_DIRS_DOCS.md` (as edited by the
docs-refresh agent on PR #29, branch `docs-refresh`), believes it
describes a real gap as if intended: **hooks are not being wired for
Codex**, and **agents are rendered (codex TOMLs) but not linked
properly** for Codex. Suspected genuine bug/gap, not by-design.

## Deliverable 1 — Gap audit & analysis

Investigate the platform rendering/linking path and establish ground
truth:

1. **Codex hooks.** Does dot-agents render and *wire/link* hooks for the
   Codex platform the way it does for Claude Code? Trace
   `internal/platform/` (codex.go, hooks.go, buckets.go,
   render_manifest.go, resource_plan.go, the link wiring in
   `internal/links/`) and the relevant `commands/` (hooks, refresh,
   install, sync). Identify exactly where Codex is included/excluded in
   hook rendering AND in hook *linking* (rendering without linking is
   the suspected gap).
2. **Codex agents.** Are agent definitions rendered into Codex config
   (codex TOMLs / AGENTS.md) AND linked into the right target paths?
   "We do render it, but they should be linked properly" — verify
   render vs link separately; pinpoint the broken/missing link step.
3. **Intended vs actual.** Decide: is this a genuine bug/gap, an
   intentional limitation, or a partial implementation? Cite code
   (`file:line`) and any spec (the recovered specs under
   `.agents/workflow/specs/` — esp. platform-session-integration,
   app-type-profiles, config-distribution-model — and
   `CANONICAL_HOOKS_DESIGN.md`) that states intended Codex behavior.
4. **Scope a remediation** (do NOT implement): what would correctly
   wire Codex hooks + link Codex agents; blast radius; which
   packages/tests; whether it warrants a spec→plan or is a bounded fix.

Write the analysis to `.agents/proposals/codex-hooks-agents-linking-gap.md`
(project-local markdown proposal per proposal-routing rules) — problem,
ground-truth findings with file:line evidence, intended-vs-actual
verdict, remediation options + recommended path, and whether it should
graduate to a spec/plan.

## Deliverable 2 — Review the #29 PLATFORM_DIRS_DOCS.md changes

Get the exact #29 edit: `git -C /Users/nikashp/Documents/dot-agents
fetch origin -q && git diff origin/master origin/docs-refresh --
docs/PLATFORM_DIRS_DOCS.md`. Assess against the ground truth from
Deliverable 1:

- Did the docs-refresh agent faithfully represent **actual code
  behavior**, or did it rewrite the doc to present the Codex-hooks /
  agents-linking gap as intended/correct (laundering a bug)?
- Flag every line that asserts as correct/by-design something that is
  actually a gap.
- Verdict: is #29's PLATFORM_DIRS_DOCS.md change SAFE TO MERGE as-is,
  must it be AMENDED (specify the exact corrected wording — but do NOT
  edit the file yourself; propose the text), or should that file's
  change be reverted from #29 pending the fix?

Append Deliverable 2 as a `## #29 doc-change review` section in the same
proposal file.

## Closeout

No commits, no PRs, no file edits except creating the one proposal
markdown. Final message: the intended-vs-actual verdict (1-2 lines),
the #29 merge recommendation (safe / amend-with-this-text / revert),
and whether a spec→plan is warranted.
