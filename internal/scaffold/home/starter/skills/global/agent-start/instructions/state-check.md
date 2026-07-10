# Technical State Assessment

How to assess the technical state of the codebase at session start.

## Git State

- `git status` — check for uncommitted changes, untracked files, merge conflicts
- `git log --oneline -10` — review recent commits for context on what was done
- `git branch` — confirm which branch you are on and whether it is correct for the task
- `git stash list` — check for stashed work that may need to be restored

## Workflow State (if this is a `da`-managed project)

- Run `da workflow orient` for a session-readiness summary (active plans, current focus task, drift warnings)
- Run `da kg health` for knowledge-graph readiness if the project uses the code review graph

## Build and Test Health

- Run the project's build command to check for compilation errors
- Run the test suite (or a targeted subset) to see if anything is currently failing
- Check CI status on the current branch if applicable

## Branch State

- Is the branch up to date with the base branch (usually main)?
- Are there upstream changes that need to be pulled or rebased?
- Is this a feature branch with an open PR? Check PR comments for feedback

## Uncommitted Changes

- If there are uncommitted changes, understand what they are before touching anything
- They may be intentional work-in-progress from a previous session
- Check `.agents/active/` plans for context on why changes were left uncommitted
