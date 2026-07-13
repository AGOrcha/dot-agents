---
name: stale-plan-status-vs-reality
description: A plan/spec's status field and prose are a claim, not ground truth — reconcile against the actual repo/git state before treating a plan as authoritative for fanout, resume, or analysis.
type: lesson
---

# Stale plan status vs. reality

## Pattern observed

A canonical plan file's `status`/`updated_at` fields and its narrative can lag
the actual state of the work by a wide margin — in either direction. A board
that says `draft` can already be mostly implemented and merged; a board that
still names a component can be describing something renamed or archived out
from under it. An agent that trusts the file over the repo either re-does
already-shipped work, cites dead context as live, or misjudges readiness.

Four concrete, independently verified instances (same review pass, two repos):

1. **`dot-agents/da-recipe-scripts`** — `PLAN.yaml` sat at `status: draft`
   while `da run <file>`, fail-fast, env-var substitution, the cross-platform
   acceptance test, and the checkpoint-advance dogfood recipe were all already
   implemented, tested, and documented. Commit `c421eda4` had to actively
   reconcile the plan/spec to shipped reality ("the plan/spec were just left
   at stale draft status") — a full 5-task arc landed before the board caught
   up.
2. **`payout/endpoint-identity-order-sessions`** — `PLAN.yaml` reads
   `status: draft`, but `TASKS.yaml` already shows 9 of 17 tasks
   `status: completed`. A reader who stops at the plan-level status field
   would misjudge this as unstarted design work.
3. **`payout/oci-secure-node-provisioning`** — same shape: `PLAN.yaml` reads
   `status: draft`, but `TASKS.yaml` shows 4 of 6 tasks `status: completed`
   (provisioning + hardening already applied).
4. **`payout/luma-prototype-sprint`** — archived cleanly to `.agents/history/`,
   yet still cited as live context from two still-active/draft plans
   (`payout-nats-leaf-realtime`, `payout-personalization-recsys`). The
   archival itself is correct; the *referencing* plans didn't update their
   own citations after the referent moved.

## Root cause

A plan/spec file is written once and then updated only when someone
remembers to groom it — but the underlying work (tasks completed, PRs
merged, files renamed/moved) advances independently via normal git activity
that never writes back to the plan file automatically. There is no
change-detection between "plan says X" and "repo now does Y"; the two only
reconcile when a human or agent explicitly runs a grooming pass (like
`c421eda4` above). Board-level `status` fields are especially prone to this
because they're a single coarse field summarizing dozens of finer-grained
`TASKS.yaml` entries or commits — it's easy to flip five tasks to `completed`
and forget the one line that says `status: draft` at the top.

## Rule

A plan/spec file's `status`, `updated_at`, and prose are a **claim**, not
ground truth. Before treating any plan as authoritative — for fanout, resume,
or citing it in analysis — do all three:

1. **Diff stated artifacts/commands/paths against the actual repo state or
   shipped release.** Grep for the plan-named file/command/package and
   confirm it still exists (or still has the name the plan uses).
2. **Check `TASKS.yaml` task-level statuses against the `PLAN.yaml` header
   status**, and check `git log --grep=<plan-id>` for post-dated
   reconciliation commits the plan file itself doesn't reflect. A plan
   showing `draft` with most tasks `completed` is the tell.
3. **Flag and separately report drift** rather than silently repeating the
   stale claim — don't quietly "correct" the board yourself unless grooming
   the plan is in scope; surface it.

## How to apply

- On plan pickup (orchestrator/loop start), before selecting or fanning out
  a task: read `PLAN.yaml` status **and** scan `TASKS.yaml` statuses; if they
  disagree, treat the more-advanced state (task-level `completed`/PR-merged
  evidence) as closer to truth and groom the header status rather than
  re-dispatching already-done work.
- Before citing a sibling plan as live context (e.g. "see `<plan-id>` for
  background"), confirm it's still under `.agents/workflow/plans/` and not
  moved to `.agents/history/`; if archived, cite the history path or drop
  the reference.
- Before building against a plan-stated interface name (a CLI subcommand, a
  package, a file path), grep the current source for that name — a plan can
  describe a since-renamed shipped feature (see
  [[stale-dev-binary-vs-shipped-feature]] for the binary-probe angle of the
  same drift).

## Cross-references

- `[[stale-dev-binary-vs-shipped-feature]]` — the binary/CLI-probe sibling:
  verifying a shipped feature exists against the built artifact, not just the
  plan that describes it.
- `[[single-source-of-truth-across-specs-and-plans]]` — the content-drift
  sibling (two docs disagreeing with each other); this lesson is the
  temporal-drift sibling (one doc disagreeing with the repo it describes).
- `[[verify-plan-readiness-against-canonical-ref]]` — verifying readiness
  against the canonical spec ref before fanout; this lesson generalizes that
  check to any plan-vs-reality read, not just spec-readiness gating.
- payout `[[keep-plans-in-sync-with-worktree]]` — the original, narrower
  instance of this pattern (a migration plan describing shims that had
  already been deleted); this lesson generalizes it beyond worktree/shim
  existence to status fields and cross-plan references.
