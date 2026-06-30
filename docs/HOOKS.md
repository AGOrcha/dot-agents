---
title: Hooks
description: Store agent lifecycle hooks once and wire them into every platform that supports them.
sidebar:
  order: 1
---

# Hooks

Hooks let your AI agents run a command automatically at a point in their
lifecycle — for example, formatting a file after it is edited, blocking a
write that would commit a secret, or loading session context when a session
starts. `da` keeps your hooks in one place and wires them into each platform
that supports them.

> This is the user-facing guide. For a quick command summary see the
> **Hooks** entry in the [README](../README.md#hooks); this document
> covers the storage model and per-platform behavior in more detail.

## The canonical model

Hooks live under a single source of truth at `~/.agents/hooks/`, organized by
scope:

```text
~/.agents/hooks/
  global/        # applied to every managed project
  <project>/     # applied only to that project
```

Scope names match the project names shown by `da status` (`global` is the
all-projects scope).

A hook is a **bundle directory** containing a `HOOK.yaml` manifest plus any
sidecar assets it needs (a script, a template, data):

```text
~/.agents/hooks/global/
  format-write/
    HOOK.yaml      # the hook manifest
    run.sh         # a sidecar script the hook invokes
```

Older single-file JSON hooks (`hooks/<scope>/<name>.json`) are still
recognized and listed for visibility, but bundle directories are the
preferred form for new hooks. `da import` and `da refresh` canonicalize
hook content into this same layout.

## Per-platform behavior

Hooks are distributed only to platforms that can represent them. Coverage
today (consistent with the Hook Wiring Audit in
[PLATFORM_DIRS_DOCS.md](./PLATFORM_DIRS_DOCS.md)):

| Platform | Where hooks land | Status |
|----------|------------------|--------|
| Claude Code | `.claude/settings.json` / `.claude/settings.local.json` | Wired |
| Cursor | `.cursor/hooks.json` (project) and `~/.cursor/hooks.json` (user) | Wired |
| Codex | `.codex/hooks.json` | Wired (project `.codex/hooks.json` is rendered from the canonical hook spec and removed again on project teardown) |
| GitHub Copilot | `.github/hooks/*.json` plus Claude-compatible settings | Wired (canonical hooks fan out to `.github/hooks/*.json`; legacy single-file hooks still emit) |
| OpenCode | — | No dedicated hook file is documented upstream, so none is created |

If a platform cannot represent a particular hook, it is skipped for that
platform rather than emitted incorrectly.

## Canonical event mapping (`HookSpec.When`)

`HookSpec.When` uses platform-neutral snake_case canonical values. Each
canonical value maps to **at most one** documented vendor event per
platform. If a vendor does not document an event, the mapper omits the
hook for that platform — there is no semantic-equivalence fan-out
(see [decision D2 in the design spec][d2]). Operators who want a single
semantic concept on multiple platforms declare additional `HookSpec`
entries, each targeting that platform's nearest documented event.

GitHub Copilot's terminal event for the top-level agent is named
**`agentStop`**, NOT `stop`. The canonical `stop` value renders as
`agentStop` on Copilot only; on Claude, Codex, and Cursor it renders as
`Stop` / `stop`. This is a deliberate per-vendor footgun guard — the
Claude/Cursor `stop` literal does not exist in Copilot's event surface.

The mapping table below records the canonical `HookSpec.When` values
shipped today. An empty cell means the vendor does not document the
event and the renderer omits it for that platform (the
`hookRequiredOnPlatform` fall-through is the right behavior — if you
mark such a hook `required_on` that vendor, the renderer errors).

| Canonical `When`              | Claude Code        | Codex               | Cursor                  | GitHub Copilot         |
|-------------------------------|--------------------|---------------------|-------------------------|------------------------|
| `pre_tool_use`                | `PreToolUse`       | `PreToolUse`        | `preToolUse`            | `preToolUse`           |
| `post_tool_use`               | `PostToolUse`      | `PostToolUse`       | `postToolUse`           | `postToolUse`          |
| `post_tool_use_failure`       | `PostToolUseFailure` |                   | `postToolUseFailure`    | `postToolUseFailure`   |
| `user_prompt_submit`          | `UserPromptSubmit` | `UserPromptSubmit`  | `beforeSubmitPrompt`    | `userPromptSubmitted`  |
| `notification`                | `Notification`     |                     |                         | `notification`         |
| `session_start`               | `SessionStart`     | `SessionStart`      | `sessionStart`          | `sessionStart`         |
| `session_end`                 | `SessionEnd`       |                     | `sessionEnd`            | `sessionEnd`           |
| `stop`                        | `Stop`             | `Stop`              | `stop`                  | `agentStop`            |
| `subagent_start`              | `SubagentStart`    | `SubagentStart`     | `subagentStart`         | `subagentStart`        |
| `subagent_stop`               | `SubagentStop`     | `SubagentStop`      | `subagentStop`          | `subagentStop`         |
| `pre_compact`                 | `PreCompact`       | `PreCompact`        | `preCompact`            | `preCompact`           |
| `post_compact`                | `PostCompact`      | `PostCompact`       |                         |                        |
| `permission_request`          | `PermissionRequest`| `PermissionRequest` |                         | `permissionRequest`    |
| `error_occurred`              |                    |                     |                         | `errorOccurred`        |

### Claude-wider surface (Claude-only canonical values)

Claude Code documents a wider lifecycle event surface than the
gate-critical core (P1d, R6.6). These events are promoted to canonical
`HookSpec.When` values so an operator who wants to attach a hook to
one of them does not need to bypass the canonical model with a
`platform_overrides.event` escape hatch. Other platforms have no
documented equivalent today; their mappers no-op for these values per
D2 (no semantic-equivalence inference).

Adding these to the canonical mapper does **not** attach any gate. The
loop-discipline gate scripts (`p2-hook-scripts`) and the
`workflow hook-sentinel` write paths (`p4-sentinel-wiring`) listen on
their own pre-existing events. Promoting an entry here to a gate
requires its own task with a documented invariant.

| Canonical `When`              | Claude Code           |
|-------------------------------|-----------------------|
| `setup`                       | `Setup`               |
| `user_prompt_expansion`       | `UserPromptExpansion` |
| `post_tool_batch`             | `PostToolBatch`       |
| `permission_denied`           | `PermissionDenied`    |
| `stop_failure`                | `StopFailure`         |
| `teammate_idle`               | `TeammateIdle`        |
| `task_created`                | `TaskCreated`         |
| `task_completed`              | `TaskCompleted`       |
| `worktree_create`             | `WorktreeCreate`      |
| `worktree_remove`             | `WorktreeRemove`      |
| `file_changed`                | `FileChanged`         |
| `config_change`               | `ConfigChange`        |
| `cwd_changed`                 | `CwdChanged`          |
| `instructions_loaded`         | `InstructionsLoaded`  |
| `elicitation`                 | `Elicitation`         |
| `elicitation_result`          | `ElicitationResult`   |

Source: <https://code.claude.com/docs/en/hooks> (verified 2026-05-26).

### Cursor-wider surface (Cursor-only canonical values)

Cursor publishes a fine-grained event surface that no other vendor
mirrors today. These events are still promoted to canonical
`HookSpec.When` values (see [decision D3 in the design spec][d3]) so
operator `HookSpec` entries do not need to be rewritten when another
vendor copies the surface. Until then, the Claude / Codex / Copilot
mappers no-op for these values.

| Canonical `When`              | Cursor                 |
|-------------------------------|------------------------|
| `before_shell_execution`      | `beforeShellExecution` |
| `after_shell_execution`       | `afterShellExecution`  |
| `before_mcp_execution`        | `beforeMCPExecution`   |
| `after_mcp_execution`         | `afterMCPExecution`    |
| `before_read_file`            | `beforeReadFile`       |
| `after_file_edit`             | `afterFileEdit`        |
| `after_agent_response`        | `afterAgentResponse`   |
| `after_agent_thought`         | `afterAgentThought`    |
| `workspace_open`              | `workspaceOpen`        |
| `before_tab_file_read`        | `beforeTabFileRead`    |
| `after_tab_file_edit`         | `afterTabFileEdit`     |

### Cross-platform bundles assume canonical coverage, not parity

A single canonical `HookSpec` is portable to every vendor whose mapper
documents the event. If you author a cross-platform hook bundle, use
the canonical values from the table above and accept that **not every
vendor will implement every non-critical event**. The `Stop` /
`SubagentStop` terminal events are documented on every supported
platform; the wider lifecycle surface is uneven and will stay uneven
until vendors converge.

If a hook is genuinely required on a platform that does not document
the event, set `required_on:` for that platform — the renderer will
error loudly on `da refresh` rather than silently omit the hook.

### Non-terminal lifecycle events: prevent or preserve, do not gate

Per [decision D8 in the design spec][d8], three non-terminal events are
approved for hook use under the loop-discipline gates. They are
intentionally **not** treated as terminal proofs of completion:

- `PreToolUse` (canonical `pre_tool_use`) may hard-remediate a
  deterministic command-boundary violation that is already forbidden by
  the calling skill's contract — for example, a delegated
  `iteration-close` path attempting `workflow advance`. It does not
  replace terminal artifact validation.
- `SubagentStart` (canonical `subagent_start`) supplies `loop-worker`
  bootstrap and correlation information. It is not evidence the worker
  complied.
- `PreCompact` (canonical `pre_compact`) emits continuity advice while
  an active sentinel still expects closeout. It does not block
  compaction in v1.

These three events are wired only where this plan establishes a vendor
contract (gate-script wiring lands in plan task `p2-hook-scripts`).
Terminal events — `Stop`, `SubagentStop`, Copilot's `agentStop` — remain
the authoritative artifact-validation point.

### Session-handoff: crash-survivable compaction recovery

Two bundles wire the session-handoff journal (`da workflow journal`) into the
compaction lifecycle so a context compaction is survivable: the verifiable
*live state* (task↔PR↔merge, locks, recent durable deltas) that the
auto-summary drops is captured before compaction and re-injected, re-verified,
after it.

- **`session-handoff-snapshot`** fires on `PreCompact` (no matcher — every
  compaction) and runs `da workflow journal snapshot`, capturing a fresh
  deterministic snapshot to durable, non-git-tracked state that *survives*
  compaction. It writes nothing into the context (that context is about to be
  discarded). Enabled on `claude`, `codex`, `cursor` — every platform that
  maps `pre_compact`.
- **`session-handoff-recover`** fires on `SessionStart` narrowed to the
  `compact` start source (the manifest's `match.expression: compact`) and runs
  `da workflow journal recover`, printing the **verified recovery view** on
  stdout. A `SessionStart` hook's stdout re-enters the fresh post-compaction
  context, which is exactly how the recovered state comes back. A normal
  `startup` / `resume` / `clear` session start is untouched. Enabled on
  `claude` and `codex` — the two platforms whose `SessionStart` surface
  documents the `compact` source (see the [start-source matcher table](#matcher-support-which-renderer-emits-a-non-empty-matcher)).

Both hooks are **strictly best-effort**: a journal failure (or a `da` binary
that predates the journal surface) must never block compaction or session
start. Each script reports failures on stderr only and always exits 0. They
reuse the existing `pre_compact` / `session_start` event model — no new
canonical event was required.

### `PostToolUse` and `PostToolUseFailure` are observation candidates

Per [decision D9 in the design spec][d9], `post_tool_use` and
`post_tool_use_failure` are mapped today so operators *may* author
**observation** hooks against them, but post-tool observation as a
scored signal was **evaluated and deferred to R1.5.1** in
`r1-5-hook-enforcement-telemetry/t1b-post-tool-observation-evaluation`
(PR #97): the four boundary criteria (payload stability, workflow-command
filter, redaction, dedup) did not all clear v1. As a result, no
`observe_tool_result` records contribute to the v1 `hook_outcomes`
sub-score — see [`OUTCOME_SCORING_RUBRIC.md` — post-tool deferral](./OUTCOME_SCORING_RUBRIC.md#post-tool-observation-evaluation-r15-t1b).
These events are also **not** implicit blocking hooks: a failed workflow
command produces useful improvement evidence, but recording an error is
not by itself proof that the session should be blocked. Any future
blocking or scored use must document a deterministic invariant, a
portable vendor contract, and an acceptable noise / privacy boundary.

### Lifecycle gates and the hook-outcome telemetry channel

The lifecycle gates wired above emit a structured per-iteration record
every time they fire. The schema for that record lives at
[`schemas/workflow-hook-outcome.schema.json`](../schemas/workflow-hook-outcome.schema.json)
(also bundled under
[`commands/workflow/static/workflow-hook-outcome.schema.json`](../commands/workflow/static/workflow-hook-outcome.schema.json)
for the validator pipeline). It defines the durable contract for the
fields a gate may emit — `sentinel_id`, `rule_id`, `lifecycle_point`,
`intervention_class`, `result` (`allow` / `advise` / `remediate`),
`correlation_id` — and explicitly forbids transcript bodies, raw tool
output, free-text failure messages, and other unbounded fields. The
validator rejects any record that includes a disallowed field.

Gates never write the sidecar directly. They invoke
`da workflow hook-outcome write`, which resolves the current iteration N
from `.agents/active/loop-state.md` and appends to
`.agents/active/iteration-log/iter-N.hook-outcomes.yaml`. Append-only
semantics and `(sentinel_id, rule_id, lifecycle_point,
intervention_class)` idempotency mean a recoverable platform retry of
the same gate does not inflate the record list. If no active iteration
exists, the write exits 0 silently with an stderr advisory — the
session-only outcome is dropped from scoring rather than orphaned.

The sidecars feed the `hook_outcomes` signal of the outcome-scoring
rubric. The rubric document explains how `prevent_before_action` and
`remediate_at_stop` records collapse into a per-iteration sub-score, how
pre-action and terminal records sharing a `(correlation_id, rule_id)`
deduplicate to the more severe result, and which rule-ID families
contribute to v1 scoring versus which are persisted as audit-only
observation. See:

- [`OUTCOME_SCORING_RUBRIC.md` — `hook_outcomes` signal](./OUTCOME_SCORING_RUBRIC.md#6-hook_outcomes--hook-gate-outcomes-weight-010-r15)
  for the sub-score mapping and dedup rule.
- [`OUTCOME_SCORING_RUBRIC.md` — approved rules](./OUTCOME_SCORING_RUBRIC.md#approved-rules-feeding-the-v1-sub-score-per-r15-design-d6)
  for the rule-ID families that vote in v1.
- [`OUTCOME_SCORING_RUBRIC.md` — sidecar retention](./OUTCOME_SCORING_RUBRIC.md#hook-outcome-sidecar-retention-r15)
  for the indefinite-retention policy and the deferred admin-only prune
  command spec.
- [`OUTCOME_SCORING_RUBRIC.md` — post-tool deferral](./OUTCOME_SCORING_RUBRIC.md#post-tool-observation-evaluation-r15-t1b)
  for why `post_tool_use` records do not contribute to v1 scoring.

## `when_events` — multi-event hook bundles

A `HOOK.yaml` manifest may target several lifecycle events with a single
bundle by replacing the scalar `when` field with a `when_events` array:

```yaml
# ~/.agents/hooks/global/loop-worker-gate/HOOK.yaml
name: loop-worker-gate
description: Terminal validation for delegated loop-worker sessions.
when_events:
  - pre_tool_use
  - stop
  - subagent_stop
run:
  command: ./gate.sh
  timeout_ms: 5000
```

Loader / render rules (enforced in
[`internal/platform/hooks.go`](../internal/platform/hooks.go)):

- The scalar `when` field is still supported and behaves exactly as
  before; existing manifests need no change.
- A manifest must specify **exactly one** of `when` or a non-empty
  `when_events`. Setting both is rejected at load time.
- `when_events` entries must be **canonical** event names (the
  snake_case strings in the mapping tables above). Unknown values
  are rejected so typos cannot silently no-op on every platform.
- Duplicate entries inside `when_events` are rejected.
- At render time the bundle expands into one action per canonical
  event the target platform documents. Events the platform does not
  document are omitted silently (unless the platform appears in
  `required_on`, which keeps the explicit-opt-in error surface).
- Copilot's per-event JSON fanout disambiguates filenames by
  appending the canonical event name (e.g.
  `loop-worker-gate-stop.json`, `loop-worker-gate-subagent_stop.json`).
  Scalar `when` bundles keep their pre-P1c `<name>.json` filename.

## Per-event input and native-remediation reference

The tables below record the vendor-documented input and output surface
each canonical event exposes. They are the evidence base for which
events can support deterministic prevention, continuity advice, or
trace-dependent terminal validation. Sources:

- Claude Code hooks: <https://code.claude.com/docs/en/hooks>
- Codex hooks: <https://developers.openai.com/codex/hooks>
- GitHub Copilot hooks: <https://docs.github.com/en/copilot/reference/hooks-configuration>
- Cursor hooks: <https://cursor.com/docs/hooks.md>

### Trace / transcript input

| Vendor          | Event                                | Trace field name                | Notes                                                              |
|-----------------|--------------------------------------|---------------------------------|--------------------------------------------------------------------|
| Claude Code     | every hook                           | `transcript_path`               | Common hook input; available to all events.                        |
| Codex           | every hook                           | `transcript_path`               | Common hook input; available to all events.                        |
| GitHub Copilot  | `agentStop`, `subagentStop`          | `transcriptPath`                | Camel-cased per Copilot reference; absent on other events.         |
| Cursor          | every agent hook                     | `transcript_path`               | Common agent input.                                                |
| Cursor          | `subagentStop`                       | `agent_transcript_path`         | Sibling field on the subagent terminal event; readable when present. |

Trace-dependent terminal checks (R1.5 evidence reads, sentinel
trace-replay) can rely on the trace field on every vendor's terminal
event. On `PreToolUse` the trace field is available on Claude / Codex /
Cursor but not on Copilot.

### Native hard-remediation output

| Vendor          | Stop / SubagentStop / Stop-equivalent | Output mechanism documented            |
|-----------------|----------------------------------------|-----------------------------------------|
| Claude Code     | `Stop`, `SubagentStop`                 | JSON block / continuation decision (`{"decision": "block", "reason": "..."}`) |
| Codex           | `Stop`, `SubagentStop`                 | JSON block / continuation decision      |
| GitHub Copilot  | `agentStop`, `subagentStop`            | JSON block / continuation decision      |
| Cursor          | `stop`, `subagentStop`                 | `followup_message` field (NOT a JSON block / decision) |

The renderer and gate-script contract preserves this difference:
Claude / Codex / Copilot gates emit a JSON block decision; Cursor
gates emit a `followup_message`. They are not interchangeable, and the
gate-script template under task `p2-hook-scripts` keeps the per-vendor
output paths distinct.

### Matcher support (which renderer emits a non-empty matcher)

A matcher is only set when the vendor reference documents matcher
narrowing for that event. The Codex matcher whitelist is encoded in
`codexMatcherWhitelist` in
[`internal/platform/hooks.go`](../internal/platform/hooks.go); the
P1c verification round (May 2026) reviewed the Codex hooks reference
and confirmed the per-event matcher surface below.

| Vendor          | Events that render a matcher                          | Events that render `matcher=""` (and gate scripts must parse input instead) |
|-----------------|-------------------------------------------------------|-----------------------------------------------------------------------------|
| Claude Code     | every documented event                                | n/a — Claude matcher applies to every event in the table                    |
| Codex           | `PermissionRequest`, `PostCompact`, `PostToolUse`, `PreCompact`, `PreToolUse`, `SessionStart`, `SubagentStart`, `SubagentStop` | `Stop`, `UserPromptSubmit` (vendor docs: matcher explicitly ignored)        |
| Cursor          | per-spec `match.expression` is rendered when set; vendor defaults applied otherwise | n/a — Cursor renderer emits the configured matcher verbatim                |
| GitHub Copilot  | none — Copilot single-event files do not accept matcher narrowing in the documented schema; bundles with matchers are skipped | every event                                                                |

Codex matcher semantics per event (from the Codex hooks reference):

| Codex event          | What matcher filters       | Value examples                                |
|----------------------|----------------------------|-----------------------------------------------|
| `PermissionRequest`  | tool name                  | `Bash`, `^apply_patch$`, `mcp__filesystem__.*`|
| `PostToolUse`        | tool name                  | `Bash`, `Edit\|Write`, `mcp__filesystem__.*`  |
| `PostCompact`        | compaction trigger         | `manual`, `auto`, `manual\|auto`              |
| `PreCompact`         | compaction trigger         | `manual`, `auto`, `manual\|auto`              |
| `PreToolUse`         | tool name                  | `Bash`, `^apply_patch$`, `mcp__filesystem__.*`|
| `SessionStart`       | start source               | `startup`, `resume`, `clear`, `compact`       |
| `SubagentStart`      | subagent type              | depends on the subagent that starts           |
| `SubagentStop`       | subagent type              | depends on the subagent that stops            |
| `Stop`               | (matcher ignored by Codex) | `""`                                          |
| `UserPromptSubmit`   | (matcher ignored by Codex) | `""`                                          |

For `apply_patch`, matcher values can also use `Edit` or `Write`.

### Approved non-terminal lifecycle capabilities (verified)

| Canonical event   | What each vendor input enables (verified)                                                                                                                                                                                          |
|-------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `pre_tool_use`    | Claude / Codex / Cursor expose tool name + tool input to the hook; deterministic command-boundary remediation is possible by parsing input. Copilot exposes `preToolUse` but documents no matcher surface, so gates must parse input. |
| `subagent_start`  | Claude / Codex / Cursor / Copilot all expose subagent identity / agent_type input. Used only for `loop-worker` bootstrap and later correlation; output is non-blocking. |
| `pre_compact`     | Claude / Codex / Cursor / Copilot expose hook input but no block-the-compaction contract; used only for non-blocking continuity advice. |
| `post_tool_use` / `post_tool_use_failure` | Bounded result metadata is documented on Claude / Cursor / Copilot (`post_tool_use` and `post_tool_use_failure`); Codex exposes `post_tool_use` only. Operator-authored observation hooks are permitted, but post-tool observation as a scored signal is **deferred to R1.5.1 per t1b PR #97** — no `observe_tool_result` records contribute to the v1 `hook_outcomes` sub-score. **Not** for transcript body persistence under either path. |

[d2]: ../.agents/workflow/specs/loop-discipline-stop-hooks/design.md#d2-one-to-one-canonical-hookspecwhen--platform-event-mapping
[d3]: ../.agents/workflow/specs/loop-discipline-stop-hooks/design.md#d3-cursors-wider-event-surface-gets-canonical-hookspecwhen-values
[d8]: ../.agents/workflow/specs/loop-discipline-stop-hooks/design.md#d8-approved-non-terminal-hooks-prevent-or-preserve-terminal-hooks-prove
[d9]: ../.agents/workflow/specs/loop-discipline-stop-hooks/design.md#d9-post-tool-events-are-observation-candidates-not-assumed-gates

## Using hooks

`da hooks` inspects and manages the canonical hook resources under
`~/.agents/hooks/`:

```bash
# List hooks for the global scope (or a project scope)
da hooks list
da hooks list my-app

# Show one hook bundle (or a legacy single-file hook)
da hooks show global session-orient

# Remove a hook bundle directory or a legacy hooks/*.json file
da hooks remove global old-hook-bundle
```

All three subcommands accept the global flags (`--json`, `--dry-run`,
`--verbose`, `--yes`).

### Removing a hook vs. removing a project

These are different operations with different blast radius:

- `da hooks remove <scope> <name>` — **granular**: deletes a single
  hook bundle directory (or legacy `hooks/*.json` file) from
  `~/.agents/hooks/<scope>/`. Nothing else in the scope is touched.
- `da remove <project>` — **project teardown**: unlinks the project
  from every platform and clears the *contents* of its canonical
  directories (including `~/.agents/hooks/<project>/`), but keeps the
  now-empty directories in place.
- `da remove <project> --clean` — teardown that also removes the
  canonical directories themselves (including
  `~/.agents/hooks/<project>/`), leaving no skeleton behind.

The `global` hook scope is shared by every project and is never removed
by a project teardown; prune global hooks explicitly with
`da hooks remove global <name>`.

### Adding a hook

A hook is added by creating its bundle directory under the appropriate
scope — for example `~/.agents/hooks/global/<name>/HOOK.yaml` with any
sidecar scripts beside it. Hooks already present in a project can be pulled
into the canonical store with:

```bash
da import <project>
```

which detects existing hooks (along with rules, skills, and agents) and
copies them into `~/.agents/`. After changing hooks under `~/.agents/`,
re-apply them to your projects with:

```bash
da refresh
```

`da refresh` re-distributes the canonical hooks to each managed project for
every platform that supports them.

## See also

- [README — Hooks](../README.md#hooks) — quick command summary
- [PLATFORM_DIRS_DOCS.md](./PLATFORM_DIRS_DOCS.md) — full per-platform resource
  locations and the Hook Wiring Audit
