# Lessons Index

- [`tests-for-each-slice`](./tests-for-each-slice/LESSON.md): Each implementation slice should add automated coverage for the behavior it introduces before the slice is considered complete.
- [`additive-state-fields`](./additive-state-fields/LESSON.md): When extending a central state struct, add a new field rather than reusing an existing one — keeps JSON contract stable and callers unbroken.
- [`test-string-count-ids`](./test-string-count-ids/LESSON.md): When checking for duplicate entries in a markdown index, count `"- [id]"` (the entry prefix) not the raw ID string — the ID can appear multiple times in a single valid link entry.
- [`kg-test-fixture-helper`](./kg-test-fixture-helper/LESSON.md): KG tests needing a populated graph should use `setupKGWithNotes(t)` — don't re-seed notes inline per test.
