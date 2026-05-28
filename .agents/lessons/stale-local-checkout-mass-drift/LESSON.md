# Stale local checkout → mass working-tree drift

**Captured:** 2026-05-28
**Sibling:** `[[stale-local-master-ref]]` (the read-side version — audits reading the stale ref). This is the write-side / workspace version.

## The trap

In a long parallel-PR session, the orchestrator's **local checkout silently falls far behind `origin/master`** (here: 95 commits behind, 0 ahead). All the real work happens on worker branches cut from fresh `origin/master`, so the PRs are fine — but the orchestrator's own working tree, and every `da workflow` command that reads local plan files, operate on a 95-commits-stale base.

Symptoms that accumulate and mislead:

- `da workflow eligible` surfaces tasks whose PRs already merged (it reads stale local TASKS.yaml).
- Plans/specs look "un-updated" or "untracked" (e.g. config-v2-migration appeared untracked because the commit that added it was in the 95 unpulled commits).
- The working tree shows a large pile of uncommitted **deletions** of files that still exist on `origin/master` (stale local drift, not real archive actions) plus **modified** plan yamls and **untracked** session artifacts — a confusing mixed dirty state.
- Coherence audits must be told to read `origin/master`, not local (per `[[stale-local-master-ref]]`).

## Why it happens

The orchestrator rarely `git pull`s its own checkout during a session — it merges PRs on GitHub (origin advances) and spawns workers (who fetch origin themselves). The local `master` ref is only updated by an explicit pull, which never happens. Meanwhile local-side `da` commands and ad-hoc file writes mutate the stale tree. Over a multi-hour session this compounds into large drift.

## The recovery (safe, reversible)

Per "should have put them in a branch first" — **never hard-reset a dirty tree without a backup branch.**

1. **Snapshot everything to a backup branch** (full safety net, never merged):
   ```bash
   git checkout -b backup/local-dirty-pre-sync-<date>
   git add -A && git commit -m "snapshot: full local dirty state before origin sync"
   ```
2. **Identify genuinely-local-only authored artifacts** (things you created this session that were never committed/pushed) vs spurious drift. For each candidate:
   ```bash
   git cat-file -e origin/master:<path> 2>/dev/null && echo ON-ORIGIN || echo RECOVER
   ```
   The RECOVER set is what must survive the sync.
3. **Hard-sync local to origin** (clean because backup holds everything):
   ```bash
   git checkout master && git reset --hard origin/master
   ```
4. **Re-apply the RECOVER set** onto a fresh branch off the synced master:
   ```bash
   git checkout -b feature/<recovery> origin/master
   git checkout backup/local-dirty-pre-sync-<date> -- <recover-paths...>
   ```
   → PR it. Do NOT cherry-pick the spurious deletions or the stale plan-yaml modifications (those were drift against the old base; the real plan state is on origin).

## Prevention

- **Pull local `master` after each batch of merges** (or before any local `da workflow` read). A stale `eligible`/`orient` is worse than useless — it actively misleads task selection.
- Treat local `da workflow eligible` output as suspect if you've merged PRs since the last pull; cross-check against `gh pr list --state merged` (per `[[verify-task-status-vs-pr-history]]`).
- The genuinely-canonical workflow state lives on `origin/master`. Local is a working copy that drifts; sync it deliberately.

## Cross-references

- `[[stale-local-master-ref]]` — the read-side sibling (audits against stale ref → 3-way cascade)
- `[[verify-task-status-vs-pr-history]]` — `eligible`/TASKS.yaml lie after parallel waves
- `[[validate-bundle-against-head]]` — HEAD-validate write_scope before fanout
- `[[seam-loop-lock-2026-05-19]]` — the FDA-lock recovery patterns that contributed to this drift (closeouts/archives interrupted, never committed)
