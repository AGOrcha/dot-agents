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
