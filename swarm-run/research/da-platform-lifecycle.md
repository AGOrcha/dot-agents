# `da` (dot-agents) — Project / Platform Lifecycle Reference

Exhaustive, source-cited reference of the `da` CLI's project/platform lifecycle:
`init/add/remove/refresh/import/status/doctor/sync/install/session` + the
`skills/agents/hooks/rules/mcp/settings` resource commands + the platform
projection engine, managed gitignore contract, hook system, sentinels, and
hook-outcomes.

- **Repo:** `~/proj-docs/dot-agents`, Go module `github.com/AGOrcha/dot-agents`.
- **Installed binary:** `da version 0.4.2` (`/opt/homebrew/bin/da`). `bin/da` in-repo is ~1 day old.
- **Method:** recursed `da <cmd> --help` for every command below and read the implementing Go / shell. Every claim cites `path:line` (line numbers from the repo checkout at read time; treat as ±a few lines if the tree moves).
- **Divergences** between the shipped 0.4.2 binary and repo HEAD are called out inline and collected in [§13](#13-binary-vs-repo-divergences-read-before-designing-the-swarm).

---

## 1. The canonical store `~/.agents/` and scope model

`config.AgentsHome()` = `~/.agents` (overridable via env in `internal/config`). Everything `da` manages is projected FROM this store INTO consuming project repos.

### 1.1 Canonical buckets (`internal/platform/buckets.go`)

`CanonicalBucket` enum (`buckets.go:5-21`): `rules, settings, mcp, skills, agents, hooks, commands, output-styles, ignore, modes, plugins, themes, prompts`.

Two stages (`buckets.go:70-91`):
- **Stage-1** (`canonicalStoreStage1BucketSpecs`): `rules`, `settings`, `mcp`, `skills`(marker `SKILL.md`, dir-counted), `agents`(marker `AGENT.md`, dir-counted), `hooks`(marker `HOOK.yaml`, dir-counted).
- **Stage-2** (`canonicalStoreStage2BucketSpecs`): `commands`, `output-styles`, `ignore`, `modes`, `plugins`(marker `PluginManifestName`, dir-counted), `themes`, `prompts`.

Each bucket is scoped: `~/.agents/<bucket>/<scope>/…`, where `scope` is either `global` or a managed **project name** (`CanonicalBucketScopeRoot`, `buckets.go:47-49`). Registry keys are POSIX-slash-normalized (`CanonicalBucketPath`, `buckets.go:51-64`).

**Scope resolution order** for a project is `scopedNames(project) = [project, "global"]` — a project-scoped resource wins, global is the fallback that auto-resolves for every project (`internal/platform/resources.go`; documented at `agentsrc.go:983-994`). This is why `da install --generate` records only **project-scope** resources — globals resolve for free.

### 1.2 `~/.agents/` tree scaffolded by `da init` (`commands/internal/lifecycle/init.go:412-435`)

`createInitialAgentsDirs` MkdirAll's (idempotent):
```
~/.agents/
  resources/            # backup/restore mirror (see §6.7)
  rules/global/
  settings/global/
  mcp/global/
  skills/global/
  agents/global/
  hooks/global/
  <AgentsContextDir>/   # .agents context dir (config.AgentsContextDir())
  scripts/
  local/                # machine-local: bindings.json (id→abs path); NEVER synced
  <bucket>/global/      # for EVERY CanonicalStoreBucketSpec (stage1+stage2)
```
Then: `seedInitialConfig` writes `~/.agents/config.json` (`init.go:441-467`), `scaffoldStarterHomeAssets` copies embedded starters (missing-only, `init.go:397-399`), `scaffoldWorkflowAssets` copies starter hook bundles into `hooks/global` (`init.go:401-406`), `InitEnsureGlobalKGMCPConfigsFn` scaffolds KG MCP configs (`init.go:328-330`), and best-effort creates `config.AgentsStateDir()` (`init.go:343-347`).

### 1.3 Config / manifest / lock files

- **`~/.agents/config.json`** — the machine registry. `config.Config{Version, Projects map[string]Project, Agents map[string]Agent}` (`init.go:454-458`; struct in `internal/config/config.go`). Populated at init with each platform's detected state+version via `recordPlatformState` (`init.go:473-486`).
- **`~/.agents/local/bindings.json`** — machine-local `id → absolute-path` binding table; NEVER synced (see §9.2).
- **`.agentsrc.json`** (`config.AgentsRCFile`, `agentsrc.go:916`) — the committed, portable per-project manifest. Full shape in [§4.6](#46-install--the-manifest-agentsrcjson).
- **`.agentsrc.lock`** — resolved STATE (units + `inputs_digest` + a `refresh` section). NEVER committed-as-ignored (it is `neverIgnored`, see §9). Written by `config.EnsureResolved` and stamped by install/refresh.
- **`.agentsrc.local.json`** — machine-local overlay (the `.git/config` analog); always gitignored (`alwaysIgnored`, see §9).

---

## 2. Command tree and global flags (`da --help`)

```
da
├─ init        Initialize ~/.agents/ directory structure           (--from <git-url>)
├─ add         Add a project to da management                      (--name)
├─ remove      Remove a project from da management                 (--clean)
├─ refresh     Refresh managed setup in projects from ~/.agents/   (--import, --inexact)
├─ import      Import configs from project/global scope            (--scope project|global|all)
├─ status      Show managed projects and link health               (--audit, --agent)
├─ doctor      Check installations, validate links, detect issues
├─ skills      list | new | promote
├─ agents      list | new | promote | import | remove
├─ hooks       list | show | remove
├─ rules       list | show | remove
├─ mcp         list | show | remove
├─ settings    list | show | remove
├─ config      (explain/sync/lint/verify/migrate — sibling scope, not covered here)
├─ review      Review pending workflow proposals
├─ sync        init | commit | pull | push | status | log
├─ explain     Explain da concepts
├─ install     Set up project from .agentsrc.json                  (--generate, --strict, --inexact, --force)
├─ session     stats
├─ workflow    (hook-sentinel, hook-outcome, plan, advance, … — see §8/§11)
├─ kg | score | help | completion
```

**Persistent global flags** (apply to every command; `commands/root.go:193-199`):

| Flag | Short | Meaning |
|---|---|---|
| `--dry-run` | `-n` | Show what would be done, no changes |
| `--force` | `-f` | Overwrite existing configurations |
| `--json` | | Output as JSON (honored only where a command implements it) |
| `--verbose` | `-v` | Detailed output |
| `--yes` | `-y` | Auto-confirm prompts |

> Swarm note: `--json` is a *persistent* flag but not every command emits JSON. `status` does; `doctor` does **not** (§4.8). `hook-sentinel read` and most workflow commands do.

---

## 3. The platform projection model (the heart)

Six platforms. `platform.All()` order (`internal/platform/platform.go:75-84`): **Cursor, Claude, Codex, OpenCode, Copilot, Antigravity**. `platform.ByID(id)` looks one up (`platform.go:87-94`). **`omp` is NOT a platform** — see [§13](#13-binary-vs-repo-divergences-read-before-designing-the-swarm).

### 3.1 The `Platform` interface (`platform.go:48-72`)

```
ID() string                                       // "cursor" | "claude" | ...
DisplayName() string
IsInstalled() bool                                // CLI-on-PATH / app probe
Version() string
CreateLinks(project, repoPath string) error       // materialize repo-local managed paths
RemoveLinks(project, repoPath string) error       // teardown
HasDeprecatedFormat(repoPath) bool / DeprecatedDetails(repoPath) string
SharedTargetIntents(project) ([]ResourceIntent, error)   // cross-platform shared targets
```
Optional read-side interfaces a platform *may* also implement: `SessionReader` (agent identity, `platform.go:11-24`), `StatsReader` (`platform.go:29-31`), `SessionTokenScanner` (`platform.go:37-39`), `BranchSessionFinder` (`platform.go:44-46`).

**Two projection paths:**
1. `CreateLinks` — platform-OWNED repo-local links + rendered configs (rules, settings, mcp, ignore, per-platform hooks, user-home mirrors).
2. `SharedTargetIntents` → merged `ResourcePlan` → executed centrally — cross-platform shared targets (skills, agents, plugins) so compatible targets dedupe and conflicts are caught before any write.

### 3.2 `ResourceIntent` (`internal/platform/resource_intent.go`)

The declarative unit of a shared-target projection (`resource_intent.go:98-117`). Enums:

| Field | Values | Source |
|---|---|---|
| `Ownership` | `shared_repo` \| `platform_repo` \| `user_home` | `:8-14` |
| `Shape` | `direct_dir` \| `direct_file` \| `render_single` \| `render_fanout` | `:16-23` |
| `Transport` | `symlink` \| `hardlink` \| `write` | `:25-31` |
| `ReplacePolicy` | `never` \| `if_managed` \| `allowlisted_imported_dir_only` | `:33-39` |
| `PrunePolicy` | `none` \| `target_only` \| `generated_children` | `:41-47` |
| `SourceKind` | `canonical_file` \| `canonical_dir` \| `canonical_bundle` | `:49-55` |

Validation enforces the shape↔transport matrix: direct shapes must NOT use `write`; render shapes MUST use `write` (`validateShapeTransport`, `resource_intent.go:217-229`). `EffectiveConflictKey()` = `ConflictKey` or falls back to `TargetPath` (`:119-124`).

### 3.3 Shared-target plan: build → execute → prune (`internal/platform/resource_plan.go`)

- `collectSharedTargetIntents(project, platforms)` gathers `SharedTargetIntents` from every platform (`:538-548`).
- `BuildSharedTargetPlan` → `BuildResourcePlan` dedupes by conflict key and rejects incompatible collisions (`resourceIntentCompatible` compares every field, `:95-111`).
- `RunSharedTargetProjection` — additive write path (dry-run preview or apply) (`:568-573`).
- **`RunSharedTargetProjectionExact(project, repoPath, platforms, dryRun, exact)`** (`:591-615`) — the **default** projection for refresh/install. When `exact=true`: `executeResourcePlan` then `PruneStaleSharedTargets` so the repo converges to EXACTLY what the plan declares. `exact=false` (`--inexact`) falls back to additive `RunSharedTargetProjection`.
- `PruneStaleSharedTargets` (`:632-662`) scans ONLY directories owning ≥1 `ResourcePruneTarget` intent, and deletes entries that are (a) not a wanted target and (b) a managed link under `agentsHome`. User files / links pointing outside `agentsHome` are never touched. Errors are `errors.Join`-aggregated so one stuck removal cannot hide the rest.
- `CollectAndExecuteSharedTargetPlan` (`:732-744`) is the apply-only entry used by add/refresh/install *before* `CreateLinks` runs.
- `RemoveSharedTargetPlan` / `RemoveSharedTargets` (`:746-771`) mirror teardown for `da remove`.

**Shared mirror intent shape** (`buildSharedMirrorIntentsForRoot`, `:317-353`): for each canonical entry under `~/.agents/<bucket>/<project>/` owning the marker, emit `Shape=direct_dir`, `Transport=symlink`, `ReplacePolicy=allowlisted_imported_dir_only`, `PrunePolicy=target_only`, `MarkerFiles=[<marker>]`. A missing bucket dir (ENOENT) yields zero intents (legit empty), other errors propagate.

**Allowlist for destructive imported-dir replacement** (`isAllowlistedSharedMirrorTarget`, `:267-278`): `.agents/skills/`, `.claude/skills/`, `.claude/agents/`, `.codex/agents/`, `.opencode/plugins/`, `.opencode/agent/`, `.github/agents/`, `.antigravity/skills/`, `.antigravity/agents/`. A replace outside this list errors; a replace inside requires the marker file present or refuses (`removeImportedDirIfAllowlisted`, `:252-265`).

Intent builders: `BuildSharedSkillMirrorIntents` (`:280-294`), `BuildSharedAgentMirrorIntents` (dir mirror, `:402-418`), `BuildSharedAgentFileSymlinkIntents` (per-`AGENT.md` file symlink to `<root>/<name><suffix>`, `:444-484`), `BuildSharedCodexAgentTomlIntents` (render `.codex/agents/<name>.toml`, `:493-526`), `BuildSharedPluginBundleIntents` (`:365-385`).

### 3.4 Managed-link primitives (`internal/links/links.go`)

The link contract NEVER destroys unmanaged user data:
- `Symlink(target, linkPath)` / `Hardlink` — idempotent for managed state; an unmanaged occupant (regular file / non-empty dir / user-owned link) yields `ErrUnmanagedTarget` (`links.go:82-104,189-198`).
- `SymlinkReplacing` / `HardlinkReplacing(…, backup func(path) error)` — back up the occupant first; a backup failure aborts and preserves the file (`links.go:106-115,200-205`).
- **"Managed link" = OS-specific**: POSIX symlink; Windows dir-junction (dirs) / hard link (files). Hard links have no reparse point → identity via inode/link-count (`ownedManagedHardlink`, `:23-41`; `IsManagedFileLink`, `:396-429`).
- `ManagedLinkTarget` (`:238-249`), `IsManagedLink`/`IsManagedLinkUnder` (`:251-312`), `RemoveIfSymlinkUnder` (`:347-358`), `RemoveIfHardlinkedToAny` (returns `(matched, err)`, `:360-394`). `pathUnder` uses `filepath.Rel` boundary check, not raw prefix, so `.agents-old` is not treated as under `.agents` (`:314-334`).

### 3.5 Per-platform projection — repo-local outputs

Legend: **[sym]** = managed symlink to `~/.agents` canonical; **[hard]** = managed hardlink; **[gen]** = rendered/written file; **[shared]** = produced by the shared-target plan, not `CreateLinks`.

#### Claude — `id="claude"`, "Claude Code" (`internal/platform/claude.go`)
- Install probe: `probeInstalled("claude")` on PATH; `~/.claude` is deliberately NOT an install signal (`claude.go:234-242`).
- Repo outputs:
  - `.claude/rules/<scope>--<stem>.md` **[sym]** → `~/.agents/rules/<scope>/<name>.(md|mdc|txt)` (ext stripped, link always `.md`) (`:332-390`).
  - `.claude/settings.local.json` — **[gen]** (`renderClaudeHookSettings`) when canonical hook bundles exist, else **[sym]** to legacy `claude-code.json` (`:294-329`; `hooks.go:367-406`).
  - `.mcp.json` **[sym]** → first of `~/.agents/mcp/<scope>/{claude.json,mcp.json}` (project then global) (`:320-329`).
  - `.claude/agents/<name>` **[shared]** dir mirror, `.agents/agents/<name>` healed (`:537-544`).
  - `.claude/skills/*`, `.agents/skills/*` **[shared]** (`:547-551`).
- User-home: `~/.claude/CLAUDE.md` **[sym]** (candidates `claude-code.mdc|claude-code.md|rules.mdc|rules.md|rules.txt`), `~/.claude/settings.json` **[gen or sym]**, `~/.claude/agents/<name>` **[sym]**, `~/.claude/skills/<name>` **[sym]** (`:446-526`).
- `SharedTargetIntents`: `BuildSharedSkillMirrorIntents(project, ".claude/skills", ".agents/skills")` + `BuildSharedAgentMirrorIntents(project, ".claude/agents")` (`:767-777`).

#### Codex — `id="codex"`, "Codex CLI" (`internal/platform/codex.go`)
- Install probe: `probeInstalled("codex")` (`codex.go:100-105`).
- Repo outputs:
  - `AGENTS.md` **[sym]** → first of `~/.agents/rules/global/{agents.md,agents.mdc,rules.md,rules.mdc}` then project `agents.md|agents.mdc` (`:154-196`).
  - `.codex/config.toml` **[sym]** → `~/.agents/settings/<project>/codex.toml` (`:198-207`).
  - `.codex/hooks.json` — **[gen]** (`renderCodexHookConfig`) when canonical bundles exist, else **[sym]** legacy `codex.json|codex-hooks.json` (`:259-297`).
  - `.codex/agents/<name>.toml` **[shared]** (`Shape=render_single`, `Transport=write`, `codexAgentTomlMaterializer`); `CreateLinks` only PRUNES stale ones (`:242-245,303-317`).
  - `.agents/skills/*` **[shared]**.
- User-home: `~/.codex/agents/*.toml` generated from global agents, `~/.codex/hooks.json` **[gen/sym]**, `~/.agents/skills/<name>` mirror.
- `SharedTargetIntents`: `BuildSharedSkillMirrorIntents(project, ".agents/skills")` + `BuildSharedCodexAgentTomlIntents(project)` (`:621-629`). Codex TOML render writes `name`, `description`, optional `model`, `developer_instructions="""…"""` from AGENT.md body (`:438-487`).

#### Cursor — `id="cursor"`, "Cursor" (`internal/platform/cursor.go`)
- Install probe: `/Applications/Cursor.app` exists, else `probeInstalled("agent"||"cursor")` (`cursor.go:149-155`). Version prefers app-bundle Info.plist (`:157-201`).
- Repo outputs (`CreateLinks` order: rules, settings, mcp, ignore, agents, hooks — `:243-262`):
  - `.cursor/rules/<scope>--<name>.mdc` **[hard]** (`.md`→`.mdc` normalized; `global--`/`<project>--` prefixed) (`:266-319`).
  - `.cursor/settings.json` **[hard]** → `~/.agents/settings/<project>/cursor.json` (`:344-353`).
  - `.cursor/mcp.json` **[hard]** → `~/.agents/mcp/<scope>/{cursor.json,mcp.json}` (`:357-367`).
  - `.cursorignore` **[hard]** → `~/.agents/settings/<project>/cursorignore` (`:369-375`).
  - `.cursor/hooks.json` — **[gen]** (`renderCursorHookConfig`) when canonical bundles exist, else **[hard]** legacy `cursor.json` (`:378-416`).
  - `.claude/agents/<name>` **[shared]** — Cursor mirrors agents into the `.claude/agents` root, not `.cursor/agents` (`:638-640`).
- User-home: `~/.cursor/hooks.json` **[gen/hard]** (global only).

#### Copilot — `id="copilot"`, "GitHub Copilot" (`internal/platform/copilot.go`)
- Install probe: scans `~/.vscode{,-insiders,-server}/extensions` for `copilot`, else `probeInstalled("copilot")` (`copilot.go:129-148`).
- Repo outputs (`CreateLinks` order: instructions, skills, agents, mcp, Claude-compat settings, repo hooks, user hooks — `:187-219`):
  - `.github/copilot-instructions.md` **[sym]** → `rules/<scope>/copilot-instructions.md` then `rules/<scope>/{rules.md,rules.mdc,rules.txt}` (`:225-277`).
  - `.vscode/mcp.json` **[sym]** → `~/.agents/mcp/<scope>/{copilot.json,mcp.json}` (`:288-296`).
  - `.claude/settings.local.json` — **[gen]** Claude-shaped hook settings when bundles exist, else **[sym]** legacy `claude-code.json` (`:299-323`).
  - `.github/hooks/*.json` — **[gen]** per-event fanout when bundles exist, else legacy symlink fanout (`:325-380`).
  - `.agents/skills/*` **[shared]** (`copilotAgentsDir/skills`), `.github/agents/<name>.agent.md` **[shared]** file-symlink from `AGENT.md` (`:620-626`; `resource_plan.go:444-477`).
- User-home: `~/.copilot/hooks/*.json` **[gen fanout]** (global only). ⚠ `RemoveLinks` does NOT clean the user-home fanout (`copilot.go` teardown `:430-523`).

#### OpenCode — `id="opencode"`, "OpenCode" (`internal/platform/opencode.go`)
- Install probe: `probeInstalled("opencode")` (`opencode.go:97-102`).
- Repo outputs: `opencode.json` **[sym]** → `~/.agents/settings/<scope>/opencode.json` (`:108-118`). `.opencode/agent/*.md` **[shared]** (file-symlink from `AGENT.md`), `.agents/skills/*` **[shared]**, `.opencode/plugins/*` **[shared]** (`:245-258`).
- User-home: `~/.opencode/agent/` from `~/.agents/agents/global/AGENT.md` (`:140-145`).
- No opencode-specific hook renderer / event table (`[INFERENCE]`, `:108-124`).

#### Antigravity — `id="antigravity"`, "Antigravity" (Google's coding harness, successor to Gemini CLI) (`internal/platform/antigravity.go`)
- Uses a dedicated `.antigravity/` repo root to avoid colliding with dot-agents' own `.agents/` (`antigravity.go:16-31`). Canonical source filename is `antigravity.json`.
- Repo outputs: `.antigravity/settings.json` **[hard]** ← `settings/<scope>/antigravity.json`; `.antigravity/mcp_config.json` **[hard]** ← `mcp/<scope>/antigravity.json`; `.antigravity/hooks.json` **[gen/hard]** (`renderAntigravityHookConfig`); `.antigravity/skills/*` + `.antigravity/agents/*` **[shared]** (`:75-155,205-213`).
- User-home: `~/.antigravity/hooks.json` (global only).

### 3.6 Import mapping: repo-relative → canonical (`commands/internal/lifecycle/resource_map.go`)

`MapResourceRelToDest(project, relPath)` (`:74-101`) inverts projection so `da import` can canonicalize hand-edited platform files. Exact-file map (`mapExactRel`, `:107-133`):

| repo-relative | canonical dest |
|---|---|
| `.cursor/settings.json` | `settings/<project>/cursor.json` |
| `.cursor/mcp.json`, `.mcp.json`, `.vscode/mcp.json` | `mcp/<project>/mcp.json` |
| `.cursor/hooks.json` | `hooks/<project>/cursor.json` |
| `.cursorignore` | `settings/<project>/cursorignore` |
| `.claude/settings.local.json` | `settings/<project>/claude-code.json` |
| `opencode.json` | `settings/<project>/opencode.json` |
| `AGENTS.md`, `.codex/instructions.md`, `.codex/rules.md` | `rules/<project>/agents.md` |
| `.codex/config.toml` | `settings/<project>/codex.toml` |
| `.codex/hooks.json` | `hooks/<project>/codex.json` |
| `.github/copilot-instructions.md` | `rules/<project>/copilot-instructions.md` |
| `.github/hooks/<name>.json` | `hooks/<project>/<name>/HOOK.yaml` (`mapHooksRel`, `:208-215`) |

Dir-prefix → bucket (`bucketDirMapping`, `:138-149`): `.cursor|.claude|.opencode/commands/`→`commands`; `.claude/output-styles/`→`output-styles`; `.opencode/modes|themes/`→`modes|themes`; `.github/prompts/`→`prompts`. Plus `.cursor/rules/`→`rules` (`mapCursorRulesRel`), `{.agents,.claude}/skills/`→`skills/<project>`, `{.github,.codex,.opencode}/agents/`→`agents/<project>`. Pass-through canonical prefixes at `:219-233`.

---

## 4. Lifecycle commands

### 4.1 `da init` (`commands/internal/lifecycle/init.go`)
Creates `~/.agents/`. Safe to re-run; existing files preserved unless `--force`.
- Flow (`runInit`, `:276-357`): if `--from` → `runInitFrom` (§4.1a); else header → `warnLegacyManifestInCwd` → `reportExistingInstall` (halts if home exists and no `--force`, `:259-274`) → `--dry-run` short-circuit → confirm unless `--yes` → create dirs (§1.2) → `seedInitialConfig` → scaffold starters → scaffold workflow hook bundles → KG MCP configs → `linkClaudeGlobalSettings` (`~/.claude/settings.json`; hooks/ over settings/, backup-preserving under `--force`, `:488-518`) → `linkCursorGlobalHooks` (`~/.cursor/hooks.json`, `:520-545`) → state dir.
- `--force` overwrites config.json + guarded managed links (backup-preserving); it does NOT wipe the tree.
- No command-local `--json` in this checkout (`[INFERENCE]`; only the root persistent `--json`).

### 4.1a `da init --from <git-url>` (`init_from.go`)
L3 cross-machine bootstrap: clones a remote home into `~/.agents` via a temp staging clone + atomic adoption + rebind, instead of scaffolding from embedded starters (`runInitFrom`, dispatched at `init.go:280-282`). Refuses embedded credentials; `--dry-run` prints the adoption plan.

### 4.2 `da add <path> [--name]` (`commands/add.go`)
Registers a project and links config. `runAdd` (`:233-285`) steps:
1. `resolveAddTarget` — validate path, derive name (or `--name`), validate identifier (`:290-304`).
2. `checkAddNotAlreadyRegistered` — guard; `--force` downgrades to warning (`:328-341`).
3. Preview: canonical `~/.agents/` tree + per-platform link table (installed detection) + "Files to Replace" + discovered AI configs (`:417-525`). `scanExistingAIConfigs` walks the tree, excludes `*.dot-agents-backup` (`:149-168`).
4. Confirm unless `--yes` (`:527-542`).
5. `backupAddExistingFiles` — Step 3: `BackupExistingConfigsList` into `~/.agents/resources/<project>/` (fail-closed; §6.7) (`:544-565`).
6. `scaffoldAddProjectDirs` — `CreateProjectDirs` + restore-from-resources + KG MCP configs (`:567-594`).
7. `createAddLinks` — Step 5: `CollectAndExecuteSharedTargetPlan` then each installed platform's `CreateLinks`; any failure aborts (no false success, no registration) (`:596-632`).
8. `registerAddedProject` — persist to config.json. A synced portable identity present without a local binding → `BindProject` (writes only machine-local path) rather than `AddProject` (`:634-655`).

`CreateProjectDirs` makes `~/.agents/{rules,settings,mcp,skills,agents,hooks}/<project>` (`internal/projectsync/projectsync.go:52-68`).

### 4.3 `da remove <project> [--clean]` (`commands/remove.go`)
Teardown (`runRemove`, `:58-…`; scout-confirmed):
- Resolve name→path; unknown → `project not found` + hint. Preview enumerates removals; warns on manifest git sources + prints cache-clean hint. `--dry-run` stops before mutation.
- Confirm skipped if `--yes` or `--force`; else `ui.Confirm("Proceed with removal?", false)`.
- `removeProjectLinks` (`:208-259`): set Windows mirror ctx → `platform.RemoveSharedTargetPlan(name, path, installed)` → `RemoveLinks` on EVERY `platform.All()`.
- `cleanProjectCanonicalDirs` (`:260-319`): default EMPTIES contents of `~/.agents/{rules,settings,mcp,hooks,skills,agents}/<project>` keeping the dirs; `--clean` removes the dirs (`removeProjectDirs`, `errors.Join`).
- `cfg.RemoveProject` + `cfg.Save` only after all prior steps succeed.

### 4.4 `da refresh [project] [--import] [--inexact]` (`commands/refresh.go`, `commands/internal/lifecycle/refresh.go`)
Re-applies links/config from `~/.agents/` into projects. Flow (`runRefresh`, `refresh.go:65-131`):
1. `--import` → run import first (`runImportFromRefresh`, scope from `refreshImportScope`).
2. Load config; no projects → info+return.
3. `reportEnabledPlatforms` + **`DetectAndEnableNewPlatforms`** — re-probes every platform; flips installed-but-disabled to enabled and refreshes recorded versions; NEVER auto-disables (`lifecycle/refresh.go:30-43`). `cfg.Save()`.
4. Per project: `refreshOneProject` (`:246-267`): `CreateProjectDirs` + restore-from-resources → **`ensureLockFreshForRefresh`** (`config.EnsureResolved`, LOCAL scopes only; manifest-gated; dry-run skips; upstream re-check is `da config sync`, never refresh — `:279-289`) → `SetWindowsMirrorContext` → **`runSharedTargetsForRefresh`** (`RunSharedTargetProjectionExact(..., !refreshInexact)`; EXACT/PRUNE by default — `:302-316`) → `recreatePlatformLinks` (CreateLinks per enabled+installed platform) → `finalizeProjectRefresh` writes the refresh stamp into `.agentsrc.lock` (`projectsync.WriteRefreshToLock`).
- Partial application withholds the success stamp and returns an error listing failed projects.
- **`--inexact`**: additive (write wanted set, leave stale managed outputs) instead of EXACT/PRUNE (`refresh.go:22-30`).
- ⚠ **`da refresh` does NOT write the managed `.gitignore` block** — see §9.4.

### 4.5 `da import [project] [--scope project|global|all]` (`commands/import.go`)
Scans project-managed files + user-level AI config and copies them into canonical `~/.agents/` (using the §3.6 reverse map). Hook imports are normalized to `~/.agents/hooks/<scope>/<name>/HOOK.yaml` bundles when the source can be normalized. Adopt/normalize path; then a relink pass. `--scope` default `all`. (`import.go`; registers `RestoreCanonicalResourceFileFn` at init, `import.go:1072-1085`.)

### 4.6 `da install [--generate] [--strict] [--inexact] [--force]` (`commands/internal/lifecycle/install.go`)

Manifest→home orchestration. `RunInstall(strict, deps)` (`install.go:124-174`):
1. `Getwd` → header → `loadInstallManifest` (`.agentsrc.json`) → `ensureAgentsHomeInitialized`.
2. `installProjectName` (manifest `project` or dir base).
3. **`ensureInstallResolved`** → `config.EnsureResolved` (lock half).
4. `resolveInstallSources` — resolve each `Source` to a local root; git sources cloned/updated into cache (`fetchGitSource`, `:499-524`); `--strict` fails if any declared source is missing.
5. `linkInstallResources` — materialize declared skills/agents from sources into `~/.agents/…` (`LinkResourceFromSources`, `:609-634`).
6. `ensureInstallProjectDirs` + `RegisterInstallProject` (upsert config.json, honors `--dry-run`, `:268-299`).
7. `createInstallPlatformLinks` — `runInstallSharedTargets` (**EXACT/PRUNE by default**, `--inexact` opts out via `installInexact`, `:43-51`) + `CreateLinks` per platform.
8. `finalizeInstall` — stamps the `install` section into `.agentsrc.lock` (`installLockSection="install"`, `:41,364-386`).

**The manifest `AgentsRC`** (`internal/config/agentsrc.go:266-372`) core fields: `$schema`, `version`, `project`, `skills[]`, `rules[]`, `agents[]`, `hooks` (StringsOrBool), `mcp` (StringsOrBool), `settings` (bool), `sources[]`, `kg`. v2 additive (omitempty): `repo_id`, `extends[]`, `packages[]`, `features`, `execution_profile`, `pr_source`, `stage_profiles`, `precondition_policies`, `locks`, `authority_grants`, `manifests`, plus `ExtraFields` (unknown keys round-tripped) and `LegacyKeys`. `StringsOrBool` marshals as bool (`true`=all/`false`=none) or `[names]` (`:159-236`).

#### `da install --generate` (`RunInstallGenerate`, `install.go:390-447`)
Creates/refreshes `.agentsrc.json` from current `~/.agents/` state:
- Derive project name (`FindProjectByPath` or dir base).
- `config.GenerateAgentsRC(name, path)` — **PROJECT-SCOPE ONLY** scan of skills/agents/rules/hooks/mcp/settings; fail-or-full (real I/O errors aggregate, never silently degrade) (`agentsrc.go:973-1042`).
- If a manifest exists, **`MergeGenerateAgentsRC(existing, generated)`** (`:1050-1090`): scan-derived lists come from generated; **preserved from existing**: non-empty `project`, `repo_id` (protected), `ExtraFields` (unknown keys), `stage_profiles`, `manifests`, and unioned `sources` (dedup so the default local source isn't duplicated). `--dry-run` prints the would-write summary.
- `--generate --force` documented in help; regeneration replaces stale skill/platform lists while preserving the above.

### 4.7 `da status [--audit] [--agent <p>]` (`commands/internal/lifecycle/status.go`)
Fleet/link-health reporter. Text mode prints: header, `~/.agents/` path, git status, canonical-store summary (`✓ <bucket> N scope(s), M item(s)`), user-config summary, per-project platform badges (`✓` healthy / `!` broken / dim `-` disabled-or-not-installed) (`status.go:388-465`). `--audit` adds file-level detail (`✓ …→…`, `✗ …→… (broken)`, `○ … (local file)`, `:20-34,699-753`) + shared-target registry.
- Broken-link detection: `managedLinkBroken` via `links.ManagedLinkTarget` + `os.Stat(resolved)` (`:122-146`).
- `--agent` filters PLATFORMS (`cursor|claude|codex|opencode|copilot`), not projects.
- **`--json`** report (`:46-84`): top-level `agents_home`, `git{initialized,branch,remote}`, `canonical_store` (map bucket→`{scopes,items}`), `plugins[]` (omitempty), `user_config[]` (`{name,present,broken}`), `projects[]` (`{name,path,path_exists,platforms[]}`).

### 4.8 `da doctor` (`commands/internal/lifecycle/doctor.go`)
**Read-only** diagnostics (never repairs). Checks in order (`doctor.go:93-503`): install status (`~/.agents/` + config.json), platform inventory (version/`installed`/`(not installed)`), user-config link health, config load, project inventory (missing dirs flagged), link health (healthy/broken per project), manifest health (`no manifest → da install --generate`; corrupt; unfetched git sources → `da install`; legacy-v1 → `da config migrate`), lockfile health (`declares units but has no .agentsrc.lock → da install`; per-unit drift → `da config sync`), orphan canonical resources, plugin health. Footer on broken links: `Run 'da refresh' to relink …`. **No `--json` mode** (`[INFERENCE]`, `:61-123`); always text, always returns nil.

### 4.9 `da session stats` (`commands/session_stats.go`)
Reads pre-aggregated usage stats from each installed platform implementing `StatsReader`/`SessionReader` (`internal/platform/session.go`, `stats.go`). Renders tokens-by-model, recent sessions, commit attribution, per-platform stats (`session_stats.go:32-133`). `NewSessionCmd`→`newSessionStatsCmd` (`:15-30`).

### 4.10 `da sync <cmd>` — git ops on `~/.agents/` (`commands/sync/`)
All run against `config.AgentsHome()` via `git -C <home>`:

| Subcommand | Git operation(s) | Notes / source |
|---|---|---|
| `init` | `git init` → write `.gitignore` (`ensureSyncGitignore`) → `git add .` → `git commit -m "Initial commit"` | `initSyncRepo`, `init.go:85-111`. Missing git user.email/name → actionable error. Existing repo → prints remote/next-steps. |
| `commit [msg] [-m]` | `git add -A` → `git commit -m <msg>` | `commit.go:53-74`. Default msg `Update ~/.agents/ configuration` (`resolveCommitMessage`, `:40-48`). `--dry-run` prints the two commands. "nothing to commit" is non-fatal. |
| `push [-m]` | print pending → `git add -A`+`git commit` → confirm (unless `-y`/`-f`) → `git push` | `push.go:30-62`. `--dry-run` guard before commit. |
| `pull` | `git pull` → offer `da refresh` | `pull.go:12-29`. **`--dry-run` is rejected** (git pull would still run). `postPullRefresh` prompts refresh; hints git-source projects need `da install`. |
| `status` | branch + remote + ahead/behind + `git status --porcelain` summary | `status.go:9-25`; `helpers.go`. |
| `log` | `git log --oneline --decorate -n 10` | `log.go:12-26`. |

**Sync gitignore** (`syncGitignoreEntries`, `init.go:113-119`): `local/`, `cache/`, `*.dot-agents-backup` — the machine-local boundary that must never enter the synced tree. `untrackMachineLocalState` runs `git rm --cached --ignore-unmatch` on already-tracked `local/`+`cache/` (`:156-170`). **This is a DIFFERENT gitignore from the consuming-project managed block in §9** — that one lives in each consuming repo, this one lives in `~/.agents` itself.

---

## 5. Resource commands (`skills`, `agents`, `hooks`, `rules`, `mcp`, `settings`)

All operate on the canonical store; `remove` deletes canonical storage ONLY (not repo links) and tells you to run `refresh`/`install` afterward.

### 5.1 `da skills` (`commands/skills/`)
- `list [project]` — list skills in `~/.agents/skills/<scope>/`.
- `new <name> [project]` — scaffold a new skill.
- `promote <name>` — promote repo `.agents/skills/<name>/` → `~/.agents/skills/<project>/<name>/`, register in `.agentsrc.json`, refresh shared skill mirrors for all platforms (`da skills promote --help`).

### 5.2 `da agents` (`commands/agents/`)
- `list [project]`; `new <name> [project]`.
- `promote <name> [--force]` — repo `.agents/agents/<name>/` → `~/.agents/agents/<project>/<name>/`, register in `.agentsrc.json`, ensure `.claude/agents/` symlinks. `--force` replaces an existing real dir (destructive).
- `import <name>` — reverse of promote: link a canonical `~/.agents/agents/<project>/<name>/` INTO the repo (`.agents/agents/` + `.claude/agents/`), register if absent.
- `remove <name> [--purge]` — unlink repo symlinks + drop from `.agentsrc.json agents[]`. Canonical dir kept unless `--purge` (prompts unless `--yes`).

### 5.3 `da hooks` (`commands/hooks/`)
- `list [scope]` — canonical bundles (`hooks/<scope>/<name>/HOOK.yaml`) + legacy single-file `hooks/<scope>/<name>.json` (`runHooksList`, `list.go:15-59`; legacy claude-settings listing `:61-94`).
- `show <scope> <name>` — one bundle or legacy file.
- `remove <scope> <name>` — deletes managed hook STORAGE only (not project symlinks); run refresh/install after.

### 5.4 `da rules` / `da mcp` / `da settings` (`commands/internal/{rules,mcp,settings}` + `commands/internal/cmdutil`)
Share the canonical `list|show|remove` machinery (`cmdutil.NewCanonicalResourceCmd`, `canonical_cmd.go:26-102`). Operate on `~/.agents/<bucket>/<scope>/`; `list` defaults scope to `global` when omitted (`:56-71`). `show <scope> <name>` prints metadata (rules adds descriptions). `remove <scope> <name>` deletes the canonical file ONLY; repo links stay until refresh/install.
- `mcp` sources per platform normalize into `mcp/<scope>/{cursor.json|copilot.json|claude.json|mcp.json|antigravity.json}`.
- `settings` holds `cursor.json`, `claude-code.json`, `codex.toml`, `opencode.json`, `cursorignore`, `antigravity.json`, etc.

---

## 6. The hook system (declarative)

### 6.1 `HookSpec` + `HOOK.yaml` manifest (`internal/platform/hooks.go`)

`HookSpec` (`hooks.go:57-79`): `Name, Scope, SourcePath, SourceBucket, SourceKind, Description, When, WhenEvents[], MatchTools[], MatchExpression, Command, TimeoutMS, EnabledOn[], RequiredOn[], PlatformOverrides map[string]HookPlatformOverride`.

`HOOK.yaml` manifest (`hookManifest`, `:81-101`):
```yaml
name: <logical-name>
description: |- ...
when: <canonical-event>            # scalar — OR:
when_events: [<event>, ...]        # array (mutually exclusive with `when`)
match:
  tools: [Bash, ...]
  expression: "<regex/matcher>"
run:
  command: ./gate.sh               # relative → resolved against HOOK.yaml dir
  timeout_ms: 8000
enabled_on: [claude, codex, cursor, copilot, antigravity, omp]
required_on: [<platform>, ...]
platform_overrides:
  <platform>: {event: ..., matcher: ..., file: ...}
```
- `when`/`when_events` mutual exclusion + duplicate/unknown-event rejection at load (`validateHookWhenEvents`, `:584-633`; `isKnownCanonicalEvent`, `:640-647`).
- `SourceKind`: `legacy_file` | `canonical_bundle` (`:20-23`). `collectCanonicalHookSpecsForPlatform` keeps only bundle specs enabled on the platform (`:268-297`).

### 6.2 Shapes / transports / emission (`hooks.go`)
- `HookShape`: `direct | render_single | render_fanout` (`:25-31`). `HookTransport`: `symlink | hardlink | write` (`:33-39`). Modes `directSymlinkHookMode`, `directHardlinkHookMode` (`:46-49`).
- **`emitPreferredHookFile`** precedence (`:367-389`): first non-empty canonical bundle set → **render**; else legacy spec → **direct emit** (sym/hard); else → remove the rendered file. This is why a `.claude/settings.json`/`.codex/hooks.json`/`.cursor/hooks.json` is a generated file when bundles exist and a plain symlink otherwise.
- `enabled_on` gate (`hookEnabledOnPlatform`, `:669-679`): empty = all platforms; else must contain platform id. `required_on` (`:681-688`) makes an unmapped event an ERROR instead of a skip.
- `matcherForSpec` precedence (`:715-726`): platform override matcher → `MatchExpression` → `MatchTools` joined `|` → fallback. `ResolveHookCommand` resolves `./`/`../` command paths against the HOOK.yaml dir (`:690-706`).
- Fanout: `expandHookSpecForFanout` appends `-<event>` to the name so multi-event hooks land at distinct paths (`gate-pre_tool_use.json`, `gate-stop.json`) (`:500-516`).

### 6.3 Rendered JSON shapes
- **Claude** `claudeRenderedHooks{$schema, hooks: {VendorEvent: [{matcher, hooks:[{type:"command", command}]}]}}` (`:103-116,903-951`). `$schema=https://json.schemastore.org/claude-code-settings.json`.
- **Codex** `codexRenderedHooks{hooks: {…}}` (no `$schema`); matcher emitted ONLY for `codexMatcherWhitelist` events (`PermissionRequest, PostCompact, PostToolUse, PreCompact, PreToolUse, SessionStart, SubagentStart, SubagentStop`), else `matcher=""` (`:118-120,953-1031`).
- **Cursor** `cursorRenderedHooks{version:1, hooks:{event:[{command, matcher?, timeout?}]}}` (`:122-131,1033-1051`).
- **Copilot** `copilotRenderedHooks{version:1, hooks:{event:[{type, bash, timeoutSec?}]}}`; one file per spec/event; rejects matcher-constrained hooks (`:133-142,1081-1126`).
- **Antigravity** `{hooks:{VendorEvent:[{matcher, hooks:[{type, command, timeout?}]}]}}`; TimeoutMS→seconds rounded up (`antigravity.go:317-384`).

### 6.4 Canonical → vendor event tables (`hooks.go`, `docs/HOOKS.md:91-104`)

| canonical `When` | Claude | Codex | Cursor | Copilot | Antigravity |
|---|---|---|---|---|---|
| `pre_tool_use` | `PreToolUse` | `PreToolUse` | `preToolUse` | `preToolUse` | `PreToolUse` |
| `post_tool_use` | `PostToolUse` | `PostToolUse` | `postToolUse` | `postToolUse` | `PostToolUse` |
| `post_tool_use_failure` | `PostToolUseFailure` | — | `postToolUseFailure` | `postToolUseFailure` | — |
| `user_prompt_submit` | `UserPromptSubmit` | `UserPromptSubmit` | `beforeSubmitPrompt` | `userPromptSubmitted` | — |
| `notification` | `Notification` | — | — | `notification` | — |
| `session_start` | `SessionStart` | `SessionStart` | `sessionStart` | `sessionStart` | — |
| `session_end` | `SessionEnd` | — | `sessionEnd` | `sessionEnd` | — |
| `stop` | `Stop` | `Stop` | `stop` | `agentStop` | `Stop` |
| `subagent_start` | `SubagentStart` | `SubagentStart` | `subagentStart` | `subagentStart` | — |
| `subagent_stop` | `SubagentStop` | `SubagentStop` | `subagentStop` | `subagentStop` | — |
| `pre_compact` | `PreCompact` | `PreCompact` | `preCompact` | `preCompact` | — |
| `post_compact` | `PostCompact` | `PostCompact` | — | — | — |
| `permission_request` | `PermissionRequest` | `PermissionRequest` | — | `permissionRequest` | — |
| `error_occurred` | — | — | — | `errorOccurred` | — |

Source tables: `claudeEventTable` (`:760-798`, plus wider P1d surface: `setup, user_prompt_expansion, post_tool_batch, permission_denied, stop_failure, teammate_idle, task_created, task_completed, worktree_create, worktree_remove, file_changed, config_change, cwd_changed, instructions_loaded, elicitation, elicitation_result`), `codexEventTable` (`:803-816`), `cursorEventTable` (widest surface incl. `before_shell_execution, after_shell_execution, before_mcp_execution, after_mcp_execution, before_read_file, after_file_edit, after_agent_response, after_agent_thought, workspace_open, before_tab_file_read, after_tab_file_edit`, `:820-856`), `copilotEventTable` (`:858-885`). A missing vendor cell = the renderer omits the hook (no invented equivalent). **`stop`→`agentStop` for Copilot** and **`subagent_stop` exists on all but antigravity** are gate-critical.

### 6.5 Shipped starter hook bundles (`internal/scaffold/hooks/global/`)
Copied into `~/.agents/hooks/global/` at `da init` (missing-only). Present bundles:

| Bundle | Events | enabled_on | Role |
|---|---|---|---|
| `iteration-close-gate` | `pre_tool_use, pre_compact, stop, subagent_stop` | claude, codex, copilot, cursor | Terminal closeout gate (§8) |
| `loop-worker-gate` | `subagent_start, pre_tool_use, pre_compact, subagent_stop` | claude, codex, copilot, cursor | Delegated-worker write-scope/merge-back gate (§8) |
| `isp-gate` | `pre_compact, stop` | claude, codex, copilot, cursor | Staged-runtime parent gate (§8) |
| `guard-commands` | `pre_tool_use` (match tools: Bash) | claude, codex, cursor, copilot, **omp** | Blocks destructive commands (`rm -rf ""`, force-push main, DROP DATABASE, fork bomb). Ships `guard.sh` (shell) + `omp/guard-rm.ts` (omp-native) |
| `session-orient` / `session-capture` / `session-handoff-snapshot` / `session-handoff-recover` | session events | — | Session continuity |
| `graph-orient` / `graph-update` / `graph-precommit` | — | — | KG maintenance |
| `auto-format` / `secret-scan` | pre/post tool | — | Quality guards |

⚠ `guard-commands/HOOK.yaml:14-15,22-27` declares `enabled_on: [...omp]` and ships `omp/guard-rm.ts`, but a comment states materializing it to `~/.omp/agent/hooks/pre/` "requires omp platform handling in da — see proposal omp-platform-handling." **da does not currently project to omp** (§13).

---

## 7. `da hooks` vs the runtime gate: two different "hooks"

There are TWO unrelated things named `gate.sh` in the repo:
- **`scripts/gate.sh` / `scripts/gate-cross.sh`** — repo BUILD/coverage gates (`enforce-coverage`, `covmerge`); orthogonal to lifecycle. Exit 1 on fail, 2 on unknown arg. NOT the runtime hook layer.
- **`internal/scaffold/hooks/global/<bundle>/gate.sh`** — the RUNTIME agent-lifecycle enforcement gates (below). This is the swarm-relevant one.

---

## 8. Runtime hook enforcement: sentinels + hook-outcomes + gate.sh

The runtime is **sentinel-anchored**: a hook fires on a canonical event, reads the latest active sentinel for its skill, checks repo-observable facts, emits native block/advisory output for the vendor, and mirrors the decision into an append-only hook-outcome telemetry sidecar.

### 8.1 Sentinel files (`commands/workflow/hook_sentinel.go`)
- **Active path:** `.agents/active/hook-sentinels/<skill>-<run-id>.json`. **Archive on clear:** `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/<skill>-<run-id>.json` (no record silently deleted in v1) (`hook_sentinel.go:174-186,486-516`).
- **Schema** (`commands/workflow/static/workflow-hook-sentinel.schema.json:8-80`): required `schema_version, skill, run_id, started_at, plan_id, task_id, agent_type`; optional `lifecycle_point, expected_artifacts, context`. Companion sentinels (`orchestrator-session-start`, `delegation-lifecycle`) add `operation`, `delegation_path`, `bundle_path`, `write_scope`, decision/archive/cleanup fields (`:104-198`).
- `read --latest` picks newest `started_at`, filename tie-breaker (`:408-483`).

**CLI (`da workflow hook-sentinel`):**
```
write <skill> --run-id <id> --plan <plan> --task <t> --agent-type main|loop-worker
      [--write-scope <path/glob>...]        # loop-worker gate diffs edits against this
      [--expect <artifact-path>...]         # terminal gate must find these
      [--eligible-snapshot-loaded] [--max-batch N]      # isp signals
      [--operation fanout_handoff|existing_bundle_handoff|parent_closeout]
      [--delegation-path ...] [--bundle-path ...] [--decision accept|reject]
      [--expected-archive-artifact ...] [--expected-cleanup-path ...]
read  <skill> (--latest | --run-id <id>) [--json]
clear <skill> --run-id <id>                 # archives, removes active file
```
(`da workflow hook-sentinel --help`.) Per-skill meaning:
- `iteration-close` — terminal closeout obligations (verify-record/checkpoint/merge-back); blocks delegated `workflow advance` on `pre_tool_use`; enforces `--expect` artifacts on `stop`/`subagent_stop`.
- `loop-worker` — declares `--write-scope` + merge-back; `subagent_start` bootstraps, `pre_tool_use` blocks orchestrator commands, `subagent_stop` checks write-scope escapes + missing merge-back.
- `isp` — staged runtime; `stop` blocks when parent-gate iter-log/bundle artifacts missing.

### 8.2 Hook-outcome sidecar (`commands/workflow/hook_outcome.go`)
- **Path:** `.agents/active/iteration-log/iter-N.hook-outcomes.yaml` (N = highest existing `iter-N.yaml`). Top-level `schema_version:1`, `records:[]` (`hook_outcome.go:141-167`).
- Record fields: `sentinel_id, skill, lifecycle_point, intervention_class, result, rule_id, platform, ts, correlation_id, archived_sentinel_path?`. Schema **rejects** transcript bodies / raw tool IO / stdout / stderr / command_args / failure_message (`workflow-hook-outcome.schema.json:31-53`).
- Append-only, **idempotent** on `(sentinel_id, rule_id, lifecycle_point, intervention_class)` (`:290-299`). Statuses: `written | duplicate | no-active-iteration`. Silent exit 0 + stderr advisory when no active iteration (`:217-225`).

**CLI (`da workflow hook-outcome write`):**
```
--sentinel-id <skill>-<run-id>  --skill <skill>
--lifecycle-point pre_tool_use|stop|subagent_stop|subagent_start|pre_compact|post_tool_use|post_tool_use_failure
--intervention-class prevent_before_action|remediate_at_stop|continuity_advice|observe_tool_result
--result allow|advise|remediate   --rule-id <id>   --platform claude|codex|copilot|cursor
[--correlation-id <id>] [--archived-sentinel-path <p>] [--ts <RFC3339>]
```

### 8.3 gate.sh decision logic (`internal/scaffold/hooks/global/iteration-close-gate/gate.sh`, representative)
- Inputs: `$1` optional canonical override; `$HOOK_EVENT_NAME` (vendor PascalCase); `$DA_HOOK_PLATFORM` (default `claude`); `$DA_HOOK_WHEN`; stdin = vendor payload (drained, grepped, never jq/python-parsed) (`gate.sh:19-30`).
- `vendor_to_canonical` maps PascalCase→snake (`:39-48`).
- **Sentinel absent → `exit 0` (allow) for EVERY event** (`read_sentinel` fails → `:203-205`). This is the key swarm invariant: no active sentinel ⇒ the gate never blocks.
- Dispatch (`:207-247`):
  - `pre_tool_use`: if payload contains `workflow advance` → `emit_outcome remediate R1.8` + `emit_hard_block` (exit 2); else allow (exit 0).
  - `pre_compact`: advisory-only (naming outstanding `--expect` artifacts), exit 0.
  - `stop`/`subagent_stop`: `missing_expected_artifacts` non-empty → `emit_hard_block` (exit 2); else advisory + allow (exit 0).
  - unknown event → exit 0 (fail-safe).
- **Native block output** (`emit_hard_block`, `:125-141`): Claude/Codex/Copilot → `{"decision":"block","reason":"…"}`; Cursor → `{"followup_message":"…"}`; then stderr line; `exit 2`.
- **Exit codes:** `0` advisory/allow; `2` hard block. Every decision is mirrored via `da workflow hook-outcome write` (best-effort, `|| true`).

### 8.4 Stop / lifecycle matrix (`docs/HOOKS.md`, tests/test-loop-discipline-stop-hooks.sh)

| Event | Role | Blocks when | Allows when |
|---|---|---|---|
| `PreToolUse` | prevent-before-action | iteration-close: delegated `workflow advance`; loop-worker: `workflow advance/orient/next/status` while sentinel active | benign commands; absent sentinel |
| `Stop`/`SubagentStop` | terminal validation | required `--expect` artifacts / write-scope violated | absent sentinel; artifacts present (advisory + outcome) |
| `PostToolUse`/`PostToolUseFailure` | observation only | never (not a stop gate) | always (hookable, not scored in v1) |
| `SessionStart` | continuity/recovery | never | always (best-effort recover) |

`Stop` is documented on every platform; `SubagentStop` on all except antigravity (`docs/HOOKS.md:175-180`).

---

## 9. Managed `.gitignore` contract (D14 / R8) — `internal/links/gitignore.go`

`da` owns a single delimited, idempotent block in each CONSUMING project's `.gitignore` for the things it materializes (projected links, generated platform configs, asset units, and the machine-local overlay), while keeping the committed contract tracked.

### 9.1 Markers + sets (`gitignore.go:44-64`)
```
# >>> dot-agents managed (project outputs) >>>
...sorted, deduped entries...
# <<< dot-agents managed (project outputs) <<<
```
- `alwaysIgnored = [".agentsrc.local.json"]` — always present even with zero projected paths (`:55`).
- `neverIgnored = {".agentsrc.json", ".agentsrc.lock"}` — the committed resolved-state contract (uv.lock analog); filtered OUT even if a caller passes them (`:61-64`).

### 9.2 `EnsureManagedGitignore(repoRoot, ignorePaths)` (`:79-94`)
Convergent: reads existing → strips the managed block (preserving user lines outside markers, `stripManagedGitignoreBlock`, `:114-129`) → regenerates block (`renderManagedGitignoreBlock` = alwaysIgnored + ignorePaths, minus neverIgnored, normalized: forward-slash, trim, dedup, sort — `normalizeIgnoreEntries`, `:149-168`) → atomic write. Re-running with the same inputs is byte-identical (R8: regenerated, not appended). Trailing-slash dir form (`.claude/`) ≠ plain form; both kept distinct. Empty repoRoot is an error; missing `.gitignore` is created.

### 9.3 Two DISTINCT managed gitignore blocks (do not conflate)
1. **Consuming-project block** (this file, §9.1) — in each managed repo, keeps projected/generated outputs out of git.
2. **Sync-boundary `.gitignore`** in `~/.agents` itself (`commands/sync/init.go:113-154`) — `local/`, `cache/`, `*.dot-agents-backup`; keeps machine-local state out of the synced home.
3. There is also a `local`-source provenance block owned by `internal/config EnsureProvenanceGitignore` (D7) with its own markers — a repo that is both a consumer and the local source carries two non-overlapping blocks (`gitignore.go:22-26`).

### 9.4 ⚠ CRITICAL GAP: `EnsureManagedGitignore` has ZERO production callers
Repo-wide search: only the definition + unit tests call it. It is NOT wired into `da refresh`/`install`/`add` — not in repo HEAD and not in 0.4.2. An active plan `managed-gitignore-autofill` (created 2026-07-11) intends to wire it into `da refresh` (collect all enabled-platform outputs → one `EnsureManagedGitignore` call), retire #381's ad-hoc root rules, and fix the `.agentsrc.lock` never-ignore contract (`.agents/workflow/plans/managed-gitignore-autofill/managed-gitignore-autofill.plan.md:11-32`; `TASKS.yaml`). **Today, generated per-machine outputs (e.g. Copilot `.github/hooks/*.json`) are NOT auto-ignored by any da command.**

---

## 10. Backup / restore (`commands/internal/lifecycle/backup.go`, `init.go`)

Two mechanisms:
1. **Sidecar backups** (`sidecarBackupFile`, `init.go:379-395`): before init/link replaces an unmanaged occupant with a managed link, write `<path>.dot-agents-backup`. Fail-closed: a backup failure aborts the replace, original intact. Passed into `links.SymlinkReplacing`/`HardlinkReplacing`.
2. **Project-tree backup/restore** (`backup.go`): `BackupExistingConfigsList` mirrors unmanaged project configs into `~/.agents/resources/<project>/…` (and `…/backups/<ts>/…` when a timestamp is given) then removes originals — no `*.dot-agents-backup` left in the project tree (`backup.go:108-176`). Restore via `RestoreFromResourcesCountedWithDeps` walks `~/.agents/resources/<project>/`, ignores `backups/`, dispatches canonical resources through `RestoreCanonicalResourceFileFn` (registered by import) (`:244-307`). `IsBackupArtifact` = filename contains `.dot-agents-backup` (`:43-45`); scanners skip these.

---

## 11. Config / lock touchpoints summary

| File | Written by | Read by | Synced? |
|---|---|---|---|
| `~/.agents/config.json` | init (`seedInitialConfig`), add/install register, refresh version-refresh | every command | yes (part of `~/.agents` git) |
| `~/.agents/local/bindings.json` | add/install (machine-local path binding) | resolution | **no** (sync gitignore) |
| `.agentsrc.json` | `install --generate`, promote (registers resources) | install/refresh/doctor | yes (committed to project repo) |
| `.agentsrc.lock` | `config.EnsureResolved`, install (`install` section), refresh (`refresh` section via `WriteRefreshToLock`) | doctor, install, refresh | committed, **never gitignored** (`neverIgnored`) |
| `.agentsrc.local.json` | machine overlay | resolution | **never** (`alwaysIgnored`) |

`config.EnsureResolved(path, EnsureOpts{})` re-resolves LOCAL scopes and rewrites the lock's `units`+`inputs_digest`; the explicit upstream re-check is `da config sync` (a sibling subtree, not in scope here), never `da refresh`.

---

## 12. Deterministic vs interactive behavior (swarm cheat-sheet)

| Command | Non-interactive form | Mutating? |
|---|---|---|
| refresh | `da refresh [project] [--inexact] [-n]` | yes (links, lock stamp) |
| install | `da install [--strict] [--inexact]` / `da install --generate [-n]` | yes |
| add | `da add <path> [--name] -y` | yes |
| remove | `da remove <project> [--clean] -y` | yes |
| status | `da status [--audit] [--agent p] --json` | no |
| doctor | `da doctor [-v]` (no `--json`) | no (read-only) |
| sync commit/push | `da sync commit -m "…"` / `da sync push -m "…" -y` | yes (git) |
| sync pull | `da sync pull` (**no `--dry-run`**) | yes (git) |
| hook-sentinel | `da workflow hook-sentinel write/read --json/clear` | yes (writes `.agents/active/…`) |
| hook-outcome | `da workflow hook-outcome write` | yes (append) |

Prompts are bypassed by `--yes` (`-y`) everywhere; `remove`/`sync push` also accept `--force` (`-f`) to skip confirmation. `--dry-run` (`-n`) previews for init/add/remove/refresh/install/status/commit/push but is REJECTED by `sync pull`.

---

## 13. Binary vs repo divergences (READ before designing the swarm)

1. **Managed gitignore not wired** — `EnsureManagedGitignore` exists but has zero callers in both 0.4.2 and repo HEAD (§9.4). A swarm cannot rely on `da refresh` ignoring generated outputs; either commit the managed block via `EnsureManagedGitignore` once wired, or manage the swarm's own scratch `.gitignore`.
2. **`omp` is not a da platform** — the six platforms are cursor/claude/codex/opencode/copilot/antigravity (`platform.All()`). `omp` appears only as an aspirational `enabled_on` value in `guard-commands/HOOK.yaml` + the `omp-platform-handling` proposal (2026-07-10, draft). **da does NOT materialize hooks/skills/agents/rules into `~/.omp/agent/`.** The runtime gate hooks (`iteration-close-gate`, `loop-worker-gate`, `isp-gate`) are `enabled_on: [claude, codex, copilot, cursor]` only — they will NOT fire inside an omp harness.
3. **omp hook shape differs** — omp hooks are TS factories `pi.on("tool_call", …)` returning `{block, reason}` in `~/.omp/agent/hooks/{pre,post}/*.ts`, NOT shell `gate.sh` + stdin JSON. Proposed event mapping (`omp-platform-handling.md:49`): `pre_tool_use→tool_call`, post→`tool_result`, `subagent_start→agent_start`, `pre_compact→session_before_compact`; **`subagent_stop` has NO omp equivalent** (`agent_end` is notification-only, `session_stop` never fires for subagents) — the terminal-Stop gate mechanism does not translate to omp subagents. For omp, gate at `tool_result` instead.
4. `guard-commands` bundle + `omp/guard-rm.ts` are newer than the shipped 0.4.2 binary (added ~same day as this reference). `da init` on 0.4.2 will not scaffold them unless the binary is rebuilt from HEAD.

---

## 14. Swarm-relevant hooks

An omp swarm (DAG of subagents coordinated via shared files) driving the dot-agents inner loop should treat `da` as follows.

### 14.1 What a swarm agent runs to project / refresh
- **Materialize the store into the working repo (idempotent, non-interactive):**
  - `da refresh <project>` — re-links + re-projects (EXACT/PRUNE by default) + stamps `.agentsrc.lock`. Add `--inexact` only if a stale managed output must survive. Runs `config.EnsureResolved` (local scopes) first.
  - `da install` — same but also resolves declared sources (git clones) and registers the project; use when the manifest has `sources`/git remotes (refresh does NOT re-fetch git sources — it prints a hint).
  - `da install --generate [-n]` — (re)write `.agentsrc.json` from `~/.agents/` state; preserves `project`, `repo_id`, `stage_profiles`, `manifests`, unknown keys, and unions sources. Safe to regenerate after adding project-scope skills/agents.
- **Discover state (read-only, machine-parseable):**
  - `da status --json` → `{agents_home, git, canonical_store, plugins, user_config, projects[]}` — the ONE JSON surface for fleet/link health. Use `path_exists` + per-platform `broken` flags to detect drift.
  - `da doctor` (text only, no JSON) — richest drift report incl. lockfile/manifest hints; parse its stdout lines if needed. It NEVER repairs — a swarm must itself run `da refresh`/`da config sync` on the hints.
  - `da hooks list [scope] --json`, `da skills/agents/rules/mcp/settings list [scope] --json` — enumerate what will project.
- **Persist store changes:** `da sync commit -m "…"` / `da sync push -m "…" -y`. `da sync pull` (no `-n`) then optionally `da refresh`.

### 14.2 The stop/lifecycle levers (the swarm's own gates)
The sentinel→gate→outcome mechanism is the intended way to make an inner-loop agent's Stop conditional on work being complete:
- **At skill/stage entry**, write a sentinel:
  `da workflow hook-sentinel write <skill> --run-id <id> --plan <p> --task <t> --agent-type main|loop-worker [--write-scope <glob>...] [--expect <artifact>...]`
  - Skills the gates recognize: `iteration-close`, `isp`, `loop-worker` (+ companion `orchestrator-session-start`, `delegation-lifecycle`).
  - `--expect` = artifacts the `stop`/`subagent_stop` gate must find or it HARD-BLOCKS (exit 2). `--write-scope` = loop-worker edit boundary (diffed at `subagent_stop`).
- **The gate fires on the platform's canonical event** (via the projected hook config), reads `--latest` sentinel, and blocks/allows. **No active sentinel ⇒ gate always allows** — so a swarm agent NOT under a sentinel is never blocked.
- **At stage exit**, `da workflow hook-sentinel clear <skill> --run-id <id>` archives the sentinel; the gate then allows Stop.
- **Telemetry:** gates mirror decisions via `da workflow hook-outcome write` into `.agents/active/iteration-log/iter-N.hook-outcomes.yaml` (idempotent on `(sentinel_id, rule_id, lifecycle_point, intervention_class)`). A swarm scoring layer reads this file.
- **Native block contract** the swarm's harness must honor: Claude/Codex/Copilot expect `{"decision":"block","reason":…}` on stdout + exit 2; Cursor expects `{"followup_message":…}`. **omp expects a TS hook returning `{block,reason}` on `tool_call`** — the shipped shell gates do NOT translate automatically.

### 14.3 Gotchas that will bite a swarm
1. **omp gets no da hook enforcement out of the box** (§13.2/13.3). If the swarm runs in omp and wants the iteration-close/loop-worker/isp gates, it must either (a) run in a da-supported harness for the gated stages, or (b) implement the omp-platform-handling bridge (mode-b TS wrapper shelling to `gate.sh`, mode-a native `.ts`). The `subagent_stop` terminal gate has no omp analog — gate at `tool_result`.
2. **Ship `guard-rm`** — `rm -rf ""` deletes the CWD in omp's embedded shell. `guard-commands/omp/guard-rm.ts` is the fix but is currently hand-installed (untracked) because da can't project to omp yet. A swarm running destructive commands MUST have this installed (`~/.omp/agent/hooks/pre/guard-rm.ts`) and MUST pass an explicit `cwd` on every command.
3. **Generated outputs are not auto-gitignored** (§9.4). If the swarm commits, generated per-machine files (`.github/hooks/*.json`, rendered `.claude/settings.local.json`, etc.) will appear as noise. Either wire/commit the managed `.gitignore` block via `EnsureManagedGitignore` (not yet exposed as a CLI verb) or scope the swarm's git adds.
4. **`.agentsrc.lock` must stay tracked** — it is `neverIgnored`. Do not add it to any ignore file.
5. **Refresh EXACT/PRUNE deletes stale managed links** — a swarm that hand-creates managed-looking symlinks under a projected dir (skills/agents/plugins roots) will have them PRUNED on the next `da refresh`/`install` unless they are in the resolved plan. Use `--inexact` to suppress pruning, or keep swarm scratch outside projected dirs.
6. **`da refresh` re-enables newly-installed platforms** (`DetectAndEnableNewPlatforms`) and never disables — projecting to a platform the swarm doesn't want is possible; control via config.json `enabled` state, not by expecting refresh to skip.
7. **`sync pull` has no `--dry-run`** and offers an interactive refresh — pass `-y` or handle the prompt.
8. **Sentinel/outcome files live under `.agents/active/`** — these are the shared-file coordination substrate a swarm can also read directly (sentinel JSON at `.agents/active/hook-sentinels/<skill>-<run-id>.json`; outcomes at `.agents/active/iteration-log/iter-N.hook-outcomes.yaml`). `read --latest` uses newest `started_at` + filename tie-break, so unique `--run-id`s per swarm node keep them isolated.
