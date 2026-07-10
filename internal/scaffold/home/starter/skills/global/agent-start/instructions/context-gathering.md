# Context Gathering

How to gather context at the start of a session.

## Project Documentation

- Read `CLAUDE.md` at the project root — it often contains critical project-specific rules, conventions, and architecture notes
- Check for any pinned messages, session notes, or README updates

## Code Review Graph

- Check whether the current workspace has a `.code-review-graph/graph.db`
- If a graph is present, prefer graph or MCP tooling for exploration and impact analysis over broad manual scans
- Run `da kg health` for a quick knowledge-graph readiness check before relying on query results
- During active code changes, use the `code-review-graph watch` CLI command to keep the graph fresh as files change
- If the workspace contains submodules or important nested projects, check whether they maintain their own graph and refresh the relevant graph before relying on query results

## Active Work

- Check `.agents/active/` for in-progress plans from previous sessions
  - These are partially complete task plans with checkable items
  - Read them carefully — they tell you what was being worked on and how far it got
- Look for TODO comments in recently changed files (`git diff --name-only HEAD~5` then scan those files)

## Lessons Learned

- Read `.agents/lessons/` for relevant lessons from past corrections
  - Check `.agents/lessons.md` table of contents first for quick orientation
  - Focus on lessons relevant to the current project or task type
  - These represent mistakes already made — avoid repeating them

## Scope-Evidence Sidecar

If a specific plan task is known at session start, check for a scope-evidence sidecar:

```bash
cat .agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml
```

If the sidecar exists:
- Read `decision_locks` — these are constraints that must not be violated during implementation. Surface them prominently before coding begins.
- Read `required_reads` — these are files that must be read before implementing. Add them to your initial context-gathering reads.
- Note the `confidence` level — `high`/`medium` means the sidecar was derived from KG analysis; `low` means it's a rough approximation.

If no sidecar exists, note that scope evidence is absent. Consider running `da workflow plan derive-scope <plan_id> <task_id>` to generate one if the KG graph is available.

## Task Lists

- Check issue trackers, task boards, or any project management tools referenced in the codebase
- Review any open PRs that may need attention
