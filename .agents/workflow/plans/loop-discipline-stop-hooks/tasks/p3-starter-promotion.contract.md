# P3 Starter Promotion Contract

- task: `p3-starter-promotion`
- requirements: R7.1-R7.3
- dependency: none

## Goal

Promote the existing local workflow assets into the embedded starter without
altering their behavior. P4 owns enforcement edits after the copies exist.

## Grounded Loader Decision

`internal/scaffold/home.CopyMissingStarterAssets` recursively walks the
embedded `starter/` tree, and `commands/init.go:createInitialAgentsDirs`
already creates `agents/global`. New embedded descendants under `agents/`
and `profiles/` therefore require tests, not loader implementation changes.

## Source to Destination Map

| Source | Starter destination |
| --- | --- |
| `~/.agents/skills/dot-agents/iteration-close/SKILL.md` and its `instructions/`, `scripts/`, `templates/` descendants | `internal/scaffold/home/starter/skills/global/iteration-close/` |
| `~/.agents/skills/dot-agents/isp/SKILL.md` and its `instructions/` descendants | `internal/scaffold/home/starter/skills/global/isp/` |
| `~/.agents/skills/dot-agents/loop-worker/SKILL.md` and its `instructions/` descendants | `internal/scaffold/home/starter/skills/global/loop-worker/` |
| `~/.agents/agents/dot-agents/loop-worker/AGENT.md` | `internal/scaffold/home/starter/agents/global/loop-worker/AGENT.md` |
| `~/.agents/profiles/loop-worker.md` | `internal/scaffold/home/starter/profiles/loop-worker.md` |

Use the agent-tier `AGENT.md` as authoritative; do not introduce a second
copied agent definition within the skill tier. The TOML agent registration is
not part of the starter delivery defined by this plan.

## Acceptance

- Add the mapped files without changing their source semantics.
- Extend `internal/scaffold/home/copy_test.go`
  (`TestCopyMissingStarterAssetsCopiesStarterBundle`) to assert
  representative skill, agent, and profile destinations.
- Verify existing-file preservation behavior remains unchanged.

## Out of Scope

- Sentinel invocations or gotchas edits, which belong to P4.
- Loader refactoring or refresh policy changes.
