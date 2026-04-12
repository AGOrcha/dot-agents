# Plugin Resource Salvage And Rebuild

Status: In Progress
Last updated: 2026-04-12
Depends on:
- `docs/PLATFORM_DIRS_DOCS.md`
- `docs/rfcs/resource-intent-centralization-rfc.md`
- branch analysis of `claude/scalable-skill-syncing-sfxOd`

## Goal

Salvage the useful "plugins as a first-class resource" ideas from the old Claude branch, but rebuild them on top of the current resource-intent planner/executor instead of merging the old implementation and its duplicate docs/cache payload.

## Decisions

- Treat the old branch as a donor branch, not a merge target.
- Keep the canonical `PLUGIN.yaml` and marketplace concepts.
- Rebuild plugin support through the current shared planner/executor and canonical storage model.
- Start with one low-conflict plugin emission/import path before expanding package-platform emitters.
- The `platform-docs-refresh` skill was copied from the donor branch and promoted — it is live under `.agents/skills/platform-docs-refresh/`.

## Current Slice

- [x] Audit `claude/scalable-skill-syncing-sfxOd` into keep, rebuild, and drop buckets
- [x] Define canonical `PLUGIN.yaml` schema and ownership boundaries on the current architecture
- [x] Integrate plugin resources into the shared planner and one low-conflict emitter path
- [ ] Phase 4 — Add import, status, explain, and doctor readback for plugin resources
- [ ] Phase 5 — Feed rebuilt plugin path into Stage 2 bucket expansion; retire duplicate branch artifacts

## Phase 4: Command readback for plugin resources

**Goal:** `import`, `status`, `explain`, and `doctor` surface canonical plugin bundles using the same patterns already established for skills, hooks, and agents.

**Donor reference:** `claude/scalable-skill-syncing-sfxOd` has a largely complete implementation. Port rather than rewrite:
- `internal/platform/plugins.go` — `PluginSpec`, `LoadPluginSpec`, `ListPluginSpecs`, `validatePluginSpec`
  - **Schema wiring required on port:** replace `validatePluginSpec` with `schemas.Validate(PluginManifestSchema, jsonBytes)` per `docs/SCHEMA_FOLLOWUPS.md`; add `var PluginManifestSchema = schemas.Plugin` and doc comment anchor to `PluginSpec`; see SCHEMA_FOLLOWUPS.md for full loader pattern and the `schemas/` embed package spec
- `internal/platform/package_plugins.go` — `syncPluginOverlayTree`, `collectPluginOverlayFiles`, `pruneStalePluginOverlayTree`
- `commands/import.go` — `canonicalPluginOutputsFromOpenCodeFile`, `canonicalPackagePluginManifestOutputs`, `canonicalPackagePluginComponentOutput`, `packagePluginLayout`, `directPackagePluginRefs`
- `internal/platform/plugin_marketplaces.go` — `renderClaudeMarketplace`, `renderCursorMarketplace`, `renderCodexMarketplace`, `renderCopilotMarketplace`

**Schema alignment required before porting:** The current `PLUGIN.yaml` schema uses `plugin_type`/`enabled_on`/`required_on`. Align to the donor's `kind`/`platforms` before wiring commands. See `schemas/plugin.schema.json` and `docs/PLUGIN_CONTRACT.md` for the updated field names.

### `internal/platform/plugins.go` — port `PluginSpec` and loaders

Port the `PluginSpec` struct and `LoadPluginSpec`/`ListPluginSpecs` from the donor. Key struct fields to carry over:

