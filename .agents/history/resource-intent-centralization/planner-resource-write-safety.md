# Planner Notes: MCP, Hooks, and Shared Resource Write Safety

## Purpose

Use this note when planning work that touches canonical `~/.agents` resources, platform settings projections, or repo-local shared mirrors such as `.agents/skills/`.

## Current Safety Model

### MCP

- MCP is single-source, not merged.
- Projection chooses one canonical file by precedence (`project` before `global`, platform-specific before generic fallback) and links that file into the platform path.
- Safety comes from deterministic source selection, not from incremental file editing.

### Hooks

- Hooks are canonicalized as named bundles under `hooks/<scope>/...`.
- Platform settings and hook files are rendered from the resolved canonical hook set, not patched in place.
- Project scope overrides global by hook name.
- Output order is deterministic because hook names are sorted before rendering.
- Managed rendered files are only removed when the current file still matches the bytes that dot-agents would generate.

### Shared Repo-Local Targets

- Shared mirrors such as `.agents/skills/<name>` are the riskiest paths because multiple platform implementations currently try to create the same target.
- These paths should be treated as centrally owned projection targets, not platform-owned outputs.
- Plans should avoid local fixes that make low-level link helpers broadly destructive; prefer central ownership, dedupe, and conflict handling.

## Planner Rules

1. Separate canonicalization from projection.
   Canonicalization is import/install/restore into `~/.agents`. Projection is refresh/link/render into repo or user-home outputs.
2. Treat platform settings files as full projections.
   For hooks/settings compatibility files, do not plan incremental JSON edits; plan deterministic re-render from canonical inputs.
3. Treat shared targets as single-owner outputs.
   If more than one platform wants the same repo path, plan a central resource-intent/executor slice rather than parallel platform-local writes.
4. Preserve managed-file safety.
   Removal or overwrite logic should only delete files that are still provably managed, not user-modified.

## Review-Only Skill Exception

The following skill outputs are currently review-stage artifacts, not approved managed resources:

- `.agents/skills/plan-wave-picker/SKILL.md`
- `.agents/skills/delegation-lifecycle/SKILL.md`
- `.agents/skills/provider-consumer-pair/SKILL.md`
- `~/.agents/skills/dot-agents/plan-wave-picker/SKILL.md`

These were intentionally left out of canonical re-import after generation so traces and outputs can be reviewed and condensed first.

Planner implications:

- Do not add a plan step that auto-imports these back into managed resources yet.
- Do not treat the invalid `SKILL.md` files as canonical source-of-truth inputs.
- Any future import/promotion step should first run through the `skill-architect` path so the resulting skill has valid frontmatter and passes the skill checklist.

## Planning Guidance

For upcoming workflow automation and KG self-improvement work:

- Plan review/condense work on generated skill traces before any managed import.
- If the task touches `.claude/settings.local.json`, `.agents/skills/`, or other shared outputs, include an explicit ownership/convergence step in the plan.
- Prefer a central resource-plan slice over more platform-specific `CreateLinks()` patches when the same target path is shared.
