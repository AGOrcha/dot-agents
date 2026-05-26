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
[PLATFORM_DIRS_DOCS.md](PLATFORM_DIRS_DOCS.md)):

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

### `PostToolUse` and `PostToolUseFailure` are observation candidates

Per [decision D9 in the design spec][d9], `post_tool_use` and
`post_tool_use_failure` are mapped today so operators may attach
**observation** and feedback-capture hooks to them under R1.5. They are
**not** implicit blocking hooks: a failed workflow command produces
useful improvement evidence, but recording an error is not by itself
proof that the session should be blocked. Any future blocking use must
document a deterministic invariant, a portable vendor contract, and an
acceptable noise / privacy boundary.

[d2]: ../.agents/workflow/specs/loop-discipline-stop-hooks/design.md
[d3]: ../.agents/workflow/specs/loop-discipline-stop-hooks/design.md
[d8]: ../.agents/workflow/specs/loop-discipline-stop-hooks/design.md
[d9]: ../.agents/workflow/specs/loop-discipline-stop-hooks/design.md

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
- [PLATFORM_DIRS_DOCS.md](PLATFORM_DIRS_DOCS.md) — full per-platform resource
  locations and the Hook Wiring Audit