```go
type PluginKind string
const (
    PluginKindNative  PluginKind = "native"   // OpenCode runtime JS/TS
    PluginKindPackage PluginKind = "package"  // Cursor/Claude/Codex/Copilot bundle
)

type PluginResources struct {
    Agents   []string `yaml:"agents,omitempty"`
    Skills   []string `yaml:"skills,omitempty"`
    Commands []string `yaml:"commands,omitempty"`
    Hooks    []string `yaml:"hooks,omitempty"`
    MCP      []string `yaml:"mcp,omitempty"`
}

type PluginMarketplace struct {
    Repo string   `yaml:"repo,omitempty"`
    Tags []string `yaml:"tags,omitempty"`
}

type PluginSpec struct {
    SchemaVersion     int                       `yaml:"schema_version"`
    Kind              PluginKind                `yaml:"kind"`
    Name              string                    `yaml:"name"`
    Version           string                    `yaml:"version,omitempty"`
    DisplayName       string                    `yaml:"display_name,omitempty"`
    Description       string                    `yaml:"description,omitempty"`
    Authors           []string                  `yaml:"authors,omitempty"`
    Homepage          string                    `yaml:"homepage,omitempty"`
    License           string                    `yaml:"license,omitempty"`
    Platforms         []string                  `yaml:"platforms"`
    Resources         PluginResources           `yaml:"resources,omitempty"`
    Marketplace       PluginMarketplace         `yaml:"marketplace,omitempty"`
    Dependencies      map[string]any            `yaml:"dependencies,omitempty"`
    PlatformOverrides map[string]map[string]any `yaml:"platform_overrides,omitempty"`
    Dir               string                    `yaml:"-"`
    ManifestPath      string                    `yaml:"-"`
    Scope             string                    `yaml:"-"`
}
```

`validatePluginSpec` must require `kind ∈ {native, package}`, non-empty `name`, `len(platforms) >= 1`, and all platform IDs known. File: `internal/platform/plugins.go`.

### `commands/import.go` — scaffold PLUGIN.yaml during import (all 5 platforms)

Import **always scaffolds `PLUGIN.yaml`** — the user should not have to write one from scratch. Map native fields to canonical fields on the way in; preserve the native artifact verbatim under `platforms/{platformID}/`.

**OpenCode (`kind: native`)** — port `canonicalPluginOutputsFromOpenCodeFile` from donor:
```go
// .opencode/plugins/{name}/{file} → generates:
//   plugins/{scope}/{name}/PLUGIN.yaml  (kind: native, platforms: [opencode])
//   plugins/{scope}/{name}/files/{file} (native JS/TS content)
func canonicalPluginOutputsFromOpenCodeFile(scope, relPath, sourcePath string) ([]importOutput, bool, error) {
    trimmed := strings.TrimPrefix(relPath, ".opencode/plugins/")
    parts := strings.SplitN(trimmed, "/", 2)
    // parts[0] = plugin name, parts[1] = relative file path
    manifestContent, _ := yaml.Marshal(PluginSpec{
        SchemaVersion: 1,
        Kind:          PluginKindNative,
        Name:          parts[0],
        Platforms:     []string{"opencode"},
    })
    base := filepath.Join("plugins", scope, parts[0])
    return []importOutput{
        {destRel: filepath.Join(base, PluginManifestName), content: manifestContent},
        {destRel: filepath.Join(base, "files", parts[1]),  content: <file content>},
    }, true, nil
}
```

**Package platforms (Cursor/Claude/Codex/Copilot)** — port `canonicalPackagePluginManifestOutputs` from donor:
```go
// .cursor-plugin/plugin.json (or .claude-plugin/, .codex-plugin/, .github/plugin/) → generates:
//   plugins/{scope}/{name}/PLUGIN.yaml  (kind: package, platforms: [{platformID}], all fields mapped)
//   plugins/{scope}/{name}/platforms/{platformID}/plugin.json  (native file verbatim)
func canonicalPackagePluginManifestOutputs(c importCandidate, platformID, name string, manifest importedPackagePluginManifest) ([]importOutput, bool, error) {
    spec := PluginSpec{
        SchemaVersion: 1,
        Kind:          PluginKindPackage,
        Name:          name,
        Platforms:     []string{platformID},
        Version:       manifest.Version,
        Description:   manifest.Description,
        DisplayName:   manifest.DisplayName,
        Homepage:      manifest.Homepage,
        License:       manifest.License,
        Marketplace:   PluginMarketplace{Repo: manifest.Repository, Tags: manifest.Keywords},
        Authors:       importedPackageAuthors(manifest),
    }
    // write PLUGIN.yaml + preserve native plugin.json under platforms/{platformID}/
}
```

**Field mapping from `plugin.json` → `PLUGIN.yaml`:** `name`, `version`, `description`, `display_name`, `authors`/`author.name`, `homepage`, `license`, `repository` → `marketplace.repo`, `keywords` → `marketplace.tags`.

