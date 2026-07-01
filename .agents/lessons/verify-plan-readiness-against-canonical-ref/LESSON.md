---
name: verify-plan-readiness-against-canonical-ref
description: '"Are the plans/tasks ready to implement?" must be answered against the CANONICAL ref (origin/master specs + the latest merged coherence/redesign PRs), and task NOTES must be diffed against the spec they claim to implement — a merged spec redesign silently invalidates task notes, and a wave built on stale notes fails the cross-brain gate wholesale.'
type: lesson
---

# Verify plan readiness against the canonical ref (specs move; task notes do not)

## Pattern observed

PR #162 (`workflow/r-series-coherence`, MERGED to origin/master)
redesigned the R-series communication architecture: the r3 spec gained
**§2A "Transport & protocol (single source for the R-series)"** (not
HTTP-by-default; a Surface→transport map; local control plane = Unix-
domain socket with peer-credential checks, named pipe on Windows) and
**§D4** was reshaped around an **`EventBus` interface** transport seam
(config-selected builtin-vs-external backend) instead of the concrete
`*events.Bus`.

The TASKS.yaml task notes were **never reconciled**. They still said
"net/http (no framework dep), Server{addr, mux, scheduler, bus}"
(r3 `http-server`), "POST /admin/stop gated on loopback"
(`cobra-surface` — plus a "no pidfile" anti-scope that contradicted the
spec's D1 `-d/--detach` ruling), and cross-plan contracts pinned to the
concrete bus signature (r2 `t04-sse-broker`, r5 `collection-endpoint`'s
chi-router sketch).

A full implementation wave was fanned out against those stale notes.
Every worker implemented faithfully — against a design that a merged PR
had already superseded. The cross-brain review gate compared the output
against the canonical spec and **rejected the wave wholesale**.

## Root cause

1. **Readiness was assessed from a stale local checkout**, not the
   canonical ref. The spec redesign was merged on origin/master; the
   checkout (and the reader's mental model) predated it. "The tasks
   look implementable" was true — for a dead design.
2. **A merged spec redesign invalidates task notes silently.** Nothing
   in the workflow flags "this spec section is newer than the task
   notes that claim to implement it." Task notes are snapshots frozen
   at authoring time; the spec is the moving contract. (Sibling
   failure mode of [[single-source-of-truth-across-specs-and-plans]] —
   there the copies drift between sibling docs; here the drift is
   spec-vs-task-notes across time.)

## Rule

Before answering "are the plans/tasks ready to implement?" — and
before activating any plan wave:

1. **`git fetch` and read the spec on the canonical ref**
   (origin/master), including any coherence/redesign PRs merged since
   the plan was authored. Never assess readiness from a local checkout
   without confirming it matches the canonical ref.
2. **Diff the task notes against the spec sections they claim to
   implement.** Cheap mechanical check: compare last-modified commits —
   `git log -1 --format=%h -- <spec design.md>` vs
   `git log -1 --format=%h -- <plan TASKS.yaml>`. If the spec is newer,
   grep it for sections added/changed since the notes were written
   (§-numbered additions, "RESOLVED" open questions, ruling language)
   and reconcile the notes FIRST.
3. **Reconcile before fanout, not after review.** A `RESCOPED
   <date> (<section> coherence)` prefix on the corrected notes keeps the
   original context and makes the supersession auditable.

## Why

Workers implement what the task notes say — that is the whole point of
bounded delegation. If the notes describe a superseded design, worker
fidelity *guarantees* wasted waves: the better the workers follow the
bundle, the more precisely they build the wrong thing. The only cheap
interception point is the readiness check, and it is only valid against
the canonical ref.

## How to apply

- Trigger phrases: "ready to implement?", "activate the plan",
  "launch the wave", "fan out". Each one means: run the
  spec-vs-task-notes freshness check (rule 2) first.
- When a coherence/redesign PR merges and touches a spec, treat every
  plan whose tasks reference that spec as **suspect until reconciled**
  — reconciling task notes is part of landing the redesign, not a
  follow-up nicety (the #162 reconciliation was done as a separate
  later pass, and the gap is exactly where the wave fell through).
- When reconciling, `--notes` on `da workflow task update` REPLACES the
  whole field: read the existing notes, keep the still-valid context,
  prefix the dated rescope, and correct only what the spec changed.
