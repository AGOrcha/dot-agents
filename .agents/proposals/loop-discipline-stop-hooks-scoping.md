# Scoping: Loop-Discipline Stop Hooks (iteration-close / isp / loop-worker)

- type: project-local scoping & analysis (pre-proposal)
- status: draft / for-review
- date: 2026-05-25
- scope: `internal/scaffold/home/starter/`, `internal/scaffold/hooks/global/`,
  `internal/platform/hooks.go`, `commands/workflow/` (new hook-sentinel surface),
  plus skill/agent assets at `~/.agents/skills/dot-agents/{iteration-close,isp,loop-worker}/`
- follow-up: once aligned, lands as `workflow/specs/loop-discipline-stop-hooks/design.md`
  + `workflow/plans/loop-discipline-stop-hooks/` (multi-phase plan)

## Promotion addendum (2026-05-25)

This scoping draft has been promoted and amended by
`.agents/workflow/specs/loop-discipline-stop-hooks/design.md`. Where the
sections below differ, the promoted spec and task contracts are
authoritative:

- Successful sentinels archive under
  `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/`, not below
  active state.
- Cursor documents `transcript_path` / `agent_transcript_path`, but native
  `stop` / `subagentStop` remediation returns `followup_message`, not
  Claude-style `decision: block`.
- Claude wider lifecycle mapping, `agent-handoff` /
  `delegation-lifecycle` scaffolding assessment, payout migration, and
  final downstream legacy-override migration are now in scope.
- Gate telemetry is tracked by follow-up plan
  `r1-5-hook-enforcement-telemetry`, since `r1-outcome-scoring` is already
  completed.

## Goal

Add Stop / SubagentStop hooks that enforce the discipline contracts of three
existing skills/agent — `iteration-close`, `isp`, `loop-worker` — by reading
sentinel files written at skill entry and validating that the expected
workflow artifacts are present and consistent before the agent is allowed to
stop. Violations split two-tier: **hard** = block stop via JSON `decision:
block` with feedback; **soft** = stderr advisory, exit 0.

Land all assets inside the dot-agents starter scaffold so any project
initialized via `da init` / `da refresh` inherits both the skill machinery
and the enforcement together.

## Ground truth (verified against origin/master + web May 2026)

### Current state of the three targets

| Target | Today's home | In starter? |
|---|---|---|
| `iteration-close` skill | `~/.agents/skills/dot-agents/iteration-close/` | ❌ no |
| `isp` skill | `~/.agents/skills/dot-agents/isp/` | ❌ no |
| `loop-worker` skill+agent | `~/.agents/skills/dot-agents/loop-worker/` + `~/.agents/agents/dot-agents/loop-worker/AGENT.md` | ❌ no |

Starter `internal/scaffold/home/starter/` ships only `self-review`,
`agent-start`, `review-{delta,pr}`, `build-graph`, `agent-handoff`. No
`agents/` tier exists in the starter at all.

### Current hook event coverage in `internal/platform/hooks.go`

Confirmed 2026-05-25 against vendor docs (see partial refresh in
`docs/PLATFORM_DIRS_DOCS.md` — Hook Wiring Audit → Event coverage table).

- `claudeEventName`: full coverage including `Stop` and
  `SubagentStop`. Many newer Claude events (Setup, PostToolBatch,
  PermissionDenied, PostCompact, StopFailure, Task*, Worktree*, Config*,
  Cwd*, FileChanged, etc.) are not yet representable, but irrelevant for
  this scope.
- `codexEventName`: covers `Stop` but **NOT `SubagentStop`**
  (vendor adds it). Also missing `SubagentStart`, `PreCompact`, `PostCompact`,
  `PermissionRequest`.
- `cursorEventName`: covers `stop` but **NOT `subagentStop`**
  (vendor docs `subagentStop` with a dedicated reference subsection). Also
  missing `postToolUse`, `postToolUseFailure`, `sessionEnd`, `preCompact`.
