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

**External corroboration (research, 2026-07-13):** the *Bilevel Autoresearch* paper
(arXiv 2603.23420v2; eval companion Part M) autonomously generates and injects new
mechanisms (code, and by generalization skills/prompts/workflows) at runtime and flags the
exact failure modes this lesson guards against — a mechanism silently falling back without an
error, and one reverted only because a missing `sklearn` import was caught at validation. The
paper explicitly notes it has "no stability guarantee" and calls silent fallback "dangerous."
The takeaway for any future dot-agents auto-mechanism-generation: the validation gate
(`skill-architect` + `da review`) is the safety/rollback envelope the unattended generator
lacks — generated mechanisms must pass import/frontmatter/structure validation before they are
treated as usable, never trusted on silent success.
