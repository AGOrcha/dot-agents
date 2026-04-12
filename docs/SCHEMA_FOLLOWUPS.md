# Schema Follow-Ups

This repo now has repo-local JSON Schemas under `schemas/`:

| Schema | Path | Target artifact |
|--------|------|-----------------|
| `.agentsrc.json` | `schemas/agentsrc.schema.json` | Project manifest (aligned with Go `AgentsRC`; rejects unknown top-level fields) |
| `HOOK.yaml` | `schemas/hook.schema.json` | Canonical hook bundle manifest under `~/.agents/hooks/<scope>/<name>/` |
| `PLUGIN.yaml` | `schemas/plugin.schema.json` | Canonical plugin bundle manifest under `~/.agents/plugins/<scope>/<name>/` |
| Workflow plan | `schemas/workflow-plan.schema.json` | `.agents/workflow/plans/<id>/PLAN.yaml` |
| Workflow tasks | `schemas/workflow-tasks.schema.json` | `.agents/workflow/plans/<id>/TASKS.yaml` |

Editor validation: point YAML language servers at these `$id` paths (see `# yaml-language-server: $schema=...` comments in generated bundles).

## Runtime Validation — Concrete Spec

### Design

Runtime validation should treat the JSON Schema as the authoritative contract. The flow for any YAML manifest loader:

```
os.ReadFile → yaml.Unmarshal → map[string]any → json.Marshal → jsonschema.Validate → yaml.Unmarshal → typed struct
```

The intermediate `map[string]any` → JSON round-trip is necessary because `santhosh-tekuri/jsonschema/v5` validates JSON bytes, not Go values.

### Dependency

Add one new dependency:

```
go get github.com/santhosh-tekuri/jsonschema/v5
```

Pure Go, no CGo, no transitive deps beyond the standard library. Supports JSON Schema draft 2020-12 (the dialect all five schemas use).

### Embedding the schemas

`//go:embed` cannot reference files outside the package directory (no `../` patterns). Create a new package `schemas` at the repo root:

**`schemas/schemas.go`:**
```go
package schemas

import (
    "embed"
    "fmt"

    "github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed *.json
var FS embed.FS

// compiled validators — loaded once at package init, panics on schema parse error
// (schema files are embedded at build time; a parse failure is a programmer error).
var (
    Plugin   = mustCompile("plugin.schema.json")
    Hook     = mustCompile("hook.schema.json")
    Plan     = mustCompile("workflow-plan.schema.json")
    Tasks    = mustCompile("workflow-tasks.schema.json")
    AgentsRC = mustCompile("agentsrc.schema.json")
)

// Validate validates jsonBytes against the given compiled schema.
// Returns a human-readable error listing all violations.
func Validate(s *jsonschema.Schema, jsonBytes []byte) error {
    if err := s.Validate(jsonschema.NewDecoder(bytes.NewReader(jsonBytes))); err != nil {
        var ve *jsonschema.ValidationError
        if errors.As(err, &ve) {
            return fmt.Errorf("%s", ve.DetailedOutput().String())
        }
        return err
    }
    return nil
}

func mustCompile(name string) *jsonschema.Schema {
    data, err := FS.ReadFile(name)
    if err != nil {
        panic(fmt.Sprintf("schemas: missing embedded schema %s: %v", name, err))
    }
    compiler := jsonschema.NewCompiler()
    if err := compiler.AddResource(name, bytes.NewReader(data)); err != nil {
        panic(fmt.Sprintf("schemas: add resource %s: %v", name, err))
    }
    s, err := compiler.Compile(name)
    if err != nil {
        panic(fmt.Sprintf("schemas: compile %s: %v", name, err))
    }
    return s
}
```

### Struct annotation — the "link as contract"

Each Go type that implements a schema manifest must carry two anchors:

1. **Doc comment** naming the schema file:
   ```go
   // PluginSpec is the Go model for PLUGIN.yaml.
   // Schema contract: schemas/plugin.schema.json — every required/property change in the
   // schema must be reflected in this struct and validatePluginSpec.
   type PluginSpec struct { ... }
   ```

