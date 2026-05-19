# Command Surface Decomposition Plan

Status: Active

## Outcome

Reduce worker contention and context bloat by decomposing the remaining large command surfaces after `workflow`.

This plan focuses on command families that are either:

- structurally large enough to overload worker context
- broad enough that unrelated slices keep colliding on the same file
- already naturally divided into subcommands or lifecycle surfaces

## Why This Exists

The repo still has a few command hotspots that are expensive for workers:

- `commands/kg.go`
- `commands/agents.go`
- `commands/sync.go`
- `commands/skills.go`
- `commands/hooks.go`

`status` and `import` are also large enough to benefit from extraction work, but they look more like helper-heavy commands than obvious subpackage splits, so they are tracked separately as a helper-seam task.

## Task Catalog

### `c1-kg-command-decomposition`

Split the `kg` command into narrower files or a `commands/kg/` subpackage. This is the highest-value follow-on after `workflow` because `kg` is the next major command hotspot.

### `c2-agents-command-decomposition`

Split `agents` by lifecycle surface so `list`, `new`, `promote`, `remove`, and `import` no longer share one implementation file and one test hotspot.

### `c3-sync-command-decomposition`

Split `sync` by subcommand family. This is a smaller command than `kg`, but it has very clear ownership boundaries and should decompose cleanly.

### `c4-skills-command-decomposition`

Split `skills` by lifecycle surface and keep matching tests aligned with those same boundaries.

### `c5-hooks-command-decomposition`

Split `hooks` by subcommand family so low-risk hook changes stop colliding in one command file.

### `c6-status-import-helper-extraction`

Extract helper seams in `status` and `import` without forcing a premature package split. The goal here is to make later decomposition cheaper and reduce context load now.

## Sequencing

Recommended order:

1. `c1-kg-command-decomposition`
2. `c2-agents-command-decomposition`
3. `c3-sync-command-decomposition`
4. `c4-skills-command-decomposition`
5. `c5-hooks-command-decomposition`
6. `c6-status-import-helper-extraction`

`kg` goes first because it is the biggest remaining command hotspot. `agents` is next because it sits on active lifecycle work and has obvious boundaries. `sync`, `skills`, and `hooks` are smaller but structurally clean wins.

## Exit Condition

The plan is complete when the large post-workflow command families are decomposed into narrower files or subpackages with matching tests, preserved CLI behavior, and materially lower worker collision risk.
