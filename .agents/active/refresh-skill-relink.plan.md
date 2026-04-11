# Refresh Skill Relink Plan

Status: Blocked — all items pending; blocked on resource-intent-centralization implementation (RFC accepted, centralized managed-resource planner still needs to land).
Depends on: `docs/rfcs/resource-intent-centralization-rfc.md` plus resource-intent-centralization implementation



## Problem

`go run ./cmd/dot-agents refresh dot-agents` successfully imports repo-local skill files from `.agents/skills/plan-wave-picker/**` into canonical `~/.agents/skills/dot-agents/plan-wave-picker/**`, but refresh then warns for Claude, Codex, and Copilot when relinking `.agents/skills/plan-wave-picker`.

Observed warning:

`removing existing file /Users/nikashp/Documents/dot-agents/.agents/skills/plan-wave-picker: directory not empty`

## Root Cause

- `commands/refresh.go` correctly imports project resources before relinking.
- `commands/import.go` correctly walks `.agents/skills/**`, so the skill content is imported.
- The relink phase calls `internal/platform/resources.go` -> `syncScopedDirSymlinksTargets(...)` for project skills.
- That helper delegates to `links.Symlink(...)`.
- `links.Symlink(...)` removes existing non-symlink paths with `os.Remove(...)`, which fails for non-empty directories.
- In this repository, `.agents/skills/<name>/` is both:
  - a valid unmanaged import source before refresh
  - the managed mirror target for Codex/Copilot and Claude compatibility after refresh
- Multiple platforms independently try to create the same shared mirror path, so ownership of `.agents/skills/<name>` is implicit and conflicting instead of centralized.

## Expected Behavior

After refresh imports a repo-local project skill, refresh should be able to convert the original `.agents/skills/<name>/` directory into the managed mirror pointing at `~/.agents/skills/<project>/<name>/`, without surfacing platform warnings for a normal import-and-relink flow.

Longer term, shared mirror paths should be linked once by a central coordinator from a declarative resource plan, not repeatedly by each platform implementation.

## Architectural Direction

Rather than letting each platform call generic link helpers directly for shared resources, move shared resource sync behind a central planner/executor:

- Each platform declares the managed resources it wants:
  target path, canonical source bucket/scope, marker or renderer, transport (`symlink`, `hardlink`, rendered file), and whether the target is shared across platforms.
- Refresh/install aggregates those declarations into a single resource plan.
- One executor creates shared targets such as `.agents/skills/<name>` once, dedupes identical requests, and detects conflicting requests before touching the filesystem.
- Platform-specific outputs remain platform-owned only when the target path is actually platform-specific, such as `.codex/hooks.json` or `.github/copilot-instructions.md`.

## Update Plan

- [ ] Design a declarative managed-resource plan shape for platform link intents, including target path, source resolution, transport, and shared-vs-platform ownership.
- [ ] Identify which current outputs should move under centralized shared-resource execution first:
  start with `.agents/skills/<name>` and `.claude/skills/<name>`, which currently collide across Claude, Codex, and Copilot flows.
- [ ] Refactor refresh/install relink to aggregate platform intents and execute shared directory sync once per target path, with explicit dedupe/conflict handling.
- [ ] Add a safe replacement rule for centralized managed directory targets so imported source directories can be converted into managed links after refresh.
- [ ] Keep low-level helpers such as `links.Symlink(...)` conservative; scope any recursive directory replacement to the centralized managed-resource executor instead of making all symlink writes destructive.
- [ ] Add regression coverage for refresh + skills:
  repo `.agents/skills/<name>/SKILL.md` imports into canonical `skills/<project>/<name>/SKILL.md`, then refresh relinks `.agents/skills/<name>` successfully.
- [ ] Add platform coverage for shared mirror targets used by Claude, Codex, and Copilot so one platform cannot succeed while the shared target still fails or is redundantly rewritten.
- [ ] Verify with focused tests first, then `go test ./...`, and rerun `go run ./cmd/dot-agents refresh dot-agents` to confirm warnings disappear.
