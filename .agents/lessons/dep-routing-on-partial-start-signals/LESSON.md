# Dep-routing on partial "start X" signals

**Captured:** 2026-05-28
**Triggered by:** Forced fanout of `config-v2-migration/p4-config-explain-cli` (PR #162) ahead of dep p1 (in_progress, no PR yet). Maintainer corrected: "the system and dependencies should've routed to the config-v2 p1 task to ensure it's at least in awaiting_owner_review / other deps are satisfied not forcefully started."

## The trap

When the user issues a partial-sentence directive like:

> "for release 0.4.0, since config v2 is started can start config-explain-live-surface as well"

…or similar shape ("start X since Y is in flight" / "spin up Z while we wait on W"), it is tempting to read it as: *force-spawn X immediately into master*. **That is wrong.**

The correct read is:

> "Make X **eligible**. Let the workflow dep-routing layer determine when it's actually safe to start, gated by Y reaching `awaiting_owner_review` (or whatever satisfaction criterion the plan declares)."

The user's signals are intent, not override. They presume the dep-routing layer is doing its job. Honoring intent means honoring the layer.

## Why this matters

Forcing a dep-violating fanout costs:

1. **Rebase cost** — the dependent PR (X) is built against speculation; when the predecessor (Y) lands and changes the surface X assumed, X needs rework. PR #162's `commands/config/explain.go` may need rebase once p1's `resolver-core-flat` stabilizes the resolver API.
2. **Review-pipeline noise** — X enters review state with an implicit-but-undocumented dep; reviewers don't have Y's shape to evaluate against, so review-quality drops.
3. **`[[verify-task-status-vs-pr-history]]` failure mode** — task status diverges from PR-history truth; `delegation closeout --decision accept` may auto-advance a task that should have been blocked.
4. **Layered-PR-fanout violation** — the spec at `.agents/workflow/specs/layered-pr-fanout/design.md` §3.4 introduces `blocked-on:<ref>` as a first-class state precisely so we don't force-spawn. Bypassing it ad-hoc undermines the spec we're authoring.

## The rule

When user says "start X (since/because/while Y …)" and Y is not yet at `awaiting_owner_review`:

1. **Do NOT immediately `da workflow fanout --task X`.**
2. **Check Y's status:** `da workflow eligible` OR `gh pr list --search Y` OR read `TASKS.yaml` for Y's `status:` field.
3. **If Y is `in_progress` with no PR yet:** queue X as `blocked-on:Y` (per layered-pr-fanout §3.4). Do NOT fanout. Surface this to the user: "Queued X as blocked-on:Y. Will fanout when Y reaches awaiting_owner_review (or you tell me to force it)."
4. **If Y is `awaiting_owner_review` (PR open, CI green, awaiting maintainer):** then X is safe to fanout (worst-case rebase is bounded by maintainer review window).
5. **If user explicitly says "force-start X anyway"** (knowing the dep cost): fanout, and file a follow-up note that X may need rebase if Y's surface changes.

## When to apply

Any time a user message contains one of these shapes:

- "start X **since** Y is/has …"
- "start X **while** Y is in flight"
- "spin up X **as well** [implicit: alongside Y]"
- "kick off X **now that** Y …"
- "X **can also** go since Y is …"

The connective ("since," "while," "as well," "now that," "can also") is the signal. It presumes a dep relationship. Honor it.

## Counter-pattern: when force-spawn IS correct

Some directives genuinely mean "now":

- "**fanout** X" — explicit verb, force-spawn is fine
- "X **doesn't depend on** Y, start it" — user has explicitly cleared the dep
- "I'm not going to wait on Y, just **do** X" — user has explicitly overridden the dep
- "X has **no deps**, start" — user has explicitly asserted no-deps

Absence of a connective + presence of an action verb = force-spawn signal.

## What I should do at fanout time

For every fanout call, **before** invoking `da workflow fanout`:

1. Read the task's `depends_on:` array from `TASKS.yaml`.
2. For each dep, verify its current status: `in_progress` → block, `awaiting_owner_review` → proceed, `completed` → proceed.
3. If any dep blocks, **do not fanout**. Surface to user with the exact blocking dep + a one-line plan for when to retry.
4. If user has already explicitly overridden (per the counter-pattern above), proceed but log the override in the bundle's `selection-reason`.

## Cross-references

- `[[verify-task-status-vs-pr-history]]` — pre-fanout cross-check ritual
- `[[validate-bundle-against-head]]` — bundle write_scope vs HEAD
- `[[parallel-worker-branch-drift]]` — partial-coherence trap
- `.agents/workflow/specs/layered-pr-fanout/design.md` §3.4 — `blocked-on:<ref>` parameterized state (the right mechanism for waiting on a dep)
- `[[worker-owns-pr-readiness-loop]]` — terminal-state discipline for workers
