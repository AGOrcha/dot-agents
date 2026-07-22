# Lesson: a "superseded" PR may still carry lost content — merge-check per file before closing

## Pattern

When consolidating releases, a stale/behind PR is easy to dismiss as "superseded"
on a coarse signal (e.g. "536 commits behind master; its features shipped via other
PRs"). That signal is necessary but **not sufficient**: a diverged branch can hold
*accurate content that never reached master* — explanatory callouts, contract detail,
per-command rows, config sections — written for the release and then lost when the
underlying feature shipped through a different PR.

Closing such a PR without a per-file check silently drops that content.

## Root cause (live example, 0.5.0 cut)

`#332` (`release/0.5.0`) was closed as superseded because it was far behind master and
its features had shipped elsewhere. That was true for its *feature code* but wrong for
its *docs*: it still carried unique, accurate, unshipped content — the `EVAL_HARNESS`
global-KG-store callout, the `LAYERED_CONFIG_GUIDE` Locks/authority-scopes section,
`GLOBAL_FLAG_CONTRACT` augment-vs-shadow detail, the `HOOKS` `when/when_events` rule.
The user had to redirect: "check every single doc in #332 for content to merge."

Sibling correction the same session: `release-docs-refresh` ran as fact-drift
*detection* and left stale *framing* (`new`/`(shipped)`/`(not yet shipped)`/planned)
untouched — fixed by adding the framing-drift lens + a checklist gate to the skill.

## Rule

**A "supersede-close" requires a per-file merge-check, not a behind-by-N judgment.**

For each file the closing branch touched:
1. `git diff origin/master:<file> origin/<branch>:<file>` — see what the branch has
   that master lacks (its `+` side).
2. **Verify each unique bit against today's code** (`file:line`). Fold the ones that
   are accurate AND missing; drop the stale/superseded ones and unverifiable roadmap
   promises. Files the branch touched that are now *deleted* on master (intentional
   consolidation) need nothing.
3. Only then close the branch, and say in the close comment what was folded vs dropped.

## How to apply

- Never let "stale branch" collapse into "worthless branch." Stale *code* ≠ stale *docs*.
- The fold target is the live doc on master (or the consolidated release PR), grounded
  in current code — not the branch's own stale surrounding prose.
- Pairs with the release-docs-refresh framing-drift gate: consolidating a doc to
  shipped reality and folding lost content are the same deep, code-grounded pass.
