# init.go bypasses the hooks package for Claude+Cursor global links

## Pattern

`commands/init.go` writes global Cursor and Claude settings/hooks directly (`linkClaudeGlobalSettings`, `linkCursorGlobalHooks`) and **chooses** between `hooks/global/claude-code.json` and `settings/global/claude-code.json` (whichever exists, hooks wins). The maintainer flagged this 2026-05-19: it defeats the merge intent that the hooks-vs-settings work (canonical hooks design + the merging logic in the `HOOK.yaml` model) was supposed to deliver.

## Root cause

- Intent was for **settings + hooks to be joined together** for Claude (and platforms that support the merged model), not "pick one source file with a priority rule." Cursor having its own hooks.json file is fine — that's the platform's own design.
- The global hook linking belongs in the **hooks package/command** (with proper resource targets + intents, the same way per-project hook routing now works), not as bespoke link calls in `init`.
- Right now `init.go` is doing platform-aware hooks plumbing — it should be a thin caller of the hooks subsystem.

## Rule

Open a follow-up task (under hooks-related plan — canonical-hooks-design or workflow/hooks-package work). Move the Claude+Cursor global link calls out of `init.go` into the hooks package's standard resource-target/intent flow; have `init` call the hooks package's "ensure global hooks linked" entry-point instead. Preserve the backup-preserving link contract (`SymlinkReplacing`/`HardlinkReplacing` with `sidecarBackupFile`) on the new path. Joined-settings-and-hooks merging restored to wherever it was supposed to live.

## How to apply

When auditing `commands/init.go` or designing the canonical-hooks-design plan:
1. Identify every direct platform-specific link call in `init.go` (currently Claude global settings + Cursor global hooks)
2. Trace whether the hooks package has an equivalent "ensure global" entry point
3. If not, the hooks package needs that entry point first
4. Route `init.go` through the entry point; remove the direct link calls
5. Preserve backup behavior end-to-end (sidecar backup file, symlink-replacing semantics)

Out of scope for `seam-interface-di-migration` PR #40 — that PR only converts seams, doesn't change behavior; record + park.

## Cross-references

- `[[starter-vs-project-overlay]]` — the hooks package architecture is dev-of-dot-agents concern; the canonical hooks design itself ships as part of the product
