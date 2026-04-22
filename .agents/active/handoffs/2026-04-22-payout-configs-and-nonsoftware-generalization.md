# Handoff: Payout agent configs + cross-app impact spec + non-software generalization

**Created:** 2026-04-22
**Author:** Claude Code session
**For:** AI Agent
**Status:** Spec 1 shipped; spec 2 next; plan after. Three decisions from a sibling review handoff are partially answered and need closure.

---

## Summary

Two-spec + one-plan arc covering (a) a payout agent-config suite sourced from a new `po-agents-config` git repo, (b) generalization of the `impl → verify → review` pipeline to non-software work via versioned profiles, (c) a cross-app dependency-impact workflow with CRG + scoped-KG. **Spec 1 (`app-type-profiles`) is committed in `5b31008`.** Spec 2 (`cross-app-dependency-impact`) is next. Wave-1 payout plan comes after both specs and includes creating `po-agents-config`, authoring wave-1 layers, and retiring the old `.plan.md` / `.loop.md` / `payout-session-start` artifacts **at the tail** (after new stuff lands). Three contract-level decisions flagged by an adversarial review of spec 1 are partially answered — see §Decisions Pending.

## Project Context

`dot-agents` CLI manages multi-agent workflows across Claude Code / Codex / Cursor / Copilot. Primary working dir: `/Users/nikashp/Documents/dot-agents`. Branch: `feature/PA-cursor-projectsync-phase1-extract-293f`. Master is PR base.

Relevant state:
- **Workflow commands already supply:** `orient`, `eligible` (conflict-detected, all-unblocked), `next`, `fanout`/`merge-back`/`delegation`, `fold-back`, `drift`/`sweep`, `graph`. Wave planning and native parallel fanout are live. Session-start ceremony from the old per-repo skill model is now subsumed by these.
- **CRG** (code-review-graph): live, auto-updating via git hooks, Tree-sitter structural layer.
- **KG** (narrative/semantic layer): empty. Scoped-KG spec at `specs/scoped-knowledge-graphs/design.md` is canonical v2. Bootstrap of narrative notes is tracked separately in handoff `2026-04-22-kg-bootstrap-and-profiles-review.md` — **read that handoff first** if you are picking up either thread.
- **Payout sibling repo** (`/Users/nikashp/Documents/payout/`): 15+ sub-apps, already dot-agents managed via `.agentsrc.json`. Old-model loop artifacts (`.plan.md`, `.loop.md`, `.loop-state.md`, `payout-session-start` skill) predate current workflow commands and are slated for retirement in wave 1.
- **ResumeAgent** (`/Users/nikashp/Documents/ResumeAgent/`): Python/FastAPI. Used as a non-code pressure-test domain for the profile abstraction. Does **not** need to adopt configs in lockstep.

## The Plan

### Spec 1 — `app-type-profiles` (DONE, committed as `5b31008`)

Path: `.agents/workflow/specs/app-type-profiles/design.md`

Covers: profile schema (name + version + verifier_chain + review_kind + graph_backend + impact_radius_kind + impl_defaults), composite profiles (po-core-api-se worked example composes api + batch + streaming), non-code `artifact_scope` generalization (code / document / artifact kinds), **verifier evolution with behavior preservation** (§6 — semver bump table, behavior-preservation gate with corpus diff blocking pass→fail regressions, deprecation pathway, consumer pin ergonomics), pipeline integration with `workflow orient`/`eligible`/`fanout`, four worked examples (go-http-service, po-core-api-se, research, resume-ideation), migration from `app_type_verifier_map`, six open questions.

Six decisions locked in §2: (1) profile is named bundle not flag, (2) composite composes no override, (3) versioned with behavior gate, (4) `artifact_scope` generalizes `write_scope`, (5) local → git v1.5 → oci v2 via existing sources/packages machinery, (6) review kind + graph backend + impact radius all pluggable.

### Spec 2 — `cross-app-dependency-impact` (NEXT, not yet written)

