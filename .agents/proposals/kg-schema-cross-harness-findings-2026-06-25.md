# KG Schema — Cross-Harness Adversarial Findings (codex), 2026-06-25

Source: cross-harness adversarial pass run via `codex exec -s read-only` (a different
model family / second brain) against the SDD-entity KG schema, per the owner's overnight
directive ("run an adversarial pass with codex and another with cursor"). Targets:
`work-tracking-storage-abstraction/sdd-entity-kg-schema-draft.md`,
`scoped-knowledge-graphs/design.md`, `app-type-profiles/design.md`.

**Verdict: FAIL** (5 HIGH). These are the schema-fix work items to fold in. Cursor pass
(second independent brain) still pending.

## HIGH — edge cardinality / direction bugs (validate the owner's edge concerns)

1. **`supersedes_spec` cardinality wrong** — declared `many-to-one`, but the real case
   (`scoped-knowledge-graphs` supersedes `spec.1.md` + `spec.2.md`) is one→many. Import
   either violates cardinality or drops a predecessor. *Fix:* make it `one-to-many`
   (newer→older) or `many-to-many`; add inverse `superseded_by_spec` if old→replacement
   traversal is needed. (schema L459-463; scoped spec L6)

2. **`derived_from_spec` direction breaks staleness propagation** — edge points derived→source,
   but scoped-KG staleness propagates *outward along derivation edges*; a base-spec change
   never reaches derived specs. *Fix:* add source→derivative `derives_spec`, or define
   derivation propagation as reverse-traversal of `derived_from_spec` with `derivation:true`.
   (schema L464-467; scoped KG L170-172, L336-345)

3. **`plan_for_spec` (many-to-many) vs inverse `derives_plans` (one-to-many) contradict** — a
   multi-spec plan is legal one direction, illegal the other; validation/queries disagree by
   direction. *Fix:* make `derives_plans` many-to-many, or drop the duplicate inverse and
   define bidirectional traversal over one canonical relation. (schema L423-436)

4. **result→plan policy self-contradicts** — header/reconciliation say `result_for_plan` was
   restored for wave/fold_back results, but caveat + evidence ledger still say the schema
   deliberately does NOT materialize result→plan. Implementers will diverge. *Fix:* delete the
   stale "no result→plan edge" caveat/ledger; make normative: task-scoped results →
   `result_for_task`; plan-scoped wave/fold_back results → `result_for_plan`; `plan_id` stays
   provenance metadata. (schema L353-354, L487-515, L524-528, L839-845)

5. **proposal/lesson scope inconsistent + under-typed** — scope is optional and uses
   `repo|project|global`, while scoped-KG says every node carries origin scope on the chain
   `repo→user→team→org→public`; `in_scope_of` is typed only `from: proposal` with lesson/etc.
   left as prose. A user/team/global lesson can't be represented; "which lessons are in scope
   here?" can't run. *Fix:* require `origin_scope` or `in_scope_of` on proposal/lesson; type
   `in_scope_of` for lesson/skill/rule/agent/hook/app_type_profile; SEPARATE placement vocab
   (`project-local|global`, per proposal-routing) from scoped-KG origin vocab
   (`repo|user|team|org|public`). (schema L214,226,313-332,454-457,677-681; scoped KG L83-87,113-116,260-264)
   *(This is the OQ4 scoped-proposals/lessons expansion — needs the placement-vs-origin split.)*

## MEDIUM

6. **operational-knowledge still too weak for "specific operational paths"** (the owner's
   exact concern) — `surface_vs_path` names an exercised path but there's no node/edge for the
   actual CLI/MCP/API operation (command, args, exit status, endpoint); `rubric_dimensions` is
   `list<enum>` with no declared enum values. "this exact `da workflow fanout` path failed under
   args X" collapses to prose. *Fix:* add an `operation_invocation`/`operational_path` node (or
   edge attrs) with `surface_kind`, `operation_id`, `args_hash`, `exit_status`, `evidence_uri`;
   declare the rubric enum values explicitly. (schema L228-230,362-365,623-630,657-662)

7. **app_type starter catalog stale** — schema marks `api/ui/streaming/batch` as
   "COMPOSITE-MEMBER, not independently selectable" while app-type-profiles §8A defines them as
   starter profiles; schema omits `http-service` alias, `db`, `graph-knowledge`, and `meta`.
   *Fix:* update `known_starter_values` to match §8A/§8B; drop "not independently selectable"
   for starter children; add `meta` starter or state it's custom-only. (schema L75-117;
   profiles L481-486,523-542,753-775,815-823)

## LOW

8. **open-vocab confirmed ✓** — no closed `app_type` enum found (`type:string`,
   `custom_allowed:true`); only doc-drift risk. *Fix:* single-source the starter catalog from
   the app-type-profile catalog so prose can't drift. (schema L77-79,110-117; profiles L48-68,91-101)

## Disposition

Findings 1-5 (HIGH) are schema-fix tasks against `sdd-entity-kg-schema-draft.md` (+ the two
companion specs). Finding 6 is the operational-knowledge rubric the owner asked for. Findings
7-8 reconcile the app_type starter catalog single-source (ties to Q10's published-builtin layer).
Recommend folding 1-5 + 6 into the work-tracking-storage / graph-KG schema task line; run the
cursor pass next for an independent second-brain cross-check before finalizing the edits.
