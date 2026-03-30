# Handoff: Platform Docs Matrix Refresh

**Created:** 2026-03-29
**Author:** Claude Code session
**For:** AI Agent
**Status:** Ready to execute

---

## Summary

The user wants `docs/PLATFORM_DIRS_DOCS.md` updated against the current official online docs for each platform, then expanded with a cross-platform matrix showing the most common read locations per resource type. No repo files were changed in this session; the work stopped after repo inspection, a planning pass, and partial upstream-doc lookup.

## Project Context

This repo is `dot-agents`, a config/linking tool that maps canonical resources from `~/.agents/` into platform-specific project locations for Cursor, Claude Code, Codex, OpenCode, and GitHub Copilot. The current platform comparison doc already exists in `docs/PLATFORM_DIRS_DOCS.md`, but it mixes upstream product behavior and local `dot-agents` implementation details, so the next pass should separate those clearly.

## The Plan

# Refresh Platform Resource Location Matrix

## Summary
Update `docs/PLATFORM_DIRS_DOCS.md` so it separates three things cleanly:
- Official upstream read/discovery locations per platform
- `dot-agents` implementation behavior in this repo
- A normalized cross-platform matrix showing the most commonly supported location for each resource type

Use official docs as the primary source of truth for platform behavior, then keep the repo-implementation audit as a distinct section so readers do not confuse product docs with what `dot-agents` currently wires.

## Key Changes
- Replace any stale platform-location claims with doc-verified entries for:
  - Cursor: rules/instructions, skills, subagents, hooks
  - Claude Code: `CLAUDE.md` loading, `.claude/rules`, skills, subagents, settings/hooks, MCP
  - Codex: `AGENTS.md` layering, skills, `.codex/config.toml` for MCP, hooks
  - OpenCode: rules/instructions, skills, agents, config/MCP, commands, tools
  - GitHub Copilot: repository instructions, path-specific instructions, skills, custom agents, hooks, CLI MCP
- Rework the document structure into:
  - `Official platform locations`
  - `Cross-platform common-location matrix`
  - `dot-agents implementation audit`
- In the matrix, normalize by resource type:
  - Instructions / rules
  - Skills
  - Agents / subagents
  - MCP / config
  - Hooks
  - OpenCode-only extras: commands, tools
- For each resource row, include:
  - Native path(s) per platform
  - Compat path(s) accepted by that platform
  - The “most common” path determined by count of platforms that officially read it
  - A short recommendation note for `dot-agents`
- Count only officially documented read/discovery behavior when selecting the “most common” path. Do not let current `dot-agents` link choices influence the winner.
- Call out likely no-consensus categories explicitly instead of forcing a winner. Expected examples:
  - Instructions/rules likely have no universal shared path
  - Agents/subagents likely split by ecosystem
  - Skills likely converge most strongly on `.agents/skills` and `.claude/skills`, with counts shown rather than implied

## Validation
- For every platform/resource section, include at least one official-doc citation link.
- Cross-check the updated doc against current local implementations:
  - `internal/platform/claude.go`
  - `internal/platform/codex.go`
  - `internal/platform/copilot.go`
  - `internal/platform/opencode.go`
  - `internal/platform/cursor.go`
- Preserve and update the existing hook-wiring audit so it clearly remains an implementation audit, not an upstream-doc summary.
- Sanity check that each matrix winner is consistent with the documented counts in the per-platform sections.

## Assumptions
- The deliverable is documentation-only: no code changes, only a doc refresh and matrix redesign.
- Official docs win over current repo behavior for “what platforms read,” while repo code wins for the separate implementation-audit section.
- Cursor docs are JS-heavy; use the official Cursor doc URLs already referenced in the file as the canonical citations, with current repo notes capturing any uncertainty where the scraper could not fully render the page.
- “Most common” means “supported by the highest number of platforms,” not “best for `dot-agents` to link today.”

## Key Files

| File | Why It Matters |
|------|----------------|
| `docs/PLATFORM_DIRS_DOCS.md` | Main deliverable to rewrite and expand |
| `internal/platform/cursor.go` | Current Go implementation for Cursor links and hooks |
| `internal/platform/claude.go` | Current Go implementation for Claude rules, settings, MCP, agents, skills |
| `internal/platform/codex.go` | Current Go implementation for Codex AGENTS/config/skills/agents |
| `internal/platform/opencode.go` | Current Go implementation for OpenCode config, agents, skills |
| `internal/platform/copilot.go` | Current Go implementation for Copilot instructions, agents, MCP, hooks compat |
| `src/lib/platforms/cursor.sh` | Bash implementation; useful to compare with Go drift |
| `src/lib/platforms/claude-code.sh` | Bash implementation; note that it mirrors `.agents/skills` for Claude |
| `src/lib/platforms/codex.sh` | Bash implementation for Codex |
| `src/lib/platforms/opencode.sh` | Bash implementation for OpenCode |
| `src/lib/platforms/github-copilot.sh` | Bash implementation for Copilot |

