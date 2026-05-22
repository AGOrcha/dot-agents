# Lesson: prefer interface-DI seams over package-level func-var seams

## What happened

A maintainer asked for a review of whether PR #35's `seams.go` approach
matched the project's testability strategy. A subagent audit concluded
"func-var seams are the sanctioned project strategy" because the pattern
already existed pervasively (`commands/seams.go` + 8 packages) and verdicted
#35 "conforms, safe to merge as-is."

The maintainer then corrected this: the package-level func-var seam pattern
(`var osX = os.X`, swapped in tests) is an **early detail that was not fully
caught** — it is legacy, not the target. The **interface-based
dependency-injection pattern is preferred** going forward.

## Root cause

"Pervasive in the codebase" was treated as "sanctioned / the target
pattern." Prevalence is not endorsement. An early implementation detail can
spread widely before anyone decides it is the wrong long-term shape. A
code-pattern audit that only measures *consistency with existing code*
cannot detect a pattern the maintainer has since decided against — it will
keep ratifying the debt.

## Rule

1. When auditing whether code "matches the project pattern," distinguish
   **descriptive** (what the code currently does most often) from
   **prescriptive** (what the maintainer wants going forward). State which
   one the verdict is based on. If the prescriptive direction is unknown,
   surface that as an open question instead of defaulting to descriptive.
2. For new test seams, prefer **interface-DI**: define a narrow interface,
   inject a real implementation in production and a fake in tests via the
   constructor/struct field. Do **not** add new package-level
   `var osX = os.X` func-var seams.
3. Existing func-var seams are legacy. Do not expand them in isolation
   either — a lone interface-DI conversion inside a file that is otherwise
   all func-var is its own inconsistency. Systematic migration belongs in a
   tracked plan/task, not ad hoc.
4. `prefer-test-seam-over-untestable` still holds (a seam beats a
   `[defensive-unreachable]` allowlist entry) — but the seam should be
   interface-DI, not func-var.

## How to apply

- New error-propagation branch that needs fault injection → introduce/extend
  a collaborator interface and inject a fake. See the graph-store collaborator
  pattern (`internal/graphstore` `NewHandle(store Store)`) as the reference
  shape.
- Reviewing a PR that adds/extends `seams.go` → flag it as expanding the
  legacy pattern; route through the migration plan rather than approving on
  consistency grounds.
- Audits of "does this match our pattern": ask the maintainer for the
  prescriptive direction when it is not documented; never infer it from
  prevalence alone.

## Canonical reference

The prescriptive shape is now documented: **`docs/TEST_SEAMS.md`**
(canonical convention — narrow collaborator interface, `std…` prod
impl, narrowest injection site, nil-delegating fake). Cite it in
reviews instead of re-deriving the argument. Migration of the remaining
func-var packages is tracked in `seam-interface-di-migration`.