- `copilotEventName`: covers only `sessionStart`,
  `userPromptSubmitted`, `preToolUse`. **No stop-equivalent, no
  `subagentStop`**. CRITICAL: Copilot's stop event is named **`agentStop`**
  (camelCase), NOT `stop` — so the mapper would currently fail to render any
  `stop` hook on Copilot even if we added `case "stop": return "stop"`.
- No `opencodeEventName` exists; OpenCode not a hook target.

### Platform capabilities (web-verified 2026-05-25)

| Platform | Stop event name | SubagentStop event name | Naming | Block-via-JSON |
|---|---|---|---|---|
| Claude Code | `Stop` | `SubagentStop` | PascalCase | `{"decision":"block","reason":...}` |
| Codex CLI | `Stop` | `SubagentStop` | PascalCase | yes — same JSON shape |
| Cursor | `stop` | `subagentStop` | camelCase | yes |
| Copilot CLI | **`agentStop`** | `subagentStop` | camelCase | yes |
| OpenCode | (no dedicated hook file documented) | — | — | n/a |

Footgun: Copilot's `agentStop` breaks the assumption that all platforms call
the stop event `stop`. The `HookSpec.When: "stop"` → platform-event-name
translation in `copilotEventName` must emit `agentStop`, not `stop`.

Sources (cached under `~/.agents/skills/dot-agents/platform-docs-refresh/references/cache/<platform>/hooks.md`):
- Claude Code: https://code.claude.com/docs/en/hooks
- Codex: https://developers.openai.com/codex/hooks
- Cursor: https://cursor.com/docs/hooks
- Copilot: https://docs.github.com/en/copilot/reference/hooks-configuration

## Four work threads

### Thread A — Promote skills + agent into starter

1. Copy these assets into `internal/scaffold/home/starter/`:
   - `skills/global/iteration-close/` (SKILL.md + instructions/ + templates/ +
     scripts/propose.sh)
   - `skills/global/isp/` (SKILL.md + instructions/)
   - `skills/global/loop-worker/` (SKILL.md + AGENT.md + instructions/)
2. Add `agents/global/loop-worker/` to the starter and ensure the scaffold
   loader (`commands/install.go` or `commands/refresh.go`) syncs the new
   `agents/` tier on `da install`/`refresh`. This may already work if the
   scaffold copy is recursive; otherwise add the tier explicitly.
3. Include the `loop-worker` profile under `home/starter/profiles/` (sourced
   by `loop-worker/SKILL.md` step 0).
4. Audit dependencies that must also be promoted: `delegation-lifecycle`,
   `self-review` (already in starter), `orchestrator-session-start`.

**Risk:** the starter currently doesn't have an `agents/` tier — verify
whether `internal/config/links.go` / `internal/platform/*.go` even know how
to materialize that tier. If not, this thread grows by a code change.

### Thread B — Three new starter Stop hooks

Add to `internal/scaffold/hooks/global/`:

#### `iteration-close-gate/HOOK.yaml`
```yaml
name: iteration-close-gate
description: Verify iteration-close skill produced the expected artifacts before allowing stop
when: stop
# Also wire a parallel HOOK.yaml for subagent_stop, or use a
# `when_events: [stop, subagent_stop]` extension if the platform layer
# learns it (proposed below).
match:
  tools: []
  expression: ""
run:
  command: ./gate.sh
  timeout_ms: 8000
enabled_on:
  - claude
  - codex
  - cursor
  - copilot
```

#### `isp-gate/HOOK.yaml`
```yaml
name: isp-gate
description: Verify isp orchestrator completed the staged pipeline before stop
when: stop
run:
  command: ./gate.sh
  timeout_ms: 8000
enabled_on: [claude, codex, cursor, copilot]
```

#### `loop-worker-gate/HOOK.yaml`
```yaml
name: loop-worker-gate
description: Verify loop-worker subagent stayed in write_scope and closed out via merge-back
when: subagent_stop
match:
  expression: 'subagent_type == "loop-worker"'  # claude only; other platforms degrade
run:
  command: ./gate.sh
  timeout_ms: 8000
enabled_on: [claude, codex, cursor, copilot]
```

