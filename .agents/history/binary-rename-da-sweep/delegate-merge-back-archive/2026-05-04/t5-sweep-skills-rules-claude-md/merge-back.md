---
schema_version: 1
task_id: t5-sweep-skills-rules-claude-md
parent_plan_id: binary-rename-da-sweep
title: Sweep ~/.agents/{skills,rules}/ + ~/.claude/CLAUDE.md for binary-name references
summary: 'Swept ''dot-agents <verb>'' -> ''da <verb>'' across ~/.agents/skills/ (20 files), ~/.agents/rules/ (3 files), and ~/.claude/CLAUDE.md per ADR-0006 hard cutover. 81 verb invocations substituted plus 7 backticked binary-name references in prose (e.g. `dot-agents` won''t build -> `da` won''t build, ./bin/dot-agents -> ./bin/da, command -v dot-agents -> command -v da). Hard-test grep returned 28 remaining matches, all project-name prose, source-dir paths (cmd/dot-agents), sibling-repo paths (../dot-agents), or section headings (## dot-agents binary, **dot-agents repo**). No binary ''dot-agents <verb>'' invocations remain. Anti-scope preserved: namespace directories ~/.agents/skills/dot-agents/ and ~/.agents/rules/dot-agents/ untouched; ~/.agents/proposals/archived/ untouched.'
files_changed:
    - .agents/workflow/plans/binary-rename-da-sweep/PLAN.yaml
    - .agents/workflow/plans/binary-rename-da-sweep/TASKS.yaml
verification_result:
    status: pass
    summary: Edits land in user's home directory (~/.agents/ and ~/.claude/CLAUDE.md). Per task brief, did not git-commit from inside the project repo — those paths are outside this repo's tracking. ~/.agents/ may be its own git repo with pre-existing dirty state at session start (mcp/, settings/, context/, etc.) — left untouched; only the in-scope files for t5 were modified. Project repo dirty state (PLAN.yaml, TASKS.yaml, delegation-bundles/, delegation/, history/) was the workflow runtime's own bookkeeping and is untouched. Repo-local .agents/skills/* are symlinks into ~/.agents/skills/dot-agents/, so the edits are immediately visible without running 'da skills promote' or 'da refresh'.
integration_notes: Edits land in user's home directory (~/.agents/ and ~/.claude/CLAUDE.md). Per task brief, did not git-commit from inside the project repo — those paths are outside this repo's tracking. ~/.agents/ may be its own git repo with pre-existing dirty state at session start (mcp/, settings/, context/, etc.) — left untouched; only the in-scope files for t5 were modified. Project repo dirty state (PLAN.yaml, TASKS.yaml, delegation-bundles/, delegation/, history/) was the workflow runtime's own bookkeeping and is untouched. Repo-local .agents/skills/* are symlinks into ~/.agents/skills/dot-agents/, so the edits are immediately visible without running 'da skills promote' or 'da refresh'.
created_at: "2026-05-04T02:58:56Z"
---

## Summary

Swept 'dot-agents <verb>' -> 'da <verb>' across ~/.agents/skills/ (20 files), ~/.agents/rules/ (3 files), and ~/.claude/CLAUDE.md per ADR-0006 hard cutover. 81 verb invocations substituted plus 7 backticked binary-name references in prose (e.g. `dot-agents` won't build -> `da` won't build, ./bin/dot-agents -> ./bin/da, command -v dot-agents -> command -v da). Hard-test grep returned 28 remaining matches, all project-name prose, source-dir paths (cmd/dot-agents), sibling-repo paths (../dot-agents), or section headings (## dot-agents binary, **dot-agents repo**). No binary 'dot-agents <verb>' invocations remain. Anti-scope preserved: namespace directories ~/.agents/skills/dot-agents/ and ~/.agents/rules/dot-agents/ untouched; ~/.agents/proposals/archived/ untouched.

## Integration Notes

Edits land in user's home directory (~/.agents/ and ~/.claude/CLAUDE.md). Per task brief, did not git-commit from inside the project repo — those paths are outside this repo's tracking. ~/.agents/ may be its own git repo with pre-existing dirty state at session start (mcp/, settings/, context/, etc.) — left untouched; only the in-scope files for t5 were modified. Project repo dirty state (PLAN.yaml, TASKS.yaml, delegation-bundles/, delegation/, history/) was the workflow runtime's own bookkeeping and is untouched. Repo-local .agents/skills/* are symlinks into ~/.agents/skills/dot-agents/, so the edits are immediately visible without running 'da skills promote' or 'da refresh'.
