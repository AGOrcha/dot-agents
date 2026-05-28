# Workflow: Plan Wave Picker

Use this skill at the start of a session when multiple plans exist in `.agents/active/`.

## Selection Process

1. Read plan statuses in one batch.
   Glob `.agents/active/*.plan.md`, then read all of them or grep for `Status:` to identify completed versus active plans.

   ```bash
   grep -l "Status: Completed" .agents/active/*.plan.md
   grep -L "Status: Completed" .agents/active/*.plan.md
   ```

2. Check dependency ordering.
   Read the first non-completed plan and verify any `Depends on:` relationships are satisfied before selecting it.

3. Pick the lowest-numbered non-completed wave or phase.
   - Waves are typically ordered `Wave 1`, `Wave 2`, `Wave 3`, and so on.
   - Phases are ordered `Phase 1`, `Phase 2`, `Phase 3`, and so on.
   - When dependencies allow, run waves or phases from independent plan tracks in parallel for the same loop iteration.

4. Check for existing partial work.
   Use untracked or modified files in `git status` to detect whether a phase has already started before choosing fresh work.

   ```bash
   git status --short | grep "^??"
   ```
