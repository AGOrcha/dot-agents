# Orchestration Companion Lifecycle Hooks - Design

- spec-id: `orchestration-companion-stop-hooks`
- status: draft follow-up proposal
- date: 2026-05-25
- owner: dot-agents
- upstream dependency: `loop-discipline-stop-hooks`
- telemetry integration: `r1-5-hook-enforcement-telemetry`
- investigated skills: `orchestrator-session-start`, `delegation-lifecycle`,
  `plan-wave-picker`

## Problem

The approved `loop-discipline-stop-hooks` plan governs closeout, ISP staged
runtime, and bounded worker behavior. It intentionally does not decide
whether companion orchestration skills need their own hook contracts.
Three adjacent skills now need that decision:

- `orchestrator-session-start` creates or chooses the persisted handoff that
  precedes ISP and worker execution.
- `delegation-lifecycle` owns fanout, worker merge-back guidance, and parent
  closeout.
- `plan-wave-picker` recommends which plan work should be selected when
  multiple plans exist.

Adding hooks merely because a skill is related would create duplicate or
non-observable enforcement. This follow-up identifies which obligations leave
durable terminal evidence, which deterministic prerequisite errors may be
prevented with verified `PreToolUse` input, which active contexts deserve
`PreCompact` advice, and which skills should be scaffolded without hooks.

## Evidence Readback

### Existing hook coverage

The repository currently ships generic hooks under
`internal/scaffold/hooks/global/`:

| Existing hook | Event | Relevant behavior | Coverage boundary |
| --- | --- | --- | --- |
| `session-orient` | `session_start` | Calls `da workflow orient` or prints fallback context | Reads state; does not enforce an orchestration decision |
| `session-capture` | `stop` | Calls `da workflow checkpoint` or writes fallback checkpoint/log | Records terminal session state; does not validate fanout or closeout |
| `graph-orient` | `session_start` | Prints KG readiness | No workflow-discipline enforcement |

No existing hook bundle or sentinel wiring targets any of the three
investigated skills. The approved upstream plan introduces `isp-gate` and
`loop-worker-gate`; this follow-up must not duplicate those gates.

### Existing skill and starter coverage

| Skill | Present source asset | Present in current starter | Upstream planned coverage |
| --- | --- | --- | --- |
| `orchestrator-session-start` | `.agents/skills/orchestrator-session-start/` and `~/.agents/skills/dot-agents/orchestrator-session-start/` | no | none |
| `delegation-lifecycle` | `.agents/skills/delegation-lifecycle/` and `~/.agents/skills/dot-agents/delegation-lifecycle/` | no | scaffold and hook-suitability assessment in `loop-discipline-stop-hooks` P3b |
| `plan-wave-picker` | `.agents/skills/plan-wave-picker/` and `~/.agents/skills/dot-agents/plan-wave-picker/` | no | none |

The repository-local and installed versions of all three investigated skill
trees are currently equivalent. Promotion should therefore copy repository
assets into the starter after the upstream scaffold work lands, not attempt to
merge divergent versions.

### Durable workflow evidence already available

The workflow implementation writes deterministic artifacts useful to gates:

| Operation | Durable evidence |
| --- | --- |
| `workflow fanout` | `.agents/active/delegation/<task-id>.yaml` and `.agents/active/delegation-bundles/<delegation-id>.yaml`; bundle includes `selection`, `scope`, `context`, `verification`, and `closeout` |
| `workflow merge-back` | `.agents/active/merge-back/<task-id>.md` and completed delegation state |
| `workflow delegation closeout` | `.agents/history/<plan-id>/delegate-merge-back-archive/<date>/<task-id>/{delegation.yaml,merge-back.md,closeout.yaml}`; active bundle/delegation/merge-back removed; canonical task reconciled |
| `workflow eligible` | read-only JSON recommendation including `max_batch`, conflict and scope evidence metadata |

