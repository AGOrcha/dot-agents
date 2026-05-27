# loop-worker vs general-purpose: pick by bundle presence

## Pattern

When spawning a worker for autonomous PR-shipping work, pick the `subagent_type` by whether a delegation bundle exists:

- **`loop-worker`** requires a delegation bundle (`plan_id`, `task_id`, `stage`, `write_scope`, `feedback_goal`). It will REFUSE to self-select work and will not run an orchestrator-shaped multi-tier PR flow. This is by design — the bounded-stage contract is what makes parallel loop-worker fanout safe.

- **`general-purpose`** has no profile constraints. Use it when:
  - The work doesn't fit a canonical plan task (cross-cutting hygiene PRs, ad-hoc cleanups)
  - You authored the scope directly in the prompt
  - The worker needs to run the full lifecycle (worktree → multi-commit → PR → self-monitor → auto-fix)
  - There's no `write_scope` enforceable boundary (because the scope is "every file flagged by Sonar for X rule")

## Root cause

Session 151d7271 spawned a `loop-worker` for "master Sonar hygiene PR (fix 13 BLOCKER/CRITICAL issues across 8 files)". The worker correctly refused with explicit guardrail-violation reasoning:
> Multi-commit, multi-tier PR creation is orchestrator-scope, not worker-scope... Worktree creation, branch creation, and `gh pr create` are not in the bounded-worker startup/closeout sequence... No bundle means no write_scope to enforce, which is the worker's primary constraint.

Re-spawning with `general-purpose` and the same prompt body proceeded normally. Cost of the refusal-and-respawn cycle: ~20k tokens.

## Rule

Before spawning, ask: "does this map to a canonical plan task with a delegation bundle?"
- **Yes** → `loop-worker` with the bundle path in the prompt
- **No** → `general-purpose` (or `claude` for very small tasks; `haiku` for tiny mechanical fixes)

Do NOT try to brief a `loop-worker` into acting like an orchestrator — the profile enforcement is intentional.

## Cross-references

- `[[worker-owns-pr-readiness-loop]]` — applies to BOTH worker types when in PR-shipping mode; the difference is bundle-required (loop-worker) vs scope-from-prompt (general-purpose)
- `[[validate-bundle-against-head]]` — only relevant to loop-worker (since general-purpose has no bundle)
- `[[starter-vs-project-overlay]]` — this lesson is GENERIC (about Claude Code subagent profiles, not dot-agents-specific). If promoting to a shared skill, it belongs in the generic starter, not the dot-agents dev overlay
