---
scope: "Step 0 — establish KG-context awareness before per-file review"
---

# KG-Context (Step 0)

This is the first step of self-review. It runs **before** any per-file
review. The goal is to surface global structural change-set and blast
radius so the per-file review is informed by graph context, not done
in the blind — per-file review starts with global blast-radius, not in
the blind.

## When to run

Run before Step 1 (Code Quality), unconditionally, on every self-review
invocation — both standalone and when fired from iteration-close.

## Procedure

### A. List the staged + unstaged changed files

```
git diff --cached --name-only
git diff --name-only
```

Combine the two lists, dedupe, and treat the union as the "changed set"
for the rest of Step 0. Skip pure deletions (diff lines that are all
removed) — they have no surface to inspect.

### B. Run `da kg changes --brief`

```
da kg changes --brief
```

This surfaces the structural change set the KG already knows about
(which files / nodes / edges moved). Capture the full stdout
**verbatim** — do not summarize, paraphrase, or filter. The reviewer
narrative captures what the reviewer saw, so the audit trail
preserves the raw output.

If the command exits non-zero, treat it as a graceful-degradation
signal (see "Graceful degradation" below) — do **not** abort the
self-review.

### C. Run `da kg impact <changed_files>`

```
da kg impact <file1> <file2> ...
```

Pass the changed-file list from step A as positional arguments. This
surfaces the blast radius — which downstream nodes the changed files
reach, so review can flag broken assumptions in dependent code.

If the changed-file list is empty (no diff), skip this call — there
is nothing to compute impact for.

If the command exits non-zero, treat it as a graceful-degradation
signal — same rule as `kg changes`.

### D. Capture into reviewer_notes

The combined Step 0 output (both commands' stdout, headed by a
"## KG Context (Step 0)" marker) is captured **verbatim** into the
`reviewer_notes` field of the eventual `review-decision.yaml` (see
`output-format.md`). This is what makes the review reproducible:
anyone re-reading the artifact can see the exact graph snapshot the
reviewer worked from.

The narrative passed to `reviewer_notes` looks like:

```
## KG Context (Step 0)

### kg changes --brief
<verbatim stdout from `da kg changes --brief`>

### kg impact <files...>
<verbatim stdout from `da kg impact ...`>
```

If either call degraded gracefully, replace its block with the warning
text (see below) so the absence of context is explicit in the record.

## Graceful degradation

The KG/CRG bridge is not always available. Common failure modes:

- `KG_HOME` unset or directory missing.
- The graph hasn't been built (`da kg build` never ran).
- The bridge venv is missing (clean checkout, fresh worktree).
- The graph is locked by a concurrent process.
- `da kg` itself is missing from PATH (running outside a `da`-managed project).

When any of those happen, **do not abort self-review**. Instead:

1. Skip the failed Step 0 sub-call (kg changes and/or kg impact).
2. Emit a single-line warning to the chat narrative:
   `WARN: KG bridge unavailable for Step 0; per-file review will run without blast-radius context.`
3. Capture the warning into `reviewer_notes` in place of the missing
   command's output. The warning text becomes the audit trail.
4. Proceed to Step 1.

Do **not** invoke `da kg setup` or `da kg build` from inside Step 0 —
that's outside the skill's scope and would mutate state the user
hasn't asked for. The skill remains useful even without KG, just
with reduced context.

## Why verbatim capture matters

`da kg changes --brief` and `da kg impact` output formats may drift
over time. This module deliberately treats them as **opaque text** —
no structured parse, no field extraction. The reviewer reads the
output and forms judgment; the audit trail preserves the raw output
for later review.

If a future change to `kg changes` output format breaks the reviewer's
ability to parse it visually, that's a separate signal — log it as an
`improvement_signals.tooling_gap` entry in the final review decision
(see `output-format.md`).

## Hand-off to per-file review

After Step 0 completes (success or graceful-degradation), proceed to
Step 1 (Code Quality) with the change set known. Step 0's output stays
in working memory so subsequent steps can reference blast-radius
findings (e.g., "this change touches a module with 14 downstream
callers per `kg impact`; verify the reader contract hasn't changed").
