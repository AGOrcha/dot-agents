---
scope: "Code quality rules applied to every changed file"
---

# Code Quality Rules

## Naming

- Variables, functions, and classes have descriptive names that reveal intent.
- No single-letter names outside of trivial loop counters (`i`, `j`, `k`).
- Boolean variables read as questions: `isReady`, `hasPermission`, `canRetry`.
- Constants use UPPER_SNAKE_CASE where the language convention expects it.
- No misleading names (e.g., `userList` that is actually a map).

## Readability

- Functions do one thing and are short enough to understand in a single read.
- Nesting depth does not exceed 3 levels. Use early returns or extract helpers.
- No magic numbers or strings — use named constants.
- Complex conditionals are extracted into well-named boolean variables or functions.
- Code reads top-to-bottom without requiring the reader to jump around.

## Anti-Patterns to Flag

- Copy-pasted code blocks that should be a shared function.
- God functions that do too many things (> 40 lines is a smell).
- Stringly-typed data where enums or types would be safer.
- Boolean parameters that make call sites unreadable (`doThing(true, false, true)`).
- Commented-out code left in production paths.
- Catch blocks that swallow exceptions silently.

## Error Handling

- Every external call (network, file, DB) has error handling.
- Errors propagate meaningful messages, not generic "something went wrong."
- Async operations have both success and failure paths handled.
- Resource cleanup happens in finally blocks or equivalent (defer, disposables, context managers).
- Validation happens at boundaries (user input, API responses, config parsing).

## Structure

- Imports are organized and unused imports are removed.
- No circular dependencies introduced.
- New code follows existing patterns in the file and project.
- Public API surface is intentional — don't expose internals unnecessarily.