Each `gate.sh` is a thin POSIX shell script that:
1. Reads the sentinel via `da workflow hook-sentinel read <skill> --latest`
2. If no sentinel exists for this turn → exit 0 (skill didn't run; nothing
   to enforce)
3. Else runs the violation matrix and emits either JSON-block on stdout
   (hard) or stderr+exit 0 (soft)

### Thread C — Sentinel protocol + `da workflow hook-sentinel` CLI

New CLI verbs:
```
da workflow hook-sentinel write <skill> \
  --plan <plan-id> --task <task-id> [--run-id <id>] \
  [--expect <artifact-path>...]
da workflow hook-sentinel read <skill> [--latest | --run-id <id>]
da workflow hook-sentinel clear <skill> --run-id <id>
```

Sentinel file path:
`.agents/active/hook-sentinels/<skill>-<run-id>.json`

Schema (v1):
```json
{
  "schema_version": 1,
  "skill": "iteration-close",
  "run_id": "20260525T161430Z-abc123",
  "started_at": "2026-05-25T16:14:30Z",
  "plan_id": "loop-runtime-refactor",
  "task_id": "p2-impl-agent-surface",
  "agent_type": "loop-worker",     // or "main" for non-subagent
  "expected_artifacts": [
    ".agents/active/verification/<task>/review-decision.yaml",
    ".agents/active/iteration-log/iter-<N>.yaml",
    ".agents/active/merge-back/<task>.md"
  ],
  "context": {
    "git_head_at_start": "<sha>",
    "transcript_path": "<env value if available>"
  }
}
```

Skills add ONE line at the top of their workflow:
```bash
da workflow hook-sentinel write iteration-close \
  --plan "$PLAN_ID" --task "$TASK_ID" \
  --expect ".agents/active/iteration-log/iter-$N.yaml" \
  --expect ".agents/active/merge-back/$TASK_ID.md"
```

The hook reads the latest sentinel for its skill name, validates artifacts,
and clears (or archives) the sentinel on success.

### Thread D — Platform hook-event coverage gaps

Code changes in `internal/platform/hooks.go`. Two design principles agreed
2026-05-25:

- **One-to-one mapping, with no-ops on platforms that don't implement the
  event.** A canonical `HookSpec.When` value maps to at most one event name
  per platform. If a platform doesn't document that event, the renderer
  emits nothing for it (existing `hookRequiredOnPlatform` fall-through is
  the right behavior). This is the same precedent as `permission_request`
  today.
- **No semantic-equivalence fan-out.** We do NOT translate `after-file-edit`
  to "Claude PostToolUse with matcher=Edit|Write" automatically — operators
  who want cross-platform coverage declare multiple HookSpecs or use
  `platformOverride` to set the platform-specific event explicitly.
  Rationale: semantic equivalence creates surprising behavior (a Cursor
  `after-file-edit` would unexpectedly fire on `cat`-via-Bash on Claude).

#### D.1 Codex mapper additions (`codexEventName`)

| `When` | Codex event | Notes |
|---|---|---|
| `subagent_stop` | `SubagentStop` | confirmed 2026-05-25; primary loop-worker-gate target |
| `subagent_start` | `SubagentStart` | newly documented |
| `pre_compact` | `PreCompact` | newly documented |
| `post_compact` | `PostCompact` | newly documented (new `When` value — see D.5) |
| `permission_request` | `PermissionRequest` | newly documented |

Update the `renderCodexHookConfig` matcher whitelist only if vendor docs confirm matcher
support for any of the new events. PreCompact / PostCompact / SubagentStart /
SubagentStop / PermissionRequest are almost certainly matcher-unsupported
(see "matcher-supported vs matcher-required-to-render" below); they render
with an empty matcher string and that is correct.

#### D.2 Copilot mapper additions (`copilotEventName`)

