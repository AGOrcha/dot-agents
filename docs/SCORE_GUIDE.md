---
title: Scoring Agent Runs
description: A task-oriented guide to da score — compute per-iteration and per-session outcome scores from the iteration log, query a persisted score, and read the explainable breakdown.
sidebar:
  order: 10
---

# Scoring agent runs

`da score` turns the telemetry an agent run already captures into an **explainable quality
score** — per iteration and per session — against a versioned rubric. It reads the iteration
log under `.agents/active/iteration-log/`, computes a sub-score for each input signal, combines
them, and writes the result back as sidecar files you can query later.

This guide is the task-oriented walkthrough: compute scores, query a persisted score, and read
the output. The authoritative definition of *how* a score is computed — the signals, weights,
and combination method — lives in the
[Outcome-Scoring Rubric](./OUTCOME_SCORING_RUBRIC.md); cite this guide for running the command.

> **Scope.** This guide covers the `da score` command surface. It does not redefine the rubric;
> the rubric document and `internal/scoring/rubric.go` are the authority on the scoring math.

---

## Overview

A score is computed from the **iteration log** — the per-iteration records under
`.agents/active/iteration-log/` (`iter-N.yaml`) plus their sidecars. `da score` has three
subcommands:

| Subcommand | Role |
|------------|------|
| `score run` | Score every iteration in the active log and **write** sidecars. |
| `score iteration <N>` | Render (or `--recompute`) one iteration's persisted score. |
| `score session <id>` | Render one session's persisted score. |

The lifecycle:

```
da score run              # compute + persist all iter / session sidecars
  → da score iteration N  # read back one iteration's breakdown
  → da score session ID   # read back one session's roll-up
```

Every persisted score records the **rubric version** it was computed under, so a later rubric
change never silently invalidates a historical score. Each subcommand also accepts the global
`--json` flag for machine-readable output.

---

## The model

### Inputs and outputs

`score run` loads the iteration log, assembles the signal sets, runs the rubric's scorer over
every iteration, aggregates per-session roll-ups, and — unless `--no-write` — persists:

- `iter-N.score.yaml` — one per iteration.
- `session-<id>.score.yaml` — one per session.

These sidecars live alongside the iteration records in the same log directory. `score
iteration` and `score session` read them back by default — fast, no git work, no transcript
scan.

### The iteration-log directory

All three subcommands default to `.agents/active/iteration-log` and accept an
`--iter-log-dir` override to point at a different log root:

```
--iter-log-dir <path>   Iteration-log directory (default: .agents/active/iteration-log)
```

### Transcript backfill

Some signals (notably token/cache efficiency) are reconstructed from agent transcripts when
the iteration log never captured them directly. `score run` and `score iteration --recompute`
accept one or more transcript roots:

```
--transcript-dir <path>   Agent transcript root for token backfill (repeatable)
```

For example `--transcript-dir ~/.claude/projects --transcript-dir ~/.codex/sessions` backfills
from both Claude and Codex session logs.

---

## The `da score` commands

### `da score run`

Scores every iteration in the active log and writes the per-iteration and per-session sidecars,
then prints a compact summary table.

```
da score run
da score run --iter-log-dir .agents/active/iteration-log
da score run --transcript-dir ~/.claude/projects --transcript-dir ~/.codex/sessions
da score run --no-write        # render the summary without writing sidecars
da --json score run            # machine-readable per-iteration + per-session payload
```

Flags:

| Flag | Purpose |
|------|---------|
| `--iter-log-dir` | Iteration-log directory (default `.agents/active/iteration-log`). |
| `--repo-dir` | Repository root for git topology (default: current working directory). |
| `--transcript-dir` | Agent transcript root for token backfill (repeatable). |
| `--no-write` | Render the summary without writing sidecars. |

If the log directory holds no iterations, `score run` reports that and exits without writing.

### `da score iteration <N>`

Renders the persisted `iter-N.score.yaml` sidecar — the full per-signal breakdown. By default
it reads the sidecar (no git work, no transcript scan). Pass `--recompute` to score iteration
`N` fresh from the canonical `iter-N.yaml` + git topology + transcripts, rewrite the sidecar,
and render it.

```
da score iteration 12
da score iteration 12 --recompute
da score iteration 12 --recompute --transcript-dir ~/.claude/projects
da --json score iteration 12
```

Flags:

| Flag | Purpose |
|------|---------|
| `--iter-log-dir` | Iteration-log directory. |
| `--recompute` | Recompute from canonical inputs and rewrite the sidecar. |
| `--repo-dir` | Repository root for git topology (used only with `--recompute`). |
| `--transcript-dir` | Transcript root for token backfill (repeatable; used only with `--recompute`). |

If no sidecar exists for the iteration, the command points you at the fix:
`run da score run first`.

### `da score session <session-id>`

Renders the persisted `session-<id>.score.yaml` sidecar — the session-level score plus the
per-iteration roll-up.

```
da score session 2026-06-22-a1b2c3
da --json score session 2026-06-22-a1b2c3
```

