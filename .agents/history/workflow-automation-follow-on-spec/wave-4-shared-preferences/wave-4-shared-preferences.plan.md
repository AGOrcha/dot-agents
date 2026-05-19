# Wave 4: Shared Preferences And Compatibility

Spec: `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — Wave 4
Status: Completed (2026-04-10)
Depends on: Wave 3 (structured query surface)

## Goal

Persist workflow preferences explicitly so agents stop relearning repo preferences (test command, CI expectations, review style, verification standards). Separate team-shared preferences from user-local overrides, routing shared changes through the proposal queue.

## Artifacts Introduced

| Path | Purpose |
|------|---------|
| `.agents/workflow/preferences.yaml` | Repo-shared workflow defaults |
| `~/.agents/context/<project>/preferences.local.yaml` | User-local overrides |

## Precedence Order

1. `~/.agents/context/<project>/preferences.local.yaml` (user-local)
2. `.agents/workflow/preferences.yaml` (repo-shared)
3. Built-in defaults (hardcoded)

## Implementation Steps

### Step 1: Preference schema and types

- [ ] `WorkflowPreferences` struct with category sub-structs:
  - `Verification`: test_command, lint_command, require_regression_before_handoff (bool)
  - `Planning`: plan_directory, require_plan_before_code (bool)
  - `Review`: review_order, require_findings_first (bool)
  - `Execution`: package_manager, formatter
  - All fields are `*string` or `*bool` (pointer types) to distinguish "not set" from zero values during merge
- [ ] `WorkflowPreferencesFile` wrapper struct with schema_version + `WorkflowPreferences` embed
- [ ] `defaultWorkflowPreferences()` — returns built-in defaults
- [ ] Tests: defaults are sensible, zero-value handling

### Step 2: Preference loading and merge

- [ ] `loadRepoPreferences(projectPath string) (*WorkflowPreferencesFile, error)` — read `.agents/workflow/preferences.yaml`, graceful if absent
- [ ] `loadLocalPreferences(project string) (*WorkflowPreferencesFile, error)` — read from `config.ProjectContextDir(project)`, graceful if absent
- [ ] `mergePreferences(defaults, repo, local WorkflowPreferences) WorkflowPreferences` — apply precedence: local > repo > defaults. Only override non-nil fields.
- [ ] `resolvePreferences(projectPath, project string) (WorkflowPreferences, error)` — load both, merge with defaults
- [ ] Tests: merge with no overrides, repo-only overrides, local overrides trump repo, partial overrides leave defaults intact

### Step 3: `workflow prefs` subcommand

- [ ] `prefsCmd` (Use: "prefs") with `runWorkflowPrefs()`:
  - Show resolved preferences with source annotation (default/repo/local)
  - JSON via `Flags.JSON`
- [ ] `prefsShowCmd` (Use: "show") — same as bare `prefs` (alias for consistency)
- [ ] Tests: output with various preference sources

### Step 4: `workflow prefs set-local` subcommand

- [ ] `prefsSetLocalCmd` (Use: "set-local", Args: ExactArgs(2)) with `runWorkflowPrefsSetLocal(key, value)`:
  1. Validate key is a known preference path (e.g., "verification.test_command")
  2. Load existing local preferences (or create new)
  3. Set the value at the specified key
  4. Save to `~/.agents/context/<project>/preferences.local.yaml`
  5. `ui.Success()` confirmation
- [ ] Supported key format: `category.field` (e.g., `verification.test_command`, `execution.formatter`)
- [ ] Tests: set new key, update existing key, invalid key rejected

### Step 5: Shared preference changes via proposals

- [ ] `prefsSetSharedCmd` (Use: "set-shared", Args: ExactArgs(2)) — not a direct write
  1. Validate key
  2. Generate a proposal file (YAML) under `~/.agents/proposals/` using existing proposal infrastructure
  3. Proposal contains: the preference key, new value, current value, rationale
  4. `ui.Info()` telling user to run `dot-agents review` to approve
- [ ] Integrate with existing `internal/config/proposals.go` proposal creation
- [ ] Tests: proposal is created with correct content, not applied directly

### Step 6: Integration with orient/status

- [ ] Add `Preferences *WorkflowPreferences` to `workflowOrientState` (or just key fields)
- [ ] Update `collectWorkflowState()` to resolve preferences
- [ ] Update `renderWorkflowOrientMarkdown()` to add "# Preferences" section (test command, lint command, etc.)
- [ ] Agents consuming orient can now discover repo expectations from preferences
- [ ] Tests: orient includes preference data

## Files Modified

- `commands/workflow.go`
- `commands/workflow_test.go`
- `internal/config/proposals.go` (minor — extend proposal types if needed)

## Acceptance Criteria

An agent can discover repo workflow expectations from canonical artifacts instead of inferring them from repeated corrections.

## Verification

```bash
go test ./commands -run 'Pref|Preference'
go test ./commands
go test ./...
```
