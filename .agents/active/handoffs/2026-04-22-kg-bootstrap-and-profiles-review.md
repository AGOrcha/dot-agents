# Handoff: KG bootstrap planning + app-type-profiles spec review

**Created:** 2026-04-22
**Author:** Claude Code session
**For:** AI Agent
**Status:** Ready to execute — awaiting three decisions from user before next implementation step

---

## Summary

Two related threads. **(A)** The repo's CRG (Tree-sitter structural code graph) is active and auto-updating; the narrative/semantic KG (decisions, rules, lessons, research claims) is empty. The user wants a *clean* bootstrap that imports only freshest knowledge per domain, but the scoped-KG spec (`specs/scoped-knowledge-graphs/design.md`) hasn't shipped yet. Plan: write a sibling spec at `specs/kg-bootstrap/design.md`, produce a bootstrap **manifest** (markdown, pre-ingestion trust gate), delay ingestion until the scoped-KG spec ships. **(B)** A new spec `specs/app-type-profiles/design.md` was adversarially reviewed this session and found to have three contract-level defects; three decisions are queued for the user before spec 2 (cross-app-dependency-impact) is written.

## Project Context

`dot-agents` = CLI that manages multi-agent workflows (spec → plan → tasks → history) across Claude Code, Codex, Cursor, GitHub Copilot. Primary working directory: `/Users/nikashp/Documents/dot-agents`.

Relevant architectural state:
- **CRG** (code-review-graph): structural graph via Tree-sitter, auto-updates on commit through git hooks, reachable through the `code-review-graph` MCP server. Covers the **code-structure** layer.
- **KG** (knowledge graph, narrative/semantic layer): currently empty. Lives on top of CRG via `NoteSymbolLink`. Scoped-KG spec (`specs/scoped-knowledge-graphs/design.md`) defines four-driver event-driven staleness, scoped precedence (repo→user→team→org→public), write-time-only propagation, resolver purity. This spec just went through two pivots (v1 TTL → v2 event-driven; then v2 contract defects caught in adversarial review and fixed in v2-canonical).
- **Workflow artifact model**: specs own *what/why* (requirements + decisions); plans own *how/in-what-order*; tasks own the queue; history owns the permanent record. Do not collapse tiers.
- **Research corpus**: `research/articles/*.md` (8 articles) + `research/evaluations/*.md` (5 domain evaluations) — load-bearing for Part D trust gate discipline (see Constraints).

Branch: `feature/PA-cursor-projectsync-phase1-extract-293f`. Master is the PR base.

## The Plan

### Thread A — KG bootstrap manifest

Method: **manifest-first, ingest-after-scoped-KG-ships**. Confirmed by user this session.

1. **New sibling spec** `specs/kg-bootstrap/design.md` owning bootstrap policy as a reusable concern (not tied to first-ingestion only). Should specify:
   - Manifest schema (columns below)
   - Trust-gate rule: no tier-1 entry without a derivation cite
   - Domain partition (9 domains, see below)
   - Drop policy (superseded specs, rolled-up impl-results, graduated lessons)
   - Staged rollout: Inventory → Curation → Review gate → (wait for scoped-KG ship) → Ingest

2. **Manifest artifact** (markdown): one row per candidate note across the full `.agents/` + `research/` + `rules/` tree. Columns:

   | Field | Values |
   |---|---|
   | Source path | file in repo |
   | Domain | workflow / execution / hooks / skills / lessons / memory / KG-internal / research / stance / code-convention |
   | Freshness | active / superseded-by:`<path>` / archived-only |
   | Proposed scope | repo / user / org / public |
   | Proposed author | human / agent |
   | Proposed tier | 1 (cited) / 2 (observed) / 3 (heuristic) |
   | Proposed note type | decision / rule / lesson / research-claim / spec-rationale |
   | Has derivation cite | yes / no (and where) |
   | Keep / drop / merge | disposition |

3. **Subagent-parallelized drafting**: User approved using subagents to fan out manifest drafting across domain partitions. Reasonable split = one subagent per top-level domain, then a consolidation pass.

4. **Ingestion** runs after the scoped-KG spec ships (with design.md canonical v2 contract defects resolved) — mechanical once manifest is curated.

### Thread B — app-type-profiles spec review outcomes

Spec was adversarially reviewed. Full critique delivered in conversation. Three decisions now pending from user:

- **D1: §2.2 relaxation scope.** Accept parametric-override carveout to fork-instead rule (allow `on_fail` / version-range / parameter overrides with `override_justification:` + corpus check; keep fork for structural changes), or stay strict fork-instead?
- **D2: Q2+Q3 fold.** Co-document verifier package contract with corpus entry schema in one companion doc, or separate?
- **D3: §6.2 consumer discovery.** Narrow the gate's claim to "maintainer-side regression detection against a self-provided corpus," or make `cross-app-dependency-impact` a prerequisite (not just a companion) for this spec?

