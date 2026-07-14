---
scope: "Commonly missed issues that frequently slip through self-review"
---

# Common Gotchas

These are the issues most frequently missed during self-review. Check each one explicitly.

## Debugging Artifacts

- `console.log`, `print()`, `fmt.Println`, `debugger`, `binding.pry` left in code.
- `// DEBUG`, `// TEMP`, `// HACK` comments without cleanup.
- Verbose logging that should be removed or downgraded before merge.
- Test-only configuration values left in production code paths.

## Async and Error Handling

- Promises without `.catch()` or `try/catch` around `await`.
- Fire-and-forget async calls that should be awaited.
- Error callbacks that silently swallow failures.
- Race conditions from shared state modified by concurrent operations.
- Missing timeout on network calls that could hang indefinitely.

## Tests That Lie

- Tests that pass but don't actually assert the behavior they claim to test.
- Tests with hardcoded expected values that don't derive from the logic.
- Tests that mock so heavily they only test the mock setup.
- Missing edge case tests: empty input, null, boundary values, error paths.
- Tests that depend on execution order or external state.

## Import and Dependency Side Effects

- New imports that pull in heavy dependencies for trivial usage.
- Import changes that alter initialization order or side effects.
- Circular imports introduced by the change.
- Removing an import that was a transitive dependency for another module.

## Null and Undefined

- Accessing properties on values that could be null/undefined/nil.
- Optional chaining missing where the chain could break.
- Default values that mask bugs (defaulting to empty string when null means "not found").
- Array/map operations on potentially empty or undefined collections.

## TODO and Context

- TODO comments without a ticket number, author, or explanation of what's needed.
- Comments that describe *what* the code does instead of *why*.
- Stale comments that no longer match the code they describe.
- Missing context on non-obvious decisions that will confuse future readers.
