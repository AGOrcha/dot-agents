# Loop-Discipline Stop Hooks — Design

- spec-id: `loop-discipline-stop-hooks`
- status: active — core delivered, tail tasks open (promotion of `.agents/proposals/loop-discipline-stop-hooks-scoping.md`)
- date: 2026-05-25 (coherence-reconciled 2026-06-25)
- owner: dot-agents
- predecessor: `.agents/proposals/loop-discipline-stop-hooks-scoping.md`
- related plans: `r1-outcome-scoring` (completed scoring baseline),
  `r1-5-hook-enforcement-telemetry` (follow-up consumer of gate outcomes)

> **Coherence note (2026-06-25).** The Problem-statement framing below ("none of
> the three skills ship in the starter scaffold", "platform mapper has coverage
> gaps") is the **2026-05-25 pre-implementation state** and is preserved as the
> historical rationale that motivated the plan. It is no longer the shipped
> reality: plan tasks `p0-sentinel-cli` … `p5-e2e-integration`, `p1e`, `p3-pre`,
> and `p3b` are **completed** — the three discipline skills (`iteration-close`,
> `isp`, `loop-worker`), the `loop-worker` AGENT.md, the `loop-worker` profile,
> and the three gate bundles (`iteration-close-gate`, `isp-gate`,
> `loop-worker-gate`) all ship under `internal/scaffold/`. The mapper
> extensions (R6) landed. Only the downstream-migration / release tail
> (`p6-payout-backfill`, `p7-legacy-override-migration`, `p6-rollback-plan`,
> `r1-5-t4-integration`, `release-minor`) remains open. Read the requirements
> below as the delivered contract, not as undone work.

## Problem statement

Three core dot-agents skills — `iteration-close`, `isp`, and `loop-worker` —
encode the project's discipline contracts (verify → checkpoint → merge-back;
orchestrator stage gates; subagent write-scope confinement). Today those
contracts live only in skill prose. Nothing inspects the agent's state at
turn-end to confirm the contract was actually upheld. Drift is silent and
typically only surfaces in post-mortem.

In parallel, none of the three skills ship in the dot-agents starter
scaffold, so projects initialized via `da init` / `da refresh` do not
inherit the machinery the discipline depends on, and the platform mapper in
`internal/platform/hooks.go` has documented coverage gaps for the very
lifecycle events (`SubagentStop`, Codex/Cursor stop variants, Copilot's
`agentStop`) any enforcement layer would need to attach to.

## Goal

Land lifecycle hooks anchored by Stop / SubagentStop enforcement for the
discipline contracts of `iteration-close`, `isp`, and `loop-worker`.
Sentinels written at skill entry let terminal hooks validate that expected
workflow artifacts are present and consistent before the agent is allowed to
stop. In addition, add narrowly bounded `PreToolUse` prevention for
deterministically forbidden workflow commands, `SubagentStart` bootstrap
context for `loop-worker`, and `PreCompact` continuity advice while governed
work remains active. Violations
split two-tier: **hard** = request platform-native continuation/remediation
with an actionable reason; **soft** = stderr advisory, exit 0. Claude Code,
Codex, and Copilot use documented block decisions; Cursor native hooks use
`followup_message` to continue repair. Land every asset
inside the dot-agents starter scaffold so any project inheriting the
starter picks up both the skills and the enforcement together.

## Decisions

The following were settled in scoping (2026-05-25). The plan implements
them; it does not relitigate them.

### D1. Two-tier violation enforcement (hard remediation vs soft advisory)

**Decision:** Hooks request the platform's documented native remediation on
hard violations (contract demonstrably broken: missing artifact, scope
violation, illegal CLI invocation). Claude Code, Codex, and Copilot use
JSON `{"decision":"block","reason":"..."}` where documented; Cursor native
`stop` / `subagentStop` hooks use `{"followup_message":"..."}`. Soft
violations emit a stderr advisory and exit 0 (smell, not breach: vague
verify summary, uncommitted changes).
**Rationale:** Hard remediation recovers lost work and stops drift before it
propagates. Soft advisories steer behavior without paying for false
positives. Tier choice is a per-rule policy decision, not a global setting.
**Rejected:** Always-block (too brittle, blocks recoverable smells);
always-advise (no recovery for true breaches).

