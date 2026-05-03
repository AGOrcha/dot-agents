# Self-Review ↔ Iteration-Close Wiring

**Status:** active
**Created:** 2026-05-03
**Owner:** dot-agents
**Spec/contract source:** `agent-context-resolution-architecture.md` §4 mapping row + §6.5 audit-confirmed pipeline state. No separate `design.md` — the audit findings + behavior-to-command mapping row constitute the contract.
**Related ADRs (produced by this plan):** ADR-0002, ADR-0003, ADR-0004, ADR-0005
**Foundation ADR (already accepted):** [ADR-0001](../../../../docs/adr/0001-adopt-architecture-decision-records.md) — adopt ADR format & location

---

## 1. Why this plan exists

A May-3 audit of the existing review/self-improvement pipeline established
three concrete gaps:

1. **Self-review skill is functionally orphaned.** It runs in chat,
   returns a summary, persists nothing. `iteration-close` never invokes
   it. Of 15 lessons in `.agents/lessons/`, zero are cited from any
   rule — the graduation chain has no plumbing.
2. **iter-log v2 review block is dead-coded.** The schema requires
   `review.overall_decision` (lines 8-17 of `schemas/workflow-iter-log.schema.json`).
   The writer exists (`commands/workflow/iter_log.go:397-426
   mergeReviewIterLog`). But no skill or CLI path ever calls
   `workflow checkpoint --log-to-iter --role review`, and no path writes
   the `review-decision.yaml` input the writer reads from. All 22 v2
   iter-logs have `overall_decision: ""`.
3. **A graduated KG-context behavior was lost.** The
   `crg-kg-integration/phase-g-skill-integration` task (2026-04-12)
   added `dot-agents kg changes --brief` to the older self-review
   skill template. The later skill-architect rework lost the call.
   The merge-back artifact even flagged the missing follow-up:
   *"Promote/sync `~/.agents/skills/global` from embedded templates"*
   — that step never happened. This is the exact regression pattern
   that managed-compounding is designed to prevent.

These three gaps share a single fix surface: extending the chain so
self-review writes a structured artifact that iteration-close consumes.

---

## 2. Plan-level contract (hard test + common false positive)

> **Hard test:** After plan archive, a fresh iteration through
> `iteration-close` produces an `iter-N.yaml` where
> `review.overall_decision` is non-empty AND
> `review.verify_record_appended: true` AND the populated content traces
> back to a `review-decision.yaml` written by self-review (not faked).
> Without further changes, the impact-score formula in
> `agent-context-resolution-architecture.md` §3 becomes computable for
> the first time on real data. The restored `dot-agents kg changes
> --brief` + `kg impact` calls fire as Step 0 of self-review on a real
> diff.
>
> **Common false positive:** schema looks correct on inspection but the
> call-site change isn't actually invoked, so new iter-logs still emit
> `overall_decision: ""`. Or: self-review writes the file but the
> contents are placeholder/stub. Verifier MUST read produced content,
> not just check field presence.

---

## 3. Methodology lens — applied at plan level

### 3.1 Four-question lens (annimaniac)

| Question | Today | Post-plan |
|---|---|---|
| What can AI **see**? | self-review chat-only; no visible artifact; iter-log review fields empty | structured `review-decision.yaml`; iter-log review populated; `kg changes --brief` + `kg impact` surface blast-radius before review |
| What can AI **do**? | self-review acts on no system of record | self-review writes a YAML consumed by `mergeReviewIterLog`; iteration-close calls `workflow checkpoint --role review` |
| Who can **extend**? | unchanged — `/skill-architect` flow remains the path | unchanged, but the new tier+contract frontmatter on self-review becomes a reusable template for review-pr, review-delta, plan-wave-picker |
| How has the **org** changed? | 22/22 v2 iter-logs empty; baseline 1/12 conversion rate; 0 lessons graduated | baseline measurement of new conversion starts; review-block population path proven on real data |

### 3.2 Resource graduation matrix view

This plan moves the **skill** row of `agent-context-resolution-architecture.md`
§1.5 by one tier:

- *Birth:* self-review skill exists (skill-architect generated it).
- *Use signal:* historically zero captured (chat-only).
- *Improvement signal:* this plan adds structured output that downstream
  systems (iter-log, future score formula) can score.
