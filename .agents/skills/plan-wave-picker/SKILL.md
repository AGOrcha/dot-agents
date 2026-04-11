# Skill: Plan Wave Picker

## Purpose
When there are multiple waves/phases across two or more spec files, find the next available wave to work on without manually reading every plan file.

## Usage
Run at the start of a session where multiple plans exist in `.agents/active/`.

## Steps

### 1. Read plan statuses in one batch
Glob `.agents/active/*.plan.md`, then read all of them (or just grep for `Status:`) to find which are `Completed` vs not.

```bash
grep -l "Status: Completed" .agents/active/*.plan.md   # already done
grep -L "Status: Completed" .agents/active/*.plan.md   # next candidates
```

### 2. Check dependency ordering
Each plan typically lists `Depends on:`. Read the first non-completed plan and verify its dependencies are satisfied before starting.

### 3. Pick the lowest-numbered non-completed wave
- Workflow spec waves are numbered Wave 3, 4, 5… pick the lowest not yet complete
- KG spec phases are KG Phase 1, 2, 3… pick the lowest not yet complete
- Run the two specs in parallel (one workflow wave + one KG phase per loop iteration) when dependencies allow

### 4. Check for existing partial work
The untracked files in `git status` often reveal a phase already started. Check before re-implementing from scratch.

```bash
git status --short | grep "^??"   # untracked = in-progress phase
```

## Gotchas
- Plan files use `Status: Completed (date)` — grep for the whole prefix to avoid false matches
- The spec `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` and `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` are the sources of truth; if a plan file is missing detail, read the spec
- After implementing, always update the plan file's Status line
- `commands/` is a flat package — all kg.go, workflow.go, etc. live there; don't create new packages unless absolutely needed
