# omp (Oh My Pi) platform handling in dot-agents

**Status:** proposal (draft)
**Created:** 2026-07-10
**Author:** NikashPrakash (agent-assisted)
**Related:** `platform-dirs-*` proposals; `config-content-hill-climb` plan; the 2026-07-10 payout workspace-deletion incident (motivation).

## Motivation (the incident)

A delegated loop worker ran `rm -rf "$(cat /dev/null)"; cd <worktree> && …` with its `cwd` defaulted to `~/proj-docs/payout` (it omitted an explicit cwd). In the omp harness's **embedded shell, `rm -rf ""` is not a no-op** — an empty path operand makes `rm` recursive/force delete the **current working directory**. That wiped the payout superproject (`.git`, `.agents`, all 18 submodules); recovery was only possible because the workflow state had been pushed to GitHub minutes earlier.

The fix is an omp `tool_call` safety hook — `guard-rm.ts` — that hard-blocks the empty-expansion `rm` class before `bash` runs (verified: blocks the incident command + `""`/`''`/`$(...)`/bare-`$VAR`/no-target/`/`/`~`/`$HOME`; allows literal-path deletes). It is currently **hand-installed** at `~/.omp/agent/hooks/pre/guard-rm.ts`.

**That hook should be a da-managed artifact** — tracked, reviewable, reinstalled by `da install`/`da refresh`, synced across machines. It cannot be today: **da has no omp platform handling.** This proposal adds it.

## Problem

da manages skills / agents / hooks / rules and materializes them per-platform, keyed by `enabled_on` (`claude`, `copilot`, `cursor`, `codex`, `antigravity`). Each platform is a file under `internal/platform/` (`claude.go`, `cursor.go`, `antigravity.go`, `hooks.go`, `buckets.go`) plus `enabled_on` vocabulary in each `HOOK.yaml`.

**omp is not a recognized platform** (`grep -rniE 'oh-my-pi|"omp"|\.omp/agent'` across `internal/` + `commands/` → zero hits). Consequences:
- omp-only artifacts (the `guard-rm` safety hook) live outside da: hand-installed, untracked, un-synced, never reinstalled by `da install`/`da refresh`/`da doctor`.
- da hooks (`HOOK.yaml` + `gate.sh`, e.g. `loop-worker-gate`, `iteration-close-gate`) never reach omp even when their policy is relevant there.

## omp platform conventions (target, from omp docs)

Sources: `omp://hooks.md`, `omp://extensions.md`, `omp://extension-loading.md`, `omp://skills.md`, `omp://task-agent-discovery.md`, `omp://config-usage.md`, and https://omp.sh/docs/hooks.

- **Hooks** — `~/.omp/agent/hooks/pre/*.ts` (pre → `tool_call`) and `hooks/post/*.ts` (post → `tool_result`); project-level `.omp/hooks/pre|post/*.ts`. Discovery is one level deep. Profile-aware: `~/.omp/profiles/<name>/agent/…` and honors `PI_CODING_AGENT_DIR`.
- **Extensions** — `~/.omp/agent/extensions/<name>/` (and `.omp/extensions`); TS/JS default-export factory (`ExtensionAPI`, a superset of `HookAPI`).
- **Hook shape** — a TS/JS module `export default function (pi) { pi.on("tool_call", (event) => …) }` that returns `{ block: true, reason }` to refuse a tool call; **not** a shell `gate.sh` reading a stdin JSON payload. `ctx.hasUI` is `false` for subagents (headless), so a block is unconditional there.
- **Event vocabulary** — `tool_call` (pre, `{block,reason}`), `tool_result` (post, `{content,details,isError}`), `context` (per-call message rewrite), `session_before_compact`/`session_*`. This differs from da's canonical shell-gate event names (`pre_tool_use`, `subagent_stop`, `pre_compact`, `subagent_start`).
- **Skills / agents / rules** — omp skills (`skill://`, `manage_skill`), subagent discovery (`task-agent-discovery`), context/rules via `omp://context-files.md`.
- **Debug** — `omp -p '/extensions'` lists loaded hooks/extensions and their paths.