## Key Files

| File | Why It Matters |
|---|---|
| `.agents/workflow/specs/scoped-knowledge-graphs/design.md` | Canonical v2 — defines the scopes/drivers/staleness contract that manifest ingestion will target. Must ship before bootstrap ingestion. |
| `.agents/workflow/specs/scoped-knowledge-graphs/spec.1.md` | Tombstoned v1 (TTL model). Preserved for audit. Drop from manifest. |
| `.agents/workflow/specs/scoped-knowledge-graphs/spec.2.md` | Tombstoned v2-draft (event-driven, pre-Codex). Drop from manifest. |
| `.agents/workflow/specs/app-type-profiles/design.md` | Draft spec under review. Three decisions pending (D1–D3 above). |
| `.agents/workflow/specs/config-distribution-model/design.md` | `app_type_verifier_map` field surface — app-type-profiles owns the schema behind that name. |
| `.agents/workflow/specs/external-agent-sources/design.md` | OCI-distribution surface. NOT directly about KG ingestion (verified this session). Referenced by app-type-profiles §2.5. |
| `.agents/workflow/specs/org-config-resolution/design.md` | Layer precedence referenced by Q4 of app-type-profiles. |
| `research/evaluations/*.md` | Five per-domain evaluation docs. Part D trust gate lives here. Load-bearing for any priority-ranking the receiving agent produces. |
| `research/articles-evaluation-kg-and-adjacent.md` | KG + adjacent domain evaluation. Part D trust gate also appears here (added 2026-04-22). |
| `.agents/active/handoffs/2026-04-19-kg-freshness-remaining.md` | Prior handoff context for KG freshness work. Worth reading first. |

## Current State

**Done:**

- Five non-KG evaluation docs written with four-lens risk-profile rubric (`research/evaluations/`)
- Part D trust gate installed across evaluation docs (disciplines priority ranking against evidence strength)
- scoped-knowledge-graphs spec v2 canonicalization: `design.md` now authoritative with both Codex-caught defects fixed (legacy-config diagnostic + same-scope/cross-scope contradiction split)
- spec.1.md and spec.2.md tombstoned in place with frontmatter notices
- app-type-profiles adversarial review completed; subagent findings + my critique delivered; three specific decisions framed for user
- All in-scope work committed in four logical commits (see git log 9401305, d39fecc, 7108261, 5b31008)

**In Progress:**

- Awaiting user decisions on D1–D3 for app-type-profiles before spec 2 is scoped
- Bootstrap manifest plan agreed in method; sibling-spec location (`specs/kg-bootstrap/design.md`) agreed; no manifest content written yet

**Not Started:**

- `specs/kg-bootstrap/design.md` — not yet written
- Manifest itself — not yet started
- Ingestion — blocked on scoped-KG spec shipping

## Decisions Made

- **Manifest-first, ingest-later** — User explicitly chose option (B) from the three-option lean (A=wait, B=manifest-first, C=minimal prototype ingest). Reason: manifest work is valuable independent of the spec shipping; re-authoring markdown is cheaper than re-migrating a store if the spec shape changes.
- **Sibling spec location** for bootstrap: `specs/kg-bootstrap/design.md`, not a section inside scoped-knowledge-graphs. Reason: bootstrap policy is reusable beyond first ingestion.
- **Full-tree manifest scope** — user approved going wide (full `.agents/` + `research/` + `rules/` tree) with subagents helping. Not just active specs.
- **scoped-KG same-scope vs cross-scope contradiction split** — Same-scope disagreements fire write-time staleness driver with `stale.reason: contradiction`; cross-scope disagreements surface as read-time metadata in `contradictions` field only, both sides stay fresh. Rationale: keeps the write-time-only propagation commitment intact while still surfacing cross-scope friction. Rejected Codex's alternatives (drop contradiction from enum, OR add cross-scope materialization job).
- **Legacy config translation is explicit + diagnostic-mandatory** — v2 canonical §3.1 and §5.13 added. Rationale: rejected Codex's "mandatory default drivers" because that would silent-activate staleness for existing users; instead preserve no-staleness behavior *exactly* and make the absence loud through runtime diagnostic.
- **Tombstone-in-place over delete** for spec.1.md and spec.2.md. Rationale: preserves audit trail of the two pivots (TTL → event-driven → contract-defect-fixed).
- **Part D trust gate added to all evaluation docs** — disciplines priority ranking against evidence strength. Single-operator anecdotes cannot produce P0 without downgrade.
- **Lean for app-type-profiles D1 (composite override)**: parametric-override-with-justification for `on_fail` / version / parameter tweaks; fork-instead only for structural changes. This is a *lean from the session*, not a user decision yet.

## Important Context