2. **Package-level schema constant** in the same file:
   ```go
   // PluginManifestSchema is the embedded compiled schema for PLUGIN.yaml.
   // Loaders must validate raw YAML bytes against this schema before unmarshaling.
   var PluginManifestSchema = schemas.Plugin
   ```

The constant makes the binding explicit and navigable (grep for `PluginManifestSchema` finds both the declaration and every call site that runs validation).

### Loader pattern

Replace the current YAML-only unmarshal in each loader with the validate-then-unmarshal pattern:

```go
func LoadPluginSpec(pluginDir string) (PluginSpec, error) {
    manifestPath := filepath.Join(pluginDir, PluginManifestName)
    raw, err := os.ReadFile(manifestPath)
    if err != nil {
        return PluginSpec{}, fmt.Errorf("reading %s: %w", manifestPath, err)
    }

    // 1. YAML → generic map for schema validation
    var generic map[string]any
    if err := yaml.Unmarshal(raw, &generic); err != nil {
        return PluginSpec{}, fmt.Errorf("parse %s: %w", manifestPath, err)
    }
    jsonBytes, err := json.Marshal(generic)
    if err != nil {
        return PluginSpec{}, fmt.Errorf("json encode %s: %w", manifestPath, err)
    }
    if err := schemas.Validate(PluginManifestSchema, jsonBytes); err != nil {
        return PluginSpec{}, fmt.Errorf("schema %s: %w", manifestPath, err)
    }

    // 2. YAML → typed struct (schema passed; struct fields trusted)
    var spec PluginSpec
    if err := yaml.Unmarshal(raw, &spec); err != nil {
        return PluginSpec{}, fmt.Errorf("unmarshal %s: %w", manifestPath, err)
    }
    spec.Dir = pluginDir
    spec.ManifestPath = manifestPath
    spec.Platforms = sortedUniqueStrings(spec.Platforms)
    // ... other normalization
    return spec, nil
}
```

Drop the manual `validatePluginSpec` function — the schema covers all the same constraints (`required`, `enum`, `minItems`, `minLength`, `additionalProperties`). Keep only normalization (dedup/sort) that is outside schema scope.

### Apply to all five manifest types

| Schema | Loader location | Loader function | Compiled var |
|--------|----------------|-----------------|--------------|
| `plugin.schema.json` | `internal/platform/plugins.go` | `LoadPluginSpec` | `PluginManifestSchema` |
| `hook.schema.json` | `internal/platform/hooks.go` | `loadHookBundleSpec` | `HookManifestSchema` |
| `workflow-plan.schema.json` | `commands/workflow.go` (plan load) | `loadCanonicalPlan` | `PlanManifestSchema` |
| `workflow-tasks.schema.json` | `commands/workflow.go` (tasks load) | `loadCanonicalTasks` | `TasksManifestSchema` |
| `agentsrc.schema.json` | `internal/config/agentsrc.go` | `Load` | `AgentsRCSchema` |

### Tests

`schemas/schemas_test.go`:
- `TestSchemasCompile`: assert all five schema vars are non-nil (panics on failure; this test guards against schema parse regression)
- `TestPluginSchemaValid`: marshal a known-good minimal `PluginSpec` to YAML → JSON, assert no validation error
- `TestPluginSchemaRejectsUnknownKind`: send `{"schema_version":1,"kind":"unknown","name":"x","platforms":["opencode"]}`, assert error contains "kind"
- `TestHookSchemaValid`: known-good `hookManifest`, assert no validation error

### Status

- [ ] `schemas/schemas.go` — new embed + validator package
- [ ] `go get github.com/santhosh-tekuri/jsonschema/v5`
- [ ] Wire `LoadPluginSpec` (implement as part of `plugin-resource-salvage` Phase 4)
- [ ] Wire `loadHookBundleSpec`
- [ ] Wire `loadCanonicalPlan` / `loadCanonicalTasks` in `commands/workflow.go`
- [ ] Wire `config.Load` for `.agentsrc.json`
- [ ] Drop `validatePluginSpec` manual validator after schema covers same ground

---

**Still deferred:**

- Deciding which schema families remain repo-local versus moving into exported/public schema paths for downstream consumers.
