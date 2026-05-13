# Proposal: `workflow app-types` Discovery Command — ACCEPTED

**Status:** ACCEPTED 2026-05-12 — implemented in workflow CLI
**Implementation artifacts:** `commands/workflow/cmd.go`, `commands/workflow/app_types.go`, `commands/workflow/delegation.go`, `commands/workflow/app_types_test.go`, `commands/workflow/delegation_fanout_test.go`
**Written:** 2026-05-12 / accepted and implemented 2026-05-12
**Author:** drafted with agent assist

---

## Acceptance note

This proposal is now implemented.

Delivered behavior:

- `da workflow app-types` lists the currently valid `app_type` values for the repo using the same `.agentsrc.json` dispatch map that `workflow fanout` uses today.
- `da workflow app-types --json` emits machine-readable output for scripts and editor integrations.
- `da workflow app-types --format flag|task|plan|doc` prints ready-to-paste authoring snippets when there is a single recommended key.
- `da workflow app-types --verbose` shows the current source file and recommendation / alias reasoning.
- `da workflow task add --app-type` help text now points users at `da workflow app-types`.
- `workflow fanout` now fails clearly on unknown `app_type` values and points users at `da workflow app-types` to inspect valid values.

Validation completed from the maintained WSL shell:

- `go test ./commands/workflow -run 'TestWorkflowAppTypes|TestFanout_VerifierSequenceRejectsUnknownAppType'`
- `go build -o /tmp/da-app-types ./cmd/dot-agents && /tmp/da-app-types workflow app-types --format flag` from `provider-admin-batch`, which returned `--app-type pa-java-batch`

This proposal file is preserved as the historical record of the design and acceptance. Future follow-on work should treat the CLI implementation as the active contract for the repo-local v1 path.

---

## Original proposal text (preserved for context)

## Problem

The repo now tells authors that `TASKS.yaml app_type`, `PLAN.yaml default_app_type`, and
`da workflow task add --app-type ...` must exactly match a key in
`.agentsrc.json.app_type_verifier_map`. That is the right rule, but the operator still has to
discover the valid keys manually.

Today the common workflow is:

1. Open `.agentsrc.json`.
2. Find `app_type_verifier_map`.
3. Guess which key is the canonical authoring key versus a compatibility alias.
4. Copy that value into:
   - `da workflow task add --app-type ...`
   - `TASKS.yaml app_type`
   - `PLAN.yaml default_app_type`
   - human plan / handoff docs that mention verifier routing

That is too indirect for a field that is used during everyday task authoring.

---

## Root Cause

Available `app_type` values are dynamic project config, not static schema.

- The current runtime reads `task.app_type`, falling back to `plan.default_app_type`.
- It resolves that string by exact lookup against `.agentsrc.json.app_type_verifier_map`.
- No live command enumerates the available keys for the current repo.
- `da explain` is concept documentation, not repo-state inspection.
- `da config explain` exists in design docs but is not a live command surface in the current CLI.

So the authoring contract is real, but discoverability is missing.

---

## Proposed Solution

Add a live command:

```text
da workflow app-types
```

This command should expose the currently valid `app_type` values for the repo the user is in,
using the same dispatch source that `workflow fanout` uses today.

### Why `workflow` and not `config`

This is primarily a workflow-authoring need:

- tasks use `app_type`
- plans use `default_app_type`
- fanout consumes those fields

The current command tree already has a live `workflow` namespace and no live `config` namespace.
Placing the command under `workflow` makes it usable now without blocking on the larger
effective-config command family.

Later, if config-distribution lands, the implementation can swap from repo-local parsing to
effective-config resolution without changing the author-facing command. The companion proposal
`config-explain-live-surface.md` defines the shared introspection backend this command should move
onto once `da config explain` lands.

---

## Command Contract

### Default output

```text
$ da workflow app-types

Available app_types for this repo:

  pa-angular-ui            -> [pa-ui-unit, pa-ui-lint, pa-ui-e2e]    recommended
  prov-provider-admin-ui   -> [pa-ui-unit, pa-ui-lint, pa-ui-e2e]    alias of pa-angular-ui

Authoring examples:
  --app-type pa-angular-ui
  app_type: pa-angular-ui
  default_app_type: pa-angular-ui
```

The command should answer four questions directly:

1. What keys are valid here?
2. What verifier sequence does each key resolve to?
3. Which key should I author into tasks and plans?
4. What does the exact flag / YAML value look like?

### Suggested flags

```text
da workflow app-types
da workflow app-types --json
da workflow app-types --verbose
da workflow app-types --format flag
da workflow app-types --format task
da workflow app-types --format plan
da workflow app-types --format doc
```

#### `--json`

Machine-readable output for editors, scripts, and future plan generators.

Suggested shape:

```json
{
  "project": "prov-provider-admin-ui",
  "app_types": [
    {
      "name": "pa-angular-ui",
      "verifier_sequence": ["pa-ui-unit", "pa-ui-lint", "pa-ui-e2e"],
      "recommended": true,
      "alias_of": ""
    },
    {
      "name": "prov-provider-admin-ui",
      "verifier_sequence": ["pa-ui-unit", "pa-ui-lint", "pa-ui-e2e"],
      "recommended": false,
      "alias_of": "pa-angular-ui"
    }
  ]
}
```

#### `--verbose`

Adds provenance and reasoning.

For the current v1 implementation this may be as simple as:

- source file: `.agentsrc.json`
- recommendation reason: `non-repo alias preferred for authoring`

Once layered config exists, `--verbose` can show the winning layer and provenance chain.

#### `--format`

Print exactly the string or snippet the user needs to paste.

- `--format flag` → `--app-type pa-angular-ui`
- `--format task` → `app_type: pa-angular-ui`
- `--format plan` → `default_app_type: pa-angular-ui`
- `--format doc` → `Use app_type: pa-angular-ui in TASKS.yaml for UI tasks in this repo.`

This is the shortest path from discovery to correct authoring.

---

## Recommendation Heuristic

The command should distinguish between a valid key and a preferred authoring key.

### Initial heuristic

If multiple keys map to the same verifier sequence:

1. Prefer the non-repo-name key when one key matches the repo directory name and another does not.
2. Mark the repo-name key as an alias.
3. If no heuristic can confidently choose, show both as valid and mark none as recommended.

This helps with the current v1 reality where both a generic stack key and a repo-name alias may
exist only because fanout does exact-string lookup.

---

## UX Integration

The new command should be wired into existing authoring paths.

### 1. `workflow task add --help`

Adjust the `--app-type` help text to say:

```text
Run `da workflow app-types` to list valid values for the current repo.
```

### 2. Invalid app-type errors

When `workflow fanout` cannot resolve a task or plan app type, the error should end with:

```text
Run `da workflow app-types` to list valid values for this repo.
```

### 3. Authoring docs

Workflow docs should use the command in examples instead of telling users to inspect
`.agentsrc.json` manually.

---

## Implementation Outline

### Phase 1: repo-local v1 support

Use the same loader that current fanout uses:

- `commands/workflow/delegation.go`
- `loadAgentsrcFanoutDispatch(projectPath)`

Implementation sketch:

1. Load `.agentsrc.json` via `loadAgentsrcFanoutDispatch`.
2. Read `AppTypeVerifierMap`.
3. Sort keys for stable output.
4. Detect duplicate verifier sequences and mark likely aliases.
5. Render table or JSON.

### Phase 2: layered effective config

When config-distribution lands, keep the same command name and swap the backing resolver to the
same shared effective-config snapshot used by `da config explain`. `--verbose` can then show
provenance by layer without duplicating resolver logic inside workflow commands.

This keeps the authoring command stable across v1 and v2.

---

## Suggested Command Placement

Add alongside existing workflow authoring commands in `commands/workflow/cmd.go`:

```text
da workflow app-types
```

This is a better fit than:

- `da explain app-types` — documentation only, not repo-state discovery
- `da config explain ...` — not a live command family today
- static schema enums — invalid because available values vary by repo

---

## Implementation Scope

| File | Change |
|---|---|
| `commands/workflow/cmd.go` | Add `newWorkflowAppTypesCmd()` under `workflow` |
| `commands/workflow/delegation.go` or new helper file | Extract reusable app-type listing logic from fanout dispatch loader |
| `commands/workflow/*_test.go` | Add table-driven tests for list output, alias detection, JSON mode |
| `commands/workflow/cmd.go` | Update `--app-type` help text to point to `da workflow app-types` |
| future `internal/config/...` resolver | Replace direct `.agentsrc.json` parsing with shared effective-config snapshot once `da config explain` lands |
| workflow docs | Reference the new command in authoring guidance |

No schema changes are required for the command itself.

---

## Rejected Alternatives

### 1. Tell users to inspect `.agentsrc.json`

Rejected because it exposes storage, not intent. It also forces users to infer which keys are
aliases versus preferred authoring values.

### 2. Add static enum completion to the schemas

Rejected because the set of valid `app_type` values is repo-specific and eventually layer-driven.
Static schema enums will always drift or be wrong.

### 3. Wait for `da config explain`

Rejected because the authoring need exists now, while the broader config-resolution surface is
still partially design-only.

---

## Open Questions

1. Should the command also show the repo's current `PLAN.yaml default_app_type` when run inside a
   specific plan directory or with `--plan <id>`?
2. Should alias detection remain heuristic-only, or do we eventually want an explicit
   `preferred_app_type` field in config?
3. When layered config ships, should `da workflow app-types --verbose` show full layer provenance,
   or should that remain reserved for a future `da config explain` surface?