**Adversarial review loop produces load-bearing insights.** The spec.2.md → design.md pivot on scoped-KG happened because Codex caught two defects a direct read missed. app-type-profiles went through the same loop this session. Don't skip it on specs with architectural commitments.

**spec.1.md / spec.2.md are not delete candidates despite being superseded.** They document the pivot journey. Manifest should mark them `archived-only`, not `drop`.

**The `research/evaluations/` docs use a four-lens risk-profile rubric**: failure mode / evidence strength / reversibility / second-order effects. Preserve this rubric if extending. Part D's role: if evidence strength is "single operator," any P0/P1 label must downgrade or be justified.

**CRG auto-updates; KG ingestion does not.** The receiving agent should not confuse "knowledge graph is empty" with "structural analysis is missing" — CRG is live. The empty thing is the narrative/decision layer.

**app-type-profiles §6.2 gate has no consumer discovery mechanism.** This is the core load-bearing defect. It's entangled with Q2 (verifier package contract is undefined, so the gate's "re-run new version against same corpus inputs" step isn't mechanically specifiable) and Q3 (corpus entry schema isn't specified). D2 + D3 address this.

**spec.2.md was byte-identical to an old design.md in scoped-KG's history.** Verified during canonicalization — design.md is now the v2 with both defects fixed, not a stale duplicate of spec.2.md.

**Working tree cleanup was done by the user this session, not by me.** Don't assume I touched anything under `commands/`, `tests/test-workflow-*.sh`, `.agentsrc.json`, or other active plans' PLAN.yaml / TASKS.yaml.

## Next Steps

1. **Pull D1/D2/D3 decisions from user for app-type-profiles.** Acceptance: user answers each explicitly; edits to §2.2 and Q1 reflect D1; companion-doc structure for verifier package contract + corpus schema reflects D2; §6.2 language reflects D3.

2. **Draft `specs/kg-bootstrap/design.md`** as a sibling spec covering: manifest schema (columns listed above), trust-gate rule (no tier-1 without cite), domain partition, drop policy, staged rollout. Acceptance: spec is reviewable; references scoped-KG design.md for the consumer contract; does not duplicate scoped-KG content.

3. **Wait for scoped-KG spec to reach "ready to plan" status.** Dependency: D1–D3 on app-type-profiles don't block this; the scoped-KG defects are fixed. Acceptance: scoped-KG has plan + task breakdown and implementation is unblocked.

4. **Generate the bootstrap manifest.** Fan out by domain using subagents (one per domain partition). Acceptance: every `.agents/**/*.md`, `research/**/*.md`, and `rules/**/*.md` file is either classified in the manifest or explicitly marked `drop` with reason. Full-tree coverage, not just active specs.

5. **Curate the manifest** through human review. Acceptance: every tier-1 row has a cite; every `superseded-by` pointer resolves; drops have reasons.

6. **Ingestion** — mechanical; runs the curated manifest against the scoped-KG implementation once it lands. Acceptance: resulting KG has zero "orphan note" rows; fresh-only policy confirmed.

## Constraints

- **Do not modify uncommitted code work from other plans.** The `commands/`, `tests/test-workflow-*.sh`, `.agentsrc.json`, and `PLAN.yaml`/`TASKS.yaml` changes for other active plans belong to separate sessions. User committed them this session; do not revert or touch.
- **Do not collapse workflow tiers.** Specs own *what/why*; plans own *how/in-what-order*; tasks own the queue; history owns the record. A spec that's accumulating file paths has become a plan — split it.
- **Do not delete spec.1.md or spec.2.md.** Tombstone-in-place is the intentional pattern for preserving pivot audit trails.
- **Part D trust gate applies to everything produced from `research/evaluations/`.** Any P0/P1 label derived from a single-operator anecdote must downgrade or include explicit justification.
- **KG ingestion must respect write-time-only propagation and resolver purity.** These are §5.6 and §2.8 commitments in scoped-KG design.md. Read-time fanout only for reads; no materialization jobs; no side-effects in resolvers.
- **Do not ingest anything before the scoped-KG spec ships.** This is the explicit user constraint that shapes the staged approach.
- **`research/articles/*.md` are extractions, not claims.** They feed the evaluation rubric; they are not themselves load-bearing sources. Manifest should treat them as domain=research, tier=3 (heuristic) unless they contain a cite chain.
- **YAML plain-scalar colon-space rule** (from `rules/dot-agents/schema-usage.md`): any free-text YAML field that may contain `: ` must use block scalar (`|-`) syntax. Enforce in any bootstrap artifact that emits YAML.
- **AgentsRC field lifecycle** (from same rule file): if any part of this work adds a top-level `.agentsrc.json` field, the six-step update (struct + core mirror + Unmarshal + Marshal + known map + JSON schema) must be atomic.
