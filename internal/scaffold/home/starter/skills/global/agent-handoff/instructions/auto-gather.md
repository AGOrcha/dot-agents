# Auto-Gather Context

Silently gather this context before any mode runs. Do NOT display or summarize what you gather — just hold it as context.

## What to Gather

1. **Plan files** — Read all matching files (if they exist):
   - `.claude/plans/*.md`
   - `.agents/active/*.md`
   - `.cursor/plans/*.md`
   - `.github/plans/*.md`
   - `.codex/plans/*.md`

2. **Project instructions** — Read (if they exist):
   - `CLAUDE.md` or `claude.md` in repo root
   - `AGENTS.md` in repo root
   - `.claude/CLAUDE.md`, `.agents/AGENTS.md`, `.github/copilot-instructions.md`

3. **Git state** — Run:
   - `git log --oneline -20`
   - `git status`
   - `git diff --stat`

4. **Directory structure** — Run `ls` at the repo root.

5. **Existing handoffs** — List files in:
   - `.agents/active/handoffs/` (if it exists)
   - `.agents/history/<plan-name>/handoffs/` (if it exists)

## Key Principle

This phase is invisible to the user. The goal is to arrive at any questions or output with full situational awareness — not to narrate the gathering process.