## The materialization gap (key design point)

da's hook model (declarative `HOOK.yaml` + a shell `gate.sh` that reads stdin and exits/emits a decision, Claude-`PreToolUse` style) is a **different shape** from omp's (a TS/JS factory registering `pi.on("tool_call", …)`). omp handling therefore needs a hook **adapter**, with two modes:

- **(a) omp-native TS hooks** — a new artifact kind: a `.ts` hook shipped verbatim to `~/.omp/agent/hooks/{pre,post}/<name>.ts`. Best for omp-shaped policy like `guard-rm`.
- **(b) shell-gate bridge** — for existing substantial shell-gate da hooks (`loop-worker-gate`, etc.) with `enabled_on: omp`, generate a thin omp TS wrapper that shells out to the existing `gate.sh` (feeding it the `tool_call` payload on stdin, mapping the exit/decision back to `{block,reason}`). Reuses the shell logic unchanged.

## Proposal

1. Add `omp` to the platform registry + the `enabled_on` vocabulary; new `internal/platform/omp.go` alongside `claude.go`/`cursor.go`/`antigravity.go`, wired through `hooks.go`/`buckets.go`.
2. omp materialization in `da install` / `da refresh` / `da sync`:
   - **Hooks** — omp-native `.ts` hooks → `~/.omp/agent/hooks/{pre,post}/<name>.ts`; shell-gate da hooks with `enabled_on: omp` → generated TS bridge (mode b). Profile-aware (honor `PI_CODING_AGENT_DIR` / `~/.omp/profiles/<name>/agent`).
   - **Extensions** → `~/.omp/agent/extensions/<name>/`.
   - **Skills** → omp skills location; **agents** → omp subagent defs; **rules/context** → omp context-files.
   - **Event-name mapping** (da canonical ↔ omp): `pre_tool_use`→`tool_call`; post→`tool_result`; `subagent_start`→`agent_start`; `pre_compact`→`session_before_compact`; `subagent_stop`→(no omp subagent-stop; `agent_end` is notification-only and `session_stop` never fires for subagents — document the loss / gate at `tool_result` instead).
3. `da doctor` / `da refresh` detect `~/.omp/agent/` and report omp materialization state (loaded via `omp -p '/extensions'`).
4. Ship `guard-rm` as a **starter omp safety hook** (`kind: omp tool_call hook`, `enabled_on: [omp]`) — the seed artifact.

## Seed artifact: `guard-rm`

The dangerous-`rm` safety hook. Currently at `~/.omp/agent/hooks/pre/guard-rm.ts`; verified against 15 block cases (incl. the exact incident command) + 9 allow cases. It refuses only the empty-expansion class + catastrophic roots, so literal-path deletes (`rm -rf dist`, `rm -rf /tmp/x`, `rm -rf "$VAR/sub"`) pass through. Under this proposal its source moves into dot-agents (tracked) and materializes to omp via mode (a).

## Acceptance

- `enabled_on: [omp]` is honored; `da install`/`da refresh` materialize hooks/extensions/skills/agents/rules into `~/.omp/agent/` (profile-aware).
- `guard-rm` ships as a da-managed starter omp safety hook and materializes to `~/.omp/agent/hooks/pre/guard-rm.ts`.
- `da doctor` reports omp platform state.
- A substantial existing shell-gate da hook with `enabled_on: omp` bridges to omp (mode b) and actually gates an omp `bash` call.

## Companion lesson

Capture the two-factor root cause as a lesson: (1) **omp embedded-shell `rm -rf ""` deletes the cwd** (an empty path operand is NOT a no-op there — it is not GNU rm); (2) **delegated workers must pass an explicit `cwd`** on every command and never issue a destructive `rm` with a target that can expand to empty. The `guard-rm` hook is the enforcement; the lesson is the why.