Target path: `.agents/workflow/specs/cross-app-dependency-impact/design.md`

Outline already agreed with the user. Required sections:

1. **Problem statement** — cross-repo blast radius is invisible today; the three existing update flavors (Renovate-style auto, devops manual-trigger, dev manual) all need the same impact/plan/execute backbone.
2. **Five dependency layers as KG nodes:**
   - Explicit packages: `go.mod` / `pyproject.toml` / `package.json` manifest parse + Tree-sitter symbol-usage resolution
   - Events (pub/sub): `publish()` / `subscribe()` call sites + schema registry; flavors = event-sourcing vs event-driven
   - HTTP internal: ingress manifests → service → router → handler matching; dynamic URLs use path regex + confidence score
   - HTTP external: URL patterns → external API registry nodes → docs-pointer URLs; public scope reserved for future
   - DB schemas + config: `po-mongodb-management` DDL + vault/infisical keys → consumer mapping
3. **Schema-registry preference** (user confirmed 2026-04-22): **AsyncAPI** as schema source of truth for events. Proto deferred to future consideration (binary/wire efficiency gains don't justify the redesign cost now). Spec should show how AsyncAPI docs register as KG nodes and how consumers link.
4. **Seven SDLC phases:** Detect → Triage (green/yellow/red) → Plan generation (per-repo PLAN.yaml + TASKS.yaml with evidence sidecars; dep-ordered libs → services, producers → consumers) → Execute (parallel fanout) → Review (KG-backed; aggregate cross-repo) → Deploy (po-control-plane via swarm-cd, staged dev/staging/prod with gates) → Learn (`workflow fold-back` updates KG).
5. **Three update flavors → one pipeline.** Renovate/Dependabot, devops manual-trigger, developer manual — all differ only in trigger; same impact/plan/execute machinery.
6. **Bootstrap strategy** (proposal agreed structurally with user; user asked "what do you propose" and accepted my pitch):
   - Explicit-manifest layer: eager one-shot, cheap (text parse only, no Tree-sitter). Populates `team`-scope package nodes + cross-repo edges. Idempotent, resumable (per-repo checkpoints).
   - Symbol layer: per-repo lazy via existing CRG; team-scope federates at read time, no upfront unification pass.
   - Implicit layers: manual-first via `workflow deps register event|http|collection|config <spec>`; CRG suggests ("saw publish('order.placed') at X:42, not registered"); human confirms; confidence closes over time.
   - Entry command: `workflow graph bootstrap --team <name>`.
7. **Confidence closure as v2 slot.** Ship v1 without prod-metrics feedback — separable phase, needs OTel story that doesn't exist yet. But design the `confidence` field into the KG node schema now so v2 writes without migration. V2 sketch (user asked for more thought, accepted "add as reserved slot"): traces primary (span-to-edge mapping confirms/contradicts/suggests), metrics confirm liveness only (hot vs cold edge), logs enrich attribution. Closure runs through `workflow fold-back` on a schedule. Requires operator-authored span-name → KG-node mapping file (spec should define its schema).
8. **New command surface proposed:**
   - `workflow impact --change <ref>` — compute cross-repo blast radius
   - `workflow impact --propose` — generate downstream per-repo plans with evidence sidecars
   - `workflow deps register event|http|collection|config <spec>` — manual registration
   - `workflow deps verify` — lint registrations against source (catches stale entries)
   - `graph query cross-app --symbol <x>` — shorthand for common queries
9. **Generic first, payout as worked example** (user confirmed).
10. **Open questions** at minimum: dynamic-URL regex confidence thresholds, out-of-repo public-API unknown-consumer marker shape, span-to-KG-node mapping file ownership, staleness interaction with the scoped-KG event-driven drivers (four drivers defined in scoped-KG v2).

### Plan — `payout-agent-config-wave-1` (after both specs)

Target path: `.agents/workflow/plans/payout-agent-config-wave-1/PLAN.yaml + TASKS.yaml + payout-agent-config-wave-1.plan.md`

Scope:
- **Create `po-agents-config` git repo** (not yet created — user confirmed it needs creation). Separate repo; sourced into payout via `sources: [{id: payout, type: git, url: git@...:<owner>/po-agents-config, ref: main, cache_ttl: 4h}]` in each app's `.agentsrc.json`.
- **Wave 1 layer files:** `workspace/base`, `lang/go-service`, `lang/next-frontend`, `lang/next-frontend-po`, `app/po-cluster-manager`, leaf layers for po-core-api-se (composite: api + batch + streaming), client-ui, manager-ui, po-cluster-manager. Plus profile files for the above `app_type`s.
- **`SessionStart` hook in `workspace/base`:** single hook that runs `dot-agents workflow orient --plan <scope>` and surfaces a 3-line summary. Replaces the retired `payout-session-start` skill and the 867 context-reestablishment mentions measured in prior session analysis.
- **Retirement (tail tasks)** — after new stuff lands: migrate `.plan.md` / `.loop.md` / `.loop-state.md` content into `workflow/plans/<id>/` shape, delete the old `payout-session-start` skill, delete old `.agents/active/*.plan.md` files from payout repo.

Wave 2 (separate plan, not in scope for wave 1): `lang/python-service`, `lang/python-lib`, `lang/python-script`, leaf layers for po-common-lib, po-logging-lib, po-mongo-lib, po-vault-lib, po-mongodb-management, client-se, manager-se, po-web-starter, manager-native-app, po-control-plane, po-cicd-pipeline.

## Key Files

| File | Why It Matters |
|---|---|
| `.agents/workflow/specs/app-type-profiles/design.md` | Spec 1 — committed. Read §2 (decisions), §4 (composite resolution), §6 (behavior-preservation gate — the load-bearing bit). |
| `.agents/workflow/specs/config-distribution-model/design.md` | Defines `sources` / `extends` / `packages` / `app_type_verifier_map` field surface. Spec 1 is the schema behind `app_type`. Spec 2 consumes `sources` for cross-repo indexing. |
| `.agents/workflow/specs/external-agent-sources/design.md` | OCI / auth / transport. Referenced by spec 1 §2.5. Do not over-couple spec 2 to OCI — v1 uses local/git only. |
| `.agents/workflow/specs/scoped-knowledge-graphs/design.md` | v2 canonical. Team scope + write-time-only propagation + four event-driven staleness drivers + resolver purity. Spec 2's cross-repo graph lives in `team` scope. |
| `.agents/workflow/specs/workflow-parallel-orchestration/design.md` | `max_parallel_workers`, write-scope conflict detection, eligible fanout set. Spec 2's parallel execution reuses this. Spec 1 §5.2 extends conflict detection to non-code `artifact_scope`. |
| `.agents/workflow/specs/planner-evidence-backed-write-scope/` (check dir) | Evidence-sidecar mechanism already landed (see commit `5fbb497`, `f6430bc`). Spec 2's plan-generation phase produces sidecars through this. |
| `.agents/active/handoffs/2026-04-22-kg-bootstrap-and-profiles-review.md` | **Read first.** Prior-session handoff containing adversarial review of spec 1 with three decisions (D1/D2/D3) partially answered below. |
| `.agents/active/handoffs/2026-04-19-kg-freshness-remaining.md` | Older KG-thread handoff. Only relevant if working the narrative-KG bootstrap thread. |
| `/Users/nikashp/Documents/payout/.agentsrc.json` | Payout's current agentsrc — `payout-session-start` skill listed here; wave 1 retirement must update this file. |
| `/Users/nikashp/Documents/payout/.agents/active/*.plan.md` | Old-model payout plans slated for migration in wave 1 tail. |

## Current State

**Done this session:**
- Spec 1 `app-type-profiles/design.md` authored and committed (`5b31008`, ~570 lines)
- Layering strategy re-anchored to spec vocabulary (`sources` / `extends` / `packages` / `repo_id`) after initial proposal invented a non-existent `scopes:` field
- Pain-point mining complete (payout `.agents/` + cross-harness session transcripts) — findings used to scope the `SessionStart` hook and to validate that per-app session-start skills are no longer needed
- Bootstrap strategy and confidence-closure-v2-slot pitched for spec 2 and agreed structurally
- Schema registry decision: AsyncAPI confirmed, proto deferred
- `po-agents-config` repo creation confirmed as needed; separate repo chosen over in-payout-repo layer home (exercises git-source path early, cleaner review surface)

**In Progress:**
- Three decisions (D1/D2/D3) from sibling review handoff have a session lean but need user confirmation before spec 2 finalizes §6.2-adjacent content. Spec 2 drafting is **not blocked** — it can be written against lean answers and revised on confirmation.

**Not Started:**
- Spec 2 `cross-app-dependency-impact/design.md`
- Plan `payout-agent-config-wave-1/` (directory, PLAN.yaml, TASKS.yaml, plan.md)
- `po-agents-config` git repo (does not exist yet)

## Decisions Made (this session)

- **Separate `po-agents-config` git repo** — not in-payout-repo layer dir. Reason: pressure-tests config-distribution-model two-pass resolution on git sources early; clean review surface for policy changes.
- **Two-wave split** — wave 1 = base + Go service + Next frontends + cluster-manager + active apps; wave 2 = Python variants + less-touched libs. Reason: scope de-risk; avoid authoring layers for apps nobody edits.
- **Retire old-model artifacts at tail of wave 1, not upfront** — user explicit: "retire the old artifacts in wave 1 after new stuff lands." Reason: keep payout functional throughout migration; no period where both new and old are broken.
- **Per-app session-start skills eliminated; one `SessionStart` hook in `workspace/base`** — reason: `workflow orient` + `workflow eligible --json` already supply the 867 context-reestablishment mentions' worth of value. Per-app skill was a pre-workflow-command-era workaround.
- **`po-core-api-se` as composite profile** — `composes: [api, batch, streaming]` + composite-additional `webhook-replay`. Exercises spec 1 §4 composition resolution on a real payout case. Streaming composite-additional captures webhook processing + event publishing bundled into streaming profile.
- **AsyncAPI over proto for event schema source of truth** — user confirmed. Proto deferred to future (binary/wire efficiency doesn't justify redesign cost now).
- **Spec 2 is generic-first, payout as worked example** — user confirmed. Same pattern as spec 1.
- **Confidence closure deferred to spec-2-v2 as a reserved field slot** — user: "needs more thought." Reserve the KG node `confidence` field now; write-through mechanism in a later phase after OTel story stabilizes.
- **Bootstrap is hybrid three-layer** — eager manifest for explicit deps, lazy per-repo CRG for symbol layer (read-time federation), manual-first for implicit layers with CRG-suggested auto-detection. Reason: answers "when is team-scope useful?" with "day 1 for explicit deps"; avoids the massive upfront unification pass; lets implicit layers grow with confirmation.

## Decisions Pending (from sibling review handoff, partially answered this session)

**D1: §2.2 composite-override relaxation scope.**
- Sibling-handoff session lean: *parametric-override carveout* (allow `on_fail` / version-range / parameter overrides with `override_justification:` + corpus check; keep fork-instead for structural changes like review_kind / graph_backend / impact_radius_kind).
- This session's spec 1 locked the strict fork-instead rule in §2.2 with Q1 flagging the parametric carveout as an open question.
- **User has not confirmed.** Next session: pull explicit answer. If parametric carveout accepted, amend §2.2 + Q1 to reflect; otherwise confirm strict stance.

**D2: Q2 (verifier package contract) + Q3 (corpus entry schema) — co-document or separate?**
- Sibling handoff framed these as entangled (gate in §6.2 isn't mechanically specifiable without both).
- This session did not push explicitly on this in spec 1; Q2 and Q3 remain open at end of `app-type-profiles/design.md`.
- **User has not confirmed.** Next session: pull answer. Lean toward co-document because the entanglement argument is correct — a verifier package contract that cannot describe its behavior corpus is half-specified.

**D3: §6.2 gate claim narrowing, or make `cross-app-dependency-impact` a prerequisite?**
- Sibling handoff: the §6.2 gate currently claims "re-run new version against same corpus inputs" but has no mechanism to discover which repos use the verifier and therefore no way to run against real inputs across consumers.
- This session's spec 2 (cross-app-dependency-impact) answers the discovery mechanism — a cross-repo graph that knows which repos resolve which profiles and verifiers.
- **Lean: spec 2 becomes a prerequisite, not a companion, for the §6.2 gate.** Until spec 2 ships, §6.2 should be narrowed to "maintainer-side regression detection against a self-provided corpus." Once spec 2 ships, consumer discovery becomes automatic.
- **User has not confirmed the narrowing.** Next session: either (a) confirm narrowing and amend §6.2, or (b) hold the broader claim and note that it is gated on spec 2 shipping.

**Q5 from spec 1 — multi-profile tasks.**
- Not addressed in sibling handoff.
- Current lean: composites handle this (add a `docs-update` composite to profile a task that touches both code and docs). Left open.
- Lower priority than D1–D3; can be resolved after spec 2.

## Important Context

- **Spec 1 passed adversarial review before commit**, per sibling handoff. Three findings produced D1/D2/D3 above. Do not skip adversarial review on spec 2 — the same failure mode applies (a gate that claims a mechanism it cannot specify).
- **Schema-registry decision is not about payout's current stack** — payout doesn't use AsyncAPI today. The spec writes the abstraction to assume a schema source of truth is AsyncAPI-shaped; adoption is a downstream migration not scoped in spec 2.
- **`po-agents-config` repo GitHub owner is not confirmed.** User will need to specify (likely `NikashPrakash` based on existing agents-config repo in `.agentsrc.json` sources). Wave-1 plan should flag this as a blocking input before any `.agentsrc.json` edits in payout sub-apps.
- **ResumeAgent pressure-test is a worked example in the spec only.** Do not modify ResumeAgent's code or configs; the abstraction is validated by the example, not by a live integration.
- **Old `payout-session-start` skill should be retired, not rewritten.** The workflow commands subsume its value. If any ceremony is worth preserving, it goes into the `SessionStart` hook in `workspace/base`.
- **Wave 1 plan must NOT pre-delete old artifacts** — the user specified new lands first, retirement tasks come at the tail. Enforce this as task ordering in TASKS.yaml (retirement tasks depend on all layer-authoring tasks being completed).
- **Bootstrap `team` scope interaction with scoped-KG four drivers** — scoped-KG v2 defines four event-driven staleness drivers. Spec 2's bootstrap writes to team scope and must respect write-time-only propagation and resolver purity (scoped-KG §5.6 and §2.8). Do not propose a materialization job even if it looks simpler.
- **Confidence field reservation must not trigger staleness in scoped-KG v2.** If confidence writes emit staleness events, every trace observation flaps the KG. Confidence must be annotated (not load-bearing on node identity) — call this out explicitly in spec 2.
- **Workflow state at session end:** only `.gitignore` has uncommitted modification (not touched by this session; leave for the user). All spec-1 work is in commit `5b31008`.

## Next Steps

1. **Resolve D1/D2/D3 with the user.** Acceptance: each decision explicitly answered; amendments queued for `app-type-profiles/design.md`. This can happen in parallel with step 2 — the decisions do not block spec 2 drafting, only its §6.2-adjacent language.

2. **Draft spec 2 at `.agents/workflow/specs/cross-app-dependency-impact/design.md`.** Match the shape of spec 1 (Status/Written/Plan/Related header, numbered TOC, §2 Decisions with Why + Rejected alternatives, worked examples, open questions). Sections per the outline in The Plan above. Acceptance: spec is reviewable; references scoped-KG for team-scope writes; references planner-evidence for per-repo sidecar generation; references workflow-parallel-orchestration for fanout; does not invent new field surface beyond what config-distribution-model and external-agent-sources already define; run adversarial review before commit.

3. **Run adversarial review on spec 2** (subagent, then direct critique). Same pattern as spec 1. Acceptance: at least one load-bearing defect caught or explicit "no defects found" stated.

4. **Get `po-agents-config` repo-creation prerequisites from user:** GitHub owner/org, whether to seed with README + LICENSE, what auth the payout `.agentsrc.json` should use for the git source (SSH vs credential-helper per external-agent-sources §4). Acceptance: user answers; wave-1 plan references concrete values.

5. **Write plan `payout-agent-config-wave-1/`.** Directory with PLAN.yaml + TASKS.yaml + `payout-agent-config-wave-1.plan.md`. Tasks: (a) create po-agents-config repo, (b) seed with layer skeleton, (c) author wave-1 layers + profiles (one task per layer; write_scope = layer file paths; depends_on = skeleton task), (d) author `SessionStart` hook, (e) update payout sub-app `.agentsrc.json` to source from po-agents-config (one task per sub-app), (f) **tail retirement tasks** depending on all author tasks completed: migrate old `.plan.md`/`.loop.md` content into `workflow/plans/<id>/`, delete `payout-session-start` skill, update payout `.agentsrc.json` skills list. Acceptance: plan validates against schema; TASKS.yaml uses block scalars for any free-text `notes:` fields that may contain `: ` (schema-usage.md rule).

6. **Commit spec 2 and the wave-1 plan.** One commit per artifact, per project commit style. Commit messages follow `feat: add <name> spec (status)` pattern per recent log.

## Constraints

- **Do not touch `.gitignore` or other uncommitted files that predate this session.** The only modified file in working tree at handoff time is `.gitignore`, which was not touched in this session.
- **Do not modify spec 1 (`app-type-profiles/design.md`) until D1/D2/D3 are confirmed.** Amendments from the decisions are queued, not speculative.
- **Do not collapse workflow tiers** — spec vs plan vs tasks vs history. A spec accumulating file paths has become a plan; split it.
- **AsyncAPI preference is a schema-registry choice, not a payout migration directive.** Spec 2 must describe the abstraction without requiring payout to adopt AsyncAPI before the spec ships.
- **Bootstrap writes must honor scoped-KG v2 commitments:** write-time-only propagation (§5.6), resolver purity (§2.8), no materialization jobs. These are load-bearing — they survived an adversarial review that caught defects in earlier versions of the scoped-KG spec.
- **Confidence field is annotated, not identity-load-bearing.** Writes to confidence must not emit staleness events in scoped-KG v2. Call this out in spec 2 where the schema is defined.
- **Wave 1 retirement tasks MUST depend on all author tasks** — the user was explicit that retirement happens after new lands. Enforce via `depends_on` in TASKS.yaml.
- **YAML block-scalar rule** (from `rules/dot-agents/schema-usage.md`): any free-text YAML field that may contain `: ` uses `|-` block scalar. Enforce in TASKS.yaml, PLAN.yaml, and any layer files.
- **AgentsRC field lifecycle rule** (same file): if spec 2's command surface necessitates a new top-level `.agentsrc.json` field, the six-step atomic update is required (struct + core mirror + UnmarshalJSON + MarshalJSON + known map + JSON schema). Flag this as an implementation concern in the wave-2 plan if it comes up; wave 1 should not add fields.
- **`po-agents-config` repo creation requires user authorization.** Do not create GitHub repos or push without explicit go-ahead — this is a shared-state action.
- **Adversarial review before commit on spec 2.** Pattern established by spec 1 and scoped-KG. Cheap to run, caught load-bearing defects in both.
