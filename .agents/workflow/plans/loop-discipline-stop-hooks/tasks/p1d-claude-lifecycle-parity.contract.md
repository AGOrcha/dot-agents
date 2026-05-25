# P1d Claude Lifecycle Parity Contract

- task: `p1d-claude-lifecycle-parity`
- requirements: R6.6, DC9, D2
- dependency: `p1b-canonical-when-values`

## Goal

Bring the officially documented Claude Code lifecycle surface into the
canonical hook mapping without conflating platform parity with the three
discipline enforcement bundles.

## Required Reads

- Claude Code hooks reference: <https://code.claude.com/docs/en/hooks>
- `internal/platform/hooks.go` event mapping and rendering behavior.
- `internal/platform/hooks_test.go` mapper/render test conventions.
- `schemas/hook.schema.json` canonical value validation.
- `docs/HOOKS.md` platform event coverage table.

## Event Scope

Verify and, where currently absent, map the documented Claude events
represented by R6.6. The following exact names were confirmed from the
official hook reference on 2026-05-25:

```text
Setup
UserPromptExpansion
PostToolBatch
PermissionDenied
StopFailure
TeammateIdle
TaskCreated
TaskCompleted
WorktreeCreate
WorktreeRemove
FileChanged
ConfigChange
CwdChanged
InstructionsLoaded
Elicitation
ElicitationResult
```

If the documentation changes before implementation, reconcile the delta in
`docs/HOOKS.md` rather than retaining an obsolete mapping.

## Boundary

This task adds canonical availability and documentation only. It does not
make `iteration-close-gate`, `isp-gate`, or `loop-worker-gate` listen on
additional events, and it does not infer semantic equivalents on other
platforms.

## Acceptance

- Update the canonical schema when new `when` values are required.
- Add table-driven tests showing verified Claude mappings render and
  unsupported platform mappings remain omitted unless independently
  documented.
- Document the verified event list and date/source in `docs/HOOKS.md`.

## Out of Scope

- New gate behavior.
- OpenCode plugin bridging.