`plan-wave-picker` currently produces no persisted receipt stating which
recommended task or batch the user accepted. Its selection quality cannot be
hard-validated at Stop from repository state alone.

## Platform Evidence Boundary

This plan consumes, rather than reimplements, the upstream platform event and
native remediation mapping. As of the evidence review on 2026-05-25:

| Platform | Useful stop event | Transcript evidence exposed | Consequence for this plan |
| --- | --- | --- | --- |
| Claude Code | `Stop` | common hook input includes `transcript_path`; `Stop` can return a block decision | Artifact rules are portable; trace-only advice may be emitted when input is readable |
| Codex | `Stop` | hook input documents `transcript_path`; continuation output must be consumed through upstream's verified adapter | Do not assume Claude output shape in companion scripts |
| Cursor | `stop` | agent hooks expose `transcript_path`; native continuation uses `followup_message` | Portable hard outcomes use upstream Cursor output adapter |
| GitHub Copilot | `agentStop` | `transcriptPath` is documented for stop-style hooks | Portable hard outcomes use upstream verified output adapter |

No proposed hard rule depends on transcript content. Trace input may support an
advisory explaining apparent process drift, but unavailable trace never creates
a hard violation. Official evidence sources are:

- <https://code.claude.com/docs/en/hooks>
- <https://developers.openai.com/codex/hooks>
- <https://cursor.com/docs/hooks.md>
- <https://docs.github.com/en/copilot/reference/hooks-reference>

## Decisions

### D1. Add an orchestrator handoff gate only for delegated handoff paths

`orchestrator-session-start` gets a `stop` gate only when it opens an
enforcement sentinel for a fanout or existing-bundle handoff. A direct
execution decision has no current durable handoff artifact and is not gated in
v1.

This gate checks the portion before ISP begins: the delegation contract and
bundle exist, agree on plan/task/scope, and carry required evidence context.
It must not check ISP stage completion or worker closeout because the upstream
`isp-gate` and `loop-worker-gate` own those surfaces.

Where upstream P1c establishes verified `PreToolUse` input/output support,
the same bundle may prevent invoking `workflow fanout` when the active
handoff sentinel declares a required prerequisite sidecar that is already
missing. It cannot pre-validate the bundle that `fanout` has not yet created.
When `PreCompact` occurs with an unresolved handoff sentinel, it emits
non-blocking continuity advice naming the pending handoff obligation.

### D2. Add a delegation closeout gate only for the parent-closeout operation

`delegation-lifecycle` spans several roles. Its worker execution path is
already governed by `loop-worker-gate`; fanout handoff is governed by the new
orchestrator gate where the orchestrator skill was invoked. The unique
remaining deterministic boundary is parent closeout.

A `delegation-closeout-gate` sentinel is written after the parent selects
`accept` or `reject`, immediately before invoking
`workflow delegation closeout`. On Stop it validates the expected history
archive, active-artifact cleanup, and canonical task result.

Where upstream P1c establishes verified `PreToolUse` input/output support,
the bundle may prevent invoking `workflow delegation closeout` if its
sentinel-declared merge-back input or decision prerequisite is already
missing. `PreCompact` emits non-blocking advice when declared parent closeout
remains unresolved.

### D3. Scaffold and modernize `plan-wave-picker`; do not hook it in v1

`plan-wave-picker` is useful starter content but currently mixes legacy
`.agents/active/*.plan.md` selection guidance with newer
`workflow eligible` instructions. Its first follow-up work is to ship the
complete skill in the starter and make canonical `PLAN.yaml` / `TASKS.yaml` /
`workflow eligible` state authoritative.

No Stop gate ships until a separate design approves a durable selection
receipt or another observable state transition. A conversational
recommendation cannot be truthfully blocked or scored.

### D4. Reuse the upstream sentinel and native-output contracts

