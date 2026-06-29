# Verified Readback (recover mode)

When a session resumes after a compaction, a crash, or a force-kill, **do not trust a
prose handoff as fact.** The previous session's summary tells you the *intent*, but its
*live state* (what merged, what branch you are on, which PR is open) may already be stale —
acting on a stale claim is how a session re-does work that already shipped.

Instead, read state back through the **session-handoff journal**, which re-verifies every
reconstructed claim against current reality before presenting it.

## Step 1 — Run the verified recovery view

```
da workflow journal recover
```

(Add `--json` if you want to parse it programmatically.) This loads the deterministic
live-state snapshot plus the replayed event log, then runs a cheap re-verify probe for each
item against reality and labels what it finds. It does **not** hand you remembered prose — it
hands you facts graded by how well they survived re-verification.

If it reports `snapshot: (none — replay-only, degraded)`, there was no snapshot to anchor on;
treat everything as lower-confidence and re-ground manually before acting.

## Step 2 — Read the bundle-level signals first

- **`freshness: fresh | stale | orphaned`** — how recently the session wrote, relative to the
  snapshot. `fresh` → trust the bundle. `stale` → treat its claims as hypotheses. `orphaned` →
  the bundle is suspect; verify everything yourself.
- **`QUARANTINED: <reason>`** — the **whole bundle must not be auto-resumed** (wrong repo /
  branch / HEAD identity, or an orphaned overlay). Do **not** pick up its work. Treat it only
  as a stale reference, and if several candidates exist, prefer the newest verified one.

## Step 3 — Act on each item by its verification tag

Items are grouped under four tags. Each line reads
`- plan/task [trust=high|medium|low via <source>] <locus>` with an optional `delta:`.

- **`verified`** — reality matches the reconstructed claim. **Trust it as fact** and build on it.
- **`changed`** — reality differs from the claim. **Read the `delta:` line and use reality, not
  the journal's claim.** Example: "snapshot said PR #12 open; GitHub says merged." Never treat a
  `changed` item as still-in-the-state-the-journal-remembered.
- **`missing`** — the journaled thing no longer exists in reality. Treat it as **not present** —
  do not assume it is done, and do not try to resume it without re-creating it.
- **`unverified`** — no source could confirm or deny it (e.g. the remote was unreachable and the
  local tree was locked). Treat it as an **unconfirmed hypothesis**: re-verify it yourself before
  you rely on it, and **never treat it as done.**

Always read the most-trustworthy tags first (`verified`), then `changed`, then `missing`, then
`unverified` — which is the order `recover` prints them in.

## Step 4 — Respect the trust gradient

Each item carries `trust=high|medium|low`:

- **`high`** — re-verified against an authoritative store/service (the remote/API via `gh`),
  which is reliable even when the local working tree is locked or missing.
- **`medium`** — confirmed only against a local fallback (`git` / on-disk files), which is
  unavailable when the checkout is locked.
- **`low`** — no source could re-verify it.

Prefer `high`-trust facts. A `verified` item at `trust=low` is weaker than its tag suggests —
treat it as provisional.

## Step 5 — Canonical vs in-PR locus

A task can be "done in an open PR but not yet merged." `recover` prints each item's locus so you
do not conflate the two:

- **`canonical <ref>`** — landed on the default branch at that ref. Done. Build on it.
- **`in_open_pr #N`** — reconciled inside an open PR, **not merged**. Do **not** re-start it, and
  do **not** treat it as merged — track the PR.
- **`canonical (sha unconfirmed)` / `in_open_pr (PR unconfirmed)`** — the coordinate could not be
  authoritatively confirmed. **Do not cite the sha or PR number as fact**; verify before relying.

## Step 6 — Quarantined conflicts

A `quarantined conflicts (N):` block lists items whose identity did not match this session
(e.g. the same task id reconstructed under a different plan). These are surfaced as **stale
references only** — they are *not* actionable state for this session. Do not resume them.

## After reading

Fold the verified facts into your working context, then proceed. If you also need the *why*
behind the state, read the reasoned deltas via `da workflow journal show` (most-recent events
first). For raw debugging of the event stream, `da workflow journal show --all`.