| `When` | Copilot event | Notes |
|---|---|---|
| `stop` | **`agentStop`** | NOT `stop` — camelCase, confirmed 2026-05-25. Cross-platform footgun |
| `session_end` | `sessionEnd` | |
| `post_tool_use` | `postToolUse` | |
| `post_tool_use_failure` | `postToolUseFailure` | |
| `pre_compact` | `preCompact` | |
| `notification` | `notification` | |
| `permission_request` | `permissionRequest` | |
| `subagent_start` | `subagentStart` | |
| `subagent_stop` | `subagentStop` | |
| `error_occurred` | `errorOccurred` | new `When` value — Copilot-only today |

Copilot does NOT document a `post_compact` equivalent.

#### D.3 Cursor mapper additions (`cursorEventName`)

| `When` | Cursor event | Notes |
|---|---|---|
| `session_end` | `sessionEnd` | |
| `post_tool_use` | `postToolUse` | |
| `post_tool_use_failure` | `postToolUseFailure` | |
| `pre_compact` | `preCompact` | |
| `subagent_start` | `subagentStart` | |
| `subagent_stop` | `subagentStop` | dedicated reference subsection in docs |

Plus the Cursor-wide event surface — see D.4.

#### D.4 New canonical `HookSpec.When` values (driven by Cursor's wider surface)

Cursor today exposes events that no other platform documents. Per the 1-to-1
mapping principle these get canonical `When` values, render only for Cursor,
and no-op on other platforms via `hookRequiredOnPlatform` fall-through.
Other platforms may adopt them later.

| New `When` (kebab-case) | Cursor event | Use case |
|---|---|---|
| `before-shell-execution` | `beforeShellExecution` | block dangerous shell before it runs |
| `after-shell-execution` | `afterShellExecution` | parse exit codes / logs |
| `before-mcp-execution` | `beforeMCPExecution` | gate MCP tool calls |
| `after-mcp-execution` | `afterMCPExecution` | record MCP usage |
| `before-read-file` | `beforeReadFile` | gate file reads (secret scanning) |
| `after-file-edit` | `afterFileEdit` | format / lint on save |
| `after-agent-response` | `afterAgentResponse` | post-turn summary |
| `after-agent-thought` | `afterAgentThought` | extended-thinking telemetry |
| `workspace-open` | `workspaceOpen` | one-time workspace init |
| `before-tab-file-read` | `beforeTabFileRead` | tab-completion specific |
| `after-tab-file-edit` | `afterTabFileEdit` | tab-completion specific |

For each, the other three platform mappers return `("", false)` — handled by
`hookRequiredOnPlatform` fall-through with no error unless the HOOK.yaml
declares `enabled_on: [<that-platform>]`. Document in HOOKS.md that these
values are Cursor-only today; operators who want cross-platform coverage
should declare additional HookSpecs targeting each platform's nearest
documented event (e.g., a Claude HookSpec with `when: post_tool_use` and
matcher `Edit|Write` alongside a Cursor HookSpec with `when:
after-file-edit`).

#### D.5 New canonical `HookSpec.When` values from other platform-doc deltas

Independent of Cursor's surface, the May 2026 refresh surfaced events worth
canonical `When` values (no platform currently fails to map them, but they
are new since the prior audit):

| New `When` | Documented on |
|---|---|
| `post_compact` | Claude, Codex |
| `error_occurred` | Copilot |

Claude also documents Setup, UserPromptExpansion, PostToolBatch,
PermissionDenied, StopFailure, Task*, Worktree*, FileChanged, ConfigChange,
CwdChanged, InstructionsLoaded, Elicitation, ElicitationResult — these are
out of scope for this proposal (none are needed by the three target hooks),
but worth noting as the next refresh's Claude-side delta.

#### D.6 Matcher-supported vs matcher-required-to-render — verification needed

The renderer's `renderCodexHookConfig` switch emits a default `"matcher": "*"` only for
events that are matcher-supported by the vendor. The current whitelist is
`SessionStart`, `PreToolUse`, `PostToolUse`.

This proposal does NOT extend the whitelist by guessing. Before any new
events are added, verify per-event from vendor docs whether `matcher` is a
documented field for that event. The known answers:

- PreToolUse / PostToolUse / PostToolBatch / Notification — matcher-supported.
- Stop / SubagentStop / SessionEnd / PreCompact / PostCompact — almost
  certainly matcher-unsupported (no per-tool dimension). Render with empty
  matcher string.
- PermissionRequest — unclear; vendor docs may take a tool matcher. Verify
  before wiring.

The proposal will include a verification table in the plan's P1 phase.

#### D.7 OpenCode — intentional plugins-vs-hooks split

OpenCode does not publish a hooks file. OpenCode's lifecycle-event surface
ships through TypeScript / JavaScript plugin modules under
`.opencode/plugins/` and `~/.config/opencode/plugins/`, with `package.json`
dependencies installed via `bun install` at startup. That is a different
artifact shape from JSON hook files.

Decision: keep OpenCode lifecycle handlers as Plugins (already wired in our
**Plugins** resource type in `docs/PLATFORM_DIRS_DOCS.md`, not as Hooks.
A `hooks/{scope}/hooks.json` → OpenCode-plugin emitter would be bridge code
maintained for one platform with no other consumer. Document this as an
intentional split, not a coverage gap, in `docs/HOOKS.md` and the Hook
Wiring Audit "Event coverage" preamble.

#### D.8 Optional schema extension — `when_events: []`

The `iteration-close-gate` hook needs to fire on both `stop` and
`subagent_stop` (the skill runs in main-agent and subagent contexts).
Without a schema change, it ships as two HOOK.yaml entries sharing one
script. With a `when_events: [stop, subagent_stop]` array on `HookSpec`, it
ships as one entry that fans out at render time.

Recommendation: the array extension. Small, well-scoped, avoids drift
between two scripts. Leave it as a yes/no decision for the spec phase.

### Two-tier violation matrix

| Hook | HARD (`decision: block`) | SOFT (stderr) |
|---|---|---|
| iteration-close-gate | sentinel present + missing `verify record` log entry for this run_id; delegation active + `workflow advance` invoked; `/self-review` returned `reject` but `merge-back` was written anyway | generic verify summary ("tests passed"); commit hash in iter-log doesn't match `git log -1 --format=%h`; `make build-prod` ran mid-iteration |
| isp-gate | bundle file under `.agents/active/delegation-bundles/` written but no parent-gate entry in iter-log after merge-back; sentinel says `eligible_snapshot_loaded: false` and orchestrator re-ran `workflow orient` | no direct-vs-fanout decision text in transcript/log; `max_batch > 1` but only one bundle materialized |
| loop-worker-gate | file modified outside `write_scope` (git diff vs sentinel scope); `workflow advance`/`orient`/`next`/`status` invoked while sentinel present; `loop-state.md ## Current Position` edited; no `merge-back/<task>.md` artifact | no positive+negative test trace captured; uncommitted changes at subagent_stop |

Block payload format (Claude Code shape; Codex/Copilot/Cursor accept the same):
```json
{
  "decision": "block",
  "reason": "[loop-worker-gate] write_scope violation: modified internal/platform/codex.go but write_scope only allows commands/install.go. Revert the codex.go edit or write a fold-back observation before stopping."
}
```

## Open decisions

### Settled 2026-05-25

- **D mapping principle: one-to-one, no-ops on platforms that don't
  implement.** Operators who want cross-platform coverage of a semantic
  concept declare additional HookSpecs targeting each platform's nearest
  documented event. (Settled: rejects semantic-equivalence fan-out as
  surprising and unsafe; favors operator clarity.)
- **D.4 Cursor-wider event surface: add to canonical `HookSpec.When`.**
  Fine-grained events promote real cross-platform leverage as other vendors
  copy Cursor's surface; precedent set by existing partial-coverage events
  like `permission_request`.
- **D.7 OpenCode plugins-vs-hooks: intentional split, not a gap.**
  OpenCode lifecycle handlers stay under Plugins; no bridge emitter.

### Original open questions (resolved or assigned in the promoted spec)

1. **CLI scope for `hook-sentinel`** — add it under `da workflow` or
   promote it to top-level `da hook-sentinel`? Recommendation: nest under
   `workflow` since sentinels are tied to plan/task IDs.
2. **Sentinel lifetime** — clear on success vs archive under
   `.agents/active/hook-sentinels/archive/<date>/`? Recommendation:
   archive for one iteration, then prune (so post-mortem can read recent
   sentinels).
3. **Hard-vs-soft threshold tuning** — is "generic verify summary" a hard
   block or soft advisory? Listed as soft above; may need to harden after
   real-world calibration.
4. **D.8 Schema extension `when_events: []`** vs duplicate HOOK.yaml files
   for the iteration-close gate (fires on both Stop and SubagentStop).
   Recommendation: extension — small, well-scoped, avoids drift between
   two scripts. Final yes/no at spec phase.
5. **D.6 Matcher-supported events per vendor** — vendor-by-vendor
   verification of which events accept a `matcher` field, before extending
   the `renderCodexHookConfig` matcher whitelist (and the equivalent
   logic for other platforms). Plan P1 owns this.
6. **Loop-worker matcher portability** — Claude's
   `subagent_type == "loop-worker"` matcher is Claude-specific. On
   Codex/Copilot/Cursor the SubagentStop hook will fire for all subagents;
   the `gate.sh` script must self-filter by reading the sentinel's
   `agent_type` field. That's fine, but document it.
7. **Backfill payout** — the payout project also uses these skills today.
   Should the proposal include a payout migration step or rely on payout
   inheriting via `da refresh` after the starter ships?

## Estimated work breakdown (rough)

If proposal is accepted, the follow-up plan likely has phases:

- **P0** — Sentinel CLI surface (`da workflow hook-sentinel write/read/clear`)
  with unit tests. ~1–2 iterations.
- **P1** — Platform hook-event coverage. Three sub-deliverables, each
  testable in isolation:
  - **P1a** Mapper extensions: D.1 (Codex), D.2 (Copilot incl. `agentStop`
    footgun), D.3 (Cursor SubagentStop). Per-platform test tables in
    `internal/platform/hooks_test.go`. ~1 iteration.
  - **P1b** New canonical `HookSpec.When` values from D.4 (Cursor wide
    surface) + D.5 (`post_compact`, `error_occurred`). Mapper updates plus
    a doc note in `docs/HOOKS.md` describing the one-to-one principle and
    the operator pattern for cross-platform coverage. ~1 iteration.
  - **P1c** Matcher-supported verification (D.6) and `when_events: []`
    schema extension (D.8) if approved. ~1 iteration.
  Combined: ~3 iterations.
- **P2** — Three HOOK.yaml + gate.sh scripts under
  `internal/scaffold/hooks/global/`. ~1–2 iterations.
- **P3** — Promote `iteration-close`, `isp`, `loop-worker` (+ profiles)
  into `internal/scaffold/home/starter/`. Add `agents/` tier to starter
  loader if missing. ~2 iterations (depends on whether `agents/` tier is
  already supported).
- **P4** — Wire sentinel-write call into the three skills' workflow.md
  step 1 (or step 0). Update gotchas + proposal-criteria to mention the
  hook contract. ~1 iteration.
- **P5** — End-to-end test: run a full delegation loop in a sandbox repo,
  exercise hard + soft violations, confirm block JSON is consumed
  correctly by each platform. ~1 iteration.

Total: ~9–11 iterations, with P0 and P1a unblocking everything else and
P2/P3/P4 parallelizable behind P0+P1a. P1b/P1c can ship after P1a without
blocking P2/P3.

## Suggested plan-id

`loop-discipline-stop-hooks`

## Next step

Once this scoping is acknowledged, promote to:
- `workflow/specs/loop-discipline-stop-hooks/design.md` (formal spec with
  decisions, requirements, done criteria)
- `workflow/plans/loop-discipline-stop-hooks/PLAN.yaml` + `TASKS.yaml`
  (the phase breakdown above, fleshed into write_scope-bounded tasks)
