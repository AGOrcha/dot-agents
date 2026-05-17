# PR#10 Branch Split Plan

## Problem

Branch `feature/PA-cursor-projectsync-phase1-extract-293f` grew from a side task
into a 423-commit, 1155-file, 122k-line mega-branch. It incorporates the 22 commits
from `feature/workflow-auto-operator` (its original PR target). The result is
unreviewable as a single PR and blocks releasing stable, defect-free features.

## Decision

Split into 7 PRs, all targeting `master`. Retire `feature/workflow-auto-operator`
as a separate integration branch — its content is distributed across the split PRs.

## Dependency DAG

```
PR1 (Foundation) ──→ PR2 (Platform) ──→ PR3 (Command decomp) ──→ PR7 (Tests/docs)
      │                                       │
      │                                       ├──→ PR4 (Graphstore)
      │                                       └──→ PR5 (Scaffold)
      └──→ PR6 (TypeScript)
```

PRs 4, 5, 6 are independent of each other after PR3. PR7 goes last since testutil
and test dedup refactors touch files across all packages.

## Merge strategy

- Sequential merge to master; downstream PRs rebase onto new master tip
- Each PR must compile and pass CI independently before merge
- SonarQube fixes folded into the PR where they touch files (option A)
- `.agents/` workflow artifacts distributed pragmatically — not every iteration
  log needs to land, only canonical plans and lessons

## Package dependency map (from code graph analysis)

```
internal/config     ← foundation, imported by everything
    ↑
internal/platform   ← imported by all command packages
    ↑
internal/projectsync ← imported by commands, agents, skills
    ↑
commands/* (root)    ← wires subpackages into Cobra tree
    ↑
commands/{workflow,kg,agents,hooks,sync,skills}  ← subpackage extractions
```

Lateral (no upstream deps on each other):
- internal/graphstore ← only by commands/kg, commands/workflow
- internal/scaffold/* ← only by commands/init, commands/agents/new
- internal/testutil  ← only by *_test.go files
- ports/typescript/  ← zero Go coupling

## Propagation protocol

**Fix needed before upstream PR merges:**
Fix on that branch, rebase each downstream branch onto it. Since branches are
built via `git checkout <mega-branch> -- <files>`, re-creating a downstream
branch after an upstream fix is cheap.

**Fix discovered after upstream PR merges:**
Make it on master (or hotfix branch), downstream PRs pick it up on next rebase.

**Compile failure from partial extraction:**
If a file touches both sides of a seam, move it to the earlier PR. Adjust
write_scope in TASKS.yaml to match.

## File inventory per PR

| PR | New | Mod | Del | ~Total |
|----|-----|-----|-----|--------|
| 1 Foundation | ~15 | ~20 | 0 | ~40 |
| 2 Platform | ~15 | ~15 | 0 | ~30 |
| 3 Command decomp | ~55 | ~15 | 0 | ~70 |
| 4 Graphstore | ~13 | 0 | 0 | ~13 |
| 5 Scaffold | ~45 | 0 | ~34 | ~80 |
| 6 TypeScript | ~36 | 0 | 0 | ~40 |
| 7 Tests/docs | ~60 | ~40 | 0 | ~100 |

## Verification

Per-PR:
1. `go build ./...`
2. `go test -race -count=1 ./...`
3. `gofmt -l ./cmd ./commands ./internal` (no output)
4. CI green (push to feature branch, check GitHub Actions)

Final:
- `git diff master <original-branch-tip>` — only `.agents/` workflow artifacts
  should differ (iteration logs, loop state, etc.)
- master CI green after all merges