- *Graduation:* T2 compound today; gains a `tier:` declaration and a
  `contract:` block for the first time.

### 3.3 Telemetry-pillar seed

The `review-decision.yaml` envelope chosen in ADR-0002 is the
**first concrete instance** of the §1.6 execution-telemetry trace
schema in the architecture note. ADR-0004 records this explicitly so
future hook/subagent/rule traces follow the same envelope.

---

## 4. Task graph

```
t1-audit-decide-output-schema  (produces ADR-0002, ADR-0003, ADR-0005)
     │
     ├──────► t2-extend-self-review-skill
     │              │
     │              └──► t5-verify-end-to-end ──► t6-archive-and-baseline
     │
     ├──────► t3-extend-iteration-close
     │              │
     │              └──► (joins t5)
     │
     └──────► t4-trace-schema-seed-doc  (produces ADR-0004; parallel to t2/t3)
```

`t4` runs in parallel with `t2`/`t3` (no shared write scope).
`t5` joins after both `t2` and `t3` complete.
`t6` is the final archive step.

---

## 5. ADRs produced

| ADR | Title | Produced by | Status at plan start |
|---|---|---|---|
| 0001 | Adopt Architecture Decision Records | (foundation, written before plan) | accepted |
| 0002 | Self-review output destination and schema | t1 | proposed → accepted by t1 |
| 0003 | Self-review fire ordering vs verify-record-test | t1 | proposed → accepted by t1 |
| 0004 | Execution-telemetry schema seeded by review-decision.yaml | t4 | proposed → accepted by t4 |
| 0005 | Restore KG-context calls in self-review (regression fix) | t1 | proposed → accepted by t1 |

ADR-0002 through ADR-0005 are pre-stubbed in `docs/adr/README.md`'s index
as `proposed` and will be filled in during the corresponding tasks.

---

## 6. Skill changes (concrete diff scope)

### 6.1 self-review (~/.agents/skills/dot-agents/self-review/)

**Add:**
- `instructions/kg-context.md` — Step 0 module that runs
  `dot-agents kg changes --brief` and `dot-agents kg impact <files>`.
- `instructions/output-format.md` — describes the
  `review-decision.yaml` envelope per ADR-0002, with worked example.

**Modify:**
- `SKILL.md`:
  - Add `tier: T2` to frontmatter.
  - Add `contract:` block with `reads:`, `writes:`, `escape_hatches:`.
  - Insert reference to `kg-context.md` as Step 0 in the Workflow section.
  - Insert reference to `output-format.md` as the final step (write the
    structured output before returning).

**Preserve unchanged:**
- `instructions/code-quality.md`, `gotchas.md`, `performance.md`,
  `security.md`, `advisory-board.md`, `checklist.md` — orchestrator
  structure is preserved; additions are additive.

### 6.2 iteration-close (~/.agents/skills/dot-agents/iteration-close/)

**Modify:**
- `SKILL.md`:
  - Add `tier: T2` to frontmatter.
  - Add `contract:` block matching self-review's pattern.
- `instructions/workflow.md`:
  - Insert a new step that invokes `/self-review` at the ordering chosen
    in ADR-0003.
  - After self-review writes the file, call
    `dot-agents workflow checkpoint --log-to-iter <N> --role review` so
    `mergeReviewIterLog` reads the `review-decision.yaml` and populates
    the iter-log review block.

### 6.3 Reusable template

The `tier:` + `contract:` pattern added to self-review's `SKILL.md`
becomes the model that future plans can copy when upgrading review-pr,
review-delta, plan-wave-picker, agent-handoff, etc. This plan does NOT
ship those upgrades — it produces the canonical example.

---

## 7. Out of scope for this plan

- Building `dot-agents kg adr {index, query, supersede, sightings}`
  commands. Deferred to a future plan; ADR-0001's "ADR ↔ Knowledge Graph"
  section names the future shape but does not commit.
- Backfilling ADRs for prior conversation-level decisions
  (managed-compounding terminology, proposal-system reuse for KG
  promotions, tier declaration site, async peer review). Backfill is
  not blocking; can be scheduled later.
