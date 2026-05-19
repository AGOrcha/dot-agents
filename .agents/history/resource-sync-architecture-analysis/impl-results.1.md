# Implementation Results

## 1. Resource Sync Architecture Analysis

- Reviewed the current Stage 1 rollout plan in `.agents/active/platform-dir-unification.plan.md` against the active Go command and platform code paths.
- Confirmed that the current model is only partially centralized: `internal/platform/resources.go` centralizes source lookup, but output ownership and command orchestration remain fragmented.
- Identified shared repo-local targets currently mutated by multiple platform implementations:
  - `.agents/skills/<name>` via Claude, Codex, OpenCode, and Copilot
  - `.claude/agents/<name>` via Claude and Cursor
  - `.claude/settings.local.json` via Claude and Copilot compatibility flows
- Identified command-layer fragmentation where shared helpers live inside unrelated command files:
  - `mapResourceRelToDest()` is defined in `commands/refresh.go` but used by import/add restore logic
  - `createProjectDirs()` and `restoreFromResourcesCounted()` live in `commands/add.go` but are used by refresh/install flows
  - `writeRefreshMarker()` lives in `commands/refresh.go` but is also part of install finalization
- Recommended moving to a declarative resource-plan model with:
  - canonicalization/import handled once in a shared package
  - platform/resource intents aggregated centrally
  - a single projection executor owning shared targets, dedupe, conflict detection, pruning, and safe replacement rules
  - status/explain/remove driven from the same resource registry instead of hand-coded path lists
