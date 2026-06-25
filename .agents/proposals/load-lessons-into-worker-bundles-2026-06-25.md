# Load relevant lessons into worker bundles (not hand-copied rules)

**Owner insight, 2026-06-25.** The meta-loop's "lessons-as-enforcement" arm: the
delegation bundle should **load the relevant `LESSON.md` files** (full text — pattern,
root cause, example, rule, how-to-apply) into the worker's initial instructions, instead
of the orchestrator hand-copying a distilled one-line rule into each brief.

## Why (the owner's framing)

- **No repeated verbiage.** Guidance lives once in the lessons corpus; the orchestrator
  stops re-typing rules into every brief.
- **Workers get the WHY, not just the rule.** The example + root cause + "where we're
  coming from" is what makes a rule stick — a worker that understands the failure mode
  generalizes it; a worker handed a bare rule applies it narrowly or misses adjacent cases.
- **Single source of truth that propagates into execution.** Edit a `LESSON.md` once and
  every future worker receives the updated guidance — the corpus becomes enforcement, not
  documentation.
- This is the exact fix for the `agent-ops-hardening` finding ("lessons stayed
  documentation, not enforcement, so patterns recurred"). The recurring S3776-on-`_test.go`
  (3× across #168/#170 this session) is the proof: the lesson EXISTED
  (`sonarcloud-gate-mechanics`, `gates-must-be-locally-reproducible`) but never reached the
  worker bundle.

## Mechanism

Bundle generation **selects** the lessons relevant to a task and **injects** them (as
required-reads or inlined) into the worker's initial context.

- **Selection = relevance by `app_type` + `write_scope` (touched packages/paths) + tags.**
  This is `da config relevance` extended to the lessons corpus — today it routes
  prompts/config; add a **`lessons` facet** that returns the matching `LESSON.md` set.
- **Interim (no KG):** tag / app_type / path match over `.agents/lessons/` → attach the
  matching `LESSON.md` paths to the bundle as `required_reads`.
- **End-state:** query the **scoped-lessons KG view** (OQ4 scoped lessons; the lesson
  schema's tags/app_type/scope) for lessons whose scope + subject match the task, and
  inject. The KG makes selection precise and scope-aware (repo/user/team/org/public).

## Ties to in-flight work

- **lesson schema** (kg-ideate `t8`, #161) — must carry **selection metadata**: `tags`,
  `applies_to_app_types`, touched-path/subject globs, and the `scope` discriminator (#169
  schema v4 added `in_scope_of`/`origin_scope` for lessons). Without these, relevance
  selection is keyword-only. ADD selection fields when authoring `lesson.schema.json`.
- **`da config relevance`** (t7, #170 — just added a `graph` facet) — add a `lessons`
  facet using the same resolver pattern.
- **delegation-lifecycle bundle format** — add a `lessons` / lesson-`required_reads`
  section the orchestrator populates from the relevance query.
- **meta-loop-operating-model §3** — this is the feedback loop's enforcement arm
  (lessons → execution), the counterpart to result→lesson capture.

## Disposition (refinement tasks)

1. Add lesson-selection metadata to `lesson.schema.json` (extends #161 t8).
2. Add a `lessons` facet to `da config relevance` (relevance-select matching LESSON.md).
3. Extend bundle generation (delegation-lifecycle) to attach relevance-selected lessons as
   `required_reads`.
4. **Dogfood NOW:** until 1–3 land, the orchestrator manually points each worker at the
   relevant `LESSON.md` files (full lessons) instead of re-stating rules — starting with the
   t2/t4 graph-chain workers this session.
