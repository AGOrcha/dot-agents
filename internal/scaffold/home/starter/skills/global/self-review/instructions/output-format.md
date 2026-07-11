---
scope: "Final step — write the structured review-decision.yaml artifact"
---

# Output Format

Self-review's final step is writing a structured `review-decision.yaml`.
This is what makes the skill consumable by `workflow checkpoint
--log-to-iter --role review` (which merges the file's fields into the
iteration's review block) and by any future telemetry tooling that
reads the packed envelope in `reviewer_notes`.

This module is the load-bearing contract between self-review (writer)
and the rest of the workflow pipeline (readers). Treat the schema in
this file as authoritative; the human-readable summary you produce in
chat is **secondary** to landing the structured artifact.

## Path

```
.agents/active/verification/<task_id>/review-decision.yaml
```

`<task_id>` resolves as follows:

- **Inside iteration-close** — use the iteration's task context
  (the `task_id` from the active delegation contract or
  `workflow status`).
- **Standalone invocation** (manual `/self-review`, no iteration-close
  context) — synthesize an id of the form `adhoc-<RFC3339>`, e.g.
  `adhoc-2026-05-04T10-30-00Z`. Replace colons in the timestamp with
  hyphens so the directory name is filesystem-safe. The artifact is
  still useful as a future telemetry trace; it just isn't merged by
  `workflow checkpoint --role review` in standalone mode.

## How to write the file

Two valid mechanisms; pick the one that matches the calling context:

### Mechanism 1: Invoke `da workflow verify record --kind review`

This is the **preferred** mechanism when self-review fires inside
iteration-close (it runs after `verify record --kind test`, before
`workflow checkpoint`). The CLI writes the file in the strict on-disk
shape and validates it against its schema before persistence.

```
da workflow verify record \
  --kind review \
  --task <task_id> \
  --phase1-decision accept|reject|escalate \
  --phase2-decision accept|reject|escalate \
  --overall-decision accept|reject|escalate \
  --escalation-reason "..." \   # required iff overall == escalate
  --reviewer-notes "<see Notes section below>" \
  --failed-gate <slug> --failed-gate <slug> ... \
  --command "self-review" \
  --scope "<staged-diff or files reviewed>" \
  --summary "<one-line decision summary>"
```

The CLI overwrites any existing file at the path. The on-disk shape
is exactly the strict schema (see "On-disk schema" below).

### Mechanism 2: Direct YAML write

When self-review is invoked standalone (no iteration-close, no
delegation contract present), `verify record --kind review` may
refuse because there is no resolvable delegation context. In that
mode, write the file directly using the **strict on-disk schema** —
do not invent extra top-level keys, because the schema rejects
unknown fields.

Use the `<task_id>` resolution rule above (synthesize `adhoc-<ts>`).
Ensure the directory `.agents/active/verification/<task_id>/` exists,
then write the YAML file.

## On-disk schema (authoritative)

The file MUST match this shape:

| Field                | Type            | Required | Notes                                                   |
| -------------------- | --------------- | -------- | ------------------------------------------------------- |
| `schema_version`     | int (const `1`) | yes      | Bump only on breaking change.                           |
| `task_id`            | string          | yes      | Plan task this review belongs to.                       |
| `parent_plan_id`     | string          | yes      | Canonical plan id (or `adhoc` for standalone).          |
| `delegation_id`      | string          | no       | Optional delegation bundle id.                          |
| `phase_1_decision`   | enum            | yes      | `accept` \| `reject` \| `escalate`                      |
| `phase_2_decision`   | enum            | yes      | Same enum. Use `accept` if only single-phase reviewing. |
| `overall_decision`   | enum            | yes      | Pessimistic: any reject → reject; else any escalate.    |
| `failed_gates`       | array of string | yes      | Empty array `[]` allowed when overall is `accept`.      |
| `escalation_reason`  | string          | cond.    | Required iff `overall_decision == escalate`.            |
| `reviewer_notes`     | string          | no       | Free-form. **Pack the telemetry envelope here** (see below). |
| `recorded_at`        | RFC3339 string  | yes      | UTC.                                                     |
| `recorded_by`        | string          | no       | e.g. `self-review (skill)`.                              |

Unknown top-level keys cause schema validation to fail and the CLI
refuses to write the file. **Do not add envelope fields at the top
level** — pack them into `reviewer_notes` instead (see below).

## Telemetry envelope — pack into `reviewer_notes`

A forward-compatible telemetry envelope (`resource_type`,
`resource_id`, `invoked_at`, `outcome`, `post_invocation`,
`improvement_signals`) is not yet promoted to top-level schema fields.
Until a schema migration ships that, **pack the envelope into
`reviewer_notes` as a fenced YAML block**. Future telemetry mining can
extract it without ambiguity, and a schema migration can later promote
the fenced fields to top-level without rewriting historical artifacts.

The `reviewer_notes` payload structure:

````
## KG Context (Step 0)

### kg changes --brief
<verbatim stdout from `da kg changes --brief`>

### kg impact <files...>
<verbatim stdout from `da kg impact ...`>

## Per-file review

<one section per changed file with verdict, axis-tagged findings,
and decisive language — see `examples/good-review.md`>

## Telemetry envelope

```yaml
resource_type: skill
resource_id: self-review
invoked_at: <RFC3339>
invoked_by: orchestrator | loop-worker | user
plan_id: <if applicable>
task_id: <task_id used for the file path>

outcome:
  declared: success | failure | partial
  agent_self_assessment: <free text>

post_invocation:
  agent_actions_after_skill_returned: []
  user_corrections: []
  retries_in_loop: 0

improvement_signals:
  missing_in_skill: []
  redundant_in_skill: []
  tooling_gap: { present: false, note: "" }
  script_gap: { present: false, note: "" }
  instruction_gap: { present: false, note: "" }
```
````

This keeps the artifact schema-valid today (because everything extra
lives inside the `reviewer_notes` string) while preserving every field
a future schema revision could elevate.

## Worked example

Standalone invocation, `task_id = adhoc-2026-05-04T10-30-00Z`,
small documentation-only diff, all axes pass:

```yaml
schema_version: 1
task_id: adhoc-2026-05-04T10-30-00Z
parent_plan_id: adhoc
phase_1_decision: accept
phase_2_decision: accept
overall_decision: accept
failed_gates: []
reviewer_notes: |-
  ## KG Context (Step 0)

  ### kg changes --brief
  Changed: 1 file (docs/foo.md). 0 nodes touched in graph.

  ### kg impact docs/foo.md
  No downstream impact (docs file; not referenced by code nodes).

  ## Per-file review

  - docs/foo.md
    - Code-quality: clear; section headings consistent.
    - Security: N/A (docs).
    - Performance: N/A (docs).
    - Gotchas: none.
    - Verdict: accept.

  ## Telemetry envelope

  ```yaml
  resource_type: skill
  resource_id: self-review
  invoked_at: "2026-05-04T10:30:00Z"
  invoked_by: user
  task_id: adhoc-2026-05-04T10-30-00Z

  outcome:
    declared: success
    agent_self_assessment: "Diff is small and mechanical; no surprises."

  post_invocation:
    agent_actions_after_skill_returned: []
    user_corrections: []
    retries_in_loop: 0

  improvement_signals:
    missing_in_skill: []
    redundant_in_skill: []
    tooling_gap: { present: false, note: "" }
    script_gap: { present: false, note: "" }
    instruction_gap: { present: false, note: "" }
  ```
recorded_at: "2026-05-04T10:30:00Z"
recorded_by: self-review (skill)
```

A `reject` example would set `phase_1_decision: reject`,
`overall_decision: reject`, `failed_gates: [security, performance]`,
and replace the per-file verdicts with `Verdict: reject — <reason>`.

An `escalate` example must include
`escalation_reason: "<concrete reason>"` (non-empty string).

## Hand-off to checkpoint

Once the file is written, `workflow checkpoint --log-to-iter <N>
--role review` reads it and merges the `phase_*_decision`,
`overall_decision`, `failed_gates`, `escalation_reason`, and
`reviewer_notes` fields into the iteration's review block. **No
further work is required from self-review** — the file is the only
deliverable.

## Common false positives

- **Stub prose, no file written.** The skill produced a chat summary
  but never landed the YAML file. Any merge-back with no file at the
  expected path is a regression.
- **File written, schema-invalid.** Top-level fields like
  `outcome:` or `improvement_signals:` were added at the top level.
  `verify record` refuses to write; manual writes parse but the
  strict reader downstream may break later. Always pack envelope
  fields into `reviewer_notes`.
- **Reviewer notes empty.** The notes field is the audit trail.
  Empty notes after a substantive diff is a red flag; the reviewer
  did not actually inspect the diff.
- **Phase mismatch with overall.** The CLI rejects `--overall reject`
  with `--phase_1 accept --phase_2 accept`. Use derived
  consolidation (omit `--overall-decision`) unless explicitly
  overriding.

## References

- `instructions/kg-context.md` — Step 0, whose output lands in `reviewer_notes`.
- `da workflow verify record --kind review` — the CLI mechanism that validates and writes this file.
- `workflow checkpoint --log-to-iter --role review` — the reader that merges this file's fields into the iteration record.
