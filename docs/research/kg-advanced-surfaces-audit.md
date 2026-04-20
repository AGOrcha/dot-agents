# KG Advanced Surfaces Audit

**Audited:** 2026-04-20  
**Plan:** kg-command-surface-readiness  
**Task:** kg-advanced-surfaces-audit  
**Graph state at audit time:** READY, 34635→34646 nodes (after postprocess)

---

## Commands Audited

- `kg flows [--limit N] [--sort criticality|size] [--repo <path>]`
- `kg communities [--min-size N] [--sort size|cohesion] [--repo <path>]`
- `kg postprocess [--no-flows] [--no-communities] [--no-fts] [--repo <path>]`

---

## kg flows

### What it does

Calls `CRGBridge.ListFlows()` → `list_flows(repo_root, sort_by, limit)` via inline Python against the CRG DB.

### Observed behavior (fresh graph)

**Human output (default):**
```
Execution Flows  [Found 20 execution flow(s)]
  ◆ [] error (steps=0, criticality=0.61)
  ◆ [] collectNodes (steps=0, criticality=0.61)
  ◆ [] TestRemoveFileData (steps=0, criticality=0.57)
  ...
```

**JSON output (`--json kg flows`):**
```json
{
  "status": "ok",
  "summary": "Found 20 execution flow(s)",
  "flows": [
    {"id": 1, "name": "error", "entry_point": "", "step_count": 0, "criticality": 0.61, "kind": ""},
    ...
  ]
}
```

### Issues observed

1. **`step_count: 0` on all flows** — CRG detects flows but doesn't populate step-level detail. An agent asking "what path does this flow take?" gets no answer.
2. **`entry_point: ""` on all flows** — The most useful field for understanding a flow's scope is always empty.
3. **Flow names are function names** — `error`, `collectNodes`, `TestRemoveFileData` are ordinary function names, not business-domain "flows". This is misleading to agents trying to reason about execution paths.
4. **Count varies with postprocess** — Before explicit `kg postprocess`: 20 flows. After `kg postprocess`: 64 flows. Flow detection is not stable across postprocess runs on the same graph.
5. **Advisory already present** — `runKGFlows` already contains `ui.Info("No flows detected. Run 'dot-agents kg postprocess' to detect flows.")` but this fires only when 0 flows are returned, not when step detail is missing.

### Decision: **expert-only**

`kg flows` output is incomplete (missing step chains and entry points) and semantically misleading (function names presented as flows). Agents relying on it for execution path reasoning will draw incorrect conclusions. Add a warning to the help text.

**Help text addition (no-impl work):**
> Note: flow step chains and entry points are not currently populated. Results show highly-connected functions sorted by criticality, not full execution paths.

---

## kg communities

### What it does

Calls `CRGBridge.ListCommunities()` → `list_communities_func(repo_root, sort_by, min_size)` via inline Python.

### Observed behavior (fresh graph)

**Human output (default, no flags):**
```
Code Communities  [Found 65 communities]
  ◆ [go] commands-workflow (size=139, cohesion=0.25)
          File-based community: /...dot-agents/commands/workflow.go
  ◆ [go] commands-graph (size=121, cohesion=0.25)
  ...
```

**After `kg postprocess`:** community count grew to 522.

**With `--sort size` (after postprocess):** JavaScript `node_modules` communities dominate at the top (size=11998, 8051, 1636...). Go communities are buried.

**JSON output:** `members: []` is always empty despite `size=139`:
```json
{"id": 126, "name": "commands-workflow", "size": 139, "members": []}
```

### Issues observed

1. **`members: []` always** — The `Members []string` field exists in the struct but `list_communities_func` returns no member list. Size is reliable; membership is not accessible.
2. **node_modules pollution when sorting by size** — The `--sort size` path exposes JS node_modules communities (size ~12k) that dwarf legitimate Go communities. There is no `--language` filter. Without such filtering, size-sorted output is noisy for a Go-only analysis.
3. **Default behavior shows Go communities first** — Without `--sort size`, CRG appears to return a filtered or lower-limit set that shows Go communities. This default is useful but undocumented.
4. **Community count is postprocess-sensitive** — Default run shows 65, after postprocess shows 522. Depends on when postprocess was last run.

### Agent-usable information

Despite the gaps, community names + sizes + dominant_language are reliable and useful:
- "Which module clusters exist in this repo?" → community list by name
- "What's the largest Go community?" → filter by dominant_language=go, sort by size
- Community names (`commands-workflow`, `platform-hook`) are mechanically derived from file paths and are stable

### Decision: **agent-ready with documented caveats**

Community names, sizes, and dominant language can be consumed by agents. Add these caveats to the help text:

> Note: member lists are not currently populated — use 'kg impact' to analyze specific files in a community. Results include all indexed languages; use --min-size with language awareness to filter noise from third-party dependencies.

---

## kg postprocess

### What it does

Shells out to `code-review-graph postprocess --repo <root>`, which rebuilds flows, communities, and FTS index.

### Observed behavior

```
Running post-processing on . ...
INFO: FTS index rebuilt: 34646 rows indexed
INFO: igraph not available, using file-based community detection
Post-processing: 64 flows, 522 communities, 34646 FTS entries
```

- No JSON output even with `--json` flag — the Python process writes INFO lines to stdout regardless.
- `kg build` and `kg update` both include a `--skip-postprocess` flag; by default they DO run postprocess.
- Running `kg postprocess` explicitly can change flow and community counts on an already-built graph.

### Decision: **expert-only** (operator/maintenance)

`kg postprocess` is a graph maintenance command, not an agent query command. Agents should never call it directly. It is called automatically by `kg build`/`kg update`. Add a note to the help text:

> This command rebuilds derived data (flows, communities, FTS) from the code graph. It runs automatically as part of 'kg build' and 'kg update'. Run explicitly only to repair stale derived data.

---

## Per-command decision matrix

| Command | Agent-ready? | Decision | Required help-text change |
|---------|-------------|----------|--------------------------|
| `kg flows` | No | expert-only | Add note: step chains and entry points not populated |
| `kg communities` | Partial | agent-ready with caveats | Add note: members not populated; third-party noise in size sort |
| `kg postprocess` | No | expert-only | Add note: auto-called by build/update; manual use is maintenance-only |

---

## Implementation scope for this audit

This audit task is **audit-only**. The only allowed output is:
1. This audit document
2. Help-text additions (no-impl allowed per task notes)

Help-text additions belong in `NewKGCmd()` in `commands/kg/cmd.go` as part of each subcommand's `Long` or `Example` field — **not** as code changes to logic.

The only code change permitted is adding advisory text to the `Long` description of `kgFlowsCmd`, `kgCommunitiesCmd`, and `kgPostprocessCmd` in `commands/kg/cmd.go`. These are string literal changes with zero behavioral impact.

Structural fixes (member list population, language filtering, stable flow detection) are out of scope for this audit and belong in a future implementation task.
