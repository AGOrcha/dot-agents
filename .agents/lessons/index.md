# Lessons Index

- [`tests-for-each-slice`](./tests-for-each-slice/LESSON.md): Each implementation slice should add automated coverage for the behavior it introduces before the slice is considered complete.
- [`additive-state-fields`](./additive-state-fields/LESSON.md): When extending a central state struct, add a new field rather than reusing an existing one — keeps JSON contract stable and callers unbroken.
- [`test-string-count-ids`](./test-string-count-ids/LESSON.md): When checking for duplicate entries in a markdown index, count `"- [id]"` (the entry prefix) not the raw ID string — the ID can appear multiple times in a single valid link entry.
- [`kg-test-fixture-helper`](./kg-test-fixture-helper/LESSON.md): KG tests needing a populated graph should use `setupKGWithNotes(t)` — don't re-seed notes inline per test.
- [`test-stub-upgrade-pattern`](./test-stub-upgrade-pattern/LESSON.md): When upgrading a stub to a real implementation, grep for tests asserting stub side-effects and update them before running the full suite.
- [`refresh-import-before-relink`](./refresh-import-before-relink/LESSON.md): Refresh flows that import repo files and regenerate managed outputs must import project content before relinking, and must not replay canonical backup snapshots as live state.
- [`verify-managed-file-target`](./verify-managed-file-target/LESSON.md): When a task touches a managed file path, use the intended managed generation flow first; if a repo-local file is still required, verify the final path is not a generated managed link before closing the task.
- [`use-skill-architect-for-skill-generation`](./use-skill-architect-for-skill-generation/LESSON.md): When generating or restructuring skills, use the skill-architect workflow so skills are valid before they are treated as usable or promoted to managed resources.
- [`archive-completed-active-plans`](./archive-completed-active-plans/LESSON.md): When a plan is complete, move it out of `.agents/active/` into the matching history folder so the active set reflects current work instead of finished tasks.
- [`lint-check-count-assertion`](./lint-check-count-assertion/LESSON.md): When adding a new lint check, grep for `ChecksRun` count assertions in tests and update them to `N+1` before running the suite.
- [`sidecar-manifest-pattern`](./sidecar-manifest-pattern/LESSON.md): Store integrity hashes in a sidecar manifest file keyed by ID, not inline in the file being hashed — avoids self-referential hash problem.
- [`rfc-resolves-plan`](./rfc-resolves-plan/LESSON.md): Plans gated on "requires RFC" often already contain the answers — write a brief RFC and implement in the same session instead of spending a full cycle on research.
- [`select-star-scan-order`](./select-star-scan-order/LESSON.md): When porting DB schemas to Go, SELECT * scans must match exact column order — enumerate columns explicitly or count and comment them inline.
- [`use-existing-subdir-helpers`](./use-existing-subdir-helpers/LESSON.md): In kg.go, use `noteSubdir(t)` not `noteType+"s"` — "entity" maps to "entities" not "entitys".
