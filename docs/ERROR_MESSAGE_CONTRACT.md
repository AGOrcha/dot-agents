# Error message contract (dot-agents CLI)

**Status:** Contract draft updated from the 2026-04-19 inventory in [`docs/research/error-message-inventory.md`](./research/error-message-inventory.md).  
**Scope:** Defines the supported contract for human-facing CLI failures: primary message shape, hints, usage rendering, finite-domain validation, and the current limitation that most error paths are not machine-readable.

## Shared error UX surface

The CLI already has a shared error rendering path in [`commands/ux.go`](../commands/ux.go):

- `CLIError`
- `ErrorWithHints(...)`
- `UsageError(...)`
- `ConfigureRootCommandUX(...)`
- `RenderCommandError(...)`

This contract exists to keep command implementations aligned with that shared path instead of each command inventing its own failure wording.

## Contract priority

When a failure is user-facing, use this decision order:

1. `UsageError(...)` for command-shape and syntax mistakes
2. `ErrorWithHints(...)` for recoverable runtime or setup mistakes
3. raw wrapped errors only for true execution failures that do not have a concrete next step

This is the supported contract. String-matching enrichment in `enrichCLIError(...)` is still useful as a transition aid, but it is not the preferred steady-state design for repeated error classes.

## Core rules

### 1. Lead with one actionable primary message

The first line should state what failed in direct language.

Preferred shape:

- `workflow status expects 0 arguments, got 1`
- `invalid verification status "done"`
- `workflow commands must run inside a project directory`

Avoid:

- stack-trace style wording
- unexplained internal implementation details
- vague messages like `invalid input` when the domain is finite

### 2. Put recovery steps in hints, not inside the primary sentence

If the user can fix the problem with a next step, use `Hints`.

Examples:

- `Run \`dot-agents workflow prefs\` to list valid preference keys and resolved values.`
- `Run \`dot-agents add .\` first.`
- `Run \`dot-agents workflow status --help\` to see examples and supported flags.`

The primary message says what is wrong. Hints say what to do next.

### 3. Show usage only for usage errors

Use `UsageError(...)` when the user invoked the command with the wrong shape:

- wrong number of positional arguments
- invalid flag value / missing required flag when the remedy is command syntax
- unknown flag / malformed flag value from Cobra root parsing

Do not show usage for pure runtime failures where syntax is already correct:

- missing project state
- filesystem read/write failures
- missing resources in configured sources
- verification failures from command execution

Also:

- do not hand-author `usage: ...` strings inside raw `fmt.Errorf(...)` returns
- prefer Cobra arg helpers (`ExactArgsWithHints`, `NoArgsWithHints`, `MaximumNArgsWithHints`, `RangeArgsWithHints`) so usage rendering stays centralized
- if a validation failure is not about invocation shape, do not force `ShowUsage` just to surface recovery steps

### 4. Enumerate valid values for finite domains

When a field has a closed set of valid values, the error should list them.

Preferred shape:

- `invalid verification status "done": valid values are pass, fail, partial, unknown`
- `invalid scope "repo": supported scopes are project, global, all`

This is especially important for agent-facing commands where retrying with a corrected enum is the expected recovery path.

If a command has a finite domain and currently says only `unknown` or `invalid`, that is considered contract drift even if the underlying validation is technically correct.

### 5. Keep one canonical writer for repeated structures

If a command writes a structured artifact, validation should happen at the CLI boundary and the CLI should own the on-disk shape. Error messages should point back to the command contract rather than tell the agent to hand-edit structured files.

This mirrors the D1 / D7 direction in the loop-agent pipeline spec: weak-model reliability should come from the command surface, not from expecting the agent to author perfect YAML by hand.

### 6. Prefer typed command errors over substring-only enrichment

`enrichCLIError(...)` can add hints for legacy paths, but new or repeatedly hit failure classes should not depend on brittle substring matching alone.

Preferred shape:

- command family returns `UsageError(...)` or `ErrorWithHints(...)` directly when it knows the recovery step
- shared enrichment is reserved for broad cross-cutting cases or backwards compatibility

Avoid:

- relying on a raw `fmt.Errorf(...)` string to trigger command-specific recovery behavior elsewhere
- embedding both the primary failure and the next step into one sentence just so enrichment is unnecessary

### 7. Unknown command and parse errors should still be recoverable

Root parse failures should:

- preserve the parser message
- include a help hint
- show usage for the resolved command when possible

Current shared handling already does this through `ConfigureRootCommandUX(...)`.

### 8. Machine-readable success does not imply machine-readable failure

Some commands already support `--json` on success. That does not change the failure contract today:

- failure output is still human-first unless a command explicitly documents a machine-readable error envelope
- automation may parse success JSON, but should treat generic error paths as prose
- new command work should not imply JSON failures unless that contract is designed intentionally

## Error classes

| Class | Expected helper | Usage | Hints | Notes |
|------|------------------|-------|-------|-------|
| Positional-arg shape error | `UsageError` / arg helper | yes | yes | Include `UseLine()` and command help hint |
| Invalid enum / constrained value | `UsageError` or `ErrorWithHints` depending on syntax vs runtime validation | usually yes | yes | Must enumerate valid values |
| Missing project / machine setup | `ErrorWithHints` | no | yes | Prioritize recovery commands |
| Missing resource / source resolution | `ErrorWithHints` | no | yes | Point to likely recovery command or config file |
| Protected destructive refusal | `ErrorWithHints` when recovery exists, otherwise `CLIError` or wrapped error | usually no | yes when actionable | Explain why the command refused and what must change first |
| Unknown command / unknown flag | root parse path | yes | yes | Keep Cobra detail, add help hint |
| Execution/runtime failure with no immediate recovery | wrapped error or `CLIError` | no | optional | Avoid misleading usage dumps |

## Automation note

Error rendering is still **human-first** today.

Even when a command supports `--json` on success, callers should assume failures may still render as:

- a colored `Error:` line
- optional bullet hints
- optional usage text

Until a separate machine-readable error envelope is defined, automation should not rely on JSON failures from the generic command UX path.

## Target direction

For command families under active development:

- prefer `UsageError(...)` or `ErrorWithHints(...)` over ad hoc `fmt.Errorf(...)` for user-correctable failures
- add finite-domain value lists for enum validation
- reuse centralized hinting instead of embedding multi-step recovery prose directly into raw error strings
- replace manual `usage: ...` strings with shared arg helpers
- move repeated recovery classes out of substring-only enrichment and into command-owned typed errors
- add regression tests when a command family adopts the shared contract

## Inventory-backed priority areas

The 2026-04-19 inventory puts the highest normalization priority on:

1. `commands/kg/bridge.go` and `commands/kg/query_lint_maintain.go`
2. `commands/kg/sync_code_warm_link.go`
3. `commands/agents/remove.go`, `commands/agents/import.go`, and `commands/agents/promote.go`
4. setup-validation drift in `commands/add.go` and `commands/install.go`

The strongest current examples to preserve are:

- workflow arg-shape and repo-context errors
- root parse handling in `ConfigureRootCommandUX(...)`
- `mcp` and `settings` file lookup flows

## Related documents

- [Error Message Compliance plan](../.agents/workflow/plans/error-message-compliance/error-message-compliance.plan.md)
- [Global Flag Contract](./GLOBAL_FLAG_CONTRACT.md)