This plan starts after `loop-discipline-stop-hooks` has shipped the sentinel
CLI, multi-event/native platform verification, hook bundle pattern, and
starter copy pattern. It extends the accepted skill/operation context only as
needed for these two new gates; it does not invent a second sentinel format or
platform mapper.

### D5. Emit R1.5-compatible outcomes when telemetry is available

New gates must use the same rule/result vocabulary as the upstream gate
bundles. If `r1-5-hook-enforcement-telemetry` capture has shipped before these
hooks land, companion gates emit compatible `allow`, `advise`, or `remediate`
outcomes. If it has not shipped, hook implementation must avoid a private
telemetry format and add only fixtures proving later integration is possible.

### D6. Post-tool feedback is evaluated centrally, not privately recorded

`PostToolUse` and `PostToolUseFailure` can be useful observation inputs for
failed `workflow fanout` or `workflow delegation closeout` attempts.
Companion implementation must not introduce private observation persistence,
blocking behavior, or scoring. It provides fixtures and command-boundary
requirements to `r1-5-hook-enforcement-telemetry` T1b, which decides whether
bounded, redacted, deduplicated observation is retained.

## Requirements

### R1. Starter assets

- R1.1 Promote the complete `orchestrator-session-start` and
  `plan-wave-picker` skill trees into
  `internal/scaffold/home/starter/skills/global/`.
- R1.2 Consume upstream P3b's `delegation-lifecycle` starter tree rather than
  creating a competing copy; extend it only for approved sentinel wiring.
- R1.3 Extend starter-copy tests for the newly promoted assets.
- R1.4 Update `plan-wave-picker` guidance so canonical plan/task ledgers and
  `workflow eligible` are the primary selection source; legacy active markdown
  may be read only as compatibility context.

### R2. Companion sentinel context

- R2.1 Extend the upstream sentinel allowlist to accept
  `orchestrator-session-start` and `delegation-lifecycle`.
- R2.2 Sentinel context must distinguish
  `fanout_handoff`, `existing_bundle_handoff`, and `parent_closeout`; it must
  carry only expected artifact paths and branch facts known when written.
- R2.3 The orchestrator skill writes a sentinel only after it determines a
  delegated handoff path and before creating or briefing the handoff.
- R2.4 The delegation skill writes a sentinel for parent closeout only after a
  decision and task identity are known and before running closeout.
- R2.5 Successful sentinel records archive through the upstream history
  location; no companion record returns to active state after completion.

### R3. Orchestrator handoff gate

When an `orchestrator-session-start` delegated-handoff sentinel exists at
`Stop`, the gate must:

- R3.1 Hard-remediate if the expected delegation contract or bundle is
  missing.
- R3.2 Hard-remediate if bundle and delegation identifiers, plan/task
  ownership, or declared write scope disagree with sentinel expectations.
- R3.3 Hard-remediate when the sentinel declares a medium/high-confidence
  required scope sidecar but the generated bundle does not include that
  required context file.
- R3.4 Advise when durable handoff is valid but no selection reason was
  recorded, unless this follow-up deliberately makes that field required.
- R3.5 Exit silently if no companion sentinel exists or the session selected a
  direct-execution path.
- R3.6 Never enforce ISP parent-gate or worker merge-back results; those remain
  owned by upstream gates.
- R3.7 On verified `PreToolUse` input, prevent `workflow fanout` only when
  an active handoff sentinel declares a pre-existing required sidecar or
  equivalent prerequisite that is already missing; do not claim that the
  not-yet-created bundle has been verified.
- R3.8 On `PreCompact`, advise on an unresolved handoff sentinel without
  blocking compaction.

### R4. Delegation closeout gate

When a `delegation-lifecycle` `parent_closeout` sentinel exists at `Stop`, the
gate must:

- R4.1 Hard-remediate if the declared closeout archive is missing its
  `delegation.yaml`, `merge-back.md`, or `closeout.yaml` artifacts.
