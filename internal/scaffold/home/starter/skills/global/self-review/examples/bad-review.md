---
type: "example"
quality: "bad"
---

# Bad Self-Review Example

## What went wrong

This review is shallow, misses real issues, and rubber-stamps the change.

## Review Output (bad)

> Looks good. Code is clean. Tests pass. Ready to merge.

## Why this is bad

1. **No specific findings.** A review that finds zero issues in non-trivial code is almost certainly not thorough enough.
2. **No file-level detail.** The reviewer did not mention which files were checked or what was looked for.
3. **No evidence of checklist.** The pass/fail checklist was not run or reported.
4. **No advisory board.** The three personas were not consulted.
5. **"Looks good" is not a review.** It is a rubber stamp that provides no value.

## What the review missed (actual issues in the code)

- `console.log(userData)` left on line 38 — logs PII to stdout.
- No input validation on the `userId` path parameter — accepts any string, including SQL-injectable ones.
- The new utility function duplicates logic already in `src/utils/format.ts`.
- Tests assert that the function "returns something" but do not check the actual shape or values.
- A TODO comment with no ticket or context: `// TODO: fix this later`.

## Lesson

A self-review should take at least as long as reading the diff carefully. If it took 10 seconds, it was not a review.