## Current State

**Done:**
- Inspected the current `docs/PLATFORM_DIRS_DOCS.md`.
- Inspected the Go platform implementations and the bash platform implementations.
- Confirmed the user wants two outputs in one doc update:
  - official-doc refresh
  - cross-platform “most common location” matrix
- Produced a concrete implementation plan in-chat.

**In Progress:**
- Partial upstream doc lookup was started with web search/open calls.
- Claude docs were easier to verify than Cursor because Cursor pages are JS-heavy in this environment.
- No content has been rewritten yet.

**Not Started:**
- Rewriting `docs/PLATFORM_DIRS_DOCS.md`
- Building the final matrix
- Re-checking the hook audit wording after the doc rewrite

## Decisions Made

- **Separate upstream behavior from repo behavior** — The current doc conflates official platform discovery paths with what `dot-agents` currently creates. The rewrite should split these into separate sections.
- **Use official docs as the source of truth for the matrix** — The “most common location” should be based on documented platform read paths, not local implementation convenience.
- **Keep the hook wiring audit** — That section is still useful, but it should remain explicitly about `dot-agents` implementation state.
- **Document no-consensus cases honestly** — Do not force a single “winner” path where the ecosystems genuinely diverge.

## Important Context

- The user explicitly said: check the online docs for each platform, update `docs/PLATFORM_DIRS_DOCS.md`, then make a matrix for each resource to find the most common location between them.
- The user then asked for a handoff and added a workflow note: use subagents per platform.
- Treat that as an execution note for the next session: parallelize the upstream-doc verification by platform if helpful.
- Repo state at handoff:
  - Branch: `claude/scalable-skill-syncing-sfxOd`
  - Untracked files already present before this handoff: `.github/copilot-instructions.md`, `.mcp.json`, `codex-hooks.md`, `dot-agents.code-workspace`
- No repo-tracked files were modified in this session.

## Next Steps

1. **Resume upstream-doc verification** — Confirm each platform/resource path from official docs and collect clean citation links for the final doc.
2. **Use subagents per platform** — Split research into per-platform workstreams if delegation is available and still desired:
   - Cursor
   - Claude Code
   - Codex
   - OpenCode
   - GitHub Copilot
3. **Rewrite `docs/PLATFORM_DIRS_DOCS.md`** — Restructure into upstream docs, matrix, and implementation audit.
4. **Compute the matrix winners** — Count officially documented read paths per resource type and mark ties or no-consensus cases explicitly.
5. **Self-review the rewritten doc** — Check that every claim is either cited to an upstream doc or clearly labeled as local implementation behavior.

## Constraints

- Do not revert or disturb the existing untracked files in the repo root.
- Prefer official platform docs over secondary summaries.
- Use `apply_patch` for doc edits.
- Keep the updated doc concise and scannable; avoid turning it into a long changelog.
- If Cursor official pages remain hard to scrape, cite the official Cursor URLs in the doc and clearly mark any remaining uncertainty rather than inventing unsupported details.

---

## Update — 2026-03-29

**Status:** Completed

### What Changed

- Rewrote `docs/PLATFORM_DIRS_DOCS.md` into three clear sections:
  - official platform locations
  - cross-platform common-location matrix
  - `dot-agents` implementation audit
- Verified the Cursor pages by direct fetch because the browser tool did not render the JS-heavy docs cleanly enough for path extraction.
- Recomputed the matrix using only officially documented project-level paths instead of current repo behavior.

### Key Outcomes

- Skills are still a real tie at the official-doc level.
  - Based on the current official docs, `.claude/skills/<name>/SKILL.md` and `.agents/skills/<name>/SKILL.md` each have 4-platform coverage.
  - That makes skills a stronger shared category than agents, but it does not produce a single canonical winner without making a tradeoff.
- Instructions/rules still do not have a single clean native-path winner.
  - `AGENTS.md` is the strongest shared instruction file across Cursor, Codex, OpenCode, and GitHub Copilot agent instructions.
  - That still does not replace Claude Code's `CLAUDE.md` or GitHub Copilot's `.github/copilot-instructions.md`.
