# Rendered rule files: edit the resource source, not the rendered copy

**Date**: 2026-07-13
**Trigger**: During the e2e pipeline audit, an edit to `~/.agents/rules/dot-agents/agents.md`
(fixing the stale `.agents/active/*.plan.md` plan-layout paragraph) was silently reverted by
the next `da refresh`. Note this is temporary guidance since it's a product gap / defect.

## The pattern

Some files under `~/.agents/rules/<project>/` are RENDERED from a resource source and get
regenerated on `da refresh`:

- `~/.agents/rules/dot-agents/agents.md` ← rendered from
  `~/.agents/resources/dot-agents/AGENTS.md` (the repo's root `AGENTS.md` is a symlink to the
  rendered rule file, which makes the loop easy to miss — editing "the repo file" or "the rule
  file" both edit the rendered layer).
- Other rule files in the same dir (`proposal-routing.md`, `workflow-artifact-model.md`,
  `schema-usage.md`) have NO resource source today and hold edits across refresh.

## The rule

Before editing any file under `~/.agents/rules/` (or a repo file that is a symlink into it):

1. Check for a source: `grep -rl "<distinctive sentence>" ~/.agents/resources/<project>/`.
2. If a resource copy exists, edit THAT file, then run `da refresh` and verify the rendered
   rule file carries the change.
3. If no resource copy exists, the rule file is directly editable.

## Why it bites

The revert is silent — `da refresh` reports success, and the stale text returns without any
diff notice. An edit can survive for the rest of a session (looking done and verified) and
vanish on the next refresh, re-teaching agents the stale convention the edit was meant to
kill.
