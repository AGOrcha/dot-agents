# Common Failure Points at Session Start

Mistakes to avoid when starting a new session.

## Starting to code before understanding the full picture

- You do not have prior context. Spend time reading before writing.
- Rushing into code leads to rework when you discover constraints you missed.

## Not checking for existing in-progress work

- Previous sessions may have left partially complete plans in `.agents/active/`.
- Ignoring these means duplicating work or creating conflicts with earlier progress.

## Ignoring failing tests that were already failing

- If tests are already failing when you start, note which ones and why.
- Do not assume your changes caused them later — establish the baseline now.

## Missing partially complete plans

- `.agents/active/` plans may have detailed context about architectural decisions, edge cases considered, and the intended approach.
- Skipping these and re-planning from scratch wastes effort and may produce a worse plan.

## Not reading CLAUDE.md

- CLAUDE.md may contain project-specific rules that override default behavior.
- Missing these leads to corrections that could have been avoided entirely.
- It may also contain workflow requirements, testing conventions, or deployment notes.

## Querying the wrong graph

- Some repos keep the code review graph in a submodule or nested project instead of the top-level workspace.
- Before trusting graph or MCP query results, confirm you are querying the graph for the code you actually plan to touch.

## Trusting stale graph data

- Graph query results are only as good as the last refresh.
- During active code changes, use `code-review-graph watch` to keep the graph updated instead of waiting to refresh manually at the end.
- If the workspace, submodule, or nested project has changed recently, refresh the relevant graph before treating missing results as proof that something does not exist.

## Falling back to broad scans too early

- If a graph is available, prefer graph or MCP queries before doing broad manual scans across the codebase.
- Manual scans are still useful for verification, but starting with them wastes tokens and makes impact analysis less reliable.

## Using weak or overly literal queries

- A narrow query can miss renamed concepts, indirect relationships, or related symbols in adjacent modules.
- If initial graph results look incomplete, vary the query terms and check whether another relevant subdirectory or submodule has its own graph that also needs to be refreshed.

## Picking this skill over orchestrator-session-start (or vice versa)

- This skill is the generic, non-loop fallback. If the repo has `.agents/workflow/` and `active.loop.md`, use `orchestrator-session-start` instead — it includes this skill's context-gathering plus the required pre-flight/sentinel gates for loop-managed work.