- Agents/subagents remain a no-consensus category because both the directory names and file formats diverge.

### Important Corrections From The Old Doc

- OpenCode agent paths are documented as `.opencode/agent/`, not `.opencode/agents/`.
- GitHub Copilot official CLI docs support `.github/skills/`, `.agents/skills/`, and `.claude/skills/`; the old doc was still incomplete, but `.agents/skills/` is now clearly an official Copilot CLI compatibility location.
- GitHub Copilot official agent docs center on `.github/agents/`; `.agents/agents/` was removed from the official section.
- GitHub Copilot coding-agent MCP is configured in repository settings on GitHub.com, and Copilot CLI also officially documents `.github/mcp.json`, `.mcp.json`, `.vscode/mcp.json`, `.devcontainer/devcontainer.json`, and `~/.copilot/mcp-config.json`.
- GitHub Copilot hooks are now documented as `.github/hooks/*.json`; the old folder-based `.github/hooks/<name>/hooks.json` claim was removed from the official section.

### Repo State

- Modified: `docs/PLATFORM_DIRS_DOCS.md`
- Existing untracked files in repo root were left untouched:
  - `.github/copilot-instructions.md`
  - `.mcp.json`
  - `codex-hooks.md`
  - `dot-agents.code-workspace`

### Follow-Up

- No additional code changes were needed.
- If another pass is wanted later, the highest-value follow-up would be deciding whether the repo should keep a dual-skill strategy (`.agents/skills/` plus `.claude/skills/`) instead of trying to force a single canonical shared directory.

---

## Update — 2026-03-29

**Status:** Completed

### What Changed

- Extended `docs/PLATFORM_DIRS_DOCS.md` again after the user clarified the desired storage model for `~/.agents/`.
- Added a new `Other Documented Resources Worth Managing` section covering:
  - Cursor ignore files and custom commands
  - Claude Code legacy commands, output styles, and status line scripts
  - OpenCode modes, plugins, and themes
  - GitHub Copilot prompt files
- Added a new `Canonical ~/.agents Storage Policy` section that defines the preferred rule:
  - keep one canonical source per resource type in `~/.agents`
  - wire first to the greatest common compat path
  - then to the next best shared path if fragmented
  - finally to platform-native paths when no useful compat path exists

### Decisions

- The user explicitly wants `~/.agents` to remain the single storage source for each resource type.
- The docs now frame the storage strategy as:
  - canonical storage in `~/.agents/{resource}/{scope}/...`
  - outward translation into platform paths
- Skills are the clearest split-GCD case.
  - Canonical storage should still be one copy in `~/.agents/skills/`
  - but wiring should target both `.claude/skills/` and `.agents/skills/`
- Agents should also stay canonical in `~/.agents/agents/`, even though downstream outputs may need format translation for Codex, OpenCode, and GitHub Copilot.
- MCP and hooks do not have a clean compat winner, so the docs now treat them as canonical internal resources that must be translated outward into platform-native config files.

### Current Deliverable State

- `docs/PLATFORM_DIRS_DOCS.md` is now doing four jobs:
  - official platform locations
  - canonical `~/.agents` storage policy
  - additional resource types worth managing
  - current implementation audit
- The doc is now aligned with the user's architectural preference even where the current code has not yet caught up.

### Important Caveat For The Next Agent

- The new canonical-storage section is a design recommendation, not a claim that the current implementation already supports all listed resource types or translation steps.
- If the next task is implementation, the next agent should treat the doc as target architecture and compare it against:
  - `commands/init.go`
  - `internal/platform/*.go`
  - `src/lib/platforms/*.sh`
  - `src/lib/commands/explain.sh`

### Suggested Next Steps

1. Decide whether the next phase is still documentation-only or should start changing code to match the new canonical-storage policy.
2. If code changes are wanted, define the canonical on-disk schema additions first:
   - `commands/`
   - `output-styles/`
   - `ignore/`
   - `modes/`
   - `plugins/`
   - `themes/`
   - `prompts/`
3. Then implement wiring in precedence order:
   - greatest common compat path first
   - next-best shared path second
   - platform-native translation last

### Repo State

- Modified tracked file:
  - `docs/PLATFORM_DIRS_DOCS.md`
- The handoff file itself is also updated in this session.
- Current branch remains:
  - `claude/scalable-skill-syncing-sfxOd`
