# Verify (always run, whichever path you took)

The final, path-independent step. `da` links config into whichever editors it
detects, so install your editor **before** the link pass.

## Steps

1. **Install your editor / harness** (if not already installed). Supported:
   - Claude Code
   - Cursor (config distributed via hard links)
   - Codex
   - GitHub Copilot
   - OpenCode
   - Antigravity (Google; F4/DC0 "real harness" probe — config projected into `.antigravity/`)

2. **Re-link** — `da refresh` re-detects installed platforms and auto-enables any
   harness you just installed, projecting config into each managed project:
   ```bash
   da refresh
   ```

3. **Audit the links** — file-level detail per project (good for confirming each
   editor got its config):
   ```bash
   da status --audit
   ```

4. **Health check** — read-only; reports anything broken or missing:
   ```bash
   da doctor
   ```

## Done when

- `da doctor` reports no errors.
- `da status --audit` shows your project(s) linked for the editor(s) you use.
- Opening the project in your editor surfaces the expected rules/instructions.

If `da doctor` flags an issue, follow its suggested fix, then re-run
`da refresh` and `da doctor`.
