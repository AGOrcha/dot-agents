# Skill Import Streamline Plan

Status: Blocked — all items pending; blocked on resource-intent-centralization implementation (RFC accepted, shared skill mirror convergence still requires the centralized resource executor to land).
Depends on: `docs/rfcs/resource-intent-centralization-rfc.md` plus resource-intent-centralization implementation



## Problems Observed

- `.agentsrc.json` lost non-struct fields such as the legacy `refresh` block when rewritten by command paths that use `AgentsRC.Save()`.
- `dot-agents install --generate` rebuilt the manifest from canonical state and dropped the explicit git source because `GenerateAgentsRC()` currently emits only `[{ "type": "local" }]`.
- The earlier extra trailing `}` in `.agentsrc.json` was a bad manual patch, not command output, but it still surfaced that manifest handling around this flow is too fragile.
- Skill promotion required an awkward sequence:
  `import` to canonical state, then `skills new` to mutate the manifest, then manual manifest cleanup.
- Repo-local `.agents/skills/*` stayed as local directories rather than being converted to managed symlink mirrors after import/promotion.

## Root Causes

- `internal/config/AgentsRC` does not preserve unknown JSON fields, so any save path rewrites only the modeled subset.
- `GenerateAgentsRC()` is a lossy generator rather than a merge/update operation and therefore drops source declarations that are not rediscovered from canonical state.
- `commands/import.go` canonicalizes files, but “promote skill + update manifest + converge repo mirror” is not one command-level workflow.
- Shared skill mirror projection still depends on platform `CreateLinks()` paths that cannot safely replace imported non-empty source directories in `.agents/skills/`.

## Implementation Goals

- Preserve manifest fields and existing sources when mutating `.agentsrc.json`.
- Introduce one coherent command path for “promote reviewed repo-local skills into canonical project scope and register them in the manifest”.
- Converge repo-local shared skill paths onto managed symlinks after promotion when that is the intended ownership model.
- Avoid coupling this fix to ad hoc platform-specific patches if the target path is shared across platforms.

## Next Slice

- [ ] Add manifest round-trip preservation for unknown fields, or explicitly model the current `refresh` block if it is still supported.
- [ ] Make `install --generate` merge with existing `.agentsrc.json` where appropriate instead of replacing `sources` and other manual declarations wholesale.
- [ ] Add a project-scope “skills import/promote” command path that:
  1. imports repo-local skill content into `~/.agents/skills/<project>/`
  2. updates `.agentsrc.json`
  3. refreshes shared skill mirrors
- [ ] Fix shared `.agents/skills/*` convergence so repo-local source directories can become managed mirrors after promotion without conflicting platform relink behavior.
- [ ] Add tests for:
  - manifest save preserving `sources` and unknown fields
  - import/promote of project skills in one command path
  - repo `.agents/skills/*` becoming managed links after successful promotion
