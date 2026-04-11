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
   - Workflow follow-on spec waves are ordered `Wave 3`, `Wave 4`, `Wave 5`, and so on.
   - KG phases are ordered `KG Phase 1`, `KG Phase 2`, `KG Phase 3`, and so on.
   - When dependencies allow, run one workflow wave and one KG phase in parallel for the same loop iteration.

4. Check for existing partial work.
   Use untracked or modified files in `git status` to detect whether a phase has already started before choosing fresh work.

   ```bash
   git status --short | grep "^??"
   ```
