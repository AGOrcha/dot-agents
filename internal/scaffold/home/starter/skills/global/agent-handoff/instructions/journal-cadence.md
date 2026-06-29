# Journal Cadence (capturing state so it survives)

The verified readback (`instructions/verified-readback.md`) only works if state was actually
written to the **session-handoff journal** before the session compacted or died. This file is
the *write* side: when to capture, and what the agent is responsible for.

## Two layers, two writers

1. **Deterministic layer — automatic, no action needed.** Every state-mutating `da` command
   (`workflow advance`, `start-task`, `close-task`, `commit`, `verify record`, `checkpoint`,
   `merge-back`, `fanout`, `fold-back`, `review approve/reject`, `kg ...`, …) **already appends a
   typed event** as a side effect of running it. You do **not** hand-journal these — just running
   the command records *what changed*. A `PreCompact` hook also auto-captures a live-state
   snapshot before compaction. This layer is crash-survivable on its own.

2. **Reasoned layer — your job.** The deterministic events capture *what changed* but not *why*.
   The reasoned layer is a short delta you append carrying only what a command cannot:
   - your current mental model / working hypothesis,
   - the in-flight decision and **why now**,
   - the next step,
   - pending user intent,
   - the active blocker.

   Append it with:

   ```
   da workflow journal append --command "<short-label>" --actor main \
     --input '{"why":"...","next":"..."}'
   ```

   Keep it short and append-only — the deterministic events already carry the state changes.

## When to capture (adaptive cadence)

Write a reasoned delta at these moments, in priority order — the first listed wins:

1. **Before a risky or irreversible operation** — `push`, `merge`, `--force`, anything
   destructive, a migration. Capture *first*, so a crash mid-operation still has fresh state and
   intent on disk. (A snapshot here is cheap insurance: `da workflow journal snapshot`.)
2. **Under context pressure** — when the window is filling and a compaction is likely. The
   `PreCompact` hook snapshots the deterministic layer for you, but **hooks cannot capture
   reasoning** — so you must append the *why* yourself before the window collapses.
3. **After a consequential event** — a decision made, a user instruction received, a PR opened, a
   task advanced. (`workflow advance` and `iteration-close` already emit the deterministic event;
   you add the reasoned *why*.)
4. **At task / iteration boundaries.**

**Backstop (work-OR-time):** if none of the above has fired, capture every ~5–8 consequential
tool-calls **or** ~10 minutes, whichever comes first.

**Hard ceiling:** never let more than **one** un-recorded decision or intent accumulate. If you
are about to make a second consequential decision without having journaled the first, journal now.

**Dirty-check guard (no-op):** skip the write if nothing material has changed since the last one.
A journal full of empty deltas is noise.

## Why moderate, not paranoid

Catastrophic mid-turn kills are rare, and cheap append-only deltas are what *let* you write often
without cost. Reserve full snapshots for boundaries and the `PreCompact` hook; between them, short
deltas are enough. The honest limit: reasoning formed between your last durable append and a
mid-turn kill is unrecoverable — only what is already on disk survives. The cadence *minimizes*
that window; it cannot eliminate it.

## Dependency / current limitation (flag to the user)

The **dedicated reasoned-overlay writer (`reasoned.log`)** — the one that enforces the
dirty-check no-op guard and the hard ceiling *mechanically* and writes to a separate overlay
stream — is a **separate, not-yet-built task**. Until it lands:

- Record reasoning via the low-level `da workflow journal append`, which writes into the **shared
  event log** (not a separate `reasoned.log`).
- The dirty-check guard and hard ceiling above are therefore **agent discipline**, not
  machine-enforced — follow them deliberately.
- `da workflow journal recover` currently uses the newest event's timestamp as its freshness
  proxy, so a freshly-journaled session is not spuriously quarantined.

When the reasoned-overlay writer ships, the `append` invocation above is the seam it replaces;
the cadence rules in this file stay the same.
