# Error message contract (dot-agents CLI)

**Status:** Initial contract text aligned with the seed inventory in [`.agents/workflow/plans/error-message-compliance/error-message-compliance.plan.md`](../.agents/workflow/plans/error-message-compliance/error-message-compliance.plan.md).  
**Scope:** Describes the emerging contract for human-facing CLI failures: primary message shape, hints, usage rendering, validation wording, and the current limitation that most error paths are not machine-readable.

## Shared error UX surface

The CLI already has a shared error rendering path in [`commands/ux.go`](../commands/ux.go):

- `CLIError`
- `ErrorWithHints(...)`
- `UsageError(...)`
- `ConfigureRootCommandUX(...)`
- `RenderCommandError(...)`

This contract exists to keep command implementations aligned with that shared path instead of each command inventing its own failure wording.

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

### 4. Enumerate valid values for finite domains

When a field has a closed set of valid values, the error should list them.

Preferred shape:

- `invalid verification status "done": valid values are pass, fail, partial, unknown`
- `invalid scope "repo": supported scopes are project, global, all`

This is especially important for agent-facing commands where retrying with a corrected enum is the expected recovery path.

### 5. Keep one canonical writer for repeated structures

If a command writes a structured artifact, validation should happen at the CLI boundary and the CLI should own the on-disk shape. Error messages should point back to the command contract rather than tell the agent to hand-edit structured files.

This mirrors the D1 / D7 direction in the loop-agent pipeline spec: weak-model reliability should come from the command surface, not from expecting the agent to author perfect YAML by hand.

### 6. Unknown command and parse errors should still be recoverable

Root parse failures should:

- preserve the parser message
- include a help hint
- show usage for the resolved command when possible

Current shared handling already does this through `ConfigureRootCommandUX(...)`.

## Error classes

| Class | Expected helper | Usage | Hints | Notes |
|------|------------------|-------|-------|-------|
| Positional-arg shape error | `UsageError` / arg helper | yes | yes | Include `UseLine()` and command help hint |
| Invalid enum / constrained value | `UsageError` or `ErrorWithHints` depending on whether syntax is wrong vs runtime validation | usually yes | yes | Must enumerate valid values |
| Missing project / machine setup | `ErrorWithHints` | no | yes | Prioritize recovery commands |
| Missing resource / source resolution | `ErrorWithHints` | no | yes | Point to likely recovery command or config file |
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
- add regression tests when a command family adopts the shared contract

## Related documents

- [Error Message Compliance plan](../.agents/workflow/plans/error-message-compliance/error-message-compliance.plan.md)
- [Global Flag Contract](./GLOBAL_FLAG_CONTRACT.md)