- Upgrading review-pr, review-delta, or other skills with the tier
  + contract frontmatter. This plan produces the template; follow-ups
  apply it.
- Lessons → rules graduation surface. Distinct managed-compounding
  instance. Will be its own plan once this one establishes the
  review-decision.yaml shape it can reuse.
- Cross-tool importers (`workflow propose import --from codex|cursor`).
  Mentioned in the architecture note §4 mapping; deferred to a future
  plan.
- Stale-proposal detection in `workflow drift`. Architecture note §4
  mapping addition; deferred.

---

## 8. Verification gates per task

Each task in `TASKS.yaml` declares its own hard-test + false-positive in
the `notes:` block. The end-to-end gate is t5: a real iteration on this
branch produces a populated review block, and the produced
`review-decision.yaml` content is captured into the merge-back archive
for human cross-check.

---

## 9. Closeout signals

- `dot-agents workflow plan archive --plan self-review-iteration-close-wiring`
- Architecture note §6.5 gains a one-line addendum recording the new
  baseline conversion-rate measurement starting point.
- All four ADRs (0002–0005) are flipped from `proposed` to `accepted`
  in `docs/adr/README.md`.
- PLAN.yaml status flipped from `active` to `completed`.

---

## 10. ISP execution notes

This plan is structured for `dot-agents`' Interactive Staged Pipeline
(ISP) skill. Each task in `TASKS.yaml` declares an explicit `ISP routing`
block in its `notes:` field with five fields:

- **`mode`** — `direct` or `fanout-amenable`. The orchestrator is encouraged
  to run direct tasks itself rather than spawning a subagent; fanout-amenable
  tasks have isolated write scope and can be delegated.
- **`verifier`** — `manual`, `smoke`, or `integration`. Drives the post-impl
  verifier stage of the ISP staged runtime.
- **`review`** — `decision-quality`, `code review`, `doc review`,
  `verification review`, or `closeout review`. Drives the review stage.
- **`anti-scope`** — what this task must NOT do. Prevents scope creep
  and conflicts with parallel tasks.
- **`bundle-context`** — what a fanout subagent should load if delegated.
  Lists evidence sidecars (architecture note sections, ADRs, audit
  findings, source file ranges).
- **`output contract`** — the concrete artifact the parent will check
  on merge-back.

### Suggested ISP turn sequence

```
turn 1 → t1-audit-decide-output-schema  (direct; produces ADR-0002, 0003, 0005)
turn 2 → fanout: { t2-extend-self-review-skill,         (parallel; non-overlapping)
                   t4-trace-schema-seed-doc }            write scopes; t4 small)
turn 3 → t3-extend-iteration-close       (direct; depends on t1 + t2)
turn 4 → t5-verify-end-to-end            (orchestrator runs real iteration)
turn 5 → t6-archive-and-baseline         (orchestrator closeout)
```

### Pre-flight requirements

The plan assumes `orchestrator-session-start` has run and produced a
`workflow eligible --json` snapshot covering this plan. Before ISP fires
the orchestrator should verify:

1. `kg-command-surface-readiness` is archived (otherwise it pollutes the
   eligible list — see `loop-state.md` "Hygiene action pending").
2. Stale fold-backs are swept (orientation otherwise picks up signals
   pointing at archived plans).
3. The working tree is on a clean state appropriate for plan-driven work
   (or the in-flight changes are a known set, like the bundle of files
   that landed this plan in the first place).

### Evidence sidecars consulted by every task

- [`agent-context-resolution-architecture.md`](../../proposals/agent-context-resolution-architecture.md)
  — §1.5 (resource graduation matrix), §1.6 (execution telemetry pillar),
  §3 (data flow + score axes), §4 (behavior-to-command mapping), §6.5
  (audit-confirmed pipeline state).
- [`docs/adr/README.md`](../../../../docs/adr/README.md) — index + format.
- [`docs/adr/0001-adopt-architecture-decision-records.md`](../../../../docs/adr/0001-adopt-architecture-decision-records.md)
  — ADR convention foundation.
- The audit transcript: `.agents/active/loop-state.md` ("Loop Health"
  section captures the audit baseline numbers each task is accountable
  to).