A missing session sidecar produces the same `run da score run first` guidance.

---

## Reading the output

### The iteration breakdown

`score iteration` prints the score, rubric version, and band, then a per-signal table — exactly
the explainable shape the rubric promises:

```text
Iteration 12   rubric 2.1.0   score 0.812   band good

SIGNAL                  PRESENT  SUBSCORE  WEIGHT    CONTRIB   DETAIL
landed                  yes      1.000     0.247     0.247     reachable from master
verifier                yes      1.000     0.222     0.222     ...
tests                   yes      1.000     0.210     0.210     ...
correction_pressure     yes      0.500     0.160     0.080     1 retry
scope                   yes      1.000     0.160     0.160     ...
hook_outcomes           no       -         0.100     -
token_efficiency        no       -         0.090     -

Source: .agents/active/iteration-log/iter-12.score.yaml
```

Each row shows whether the signal was **present**, its raw **sub-score**, the renormalized
**effective weight**, and the **contribution** (`effective_weight × sub_score`). Absent signals
do not vote — their weight renormalizes out of the total, so a missing telemetry field neither
inflates nor deflates the score. The contributions of present signals sum to the final score.

When the `hook_outcomes` signal contributed, an additional **Hook outcome sources** block lists
the scored records' `rule_id` and `sentinel_id` (plus lifecycle point, result, and intervention
class) so you can attribute the sub-score to the concrete gates that fired. No transcript
content is loaded or printed.

### Score bands

A numeric score is also reported as a human-readable band:

| Band | Range |
|------|-------|
| `excellent` | `score ≥ 0.85` |
| `good` | `0.70 ≤ score < 0.85` |
| `fair` | `0.50 ≤ score < 0.70` |
| `poor` | `score < 0.50` |
| `unscored` | no signals present |

If **every** signal is absent the iteration is `unscored` (numeric score null) — the rubric
never invents a score from nothing.

### The session roll-up

`score session` prints the session score and band, then each iteration's score and band:

```text
Session 2026-06-22-a1b2c3   rubric 2.1.0   score 0.788   band good
Iterations: 3

ITER    SCORE      BAND
11      0.760      good
12      0.812      good
13      0.792      good

Source: .agents/active/iteration-log/session-2026-06-22-a1b2c3.score.yaml
```

---

## Worked example

Compute every score, then read back one iteration and one session.

### 1. Score the active log

```console
$ da score run
Outcome scoring — rubric 2.1.0
Iterations: 3   Sessions: 1   Source: .agents/active/iteration-log

ITER    TASK          SCORE      BAND                        DATE
11      rd-config     0.760      good                        2026-06-20
12      rd-score      0.812      good                        2026-06-21
13      rd-trio       0.792      good                        2026-06-22

SESSION                                   SCORE      ITERS        BAND
2026-06-22-a1b2c3                         0.788      3            good

Wrote 3 iter sidecars + 1 session sidecars to .agents/active/iteration-log
```

### 2. Read one iteration's breakdown

```console
$ da score iteration 12
```

This renders the per-signal table shown above from `iter-12.score.yaml`.

### 3. Recompute after a rubric bump

When the rubric version changes, recompute a single iteration's score fresh and rewrite its
sidecar:

```console
$ da score iteration 12 --recompute --transcript-dir ~/.claude/projects
```

### 4. Read the session roll-up

```console
$ da score session 2026-06-22-a1b2c3
```

### 5. Machine-readable output

Every subcommand accepts `--json`:

```console
$ da --json score run
$ da --json score iteration 12
$ da --json score session 2026-06-22-a1b2c3
```

---

## Reference

### Command quick reference

| Command | Reads/writes sidecars | Network/git | Notes |
|---------|-----------------------|-------------|-------|
| `score run` | writes all (unless `--no-write`) | git topology + transcript scan | compact summary table |
| `score iteration <N>` | reads `iter-N.score.yaml` (rewrites with `--recompute`) | only with `--recompute` | full per-signal breakdown |
| `score session <id>` | reads `session-<id>.score.yaml` | none | session roll-up |

### Common flags

| Flag | Subcommands | Purpose |
|------|-------------|---------|
| `--iter-log-dir` | all | Iteration-log directory (default `.agents/active/iteration-log`). |
| `--repo-dir` | `run`, `iteration --recompute` | Repository root for git topology. |
| `--transcript-dir` | `run`, `iteration --recompute` | Transcript root for token backfill (repeatable). |
| `--no-write` | `run` | Render without writing sidecars. |
| `--recompute` | `iteration` | Recompute from canonical inputs and rewrite the sidecar. |
| `--json` | all (global) | Machine-readable output. |

### See also

- [Outcome-Scoring Rubric](./OUTCOME_SCORING_RUBRIC.md) — the authoritative, versioned
  definition of the signals, weights, and combination method.
- [Global Flag Contract](./GLOBAL_FLAG_CONTRACT.md) — the `--json` flag semantics.
