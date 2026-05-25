# P1c Input, Matcher, and Multi-Event Contract

- task: `p1c-matcher-verification`
- requirements: R5.1, R6.5, D6, D7; resolves Q4 and Q5
- dependency: `p1a-mapper-extensions`

## Goal

Make one canonical hook bundle fire on multiple stop events without duplicate
manifests, while recording exactly which platforms expose evidence usable by
trace-dependent gate checks.

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

## `when_events` Contract

Extend a canonical `HOOK.yaml` with:

```yaml
when_events:
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
reference establishes matcher support for that event. The three stop gates do
not require matchers: `loop-worker-gate` self-filters on sentinel
`agent_type`.

## Acceptance

- Update `schemas/hook.schema.json` and loader/render behavior in
  `internal/platform/hooks.go`.
- Add tests for scalar compatibility, multi-event expansion, mutual
  exclusion/rejection, duplicate rejection, and per-platform omission.
- Document the verified trace field, native output, and matcher tables in
  `docs/HOOKS.md`.

## Out of Scope

- Parsing hook stdin inside shell gates.
- Expanding matcher or output support by analogy rather than documentation.
