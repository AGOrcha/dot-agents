# Unit verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **unit verification**: prove the changed code is
correct via the project's unit test suite. `--kind test`, `--verifier-type unit`.

## What to run

1. **Scoped first.** Run the unit tests that **cover** the files in `write_scope_touched` — the
   smallest set of test targets exercising the changed code.
   - **Positive:** the happy-path tests for the changed units build and pass.
   - **Negative:** where the change introduces a new failure mode, run the targeted tests that assert
     the error path.
2. **Full suite for the final pass.** Once the scoped run is green, run the project's complete unit
   suite so the change has not regressed a sibling.

If the scoped run fails, you may skip the full suite, but record `--status fail` and name the first
failing test in the summary.

## Record

```
da workflow verify record --kind test --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type unit \
  --command "<scoped test command>" --command "<full suite command>" \
  --summary "<packages/tests run, first failure, evidence>"
```

The exact test runner, flags, and touched-path → test-target mapping come from the repo-local
override for this verifier; this template is language-agnostic.
