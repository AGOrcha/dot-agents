# test-file-structure — plan (non-normative)

Spec: `.agents/workflow/specs/test-file-structure/design.md` (the contract).

## Ordering rationale

- `t1-mapping` and `t2-convention-docs` are independent and run first
  (mapping is the review gate; docs define what "conformant" means for
  the audit).
- Redistribution is split by **branch ownership**, not convenience:
  - `t3` → `commands/workflow` grab-bags live only on pr3b → pr3b worktree.
  - `t4` → `commands/ci_drift{,2,3}` are **already merged to master**
    (pr3a #15). They are not pr3b-owned; doing them on pr3b violates
    write-scope. Hence a master-direct change. This is a deliberate
    deviation from the "ship on pr3b/pr3c" directive — surfaced for
    confirmation before execution.
- `t5-audit` waits on `t2` (needs the documented convention to judge
  conformance); any moves it finds are folded into the owning branch's
  redistribute scope, not a new branch.
- `t6` is the single verification/close gate across all touched worktrees;
  the coverage gate is the hard constraint (moves must be statement-neutral).

## Key constraints

- Pure moves. Zero test-body / assertion / name / `t.Run` changes. If a
  moved test references a package-var seam, it moves verbatim — seam→DI
  is `di-refactor-rollout`'s scope, explicitly not this.
- 95%-per-package coverage gate must stay green on every touched package
  (moving a Test func between files in the same package is
  statement-neutral, so this should hold by construction — verified, not
  assumed).

## Deferred

- Root `commands` package decomposition → architecture note +
  `root-command-decomposition` draft plan (separate, unowned, not folded
  into di-refactor-rollout — different scope/write-surface).
- Seam-globals → Deps DI → `di-refactor-rollout` (existing draft).
- Source-file renames/splits — out of scope; test files only.
