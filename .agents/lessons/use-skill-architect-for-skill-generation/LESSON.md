---
name: use-skill-architect-for-skill-generation
description: When generating or restructuring skills, use the skill-architect workflow so SKILL.md files have valid frontmatter, orchestrator-only content, and required instruction files before they are treated as usable skills
type: feedback
---

Skill files should not be created as monolithic prose documents when the repository expects Anthropic-style skill structure. A raw `SKILL.md` without frontmatter or with inline rules will be skipped by skill loaders and creates noise in both repo-local and managed skill directories.

**Why:** Invalid skills fail at load time, and if they are imported or mirrored before review they spread broken artifacts into managed resources.

**How to apply:**
1. When creating or refactoring a skill, run it through the `skill-architect` path instead of writing a freeform `SKILL.md`.
2. Ensure `SKILL.md` has valid YAML frontmatter and acts only as an orchestrator.
3. Move workflow details and failure points into `instructions/` files, including `instructions/gotchas.md`.
4. Keep review-stage skills repo-local until they are valid and intentionally promoted into managed resources.
