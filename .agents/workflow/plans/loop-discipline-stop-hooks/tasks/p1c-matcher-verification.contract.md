# P1c Input, Matcher, and Multi-Event Contract

- task: `p1c-matcher-verification`
- requirements: R5.1, R6.5, D6, D7; resolves Q4 and Q5
- dependency: `p1a-mapper-extensions`

## Goal

Make one canonical hook bundle fire on its approved lifecycle events without
duplicate manifests, while recording exactly which platforms expose inputs
and outputs usable by prevention, continuity, or trace-dependent terminal
checks.

## Source Verification

Use official vendor documentation as the evidence source:

- Claude Code hooks: <https://code.claude.com/docs/en/hooks>
- Codex hooks: <https://developers.openai.com/codex/hooks>
- GitHub Copilot hooks: <https://docs.github.com/en/copilot/reference/hooks-configuration>
- Cursor hooks: <https://cursor.com/docs/hooks.md>

Record the resulting event/input, matcher, and output table in
`docs/HOOKS.md`.
Claude and Codex currently document `transcript_path` on common hook input;
Copilot documents `transcriptPath` for `agentStop` and `subagentStop`.
Cursor documents `transcript_path` on common agent input and
`agent_transcript_path` on `subagentStop`, so it participates in
trace-dependent checks when the path is readable.

Native hard-remediation output is not identical across platforms:
Claude Code, Codex, and Copilot document block/continuation decision
output; Cursor native `stop` and `subagentStop` use `followup_message`.
The renderer/gate contract must preserve those differences.

For the approved lifecycle expansion, verify and document:

- whether `PreToolUse` input identifies the attempted workflow command/tool
  sufficiently for deterministic pre-action remediation;
- what context/output each vendor permits at `SubagentStart`, used only for
  `loop-worker` bootstrap and later correlation;
- what context/output each vendor permits at `PreCompact`, used only for
  non-blocking continuity advice;
- whether `PostToolUse` or `PostToolUseFailure` provides bounded
  workflow-command result metadata suitable for R1.5 observation without
  transcript body persistence.

## `when_events` Contract

Extend a canonical `HOOK.yaml` with:

```yaml
when_events:
  - pre_tool_use
  - stop
  - subagent_stop
```

Rules:

- Existing scalar `when` remains supported without behavior changes.
- A manifest must specify exactly one of `when` or a non-empty
  `when_events`.
- Reject duplicate or unknown canonical events during load/validation.
- Rendering expands one `HookSpec` into an action for each canonical event
  supported on that platform, preserving command, timeout, platform filters,
  and any verified-safe matcher configuration.

## Matcher Boundary

Do not add an event to a renderer's matcher whitelist unless the official
reference establishes matcher support for that event. The terminal gates do
not require matchers: `loop-worker-gate` self-filters on sentinel
`agent_type`. A `PreToolUse` guard may use a matcher only when official
documentation establishes that it narrows the same command surface the gate
enforces; otherwise the script must parse verified input or omit that
vendor/event behavior.

## Acceptance

- Update `schemas/hook.schema.json` and loader/render behavior in
  `internal/platform/hooks.go`.
- Add tests for scalar compatibility, multi-event expansion, mutual
  exclusion/rejection, duplicate rejection, and per-platform omission.
- Document the verified trace field, native output, and matcher tables in
  `docs/HOOKS.md`.

## Out of Scope

- Implementing hook stdin parsing inside shell gates.
- Expanding matcher or output support by analogy rather than documentation.
