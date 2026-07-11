# Classify generated files before cleaning them up

## Pattern

Before deleting untracked files during a "sync clean to master" (`git clean`,
`reset --hard`), classify EACH untracked path by *what it is*, not just "not on
master → junk". There are at least three kinds, with opposite correct handling:

1. **Now-on-master duplicates** (a copy of something a just-merged PR persisted)
   → safe to delete; the tracked version reappears on checkout.
2. **Durable authored artifacts** in a category master *does* track (lessons,
   iteration-logs, fold-back deferrals, research, verification results) →
   **persist** them (commit), never delete — losing a lesson breaks the
   self-improvement loop.
3. **Live per-machine generated config/wiring** (hook manifests with absolute
   `$HOME` paths, lock files) → **neither delete nor archive**; `git clean`
   strips live wiring until the next `da refresh`. The right hygiene is
   **gitignore**, because absolute local paths can't be meaningfully tracked.

## Root cause of the miss

`.github/hooks/*.json` were first waved off as "local runtime junk, clean it."
Inspection showed they were **hook WIRING** (`version:1` manifests binding
`agentStop`/`preToolUse`/… → `~/.agents/hooks/global/<gate>/gate.sh`), all with
one mtime (a single `da refresh` pass), numbered suffixes = multiple hooks per
event, **no telemetry inside**. `git clean` would have disabled the local
loop-discipline gates until the next refresh. "Stale sentinel?" was the right
question — the answer was "no, live config," reached only by reading them.

## Rule

- Never `git clean`/`reset --hard` a dirty tree on the strength of "not tracked
  on master." First `git ls-files --others --exclude-standard`, then for each:
  check master-tracking of its *category dir* (`git ls-tree origin/master`),
  read a sample (config vs telemetry vs stale?), and check mtimes (one-shot
  generated vs accumulated).
- Distinguish **wiring** (regenerated, per-machine, absolute paths → gitignore)
  from **telemetry/history** (firing records, outcomes → keep/archive) from
  **stale sentinels** (old-session leftovers → archive if historical, else drop).
- Snapshot first (`git diff --binary > patch`, tar the untracked) so any
  misclassification is reversible.

## How to apply

Cleanup checklist before `clean`/`reset`:
1. `git status --short` + `git diff --binary HEAD > /tmp/dirty.patch`; tar untracked.
2. For each untracked path: `git ls-tree -r origin/master -- <dir>` (does master track this category?) + read a sample + `ls -lt` (mtime pattern).
3. Route: on-master-dup → delete; authored-in-tracked-category → commit; per-machine wiring/lock → gitignore; stale history → archive.
