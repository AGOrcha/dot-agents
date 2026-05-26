# P2 Hook Bundles Contract

- task: `p2-hook-scripts`
- requirements: R1-R3, R5, D6, D7
- dependencies: `p0-sentinel-cli`, `p1a-mapper-extensions`,
  `p1b-canonical-when-values`, `p1c-matcher-verification`

## Goal

Ship three canonical hook bundles that enforce provable loop-discipline
failures at terminal or verified pre-action boundaries, bootstrap the bounded
worker at subagent start, and preserve active context before compaction.

## Bundle Layout

Create:

```text
internal/scaffold/hooks/global/iteration-close-gate/HOOK.yaml
internal/scaffold/hooks/global/iteration-close-gate/gate.sh
internal/scaffold/hooks/global/isp-gate/HOOK.yaml
internal/scaffold/hooks/global/isp-gate/gate.sh
internal/scaffold/hooks/global/loop-worker-gate/HOOK.yaml
internal/scaffold/hooks/global/loop-worker-gate/gate.sh
```

All three manifests target only events verified through P1c and use
per-platform omission where a vendor cannot represent an event:

| Bundle | Approved lifecycle events | Purpose |
| --- | --- | --- |
| `iteration-close-gate` | `pre_tool_use`, `pre_compact`, `stop`, `subagent_stop` | prevent delegated `advance`, advise before compaction, terminally verify closeout |
| `isp-gate` | `pre_compact`, `stop` | preserve staged-runtime context, terminally verify parent gate |
| `loop-worker-gate` | `subagent_start`, `pre_tool_use`, `pre_compact`, `subagent_stop` | bootstrap worker, prevent forbidden orchestration commands, preserve context, terminally verify scope/handoff |

`loop-worker-gate` self-filters by `agent_type` rather than depending on a
platform-specific terminal matcher.

## Gate Outcomes

| Bundle | Portable hard-remediation evidence | Trace-backed hard-remediation evidence | Advisory |
| --- | --- | --- | --- |
| `iteration-close-gate` | missing required verification/checkpoint/merge-back artifact | delegated `workflow advance` visible in verified readable trace | generic verification summary; missing trace coverage |
| `isp-gate` | missing stage artifacts or sentinel-required snapshot context | prohibited sequencing visible in verified readable trace | unavailable trace coverage |
| `loop-worker-gate` | file outside sentinel `write_scope` or required handoff absent | forbidden workflow command visible in verified readable trace | dirty worktree or unavailable trace coverage |

Hard outcomes follow the vendor-supported native continuation contract:
Claude Code, Codex, and Copilot emit documented block output; Cursor emits
native `followup_message`. Advisory text is written to stderr and exits
successfully.

## Non-Terminal Outcomes

| Bundle/event | Deterministic hard remediation | Non-blocking output |
| --- | --- | --- |
| `iteration-close-gate` / `pre_tool_use` | delegated sentinel plus attempted `workflow advance` | omit when input cannot establish command identity |
| `loop-worker-gate` / `pre_tool_use` | active loop-worker sentinel plus attempted `workflow advance`, `orient`, `next`, or `status` | omit when input cannot establish command identity |
| `loop-worker-gate` / `subagent_start` | none | bootstrap context and correlation hint only |
| any active primary sentinel / `pre_compact` | none | unresolved obligation continuity advice only |

`PostToolUse` and `PostToolUseFailure` do not ship behavioral handlers from
P2. Their observation suitability is evaluated by R1.5 after P1c records
vendor input constraints.

## Input and Portability Rules

- Read persisted context through `da workflow hook-sentinel read`; do not
  scrape sentinel JSON with regular expressions.
- Repository state and file-scope checks may use portable shell plus Git.
- Trace-dependent rules run only where P1c verified a readable trace input
  and the bundle can consume it using repository-supported runtime behavior.
- Do not add an undeclared installed-hook dependency such as `jq` or
  `python3`. If no portable consumption path exists during implementation,
  implement the D7 advisory and record hard trace parsing as follow-up rather
  than emitting an unproven block.
- A missing or unreadable sentinel fails closed through the appropriate
  native remediation output for rules that require the sentinel itself,
  with a message naming the recovery action.

## Acceptance

- Extend `internal/scaffold/hooks/copy_test.go` for all bundle files.
- Add script-level fixtures or tests for one terminal artifact hard outcome,
  one deterministic `PreToolUse` hard outcome, one startup or compaction
  non-blocking output, one sentinel-filter bypass, and one advisory path
  before P5 composes the full smoke test.
- Keep scripts fail-safe: malformed input must not silently approve a
  portable hard violation.

## Out of Scope

- Telemetry aggregation.
- A new general-purpose hook-input parsing runtime.
