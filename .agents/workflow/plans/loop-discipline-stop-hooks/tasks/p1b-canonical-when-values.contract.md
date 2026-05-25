# P1b Canonical Event Parity Contract

- task: `p1b-canonical-when-values`
- requirements: R6.1-R6.4, D2, D3, D5
- dependency: `p1a-mapper-extensions`

## Goal

Refresh documented non-gate canonical event coverage after the critical stop
mappings are available. This is parity work, not enforcement logic.

## Required Reads

- `internal/platform/hooks.go` event mappers and renderer behavior.
- `docs/HOOKS.md` canonical hook manifest and platform coverage sections.
- `../../specs/loop-discipline-stop-hooks/design.md` R6.

## Required Delta

Add or document only event names supported by official platform material
collected for this plan:

- Codex: `subagent_start`, `pre_compact`, `post_compact`,
  `permission_request` where its documented surface supports them.
- Copilot: `session_end`, `post_tool_use`, `post_tool_use_failure`,
  `pre_compact`, `notification`, `permission_request`, `subagent_start`,
  and `error_occurred`, in addition to the P1a stop mappings.
- Cursor: `session_end`, `post_tool_use`, `post_tool_use_failure`,
  `pre_compact`, `subagent_start`, and the Cursor-only canonical values
  named by R6.3.
- Claude: `post_compact` only if absent from the current mapping and
  supported by the documented event surface.

The implementer must reconcile this list with the current mapper before
editing; already-supported values are verification, not churn.

## Documentation Contract

Update `docs/HOOKS.md` to explain:

- One canonical event maps to one platform event or is omitted where not
  supported.
- Copilot stop renders as `agentStop`.
- Cross-platform bundles use portable canonical values and cannot assume
  every vendor implements every non-critical event.

## Acceptance

- Table-driven mapper/render tests cover each new supported value and
  fall-through omission for genuinely unsupported values.
- The documentation table agrees with rendered behavior.

## Out of Scope

- Stop gate scripts.
- Matcher support and multi-event HookSpec syntax, both owned by P1c.
