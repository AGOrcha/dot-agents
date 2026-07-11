PASS

# CLI-runner verify stage — wire-managed-gitignore-autofill (D14/R8)

Stage: verify (cli-runner) (`swarm-da-inner-loop-d14-verify_cli_runner-0`).
Upstream gate: verify-unit=PASS (impl=DONE, commit `2bba968`). Gate honored.
Resolved prompt: `.agents/prompts/verifiers/cli-runner.project.md` (repo-local,
`da --json workflow resolve-prompt --kind verifier --slug cli-runner`).
Binary under test: slice build `.agents/worktrees/d14/bin/da` (`da version dev`).

## Isolated dogfood (main tree NEVER touched)
Fully isolated HOME + AGENTS_HOME under `mktemp -d`; only the temp project's
`.gitignore`/`.agentsrc.*` are written. Main repo tree untouched.

Setup commands (env: `HOME=$ROOT/home`, `AGENTS_HOME=$HOME/.agents`,
`DA=.agents/worktrees/d14/bin/da`):
```
ROOT=$(mktemp -d); export HOME="$ROOT/home"; export AGENTS_HOME="$HOME/.agents"
"$DA" init -y                                  # seeds ~/.agents + 13 global hook bundles
PROJ="$ROOT/proj"; git -C "$PROJ" init
printf '{"version":2,"project":"dogfood"}\n' > "$PROJ/.agentsrc.json"  # minimal manifest
printf 'my-secret-notes/\n'                > "$PROJ/.gitignore"        # pre-existing user ignore
"$DA" add "$PROJ" --name dogfood -y            # copilot enabled:true in config.json
"$DA" refresh dogfood -y                        # <-- command under test (D14 wiring)
```
Enabled platforms (config.json, PATH-detected): copilot=true, claude=true,
codex=true, cursor=true (opencode/antigravity=false). Copilot is among the
enabled set, so `da refresh` collected its dynamic outputs into the block.

Managed block produced (regenerated, sorted, deduped; user line preserved above
the markers):
```
my-secret-notes/
# >>> dot-agents managed (project outputs) >>>
.agents/agents/
.agents/skills/
.agentsrc.local.json
.claude/
.claude/settings.local.json
.codex/
.cursor/
.cursorignore
.cursorrules
.github/agents/
.github/copilot-instructions.md
.github/hooks/*.json
.mcp.json
.vscode/mcp.json
AGENTS.md
CLAUDE.md
# <<< dot-agents managed (project outputs) <<<
```

## Assertion results (all PASS)

### A1 — managed block contains copilot `.github/hooks/*.json`: PASS
The block (between `# >>> dot-agents managed (project outputs) >>>` and its
`<<<` end marker) carries `.github/hooks/*.json` (line 14 of `.gitignore`).
`da refresh` actually rendered 10 per-machine hook files under
`.github/hooks/` (pre-compact-gate*.json, pre-tool-use-gate*.json, stop-gate*.json,
subagent-start-gate.json, subagent-stop-gate*.json) — the dynamic copilot fanout
the pattern exists to cover.
Command: `ls .github/hooks/*.json` + block inspection of `.gitignore`.

### A2 — git status clean, hooks ignored VIA that block: PASS
`git check-ignore -v .github/hooks/pre-compact-gate-2.json` →
`.gitignore:14:.github/hooks/*.json	.github/hooks/pre-compact-gate-2.json`
(attributed to the managed block line, not an ad-hoc rule — #381 retirement).
After `git add -A`, `git status --porcelain` and `git ls-files` show ONLY the
committed contract: `.agentsrc.json`, `.agentsrc.lock`, `.gitignore`. Every
generated platform output (`.github/`, `.claude/`, `.codex/`, `.cursor/`,
`AGENTS.md`, …) is `!!` ignored — no per-machine output leaks as untracked.
Commands: `git check-ignore -v .github/hooks/*.json`, `git add -A`,
`git status --porcelain`, `git ls-files`.

### A3 — `.agentsrc.lock` is TRACKED (not ignored): PASS
`.agentsrc.lock` is present and `git check-ignore -q .agentsrc.lock` exits 1
(NOT ignored — filtered out of the block by `neverIgnored`), so `git add -A`
stages it (`A  .agentsrc.lock`). `.agentsrc.json` likewise not ignored (exit 1).
Neither contract file appears inside the managed block.
Commands: `git check-ignore -q .agentsrc.lock` (→1), `git check-ignore -q .agentsrc.json` (→1).

### A4 — re-running `da refresh` is byte-stable (no diff): PASS
Captured `.gitignore` (sha1 `ff5ebceb35cd9629d54dd592d321bf02b52e1c56`), ran
`da refresh dogfood -y` again, `diff` = empty; sha1 unchanged across a 2nd AND
3rd refresh. The managed block is regenerated, not appended.
Commands: `cp .gitignore before; "$DA" refresh dogfood -y; diff before .gitignore`.

## Note (finding, non-blocking)
`.agentsrc.lock` is NOT byte-stable across identical refreshes, but the drift is
timestamps ONLY (`refreshedAt`, per-unit `fetched_at`/`last_checked_at`) —
structural content (inputs_digest, units) is stable and the D14 managed
`.gitignore` deliverable is fully byte-stable. This is pre-existing
lock/refresh-metadata churn, outside the managed-gitignore contract; recorded in
findings, not verdict-blocking.

Verdict: **PASS**.
