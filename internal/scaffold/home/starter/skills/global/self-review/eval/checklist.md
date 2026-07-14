---
scope: "Pass/fail checklist — every item must pass before review is complete"
type: "gate"
---

# Self-Review Checklist

Run this checklist after completing all review steps. Every item is pass/fail. If any item fails, fix the issue and re-evaluate.

| #  | Check                              | How to Verify                                                              |
|----|-------------------------------------|---------------------------------------------------------------------------|
| 1  | No debugging code left              | Search for console.log, print, debugger, binding.pry in changed files     |
| 2  | All tests pass                      | Run the project test suite and confirm zero failures                       |
| 3  | No linting errors                   | Run the project linter and confirm zero errors in changed files            |
| 4  | Commit message clear                | Message explains *why* the change was made, not just *what* changed        |
| 5  | Changes focused                     | Every changed file relates to a single purpose; no scope creep             |
| 6  | No unrelated changes                | No formatting-only diffs, unrelated refactors, or drive-by fixes mixed in  |
| 7  | No hardcoded secrets                | No API keys, tokens, passwords, or credentials anywhere in the diff        |
| 8  | Error handling appropriate           | External calls have error paths; failures produce meaningful messages       |
| 9  | Edge cases considered                | Null, empty, boundary, and error inputs are handled or explicitly excluded  |
| 10 | No obvious performance issues        | No N+1 queries, unbounded loops, or unnecessary repeated computation       |

## Evaluation

```
Result: [PASS / FAIL]
Failing items: [list item numbers, or "none"]
Action required: [what to fix, or "none — ready to commit"]
```
