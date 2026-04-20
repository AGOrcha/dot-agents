# Error Message Inventory Findings

## Date
2026-04-19

## Scope

Plan task `error-message-compliance / inventory-error-message-behavior`.

Evidence sources:

- shared CLI UX code in `commands/ux.go`
- command-specific handlers in `commands/`, `commands/workflow/`, and `commands/kg/`
- representative runtime output from invalid invocations run against the current tree

## Shared baseline that already works

The repo has one real shared error renderer:

- `CLIError`
- `ErrorWithHints(...)`
- `UsageError(...)`
- `ConfigureRootCommandUX(...)`
- `RenderCommandError(...)`

That path already produces a consistent envelope:

- `Error:` prefix
- optional `Hint:` / `Hints:` block
- usage text only when `ShowUsage` is true

Representative current output:

```text
$ dot-agents workflow status extra
Error: dot-agents workflow status does not accept positional arguments (got 1)
Hints:
  - Usage: dot-agents workflow status [flags]
  - Run `dot-agents workflow status --help` to see examples and supported flags.
  - Run workflow status from inside the project repository.
```

```text
$ dot-agents workflow orient --plan x
Error: unknown flag: --plan
Hint:
  - Run `dot-agents workflow orient --help` to see examples and supported flags.
```

## Strongly compliant families

These command families already use the shared helpers deliberately and produce actionable failures:

### Workflow command wiring

- `commands/workflow/cmd.go` uses `NoArgsWithHints`, `ExactArgsWithHints`, and `MaximumNArgsWithHints` for many shape errors.
- `commands/workflow.go` exposes a stable `errNoWorkflowProject` sentinel so repo-context failures pick up targeted hints.
- `commands/workflow/verification.go`, `commands/workflow/plan_task.go`, `commands/workflow/prefs.go`, and `commands/workflow/graph.go` already use `ErrorWithHints(...)` on several recovery-heavy paths.

Observed behavior:

- wrong positional shape shows usage plus command-specific hint text
- root parse failures show usage and a help hint
- some enum-style workflow failures get extra valid-value hints from `enrichCLIError(...)`

### Canonical MCP and settings surfaces

- `commands/mcp.go`
- `commands/settings.go`

These are the cleanest non-workflow examples:

- empty name -> `UsageError(...)`
- missing file -> `ErrorWithHints(...)`
- runtime removal failure -> raw wrapped error only when the failure is not user-correctable

Observed behavior:

```text
$ dot-agents mcp show global does-not-exist
Error: MCP file not found: global / does-not-exist
Hints:
  - Run `dot-agents mcp list global` to see available files.
  - Run `dot-agents mcp show --help` to see examples and supported flags.
```

## Partially compliant families

These areas have some good behavior but still lean on string matching or mixed raw errors.

### Install and remove flows

- `commands/install.go`
- `commands/remove.go`
- `commands/refresh.go`

Good:

- some user-recoverable failures already use `ErrorWithHints(...)`
- `enrichCLIError(...)` adds targeted hints for common messages such as:
  - `manifest not found`
  - `~/.agents/ not initialized`
  - `project not found:`
  - `not found in any source`

Drift:

- many nearby failures still return raw `fmt.Errorf(...)`
- the shared hinting depends on message substring matches instead of command-owned typed errors
- some validation and source-resolution failures still bury the recovery step inside the primary sentence or omit it entirely

### Workflow and graph bridge internals

- `commands/workflow/graph.go`
- `commands/workflow/delegation.go`

Good:

- some guardrails use `UsageError(...)` and `ErrorWithHints(...)`

Drift:

- several important failures still return raw `fmt.Errorf(...)`, for example unsupported bridge intents, config load failures, and delegation argument conflicts
- these still render through the shared envelope, but usually only with the generic `--help` hint

## Highest-drift families

These are the best implementation targets for the next task.

### KG query / bridge / link commands

Files:

- `commands/kg/bridge.go`
- `commands/kg/query_lint_maintain.go`
- `commands/kg/sync_code_warm_link.go`

Current state:

- heavy use of raw `fmt.Errorf(...)`
- finite-domain validation often embeds valid values inline, but does not use `UsageError(...)`
- many failures end up with only the default help hint instead of a targeted recovery action

Observed behavior:

```text
$ dot-agents kg bridge query --intent nope runWorkflowComplete
Error: unknown bridge intent "nope"
Hint:
  - Run `dot-agents kg bridge query --help` to see examples and supported flags.
```

```text
$ dot-agents kg bridge query runWorkflowComplete
Error: --intent is required (valid: ...)
Hint:
  - Run `dot-agents kg bridge query --help` to see examples and supported flags.
```

Why this still drifts:

- invalid intent is a classic finite-domain validation error and should enumerate valid values in a deliberate contract
- missing KG initialization errors inline the recovery command, but do not consistently use `ErrorWithHints(...)`
- several `kg link` errors still hand-roll `usage: ...` strings instead of using the shared usage helpers

### Agent lifecycle commands

Files:

- `commands/agents/remove.go`
- `commands/agents/import.go`
- `commands/agents/promote.go`

Current state:

- repo/setup/path failures are mostly raw `fmt.Errorf(...)`
- errors are understandable, but they often omit a next step
- some destructive-protection errors are precise but not consistently actionable

Examples:

- missing `.agentsrc.json`
- agent not linked in project
- refusing to remove unmanaged symlink
- canonical path exists as a real directory

### Add and install setup paths

Files:

- `commands/add.go`
- `commands/install.go`

Current state:

- mixed model: some good hint-bearing errors, many raw config/filesystem/source failures
- invalid project names and duplicate registration still use raw `fmt.Errorf(...)`
- several "reading/writing/loading config" failures are plain wrapped errors with no routing hint

## Pattern summary

### What already has a real contract

- Cobra arg-count validation when wired through `*ArgsWithHints`
- root flag parse failures via `ConfigureRootCommandUX(...)`
- selected workflow, mcp, settings, hooks, and verification surfaces that call `UsageError(...)` or `ErrorWithHints(...)`

### What still needs normalization

- enum / constrained-value validation outside workflow helper paths
- recoverable repo/setup/source errors that still return raw `fmt.Errorf(...)`
- hand-authored `usage: ...` strings in KG link subcommands
- command families whose success path is structured but whose failure path is still ad hoc prose

## Recommended contract decisions for the next task

1. Treat finite-domain validation as first-class `UsageError(...)` or `ErrorWithHints(...)`, and always enumerate valid values.
2. Stop encoding recovery commands inside raw error strings when the failure is user-correctable.
3. Replace manual `usage: ...` strings with shared arg helpers so usage rendering stays centralized.
4. Prefer command-owned typed errors over substring-based enrichment when a family has repeated failure classes.
5. Keep raw wrapped errors for true execution failures, but do not use them for common validation/setup mistakes.

## Priority fix list

1. `commands/kg/bridge.go` and `commands/kg/query_lint_maintain.go`
2. `commands/kg/sync_code_warm_link.go`
3. `commands/agents/remove.go`, `commands/agents/import.go`, `commands/agents/promote.go`
4. remaining setup-validation drift in `commands/add.go` and `commands/install.go`
