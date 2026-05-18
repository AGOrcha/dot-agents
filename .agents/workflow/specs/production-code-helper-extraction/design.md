# Spec — production-code-helper-extraction

## Problem

PR#10's SonarCloud quality gate fails on `new_duplicated_lines_density`
at 4.7% (target ≤3%). Per ADR-0008 (option B), the test-fixture cluster
(D) is the load-bearing one and gets its own plan
(`go-test-fixture-extraction`). The remaining production-code cluster —
**Cluster E.other**, ~400–500 duplicated lines across roughly a dozen
files in `internal/graphstore/`, `internal/platform/`, `commands/`, and
`ports/typescript/src/commands/` — is what this plan addresses.

The in-PR cherries (Cluster F: `commands/{agents,skills}/list.go`; and
E.promote: `commands/{agents,skills}/promote.go`) were taken as part of
PR#10 via `internal/projectsync.{ListBucket, PromoteResource, CopyTree,
ReadFrontmatterDescription}`. Cluster E.other is what remains.

## Goals

1. Drive the production-code share of duplicated lines below the level
   where, combined with `go-test-fixture-extraction`, the PR-level
   density gate stays under 3% on future PRs.
2. Where extraction is viable, prefer a **`projectsync`-shaped helper**
   (BucketSpec-style parameterization) over copy-pasted bodies, so the
   pattern stays consistent with the agents/skills work in PR#10.
3. Where extraction is not viable (one-off coincidence rather than
   structural shape duplication), accept the duplication and tag it
   in the design memo with reasoning so a future audit doesn't
   re-litigate.

## Non-goals

- Behavior changes. Production-code refactor only; same wire format,
  same outputs, same side effects.
- Touching tests beyond what's needed to keep them green. The test
  cluster has its own plan.
- Adding new abstractions to support future not-yet-needed callers.
  Extract only what is duplicated today; YAGNI applies.

## Decisions

- **One pair-group per task.** Each "group" is a set of files Sonar
  reports as cross-duplicated (e.g. sqlite.go ↔ postgres.go is one
  group; settings.go ↔ mcp.go ↔ rules.go is another). Tasks do not
  span groups — that keeps blast radius small and makes per-task
  reverts safe.
- **Helpers live in the smallest enclosing package.** A graphstore
  helper goes in `internal/graphstore/` (or `internal/graphstore/internal/`
  if it shouldn't escape); a CLI helper used only by `commands/*.go`
  goes in `commands/internal/cmdutil/` rather than `internal/`.
- **`projectsync` is for resource-bucket abstractions only.** Don't
  bloat it with generic helpers (e.g. SQL builders, TS string utilities)
  just because it already exists. Each helper finds its right home.
- **Each task self-verifies via `go test ./<package>/...`.** No
  cross-task SonarCloud poll until plan closeout.

## Open questions

(Resolved during T1 audit on 2026-05-04. Per-group decisions and helper
APIs live in the "Scope" section below.)

## Scope (live counts re-snapped from PR#10 SonarCloud, 2026-05-04 T1)

> Note: PR#10's projectsync extraction has already landed in the branch
> tip but the SonarCloud snapshot for PR#10 still reflects pre-extraction
> blocks for `commands/agents/list.go`, `commands/skills/list.go`,
> `commands/agents/promote.go`, `commands/skills/promote.go`, and
> `commands/skills.go` (lines 41-86 / 60-93 of files that are now
> ≤15-50 lines total). Treat those Cluster F entries as already cleared
> in code; this plan does not re-extract them.

### Group GS — graphstore SQL backends

Live counts:

- `internal/graphstore/sqlite.go` (116 dup lines, 4 blocks, 14.3%)
- `internal/graphstore/postgres.go` (94 dup lines, 2 blocks, 10.4%)
- `internal/graphstore/mcp_server.go` (87 dup lines, 4 blocks, 12.6%)

Block inventory:

| Source | Target | Size | Shape |
|---|---|---|---|
| sqlite.go:475 | postgres.go:589 | ~80 lines | `GetImpactRadius` BFS body |
| sqlite.go:459 | postgres.go:570 | 15 lines | edge-rows scan loop (`SELECT source_qualified, target_qualified FROM edges` + scan into fwd/rev maps) |
| sqlite.go:721 | sqlite.go:741 | 10 lines | `scanNode`/`collectNodes` row.Scan column lists (intra-file) |
| mcp_server.go:197 | mcp_server.go:208 | ~30 lines | tool descriptor InputSchema literals (intra-file) |
| mcp_server.go:369 | mcp_server.go:506 | 23 lines | bridge readiness guard (`requireBridge` → `Status` → CRGReadiness{Unbuilt,BusyOrLocked} JSON error envelope) |

**Decision: EXTRACT (partial).**

Two of the cross-pair blocks are pure-Go algorithm copies that diverge
only at the driver query call. One intra-file mcp_server block is a
copy-paste of the readiness guard that has nothing to do with SQL. The
~30-line mcp_server tool descriptor block is data, not behavior, and
the 10-line sqlite scanNode/collectNodes block is already as factored
as it sensibly gets (each operates on a different rowsScanner type).

Proposed helpers (in `internal/graphstore/`, package-internal):

```go
// loadEdgeAdjacency drains a 2-column rows iterator (source_qualified,
// target_qualified) and returns forward+reverse adjacency maps.
// Caller passes a queryEdges closure so the same body works for both
// database/sql (*sql.Rows) and pgx (pgx.Rows) drivers.
func loadEdgeAdjacency(queryEdges func() (edgeRows, error)) (fwd, rev map[string][]string, err error)

// edgeRows is the minimal iterator surface both driver row types satisfy.
type edgeRows interface {
    Next() bool
    Scan(dest ...any) error
    Close() error
    Err() error
}

// bfsImpactFromSeeds runs the bounded BFS over fwd/rev adjacency and
// returns the impacted-set + visited-set respecting maxDepth/maxNodes.
// Pure Go — no driver knowledge.
func bfsImpactFromSeeds(seeds map[string]bool, fwd, rev map[string][]string, maxDepth, maxNodes int) (impacted map[string]bool)

// resolveImpactResult fans seeds + impacted out to ImpactResult via
// node lookup + GetEdgesAmong. The store passes itself through a
// nodeLookup interface (GetNode, GetEdgesAmong) so the helper stays
// driver-agnostic.
func resolveImpactResult(store nodeLookup, seeds, impacted map[string]bool) (ImpactResult, error)
```

Plus an mcp_server-local helper:

```go
// graphReadinessGuard returns a JSON envelope describing not-built /
// busy-or-locked states, or (nil, nil) when the graph is ready to use.
// Replaces the duplicated switch in get_impact_radius_tool and
// get_review_context_tool.
func (s *kgMCPServer) graphReadinessGuard() ([]byte, error)
```

Call sites replaced:

- `(*SQLiteStore).GetImpactRadius` (sqlite.go:434-555) → calls all three new helpers
- `(*PostgresStore).GetImpactRadius` (postgres.go:548-667) → ditto
- `kgMCPServer` get_impact_radius_tool handler (mcp_server.go:369) → calls `graphReadinessGuard`
- `kgMCPServer` get_review_context_tool handler (mcp_server.go:506) → calls `graphReadinessGuard`

ACCEPT for the remaining mcp_server.go intra-file blocks:

- mcp_server.go:197 ↔ 208 (~30 lines tool-descriptor `InputSchema` literals): these are JSON-schema literals declaring tool input contracts. The duplication is data, not control flow. Extracting a builder ("intParam"/"strArrayParam") would obscure the schema and make CI diffs noisier.
- sqlite.go:721 ↔ 741 (10 lines scanNode/collectNodes column lists): both already accept abstract scanners; the remaining duplication is the column list itself, which mirrors the SQL `SELECT` column list elsewhere. Sharing it would mask the symmetry-with-SQL invariant that makes this code reviewable.

Estimated dup-line removal: **~120 lines** (80 + 15 + 23×1, since the
two readiness-guard blocks share one helper that removes 23 lines from
one of the two call sites).

### Group RP — resource_plan self-duplication

Live counts:

- `internal/platform/resource_plan.go` (70 dup lines, 5 blocks, 10.9%)

Block inventory:

| Block | Sites | Size |
|---|---|---|
| 18-line `listScopedResourceDirs` + intent-construction loop | 284, 337, 461 | 18 lines × 3 |
| 26-line extension of same shape (loop body with full `ResourceIntent` literal) | 284, 461 | 26 lines × 2 |

**Decision: EXTRACT.**

This is the cleanest single win in the plan. Three near-identical
helpers (`buildSharedSkillMirrorIntentsForRoot`,
`buildSharedPluginBundleIntentsForRoot`,
`buildSharedAgentMirrorIntentsForRoot`) differ only in five values:
bucket name (skills/plugins/agents), manifest constant
(skillManifestName/PluginManifestName/agentManifestName), source-ref
Kind (CanonicalDir/CanonicalBundle/CanonicalDir), Origin string, and
Materializer constant.

Proposed helper (in `internal/platform/`):

```go
// sharedMirrorIntentSpec parameterizes the per-bucket symlink-mirror
// intent shape. Used by buildSharedSkillMirrorIntentsForRoot,
// buildSharedPluginBundleIntentsForRoot, and
// buildSharedAgentMirrorIntentsForRoot.
type sharedMirrorIntentSpec struct {
    Bucket       string                  // "skills" | "plugins" | "agents"
    ManifestName string                  // skillManifestName, etc.
    SourceKind   ResourceSourceKind      // CanonicalDir | CanonicalBundle
    Origin       string                  // "shared-skill-mirror", etc.
    Materializer string                  // "shared-skill-dir-symlink", etc.
}

// buildSharedMirrorIntentsForRoot returns ResourceIntents for every
// bucket entry under ~/.agents/<bucket>/<project>/ that owns the
// expected manifest, projecting them into targetRoot via symlink.
func buildSharedMirrorIntentsForRoot(project, targetRoot string, spec sharedMirrorIntentSpec) []ResourceIntent
```

Call sites replaced:

- `buildSharedSkillMirrorIntentsForRoot` (resource_plan.go:281)
- `buildSharedPluginBundleIntentsForRoot` (resource_plan.go:334)
- `buildSharedAgentMirrorIntentsForRoot` (resource_plan.go:458)

Each shrinks to a 4-5 line wrapper that constructs the spec and
delegates. Public API surface (`BuildSharedSkillMirrorIntents`,
`BuildSharedPluginBundleIntents`, `BuildSharedAgentMirrorIntents`)
unchanged.

Estimated dup-line removal: **~60 lines**.

### Group CMDS — commands/* CLI families

Live counts:

- `commands/settings.go` (55 dup lines, 3 blocks, 29.3%)
- `commands/mcp.go` (55 dup lines, 3 blocks, 29.3%)
- `commands/rules.go` (43 dup lines, 2 blocks, 17.9%)
- `commands/import_plugins.go` (50 dup lines, 3 blocks, 7.6%)
- `commands/explain.go` (23 dup lines, 2 blocks, 9.1%)
- `commands/status.go` (50 dup lines, 4 blocks, 4.2%)
- `commands/ux.go` (25 dup lines, 2 blocks, 8.7%)
- `commands/skills.go` (34 dup lines, 1 block, 15.0%) — **stale**: the cited 60-93 range duplicates the `commands/{agents,skills}/list.go` blocks that PR#10's projectsync work replaced; treat as already-cleared in code

Block inventory (settings/mcp/rules three-way):

| Sites | Size | Shape |
|---|---|---|
| settings.go:106 ↔ mcp.go:106 ↔ rules.go:122 | 19 lines × 3 | `runXList` body: `agentsHome` → `platform.ListCanonicalXFiles` → `os.IsNotExist` skip → empty-list message → header + per-spec `BaseName`/`path` printf |
| settings.go:129 ↔ mcp.go:129 | 12 lines × 2 | `runXShow` body: `findXSpec` → `os.Stat` → header → printf path/size |
| settings.go:149 ↔ mcp.go:149 ↔ rules.go:201 | 24 lines × 3 | `runXRemove` body: `findXSpec` → `EnsureUnderXScopeTree` → header + printf → DryRun branch → confirm prompt → `os.Remove` → success message |

**Decision: EXTRACT (settings/mcp/rules); ACCEPT (rest).**

The three-way is structural duplication parameterized by:
- spec type (`*platform.SettingsFileSpec` | `*MCPFileSpec` | `*RuleFileSpec`)
- the four platform helpers (`ListCanonical{X}Files`, `ResolveCanonical{X}File`, `EnsureUnder{X}ScopeTree`)
- four user-facing labels: kind noun ("Settings"/"MCP"/"Rules"), directory name, list-empty hint, remove confirm prompt

Proposed helpers (in `commands/internal/cmdutil/canonfile.go` — new
package):

```go
// CanonicalFileSpec describes a single file entry shared by settings,
// mcp, and rules subcommand families. The four helper closures abstract
// away the platform.* functions so cmdutil stays free of platform-type
// imports per the project layering rules.
type CanonicalFileSpec struct {
    Kind        string // user-facing label: "Settings", "MCP", "Rules"
    DirSegment  string // ~/.agents/<DirSegment>/ (e.g. "settings", "mcp", "rules")
    SingularRem string // shown in remove confirm: "settings file", "MCP file", "rule file"

    List           func(agentsHome, scope string) ([]CanonicalFileEntry, error)
    Resolve        func(agentsHome, scope, name string) (CanonicalFileEntry, error)
    EnsureScope    func(agentsHome, scope, target string) error
    BaseRemoveDir  string // "settings"/"mcp"/"rules" used in user prompt text
}

// CanonicalFileEntry is the projection of a SettingsFileSpec / MCPFileSpec /
// RuleFileSpec that the runXList/Show/Remove helpers actually use.
type CanonicalFileEntry struct {
    BaseName   string
    SourcePath string
}

func RunCanonicalList(scope string, spec CanonicalFileSpec) error
func RunCanonicalShow(scope, name string, spec CanonicalFileSpec, extras ...func(srcPath string)) error
func RunCanonicalRemove(deps RemoveDeps, scope, name string, spec CanonicalFileSpec) error
```

`RunCanonicalShow` accepts an `extras` variadic for callers that need
to print additional fields (rules.go prints a frontmatter description;
settings/mcp don't).

Call sites replaced:

- `runSettingsList`, `runSettingsShow`, `runSettingsRemove` (settings.go)
- `runMCPList`, `runMCPShow`, `runMCPRemove` (mcp.go)
- `runRulesList`, `runRulesShow`, `runRulesRemove` (rules.go)

ACCEPT for the remaining commands/*.go entries:

- **explain.go:143 ↔ 152 (14 lines)** — `printLinkTypesExplanation`
  prints two help-text paragraphs (HARD LINKS section, SYMLINKS section)
  with parallel structure but distinct prose. Sonar matches the
  `fmt.Fprintf(os.Stdout, "  %sHEADING%s …", ui.Bold, ui.Reset, …)`
  shape. Extracting a helper would obscure the docs for no behavioral
  win — this is text, not logic.
- **status.go:986 ↔ 1126 (14 lines) and :1005 ↔ :1167 (11 lines)** —
  per-platform symlink audit loops in `printClaudeAudit`,
  `printOpenCodeAudit`, `printCopilotAudit`. Each varies in 4+ knobs
  (directory path, label string, leading display prefix, error message
  format) and feeds the existing platform-specific surface. A helper
  would have ~4 closures + a settings struct just to remove 25 lines —
  net negative. Future cleanup belongs in a `printSymlinkAudit`
  refactor of the whole status family, not this dedup pass.
- **ux.go:247 ↔ :251 (21 lines)** — `enrichErrorWithHints` switch-case
  body. Each case branches on a substring match in error.Message and
  appends 1-2 hint strings. The match strings differ; the
  `enriched.Hints = append(...)` calls share a 1-line shape. Sonar's
  matcher counts each `case strings.Contains(...) { append(...) }`
  block as a copy. Extraction would replace declarative pattern data
  with imperative function calls — net negative on readability.
- **import_plugins.go:565 ↔ :580 (14 lines)** —
  `packagePluginComponentPath` per-platform switch arms (claude vs
  cursor branch). Each platform has a different valid prefix list with
  platform-specific quirks (cursor handles bare `mcp.json`, codex
  doesn't ship rules/, etc.). Switch-on-data is the right shape; a
  helper would fight the existing structure.
- **import_plugins.go:634 ↔ plugins.go:179 (22 lines)** —
  `sortedUniqueStrings` is byte-identical in both files. **EXTRACT
  here, but as a one-line move**: keep the canonical definition in
  `internal/platform/plugins.go` and delete the copy in
  `commands/import_plugins.go`, replacing call sites with
  `platform.SortedUniqueStrings`. No new package; no new helper API.
  Counts as part of T4 (one commit).

Estimated dup-line removal:

- settings/mcp/rules three-way: ~115 lines (19×2 + 12×1 + 24×2 net of
  the call-site wrappers each helper still needs)
- `sortedUniqueStrings` collapse: 22 lines

Total Group CMDS: **~135 lines**.

### Group MCP — internal/platform mcp_settings + plugins

Live counts:

- `internal/platform/mcp_settings.go` (26 dup lines, 2 blocks, 14.0%)
- `internal/platform/plugins.go` (22 dup lines, 1 block, 10.9%)

Block inventory:

| Sites | Size | Shape |
|---|---|---|
| mcp_settings.go:113 ↔ :139 | 13 lines × 2 | `ResolveCanonicalMCPFile` body ↔ `ResolveCanonicalSettingsFile` body — both compute root, build candidate ext list, walk + stat, return typed spec |
| plugins.go:179 ↔ commands/import_plugins.go:634 | 22 lines | `sortedUniqueStrings` (covered under Group CMDS — same fix; same commit family) |

**Decision: EXTRACT (mcp_settings intra-file twin); EXTRACT-CMDS-OWNS (sortedUniqueStrings shared with Group CMDS).**

The `ResolveCanonicalMCPFile` ↔ `ResolveCanonicalSettingsFile` twin
shares the structure used by Group CMDS but at the platform layer.
This task lands the underlying primitive that the cmdutil helper from
Group CMDS will call into.

Proposed helper (in `internal/platform/`):

```go
// resolveCanonicalFileByExt walks the candidate set (name plus
// name+ext for each known ext when name has no dot) under
// agentsHome/<bucket>/<scope>/, returns the first file that satisfies
// validate(). Used by ResolveCanonical{MCP,Settings}File.
func resolveCanonicalFileByExt(
    agentsHome, bucket, scope, name string,
    validExts []string,
    validate func(filename string) bool,
) (foundPath, baseName string, err error)
```

Each public Resolve function becomes a 4-line wrapper that wires its
extension list, validator (`isMCPFileName` / `isSettingsFileName`),
and constructs the typed spec from the returned path.

The `sortedUniqueStrings` collapse stays in Group CMDS (T4) because the
duplicate copy lives in `commands/import_plugins.go`; the canonical
definition in `plugins.go` doesn't move.

ACCEPT — none.

Estimated dup-line removal: **~13 lines** (the 22-line
`sortedUniqueStrings` is counted under Group CMDS to avoid
double-counting).

### Group TS — TypeScript port mirrors

Live counts:

- `ports/typescript/src/commands/agents.ts` (20 dup lines, 1 block, 17.1%)
- `ports/typescript/src/commands/skills.ts` (20 dup lines, 1 block, 16.9%)

Block inventory:

| Sites | Size | Shape |
|---|---|---|
| agents.ts:33 ↔ skills.ts:34 | 20 lines | combined `readXDescription` (frontmatter `description:` line scan) + `stripOuterQuotes` + `runXList` body (readdir → per-entry stat manifest → push entry) |

**Decision: EXTRACT.**

This mirrors the Go projectsync extraction PR#10 just shipped. The two
files differ only in: bucket noun (agents/skills), manifest filename
(AGENT.md/SKILL.md), entry interface name (`AgentEntry`/`SkillEntry`),
result interface name. Same shape as the Go `BucketSpec`-parameterized
`ListBucket`.

Proposed helper (in `ports/typescript/src/lib/projectsync.ts` — new
file, mirroring Go `internal/projectsync`):

```ts
export interface BucketSpec {
  bucket: string;        // "agents" | "skills"
  manifestName: string;  // "AGENT.md" | "SKILL.md"
}

export interface BucketEntry {
  name: string;
  description?: string;
  hasManifest: boolean;
}

export interface BucketListResult<E extends BucketEntry = BucketEntry> {
  scope: string;
  entries: E[];
}

export async function listBucket(
  scope: string,
  spec: BucketSpec,
  opts: { agentsHomeOverride?: string } = {},
): Promise<BucketListResult>;

export async function readFrontmatterDescription(
  manifestPath: string,
): Promise<string | undefined>;

export function stripOuterQuotes(value: string): string;
```

Call sites replaced:

- `runAgentsList` (agents.ts:57) — becomes ~6-line wrapper that calls
  `listBucket(scope, { bucket: "agents", manifestName: "AGENT.md" })`
  and renames `entries` → `agents` (preserving the existing public
  return shape `{ scope, agents: AgentEntry[] }`)
- `runSkillsList` (skills.ts:58) — same pattern, returns `{ scope, skills }`
- `readAgentDescription`, `readSkillDescription` — replaced by
  `readFrontmatterDescription`
- both copies of `stripOuterQuotes` — collapsed into the lib

The **public command exports** (`runAgentsList` / `runSkillsList` /
`runAgentsNew` / `runSkillsNew`) keep their existing return-shape
contracts. `runXNew` is not duplicated and is left in place untouched.

Coordination note: when this lands, confirm with the typescript-port
plan owner that no concurrent work touches agents.ts/skills.ts in the
same week to avoid revert-by-rebase.

Estimated dup-line removal: **~30 lines** (a 20-line block × 2 minus
the small wrapper bodies).

## Plan-level dup-line removal estimate

| Group | Decision | Lines removed |
|---|---|---|
| GS — graphstore SQL backends | EXTRACT (partial) | ~120 |
| RP — resource_plan self-dup | EXTRACT | ~60 |
| CMDS — commands/* CLI families | EXTRACT (settings/mcp/rules + sortedUniqueStrings); ACCEPT (explain/status/ux/import_plugins switches) | ~135 |
| MCP — internal/platform mcp_settings | EXTRACT | ~13 |
| TS — typescript port mirrors | EXTRACT | ~30 |

**Plan total: ~358 dup-lines removed.**

Combined with `go-test-fixture-extraction` (cluster D), this should
clear the gate margin per ADR-0008's option B math. If post-merge
SonarCloud still reports >3% on a future PR, the GS group's accepted
mcp_server.go intra-file blocks (53 lines combined) become the next
candidates for re-litigation.

## Done criteria

- All groups in scope have either (a) a shared helper landed and the
  duplicated bodies removed, or (b) an explicit "do not extract"
  classification in `design.md` with reasoning.
- `go test ./...` passes on the branch tip. Behavioral parity
  confirmed by every existing test still passing without modification
  beyond import-path changes.
- The eventual follow-up PR's SonarCloud snapshot reports
  Cluster E.other contribution reduced by ≥ 60% OR
  `new_duplicated_lines_density` below 3% (whichever first). The
  latter only achievable if `go-test-fixture-extraction` lands first
  or concurrently — see "Constraints".

## Constraints

- **Must NOT stack on PR#10.** Same reasoning as `go-test-fixture-extraction`:
  PR#10 is already 895 changed files.
- **Plan-level gate verification depends on cluster-D state.**
  Production-code dedup alone removes ~360 lines (≈ 0.6-0.8 pp);
  not enough to flip the gate. The math only works once Cluster D
  also lands. Order the merges accordingly: this plan's PR can ship
  before, after, or in parallel with `go-test-fixture-extraction`,
  but the gate-flip moment is whichever PR is second.
- **One group per task; one commit per file within a task** so any
  single extraction is bisectable and revertable independently.
