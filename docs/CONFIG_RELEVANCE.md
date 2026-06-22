---
title: Config Relevance
description: Canonical reference for execution profiles and the da config relevance command family.
sidebar:
  order: 2
---

# `da config relevance` — workflow execution profiles

This document is the **canonical reference** for the execution-profile layer and the
`da config relevance` command family. It is the user-facing companion to the
[`config-relevance-profiles`](../.agents/workflow/plans/config-relevance-profiles/) plan and the
[design proposal](../.agents/proposals/skill-relevance-filter.md); cite this file rather than
duplicating the facet contract.

## What an execution profile is

An **execution profile** is a single, scope-mergeable config layer that routes a task's *workflow
execution shape* by `app_type`. It consolidates routing that used to be scattered across
`app_type_verifier_map`, the lens-routing config, and `max_parallel_tasks` into one surface with
three independently overridable **facets** per `app_type`:

| Facet | What it controls | Filter |
|---|---|---|
| **units** | per-stage core / situational / noise classification of skills, agents, and lenses (the noise filter) | `--filter units` |
| **topology** | the executor : verifier : reviewer fan-out + `verifier_sequence` | `--filter topology` |
| **lenses** | the review-lens set + concurrency | `--filter lenses` |

The layer lives under `execution_profile` in `.agentsrc.json` (and, once config-v2 §15 lands,
merges by scope precedence org → team → repo → project-local — it is already shaped for that). It
is **purely additive**: an `app_type` with no entry is not an error, it just has no overrides, and
`default_class: situational` guarantees nothing unlisted is ever silently dropped from a working
set.

## Command surface

```
da config relevance [--filter units|topology|lenses|all] [--app-type <t>] [--stage <s>] [--task <plan/task>] [--json]
da config relevance --recompute [--write] [--app-type <t>] [--stage <s>]
```

- `--filter` (default `all`) slices the facet you want, so one command stays evolvable as new
  facets land — no verb-per-facet sprawl.
- `--json` emits a stable envelope (documented on `relevanceResult` in
  `commands/config/relevance.go`) so scripts can pin the envelope while the facet payload varies.
- `--recompute` is the **explicit driver event** (never on a timer): it reads the scored iteration
  corpus and proposes class changes; `--write` emits a *proposed* layer diff that a human accepts.
  It never auto-applies. `--write` outside `--recompute` is a usage error.

### How `app_type` is selected

The profile is resolved for an `app_type` chosen by this precedence (the `app_type_source` field in
the JSON envelope reports which selector won):

1. **`task`** — the `--task <plan-id>/<task-id>` target's own `app_type` (the `TASKS.yaml`
   override),
2. **`plan-default`** — that plan's `default_app_type` (`PLAN.yaml`),
3. **`flag`** — the `--app-type` flag,
4. **`none`** — nothing resolved; defaults/empty facets render.

`--task` must be `<plan-id>/<task-id>`; a bare task id cannot locate its plan.

## The three facets, by example

The repo ships two real profiles in `.agentsrc.json` — `go-cli` (execution/throughput: verify and
review heavily) and `ideation` (divergence: wide executor fan-out, with the verify and review gates
applied to the *design artifact* rather than to code). They are the worked examples below.

### Facet 1 — units (the noise filter)

Per stage, units are classed `core` (always-relevant working set), `situational` (conditionally
useful; also the default for any unlisted unit), or `noise` (suppressed from the working set).
Suppression is a **reversible view, not a delete**: the suppressed units are retained and reported,
so the filter can always be reversed.

```
$ da config relevance --filter units --app-type go-cli --stage review
units (stage: review, default_class: situational)
  [review]
    core        : thermo-nuclear-code-quality-review, review-pr
    situational : self-review, review-delta
    noise       : -
    working_set : thermo-nuclear-code-quality-review, review-pr, self-review, review-delta
    suppressed  : -
```

`working_set` is the live product of the noise filter (kept = core + situational); `suppressed`
lists the noise-classed units it set aside. For the `go-cli` `orchestrate` stage, `article-extract`
and `playwright` are classed noise and drop out of the working set while staying reversible.

### Facet 2 — topology (the fan-out)

```
$ da config relevance --filter topology --app-type go-cli
topology
  executors              : 1
  verifiers_per_executor : 2
  reviewers              : per_verifier
  verifier_sequence      : unit, cli-runner
```

- **`executors`** — parallel executor workers for the task.
- **`verifiers_per_executor`** — verifier passes per executor (`n` executors → `verifiers_per_executor·n`
  verifier passes). `go-cli` runs **two verifier passes** per executor — one per named profile in
  the sequence below.
- **`reviewers`** — reviewer fan-out as a keyword (`per_verifier`, `per_executor`) or a stringified
  integer count (`"0"` = no review gate). `ideation` uses `per_executor` — the full lens panel runs
  against each executor's divergent artifact.
