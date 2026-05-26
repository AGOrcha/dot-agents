# P1a Mapper Extensions Contract

- task: `p1a-mapper-extensions`
- requirements: R6 gate-critical subset, D2, D6
- dependency: `p0-sentinel-cli`

## Goal

Make the three enforcement hooks render on each required platform using only
the event mappings needed for stop enforcement. Broader event parity belongs
to P1b.

## Required Reads

- `internal/platform/hooks.go` (`codexEventName`, `cursorEventName`,
  `copilotEventName`, and the four renderer functions).
- `internal/platform/hooks_test.go` for current rendering assertions.

## Mapping Delta

| Canonical `when` | Platform | Rendered event |
| --- | --- | --- |
| `subagent_stop` | Codex | `SubagentStop` |
| `stop` | GitHub Copilot | `agentStop` |
| `subagent_stop` | GitHub Copilot | `subagentStop` |
| `subagent_stop` | Cursor | `subagentStop` |

Claude already supplies the required stop mappings and is a regression check,
not a new mapping in this task. In particular, do not render Copilot stop as
`stop`.

## Matcher Boundary

This task does not add matcher support or platform-specific matcher
exceptions. Gate selection remains in the script via sentinel `agent_type`,
as required by D6.

## Acceptance

- Add focused tests in `internal/platform/hooks_test.go` showing canonical
  `stop` and `subagent_stop` render to the mapping table above.
- Verify unsupported matcher combinations do not become accepted as a side
  effect.
- Keep event additions limited to the gate-critical table; non-gate events
  are P1b-owned changes.

## Out of Scope

- `when_events` representation and schema updates.
- Trace-input and vendor matcher research.
- Documentation of the wider event surface.