- R4.2 Hard-remediate if the corresponding active delegation, active
  merge-back, or active bundle remains after a declared successful closeout.
- R4.3 Hard-remediate if an `accept` decision does not result in a completed
  canonical task, or a `reject` decision does not result in a blocked task.
- R4.4 Advise when completed merge-back state exists without a
  parent-closeout sentinel, because it might be a deliberate cross-turn pause.
- R4.5 Never gate the worker execution path already covered by
  `loop-worker-gate`.
- R4.6 On verified `PreToolUse` input, prevent
  `workflow delegation closeout` only when its declared merge-back or
  decision prerequisite is missing before execution.
- R4.7 On `PreCompact`, advise on unresolved declared parent closeout without
  blocking compaction.

### R5. No v1 wave-picker gate

- R5.1 Do not create a `plan-wave-picker` Stop hook in this plan.
- R5.2 Record the future hook precondition: a versioned durable receipt tying
  an eligible snapshot to the selected plan/task or accepted batch.
- R5.3 If such a receipt is later approved, its hook must be advisory until a
  user-facing contract explicitly makes selection persistence mandatory.

### R6. Outcome and platform compatibility

- R6.1 Both new hook bundles use terminal `stop` through the platform mappings
  already delivered by upstream work and may add `pre_tool_use` /
  `pre_compact` only after upstream P1c verifies those event contracts.
- R6.2 Hard outcomes must use the upstream verified native remediation adapter;
  companion code must not assume one output JSON shape for all vendors.
- R6.3 Hook execution may consume transcript fields only for advice. Portable
  artifact checks remain sufficient for every hard outcome.
- R6.4 Once R1.5 capture exists, companion gates emit the upstream-compatible
  structured outcome contract without storing transcript bodies.
- R6.5 `PostToolUse` / `PostToolUseFailure` are R1.5 observation-evaluation
  inputs only; this plan does not block or score from post-tool events.

## Done Criteria

- DC1 Complete starter assets for `orchestrator-session-start` and
  `plan-wave-picker` materialize in an empty starter home.
- DC2 The sentinel protocol accepts only the new documented companion
  operations and archives completed records through durable history.
- DC3 `orchestrator-handoff-gate` has tests for valid fanout, missing bundle,
  scope mismatch, sidecar omission, pre-action missing-prerequisite
  prevention, compaction advice, and direct-path bypass.
- DC4 `delegation-closeout-gate` has tests for accept, reject, missing archive,
  incomplete cleanup, pre-action missing-prerequisite prevention, compaction
  advice, and no-sentinel advisory behavior.
- DC5 No task introduces a Stop gate for `plan-wave-picker`.
- DC6 Integration tests demonstrate both new gates using the upstream
  per-platform remediation/output mechanism and R1.5 fixture compatibility.

## Out of Scope

- Changes to the approved `loop-discipline-stop-hooks` or
  `r1-5-hook-enforcement-telemetry` plan artifacts.
- Selection of a task, spawning a worker, or running closeout directly from a
  hook.
- A general transcript parser or a new platform event mapping.
- Hard enforcement for `plan-wave-picker` without an approved durable
  selection-record contract.
- Private post-tool observation storage or scoring outside R1.5.

## Unresolved Decisions

1. Should an orchestrator delegated handoff require a non-empty bundle
   `selection.reason`, converting R3.4 from advisory to hard remediation, or
   remain optional for existing CLI compatibility?
2. Should a future selection-receipt design be a `workflow selection record`
   CLI primitive or an extension of `workflow start-task` output?
3. If R1.5 has not shipped by companion gate implementation time, should the
   integration task pause until telemetry is available or land compatibility
   fixtures and let R1.5 extend its capture task later?
4. Should `delegation-lifecycle` parent-closeout wiring live in the complete
   starter copy added upstream P3b only, or also be backfilled into
   repo-local `.agents/skills/` by a separately authorized migration?
