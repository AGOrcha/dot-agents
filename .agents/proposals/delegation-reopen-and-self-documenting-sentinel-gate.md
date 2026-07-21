# Delegation reopen + self-documenting iteration-close gate

**Status:** proposed (design) — 2026-07-15
**Trigger:** a full main-session tool deadlock during package-artifact-install. The
`iteration-close-gate` hook hard-blocked every tool for ~20 cycles; only a user `!`-shell
command broke it. Two distinct root defects, plus an opacity problem, surfaced. See lesson
`.agents/lessons/closeout-before-clearing-iteration-sentinel/`.

## What actually happened (root-cause chain)

1. t3b round-1 worker ran `/iteration-close` **from the main checkout** (its worktree `.agents`
   was stale) → wrote sentinel `S1` (run `…5847Z`), `expected_artifacts = [iter-70.yaml,
   merge-back/t3b.md]`. Its merge-back landed; delegation auto-flipped to `completed`.
2. Orchestrator ran `delegation closeout` → **archived** the merge-back to history.
   **DEFECT A:** closeout did **not** clear `S1`. `S1` now references an archived (from the
   gate's view, *missing*) artifact.
3. Orchestrator needed a round-2 fix → resumed the worker. Round-2 `/iteration-close` wrote a
   **new** sentinel `S2` (run `…0418Z`) but its `merge-back` CLI call was **rejected** — the
   delegation was already `completed`, so a second merge-back is refused.
   **DEFECT B:** the round-2 resume produced a **born-invalid** sentinel — it expects a
   merge-back it can never create.
4. Both `S1` and `S2` point at a merge-back that is archived / never-produced → the Stop +
   PreToolUse gate blocks **all** tools. The session cannot fix its own filesystem (every tool
   is gated); only an out-of-band user `!` command or settings change recovers it.
   **DEFECT C (opacity):** the gate's remediation text only said *"Missing: <path>; re-run the
   verify-record/checkpoint/merge-back step"* — which is wrong here (re-running merge-back is
   *rejected*). It never told the user the real fix: clear the stale sentinel.

## Fix 1 — Delegation round-2 lifecycle (no invalid sentinel)

Goal: an orchestrator can request a follow-up round on an already-closed-out delegation
**without** stranding an unsatisfiable sentinel.

1. **New `da workflow delegation reopen --plan <P> --task <T> [--reason <text>]`.**
   Flips a `completed` delegation back to `in_progress`, marks the prior merge-back as
   *superseded* (kept in history, no longer the live expectation), and stamps a fresh round
   counter. After reopen, a resumed worker's `/iteration-close` merge-back **records cleanly**
   instead of being rejected — so its sentinel is satisfiable.
2. **`delegation closeout` clears the iteration-close sentinel(s) for the run(s) it closes** —
   atomically with archiving the merge-back (archive artifact + `hook-sentinel clear` in one
   step). Permanently closes DEFECT A. A closeout can no longer leave a sentinel pointing at an
   artifact it just moved.
3. **`merge-back` on a `completed` delegation fails with an actionable error** that names
   `delegation reopen` — instead of a bare rejection that leaves the worker's just-written
   sentinel dangling. (Ideally the sentinel write is deferred until the merge-back is accepted,
   so a rejected merge-back writes no sentinel at all — closes DEFECT B at the source.)
4. **Orchestrator guidance (loop-discipline spec):** prefer `reopen` over
   *closeout-then-resume*; if a round-2 is anticipated (e.g. review pending), **hold closeout**
   until the round lands. Cross-reference the worktree-isolation lesson: a worker whose
   `/iteration-close` runs from main writes its sentinel to main, so main-checkout runs are the
   ones that need this discipline.

## Fix 2 — Self-documenting gate (not opaque)

The gate must tell the user *how to resolve*, case-aware, with the exact command.

`iteration-close-gate/gate.sh` stop/subagent_stop remediation should, per missing artifact:

- **Case: already closed out** — delegation for `<task>` is `completed` **and** the missing
  artifact exists under `.agents/history/.../delegate-merge-back-archive/`. Print:
  > This iteration-close run is already closed out; its sentinel is stale.
  > Fix: `da workflow hook-sentinel clear iteration-close --run-id <RUN_ID>`
- **Case: genuinely incomplete** — artifact absent everywhere. Print:
  > Produce the missing artifact — re-run `/iteration-close`
  > (`da workflow verify record` → `checkpoint` → `merge-back`).
  > If this run was abandoned: `da workflow hook-sentinel clear iteration-close --run-id <RUN_ID>`
- **Always** include the literal `--run-id <RUN_ID>` and the artifact path. Never emit a
  remediation whose named step (`merge-back`) will itself be *rejected* — detect the
  `completed`-delegation case and route to `reopen`/`clear` instead.
- Optional: `da workflow hook-sentinel doctor` — diagnoses every active iteration-close
  sentinel (satisfied / stale-closed-out / genuinely-incomplete) and prints the one-line fix for
  each. The gate can point users at `doctor` as a single entry point.

## Done criteria

- `delegation reopen` exists + tested; a reopened delegation accepts a fresh merge-back.
- `delegation closeout` clears the run's iteration-close sentinel(s) (test: closeout leaves no
  active sentinel for the closed run).
- `merge-back` on a completed delegation errors actionably (names `reopen`) and writes **no**
  dangling sentinel.
- `gate.sh` emits case-aware remediation with the exact `--run-id` command; a stale-closed-out
  sentinel's message names `hook-sentinel clear`, not `merge-back`.
- Manual repro of the original deadlock is recoverable by following the gate's own printed
  command (no external knowledge needed).

## Routing

Product work in **this repo** (`commands/workflow/delegation.go`, the shipped global hook
`internal/scaffold/.../iteration-close-gate/gate.sh` + `~/.agents/hooks/global/...`, and the
`loop-discipline-stop-hooks` spec). Graduates to `workflow/specs/loop-discipline-stop-hooks/`
(amendment) + a small canonical plan. Not gating for 0.5.0's package-artifact-install core, but
Fix 2's gate message is cheap and high-value — land it early.
