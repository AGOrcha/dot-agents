---
name: stale-dev-binary-vs-shipped-feature
description: A locally-built dev binary (/tmp/dot-agents-dev) or a plan's stated command name can lag a shipped rename — verify a feature's existence against the shipped binary's --help/source, not just an older build or an unreconciled plan file.
type: lesson
---

# Stale dev binary vs. shipped feature

## Pattern observed

A plan/spec can lag a shipped rename, and a locally-built dev binary can lag
the source it was built from — both lead to the same wrong conclusion:
"this feature doesn't exist" when it does, just under a different name or a
newer commit than what's on disk/in the binary.

Concrete, same-day instance: the `da-recipe-scripts` plan (`PLAN.yaml`,
`status: draft` until `updated_at: 2026-07-10T04:08:36Z`) described the
feature under the working name `da recipe`. The shipped feature — release
`0.5.0` (`aaeadc17`/`0d23c3bb`, "release: 0.5.0 — da eval pipeline, da run
recipes, da review admin, da workflow journal") — actually landed as `da run`
(`commands/run.go`, registered in `commands/root.go`). Anyone who (a) grepped
an older binary's `--help` output for "recipe" and concluded the feature was
unbuilt, or (b) trusted the plan's stated command name and started building a
new `da recipe` subcommand, would have wasted a cycle: the capability already
shipped, just renamed at merge time and left undocumented in the plan.

The git history shows the exact recovery sequence this pattern forces: two
`snapshot: preserve N-file uncommitted WIP from feat/da-recipe-p4-acceptance
(stalled/dead worker)` commits, then `c421eda4 workflow(da-recipe-scripts):
groom plan to shipped reality; dogfood plan-creation recipe` — the plan had
to be actively reconciled against what the release notes and binary already
shipped, under the renamed command.

## Root cause

Two independent staleness sources compound:

1. **A dev binary is a point-in-time snapshot.** `/tmp/dot-agents-dev` (or
   any locally built binary) reflects the source tree at build time, not
   HEAD. A feature merged after the last build is invisible to `--help` or
   command-not-found probes against that binary, even though the source
   already has it.
2. **A plan's stated interface name can be renamed at merge/ship time**
   without the plan being groomed to match (see
   [[stale-plan-status-vs-reality]] for the general status-drift pattern).
   Trusting the plan's vocabulary over the shipped CLI's actual vocabulary
   leads to searching for the wrong command name.

## Rule

Before either (a) building against a plan-stated interface that may have
been renamed, or (b) declaring a plan stale/incomplete/unbuilt because a
probe against a binary or old docs came up empty — verify the feature's
existence against the **shipped binary's `--help` output or the release
CHANGELOG**, not just the plan file or a possibly-stale local build.

## How to apply

1. **Rebuild before concluding "not implemented."** If a dev binary's
   `--help`/subcommand probe doesn't show an expected feature, rebuild from
   current HEAD (`go -C <repo> build -o /tmp/dot-agents-dev ./cmd/dot-agents`)
   before trusting the negative result — the binary may simply predate the
   merge.
2. **Grep the source, not just the binary.** `grep -rn "<feature-keyword>"
   commands/ root.go` finds a renamed command even when the binary is stale
   or the plan uses old vocabulary.
3. **Check the release CHANGELOG / recent `release:` commits** (`git log
   --oneline --grep="release:"`) for a feature that shipped under a name the
   plan doesn't use — release notes are the most current single source for
   "what's actually out."
4. **When a plan's stated command name comes up empty, search
   `git log --grep=<plan-id>` for a grooming/rename commit** before assuming
   the work was never done — the plan may simply not have been updated to
   the shipped name.

## Cross-references

- `[[stale-plan-status-vs-reality]]` — the general plan-vs-repo drift
  pattern this lesson specializes to the binary/CLI-probe angle.
- `[[verify-plan-readiness-against-canonical-ref]]` — verifying readiness
  against canonical specs before fanout; this lesson adds "verify against
  the shipped binary/CHANGELOG" as a concrete readiness check.