- **`verifier_sequence`** — the ordered verifier-profile ids (supersedes `app_type_verifier_map`).
  `go-cli` runs `unit` (Go tests — proves the code is correct) then `cli-runner` (builds the `da`
  binary and exercises real CLI invocations — proves the wired-up command actually runs). The two
  entries match `verifiers_per_executor: 2`, one pass per profile. Each id resolves to a layered,
  scope-composed prompt via `verifier_profiles` — see
  [VERIFIER_REVIEWER_TEMPLATES.md](VERIFIER_REVIEWER_TEMPLATES.md).

`ideation` keeps the wide divergence but gates it — three executors, each artifact verified for
integrity and reviewed by the full lens panel:

```
$ da config relevance --filter topology --app-type ideation
topology
  executors              : 3
  verifiers_per_executor : 3
  reviewers              : per_executor
  verifier_sequence      : schema-check, citation-check, task-schedule
```

The ideation `verifier_sequence` checks the **artifact**, not code: `schema-check` (generated
`PLAN.yaml`/`TASKS.yaml`/schemas validate) runs first so structural validity gates the rest, then
`citation-check` (every `[[wikilink]]`, file path, and KGNote reference resolves) and `task-schedule`
(the task DAG's deps resolve and are acyclic — cross-checked against `da workflow eligible`).

### Facet 3 — lenses (the review lenses)

```
$ da config relevance --filter lenses --app-type go-cli
lenses
  lens_set         : architecture-standards, acceptance-invariants, adversarial
  lens_concurrency : gated
```

`lens_concurrency` is `parallel`, `gated`, or `tiered`. `go-cli` gates the three lenses; `ideation`
runs the same three (`architecture-standards`, `acceptance-invariants`, `adversarial`) in `parallel`,
applied to the design artifact.

## JSON envelope (the structure contract)

`--json` wraps the requested facet(s) in a stable envelope with **one consistent rule**:

- **Resolution context** — `app_type`, `app_type_source`, `stage`, `filter`, and `matched` — is
  always at the **top level**. It is *cross-facet*: with `--filter all` one resolution either
  matched a profile or it did not, regardless of which facet you sliced.
- **Facet payloads** — `topology`, `units`, `lenses` — nest under their own key.

```json
{
  "app_type": "go-cli",
  "app_type_source": "task",
  "filter": "topology",
  "matched": true,
  "topology": {
    "executors": 1,
    "verifiers_per_executor": 2,
    "reviewers": "per_verifier",
    "verifier_sequence": ["unit", "cli-runner"]
  }
}
```

`matched` is therefore **not** a field of any facet — folding it into `topology` (or `units`/
`lenses`) would duplicate it across facets and split one piece of state into three.
**Consumers read the envelope (context + facet), never a bare facet object**, to recover whether
the resolution matched. `matched: false` means the resolved `app_type` had no profile entry —
defaults render (`executors: 0`, empty facets) — and is never an error.

## How the wave engine consumes topology

The local `ultracode-wave-engine` workflow resolves, per eligible task,
`da config relevance --filter topology --task <plan>/<task> --json` and fans each task out under its
own shape — admitting tasks by summed executor **demand** against the slot budget, and threading
`verifiers_per_executor` / `reviewers` / `verifier_sequence` into each worker prompt instead of
slicing a flat per-wave count.

The engine **mirrors the envelope structure exactly** (per the contract above): each scouted task
carries `matched` as a sibling of `topology`, never folded inside it. A task whose `app_type` has
no profile (`matched: false`, `executors: 0`) falls back to a single-executor flat run, so an
unmatched `app_type` degrades gracefully rather than failing.

## Recompute (the data-driven half)

`da config relevance --recompute` reads the scored iteration corpus
(`.agents/active/iteration-log/iter-N.yaml` + `iter-N.score.yaml`), computes a per-unit
contribution signal (cited-in-passing vs low-scoring vs never-cited), and prints proposed
core/situational/noise changes plus a gaps list of cited-but-unclassified units. With `--write` it
emits a **proposed** `execution_profile` layer diff for a human to accept — explicit-only, no clock,
never auto-applied (config-v2 D4).

## config-v2 alignment

Every element maps to a `config-distribution-model` §15 mechanism, so this rides the §15
resolution/lock/command machinery rather than duplicating it: skills/agents/lenses are artifact
units, the profile is a `kind: layer` mergeable layer, recompute is a driver event, and persisted
verdicts land in the one units lock. Phase R1 (this) ships the static, config-v2-shaped layer + the
inspector + the wave-engine topology consumer; R2 wires the units lock; R3 automates the recompute
feedback loop.