**Detection paths to walk** (add to `projectImportWalkDirs` / singles in the import candidate collector):
- `.opencode/plugins/` — walk directory, each subdir is one plugin
- `.cursor-plugin/plugin.json` — single file
- `.claude-plugin/plugin.json` — single file
- `.codex-plugin/plugin.json` — single file
- `.github/plugin/plugin.json` — single file
- `plugin.json` (repo root) — single file, Copilot only

### `commands/status.go` — `printCanonicalStoreSection`

After the existing skills/hooks/agents display blocks, add a plugins block:

```go
// Plugins
pluginIntents, _ := platform.BuildSharedPluginBundleIntents(project, agentsHome)
if len(pluginIntents) > 0 {
    ui.Section("Plugins")
    for _, intent := range pluginIntents {
        fmt.Fprintf(os.Stdout, "  %s  [%s]\n", intent.LogicalName, intent.Scope)
    }
}
// omit section if no plugins — matches existing behavior for empty buckets
```

### `commands/explain.go` — plugins section

In the canonical storage explanation block (where skills, hooks, agents are described), add:

```
~/.agents/plugins/{scope}/{name}/   Plugin bundles
  PLUGIN.yaml                        Manifest: kind, name, platforms, resources, platform_overrides
  resources/{agents,skills,...}/     Canonical shared components
  files/                             Native runtime files (OpenCode JS/TS)
  platforms/{id}/                    Platform-specific passthrough (e.g. native plugin.json)
```

### `commands/doctor.go` — plugin health checks

After existing check groups, add a `"plugins"` check group. For each canonical plugin bundle found by `platform.ListPluginSpecs(agentsHome, "")`:

1. **Manifest parse**: call `platform.LoadPluginSpec(bundleDir)` — error → `ERROR: {bundle}: PLUGIN.yaml malformed: {err}`
2. **Emitter check**: for each platform in `spec.Platforms`, check if the emitter is implemented (currently only `opencode`). Not implemented → `WARN: {bundle}: platforms includes {id} but no emitter is implemented yet`
3. **Projection link check**: for `opencode`, check `.opencode/plugins/{name}` — if symlink resolves to non-existent path → `ERROR: {bundle}: broken symlink at {path}`

**Acceptance criteria:**
- `dot-agents import` on a project with `.opencode/plugins/my-plugin/index.js` creates `~/.agents/plugins/global/my-plugin/PLUGIN.yaml` (with `kind: native`) and `files/index.js`.
- `dot-agents import` on a project with `.cursor-plugin/plugin.json` creates `~/.agents/plugins/global/{name}/PLUGIN.yaml` (with `kind: package`) and `platforms/cursor/plugin.json`.
- `dot-agents status` shows a "Plugins" section when `~/.agents/plugins/` has at least one bundle.
- `dot-agents explain` output includes the `~/.agents/plugins/` layout.
- `dot-agents doctor` reports broken symlinks and malformed manifests.
- `go test ./commands ./internal/platform` passes.

**Tests:**
- `commands/status_test.go` — `TestStatusShowsPluginsSection`: temp agentsHome with `plugins/global/my-plugin/PLUGIN.yaml` (valid minimal manifest); assert output contains "Plugins" and "my-plugin".
- `commands/import_test.go` — `TestImportFromOpencodePluginDir`: project with `.opencode/plugins/my-plugin/index.js`; assert `plugins/global/my-plugin/PLUGIN.yaml` has `kind: native` and `files/index.js` was created.
- `commands/import_test.go` — `TestImportFromCursorPluginManifest`: project with `.cursor-plugin/plugin.json` (`{"name":"my-plugin","version":"1.0.0"}`); assert `PLUGIN.yaml` has `kind: package, version: "1.0.0"` and `platforms/cursor/plugin.json` matches the original.
- `commands/doctor_test.go` — `TestDoctorPluginBrokenSymlink`: canonical bundle exists; `.opencode/plugins/my-plugin` symlink points to non-existent path; assert doctor reports ERROR with "broken symlink".

---

## Notes

- This plan is the plugin-specific feeder for Stage 2 bucket expansion in `platform-dir-unification`.
- `feature/PA-cursor-kg-build-update-commands-1b58` is already absorbed into the current stack and is not part of this salvage work.
- Sonar duplicate pressure is a reason to rebuild selectively rather than merge the old branch.
