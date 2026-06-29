# Common Failure Points at Handoff

Mistakes to avoid when handing off to the next session.

## Leaving uncommitted debugging code

- Always search for debugging statements before handoff: `console.log`, `debugger`, `print()`, `var_dump`, etc.
- The next session may not realize these are debugging artifacts and leave them in.

## Not documenting WHY something was left incomplete

- Saying "X is not done" is useless without context.
- Always explain why: blocked on what? Ran out of time? Discovered a complication?
- The next agent needs to know whether to continue the same approach or try something different.

## Forgetting to update .agents/active/ plans

- If a plan exists in `.agents/active/`, it must reflect current state.
- An outdated plan is worse than no plan — it actively misleads the next session.

## Not running tests before handoff

- Leaving a broken test suite without documenting it forces the next session to debug your changes before they can even start.
- If tests fail, either fix them or document exactly which tests fail and why.

## Writing vague notes that do not help the next session

- "Made progress on the feature" tells the next agent nothing.
- "Implemented the API endpoint in `src/api/users.ts`, tests pass, still need to add pagination (see TODO on line 47)" gives them a starting point.
- Be specific: file paths, function names, line numbers, commit hashes.

## Trusting a stale prose summary instead of the verified view

- A handoff document is the *why*; it goes stale the instant state changes (a PR merges, a branch moves). The most expensive recovery failure is acting on a remembered claim that is no longer true — e.g. re-doing work that already shipped.
- On resume, always start with `da workflow journal recover` (or `/agent-handoff recover`) and trust the verified view over the prose. Never treat a `changed`, `missing`, or `unverified` item as still-true, and never auto-resume a `QUARANTINED` bundle. See `instructions/verified-readback.md`.

## Letting un-recorded reasoning pile up before a crash

- The deterministic journal captures *what changed* automatically, but your *reasoning* survives a force-kill only if it was already appended. Reasoning formed since your last append is lost.
- Follow the cadence in `instructions/journal-cadence.md`: capture before risky operations, under context pressure, after a decision, and on the work-OR-time backstop — never carry more than one un-recorded decision.
