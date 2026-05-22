---
name: refresh-import-before-relink
description: Refresh paths that both import repo files and rewrite managed outputs must import project content before relinking, and must never replay canonical backup snapshots as live canonical state
type: feedback
---

When a command both imports unmanaged repo files and regenerates managed project outputs, the import phase must run first and must be authoritative for project-local content.

**Why:** If relinking happens before import, unmanaged files like `AGENTS.md` can be overwritten by a global fallback before their content reaches canonical `~/.agents` storage. If refresh import remains interactive, the command can still overwrite after a declined prompt. If canonical backup snapshots under `resources/` are restored as if they were original repo files, stale canonical state can undo a fresh import.

**How to apply:**
1. Run project-scope import before any relink/write phase in refresh-like commands.
2. Treat refresh-internal import as non-interactive or otherwise make overwrite semantics impossible to misorder.
3. Never restore canonical backup snapshots from `resources/<project>/rules|settings|mcp|skills|agents|hooks` back into live canonical paths.
4. Add regression coverage for both cases:
   `canonical missing + unmanaged repo file`
   `canonical exists + unmanaged repo file should replace it`