### D2. One-to-one canonical `HookSpec.When` → platform event mapping

**Decision:** Each canonical `HookSpec.When` value maps to at most one
documented event name per platform. If a platform does not document an
event, the mapper returns `("", false)` and the renderer emits nothing for
that platform — the existing `hookRequiredOnPlatform` fall-through is the
right behavior. Operators who want cross-platform coverage of a semantic
concept declare additional HookSpecs targeting each platform's nearest
documented event.
**Rationale:** Semantic-equivalence fan-out (e.g. translating Cursor's
`after-file-edit` to Claude's `PostToolUse` with matcher `Edit|Write`)
creates surprising behavior — a Cursor `after-file-edit` would
unexpectedly fire on `cat`-via-Bash on Claude. Explicit operator opt-in
preserves clarity.
**Precedent:** `permission_request` today maps only on platforms that
document it; other platforms no-op.
**Rejected:** Semantic-equivalence fan-out; whole-platform-bucket
broadcast.

### D3. Cursor's wider event surface gets canonical `HookSpec.When` values

**Decision:** Fine-grained Cursor-only events (`before-shell-execution`,
`after-file-edit`, etc.) are promoted to canonical `HookSpec.When` values
even though only Cursor implements them today. Other platform mappers
no-op for these values until the vendors document equivalents.
**Rationale:** These events deliver real cross-platform leverage as other
vendors copy Cursor's surface; promoting them now means operator
HookSpecs do not need to be rewritten when (e.g.) Codex adds an
`AfterShellExecution` event. Same precedent as `permission_request`.

### D4. OpenCode is an intentional plugins-vs-hooks split

**Decision:** OpenCode lifecycle handlers remain under the **Plugins**
resource type. No hooks-to-plugins emitter bridge is built. OpenCode does
not publish a hooks file; its lifecycle surface is TypeScript / JavaScript
plugin modules with `package.json` deps installed via `bun install`.
**Rationale:** A `hooks/{scope}/hooks.json` → OpenCode plugin emitter
would be bridge code maintained for one platform with no other consumer.
The artifact shape is sufficiently different that overloading the Hooks
resource type would confuse the model. This is an intentional split, not
a coverage gap; document as such in `docs/HOOKS.md`.

### D5. Sentinel protocol is the contract between skill and gate

**Decision:** Each skill writes a sentinel file at entry —
`.agents/active/hook-sentinels/<skill>-<run-id>.json` — declaring the
plan, task, agent type, and expected artifacts. The Stop/SubagentStop
hook reads the latest sentinel for its skill name and validates the
declared artifacts before allowing stop. No sentinel = no enforcement
(skill did not run this turn).
**Rationale:** Sentinels make enforcement opt-in per skill invocation and
decouple the gate script from skill internals — the gate only knows
sentinel shape, not skill steps. Encoding expectations declaratively
keeps the gate logic small and uniform across skills. Completed records are
historical loop evidence and are archived under
`.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/`, not retained under
active state.

> Convergence target: the sentinel shape here is one of the surfaces meant to
> adopt the generic registry-driven envelope in `[[unified-pluggable-event-contract]]`
> (so new sentinel kinds become registry entries, not central parse/route edits).

### D6. Loop-worker matcher logic self-filters in `gate.sh`

**Decision:** Claude's `subagent_type == "loop-worker"` matcher is
Claude-only. On Codex/Copilot/Cursor the SubagentStop hook fires for all
subagents; the `gate.sh` script self-filters by reading the sentinel's
`agent_type` field and exiting 0 early if the subagent is not a
loop-worker.
**Rationale:** Avoids redundant per-platform matcher logic; keeps a single
source of truth (the sentinel) for "is this the agent I care about?".

### D7. Artifact checks are portable; trace checks require observable input

**Decision:** The v1 gates require hard remediation on facts they can establish from
repository state or declared sentinel data on every supported platform
(missing expected artifacts, write-scope escape, forbidden workflow-state
file mutation). Rules that require command history or conversation content
(`workflow advance` invoked, `workflow orient` re-run, `make build-prod`
run, or direct-vs-fanout decision prose missing) are enforced only when the
hook invocation supplies a readable transcript/trace path with a
platform-verified payload contract. If that observation source is absent or
unreadable, the gate emits a soft advisory naming the unverified rule; it
does not issue evidence-free hard remediation.
**Rationale:** A sentinel written at skill entry records intent; it cannot
prove subsequent tool calls by itself. Claude Code and Codex document
transcript paths for Stop/SubagentStop inputs; Copilot documents
`transcriptPath` for `agentStop`/`subagentStop`; Cursor documents
`transcript_path` for agent hooks and `agent_transcript_path` for
`subagentStop`. This keeps enforcement truthful while allowing stronger checks
where the platform exposes the necessary evidence.

### D8. Approved non-terminal hooks prevent or preserve; terminal hooks prove

**Decision:** Ship three non-terminal lifecycle uses in the primary bundles:

- `PreToolUse` may hard-remediate only a deterministic command boundary that
  is already forbidden by the skill contract, such as a delegated
  `iteration-close` path attempting `workflow advance`, or a `loop-worker`
  attempting orchestrator-only workflow commands. It does not replace
  terminal artifact validation.
- `SubagentStart` supplies `loop-worker` bootstrap context and correlation
  information for its later sentinel/stop evaluation. It is not evidence
  that the worker complied.
- `PreCompact` emits continuity context or advice while an active sentinel
  still expects closeout work. It does not block compaction in v1.

**Rationale:** These events reduce preventable drift or loss of active
context, while Stop/SubagentStop remain the portable proof point for final
artifacts and archival state.

### D9. Post-tool events are observation candidates, not assumed gates

**Decision:** Evaluate `PostToolUse` and `PostToolUseFailure` as observation
sources for workflow-command success/failure feedback and future telemetry.
Do not ship blocking behavior from them in this plan unless a later task
documents a deterministic invariant, portable vendor contract, and acceptable
noise/privacy boundary. R1.5 owns the structured observation/outcome decision.
**Rationale:** Failed fanout, checkpoint, merge-back, or closeout attempts are
useful improvement evidence, but recording an error is not itself proof that
the session should be blocked.

## Requirements (behavioral)

### R1. iteration-close-gate

When the `iteration-close` skill has run during the current turn
(sentinel present) and the agent attempts to stop or a subagent attempts
to stop, the gate MUST:

- R1.1 Validate that every artifact declared in the sentinel's
  `expected_artifacts` exists.
- R1.2 If a verify-record entry is required for the current run_id but
  missing, emit hard remediation with a reason that names the missing
  artifact.
- R1.3 If `/self-review` returned `reject` but a `merge-back` was written
  anyway, emit hard remediation.
- R1.4 If delegation is active and a readable, verified trace proves
  `workflow advance` was invoked (an orchestrator-only action), emit a hard
  remediation request. If no usable trace is supplied, emit a soft coverage
  advisory.
- R1.5 If the commit hash recorded in iter-log does not match
  `git log -1 --format=%h`, emit a soft advisory.
- R1.6 If a readable trace proves `make build-prod` (or analogous heavy
  build) ran mid-iteration, emit a soft advisory.
- R1.7 If no sentinel is present, exit 0 silently — the skill did not
  run this turn and there is nothing to enforce.
- R1.8 On verified `PreToolUse` input, if a delegated closeout sentinel is
  active and the attempted command is `workflow advance`, hard-remediate
  before execution with the required `workflow merge-back` action. Other
  closeout sequencing remains terminally verified.
- R1.9 On `PreCompact`, when its sentinel is still active, emit advisory
  continuity context naming the unresolved verify/checkpoint/merge-back or
  advance obligation; do not block compaction.

### R2. isp-gate

When the `isp` skill has run during the current turn, the gate MUST:

- R2.1 If a bundle file under `.agents/active/delegation-bundles/` was
  written this turn but no parent-gate entry was appended to the
  iter-log after merge-back, emit hard remediation.
- R2.2 If the sentinel records `eligible_snapshot_loaded: false` and a
  readable trace proves `workflow orient` was re-run by the orchestrator
  (the orchestrator was supposed to load the snapshot from
  `orchestrator-session-start`), emit hard remediation. Without a usable trace,
  emit a soft coverage advisory.
- R2.3 If a readable trace contains no direct-vs-fanout decision text for
  this run, emit a soft advisory. Without a usable trace, emit a soft
  coverage advisory.
- R2.4 If `max_batch > 1` was declared but only one bundle materialized,
  emit a soft advisory.
- R2.5 No-sentinel → exit 0.
- R2.6 On `PreCompact`, when an ISP sentinel remains active, emit advisory
  context naming the selected task/bundle and the next unresolved stage; do
  not infer stage completion or block compaction.

### R3. loop-worker-gate

When a subagent attempts to stop and a loop-worker sentinel is present
(or the sentinel's `agent_type == "loop-worker"`), the gate MUST:

- R3.1 Diff the subagent's git changes against the sentinel's recorded
  `write_scope`. Any file modified outside `write_scope` is a hard
  remediation, naming the offending path and the allowed scope.
- R3.2 If a readable trace proves any of
  `workflow advance|orient|next|status` were invoked while the sentinel was
  present, emit hard remediation (orchestrator surface, not worker surface).
  Without a usable trace, emit a soft coverage advisory.
- R3.3 If `loop-state.md`'s `## Current Position` section was edited by
  the subagent, emit hard remediation.
- R3.4 If no `merge-back/<task>.md` artifact exists for the closing
  task, emit hard remediation.
- R3.5 If no positive+negative test trace was captured this run, emit a
  soft advisory.
- R3.6 If uncommitted changes remain at subagent stop, emit a soft
  advisory.
- R3.7 No-sentinel or `agent_type != "loop-worker"` → exit 0.
- R3.8 On `SubagentStart`, supply the loop-worker bootstrap reminder and
  correlation context needed to load the bundle, honor `write_scope`, and
  use delegated `iteration-close`; this event does not mark compliance.
- R3.9 On verified `PreToolUse` input, hard-remediate an attempted
  `workflow advance`, `workflow orient`, `workflow next`, or
  `workflow status` while a loop-worker sentinel is active, before that
  forbidden worker action runs.
- R3.10 On `PreCompact`, when a loop-worker sentinel remains active, emit
  advisory continuity context naming the task, write scope, and required
  delegated closeout; do not block compaction.

### R4. Sentinel protocol

- R4.1 The CLI MUST expose `write`, `read`, and `clear` verbs for
  sentinels keyed by skill name and run-id.
- R4.2 Sentinel files MUST be written under
  `.agents/active/hook-sentinels/<skill>-<run-id>.json` with a
  versioned schema (schema_version, skill, run_id, started_at,
  plan_id, task_id, agent_type, expected_artifacts, and context). Context
  MUST be able to carry `git_head_at_start`, `write_scope`,
  `eligible_snapshot_loaded`, `max_batch`, and an optional trace-path hint;
  hook stdin remains authoritative when it provides a platform trace path.
- R4.3 `read --latest` MUST return the most recent sentinel for a given
  skill name; `read --run-id` MUST return an exact match or error.
- R4.4 `clear` MUST remove a sentinel by run-id; on success path, the
  gate (or the skill) MUST archive the record under
  `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/`.
- R4.5 Each enforced skill MUST call `da workflow hook-sentinel write`
  in its workflow before doing any other work — sentinels declare
  intent, so missing them means no enforcement.

### R5. Two-tier output contract

- R5.1 Hard violations MUST use each platform's documented native
  continuation/remediation output. Claude Code, Codex, and Copilot use
  `{"decision":"block","reason":"<message>"}` where documented. Cursor
  uses `{"followup_message":"<message>"}` for native `stop` /
  `subagentStop` hooks. P1c MUST record the per-platform output table in
  `docs/HOOKS.md`.
- R5.2 Soft violations MUST emit human-readable advisory text to stderr
  and exit 0.
- R5.3 Reason strings MUST be actionable: name the violated rule, the
  offending artifact or path, and the remediation step.
- R5.4 Hooks MUST NOT exceed `timeout_ms: 8000` so they cannot livelock
  the stop path.

### R6. Per-platform event coverage

The platform mapper in `internal/platform/hooks.go` MUST be extended to
render the events the three gates depend on:

- R6.1 Codex: `SubagentStop` (primary loop-worker-gate target) plus
  `SubagentStart`, `PreCompact`, `PostCompact`, `PermissionRequest` for
  forward parity.
- R6.2 Copilot: `agentStop` (NOT `stop` — camelCase footgun) plus
  `sessionEnd`, `postToolUse`, `postToolUseFailure`, `preCompact`,
  `notification`, `permissionRequest`, `subagentStart`, `subagentStop`,
  `errorOccurred`.
- R6.3 Cursor: `subagentStop` (dedicated reference subsection in vendor
  docs) plus `sessionEnd`, `postToolUse`, `postToolUseFailure`,
  `preCompact`, `subagentStart`, plus the Cursor-wider surface
  (`beforeShellExecution`, `afterShellExecution`, `beforeMCPExecution`,
  `afterMCPExecution`, `beforeReadFile`, `afterFileEdit`,
  `afterAgentResponse`, `afterAgentThought`, `workspaceOpen`,
  `beforeTabFileRead`, `afterTabFileEdit`).
- R6.4 New canonical `HookSpec.When` values from D3 + the May 2026
  refresh (`post_compact`, `error_occurred`) MUST be introduced and
  documented in `docs/HOOKS.md`.
- R6.5 The Codex matcher whitelist in `renderCodexHookConfig` MUST NOT
  be extended by guessing. Each new event must be verified per vendor
  docs for matcher support before its name is added to the whitelist.
- R6.6 Claude Code's documented wider lifecycle surface MUST be mapped
  and documented in a dedicated parity task: `Setup`,
  `UserPromptExpansion`, `PostToolBatch`, `PermissionDenied`,
  `StopFailure`, `TeammateIdle`, `TaskCreated`, `TaskCompleted`,
  `WorktreeCreate`, `WorktreeRemove`, `FileChanged`, `ConfigChange`,
  `CwdChanged`, `InstructionsLoaded`, `Elicitation`, and
  `ElicitationResult`. These names were verified against current
  official documentation on 2026-05-25; the mappings do not broaden the
  three discipline gates themselves.
- R6.7 Gate behavior now uses non-terminal events only within D8: portable
  `PreToolUse` early prevention where vendor input/output verification
  supports the rule; `SubagentStart` bootstrap for `loop-worker`; and
  `PreCompact` continuity advice. `PostToolUse` and `PostToolUseFailure`
  remain parity/observation inputs pending the R1.5 evaluation in D9.

### R7. Starter scaffold delivery

- R7.1 The three skill bundles (`iteration-close`, `isp`, `loop-worker`)
  MUST ship inside the dot-agents starter scaffold so any project
  initialized via `da init` / `da refresh` inherits them.
- R7.2 The starter MUST ship the `loop-worker` AGENT.md under an
  `agents/` tier. `internal/scaffold/home.CopyMissingStarterAssets`
  already recursively materializes embedded starter descendants and
  `commands/init.go:createInitialAgentsDirs` creates `agents/global`;
  P3 extends copy assertions rather than adding speculative loader code.
- R7.3 The starter MUST ship the `loop-worker` profile referenced by
  the skill (step 0 source).
- R7.4 The three hook bundles (`iteration-close-gate`, `isp-gate`,
  `loop-worker-gate`) MUST ship under
  `internal/scaffold/hooks/global/` with HOOK.yaml + gate.sh entries.
  Their manifests MAY cover multiple approved lifecycle events using the
  P1c multi-event representation; they are not limited to terminal events.

### R8. Skill wiring

- R8.1 Each enforced skill MUST add a sentinel-write call at the start
  of governed work, after prerequisites required to construct the
  sentinel context have been resolved.
- R8.2 Each skill's gotchas / proposal-criteria documentation MUST note
  the hook contract so future skill edits cannot silently break the
  gate.

### R9. Adjacent discipline skills and downstream adoption

- R9.1 `agent-handoff` and `delegation-lifecycle` MUST be evaluated as
  discipline-skill companions. At minimum, complete starter assets ship
  for both; a hook is added only when a deterministic lifecycle invariant
  or bounded continuity purpose and observable evidence are identified.
- R9.2 The payout workspace MUST receive an explicit migration and
  readback task after enforcement verification. Silent inheritance via
  `da refresh` is not adequate for an active downstream loop: its current
  `.agentsrc.json` has `"hooks": false` and does not declare `isp` or
  `loop-worker`.
- R9.3 Migration of legacy project-specific discipline skill overrides
  MUST be the final task in this plan, after payout has served as the
  first verified downstream migration.

## Done criteria (verifiable)

- DC1 Unit tests for `da workflow hook-sentinel write/read/clear`
  covering happy path, latest selection, run-id collision handling,
  and clear-after-success.
- DC2 Per-platform golden-fixture tests in
  `internal/platform/hooks_test.go` rendering Codex `SubagentStop`,
  Copilot `agentStop`, and Cursor `subagentStop` for canonical
  `HookSpec` inputs.
- DC3 `docs/HOOKS.md` documents the one-to-one mapping principle
  (D2), the operator pattern for cross-platform coverage, the
  Cursor-wider surface (D3), and the Copilot `agentStop` footgun.
- DC4 Three HOOK.yaml + gate.sh entries exist under
  `internal/scaffold/hooks/global/{iteration-close-gate,isp-gate,loop-worker-gate}/`,
  each enabled on applicable documented platform events via D2 fall-through;
  fixtures cover terminal validation, approved early guards, worker startup,
  and compaction advice.
- DC5 `internal/scaffold/home/starter/skills/global/{iteration-close,isp,loop-worker}/`
  exist with SKILL.md + instructions tree, and
  `internal/scaffold/home/starter/agents/global/loop-worker/AGENT.md`
  exists. `CopyMissingStarterAssets` / `da init` against an empty home
  writes them.
- DC6 Each of the three skill SKILL.md / workflow files contains a
  `hook-sentinel write` call before governed actions after required
  invocation context is available.
- DC7 Integration test at `tests/test-loop-discipline-stop-hooks.sh`
  simulates an artifact/write-scope hard-remediation outcome, a
  trace-backed hard-remediation outcome where platform input makes the
  evidence observable, and an
  advisory outcome for a soft issue or unavailable trace. It asserts
  platform-native remediation output and advisory stderr content.
- DC8 Running `bin/da refresh` in a sandbox project after these
  changes materializes all three skills, the loop-worker AGENT.md,
  and the three hook scripts, and no `removing existing file ...
  directory not empty` warnings appear.
- DC9 Claude wider-lifecycle mapper tests and `docs/HOOKS.md` coverage
  land for the officially verified R6.6 event set.
- DC10 `agent-handoff` and `delegation-lifecycle` companion assessment
  is recorded; both complete starter asset trees materialize in a
  sandbox home, with any hook decision justified by observable evidence.
- DC11 Payout migration readback demonstrates that its active loop
  resolves the new skills and hooks without overwriting unrelated
  active artifacts.
- DC12 Legacy override migration inventory is completed last, recording
  each migrated or deliberately retained downstream override.
- DC13 R1.5 records an explicit decision for `PostToolUse` /
  `PostToolUseFailure` observation potential, including whether failed
  workflow commands can be persisted as non-blocking feedback without
  transcript content.

## Open questions (to be resolved during the plan)

The plan MUST resolve each of these in its task contract before the
relevant task starts.

- Q1 **CLI scope for `hook-sentinel`.** Resolved for v1: nest under
  `da workflow` since sentinels are tied to plan/task IDs.
- Q2 **Sentinel lifetime.** Resolved for v1: archive successful sentinel
  records under
  `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/`, alongside
  durable loop history. Retention/pruning is deferred.
- Q3 **Hard-vs-soft threshold tuning.** Is "generic verify summary" a
  hard-remediation outcome or soft advisory? Listed as soft in R1; may need
  hardening after real-world calibration.
- Q4 **Schema extension `when_events: []`.** Resolved for v1: add a
  backward-compatible, mutually exclusive multi-event representation
  in P1c so `iteration-close-gate` has one HookSpec for Stop and
  SubagentStop rather than duplicated entries.
- Q5 **Matcher and native output support per vendor (D.6).** Per-event
  verification of matcher input, transcript fields, and hard-remediation
  output is owned by P1c. Cursor is verified as exposing
  `transcript_path` / `agent_transcript_path` and native
  `followup_message`, rather than Claude-style `decision: block`.
- Q6 **Loop-worker matcher portability.** Resolved for v1 per D6: rely
  solely on gate-level filtering via sentinel `agent_type`; do not add a
  Claude-only matcher override.
- Q7 **Backfill payout.** Resolved: include an explicit payout
  migration/readback task after integration verification and before
  general legacy-override migration. Do not rely on silent inheritance.

## Companion safety and quality hook bundles

The discipline gates above are the enforcement layer for skill contracts.
They ship alongside a small set of general-purpose safety and quality hook
bundles that any project inheriting the starter scaffold also picks up.
These are non-discipline hooks, but they share the same lifecycle-event
plumbing and are inventoried here so the concrete blocklist, detector, and
formatter values live in one canonical place.

| Hook | Event | Behavior | Blocking policy |
|------|-------|----------|-----------------|
| `session-orient` | `session_start` | Emits orient Markdown | never blocks |
| `session-capture` | `stop` | Writes checkpoint and appends session log | never blocks |
| `guard-commands` | `pre_tool_use` on shell commands | Blocks exact destructive patterns | may block |
| `secret-scan` | `post_tool_use` on file edits | Warns on likely secrets | never blocks |
| `auto-format` | `post_tool_use` on file edits | Runs best-effort formatter by extension | never blocks |

### Guard-commands initial blocklist

`guard-commands` hard-blocks these exact destructive patterns:

- `rm -rf /`
- `rm -rf ~`
- `git push --force origin main`
- `git push --force origin master`
- `DROP DATABASE`
- `DROP TABLE`
- `truncate`
- `:(){ :|:& };:`

### Secret-scan initial detectors and allowlist

`secret-scan` warns (never blocks) on these detector patterns:

- Anthropic keys matching `sk-ant-api`
- AWS access keys matching `AKIA[0-9A-Z]{16}`
- GitHub tokens matching `ghp_`, `gho_`, or `ghs_`
- Stripe keys matching `sk_live_` or `sk_test_`
- OpenAI keys matching `sk-[a-zA-Z0-9]{20,}`

The initial placeholder allowlist suppresses matches containing
`YOUR_KEY`, `REPLACE_ME`, `example`, `xxxx`, or `test_`.

### Auto-format routing

`auto-format` runs a best-effort formatter chosen by file extension:

- `.go` -> `gofmt -w`
- `.py` -> `ruff format --quiet`, fallback `black --quiet`
- `.ts`, `.tsx`, `.js`, `.jsx`, `.css`, `.scss`, `.json`, `.yaml`, `.yml` -> `npx prettier --write`
- `.rs` -> `rustfmt`

Formatter availability is best effort: a missing formatter MUST NOT fail
the hook.

_Consolidated from the retired docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md._

## Deferred items (explicit out-of-scope)

The following are deliberately not in scope and may appear in a future
spec or refresh:

- DEF2 OpenCode hooks-to-plugins emitter bridge (see D4).
- DEF5 Telemetry / metrics emission from gates is implementation-owned by
  follow-up plan `r1-5-hook-enforcement-telemetry`, rather than reopening
  the completed `r1-outcome-scoring` baseline.

## Relationship to other specs and plans

- **Predecessor:** `.agents/proposals/loop-discipline-stop-hooks-scoping.md`.
  The scoping doc is the source of every decision in this spec.
- **Cross-plan consumer:** `r1-outcome-scoring` is completed. Follow-up
  plan `r1-5-hook-enforcement-telemetry` consumes gate outcomes and
  archived sentinels without reopening the shipped baseline.
- **Downstream migration:** payout is the first explicit migration and
  readback target before the final general legacy-override sweep.
- **Starter-copy dependency:** P3 extends existing recursive starter
  copy assertions in `internal/scaffold/home/copy_test.go`; it does not
  reserve loader implementation files.
