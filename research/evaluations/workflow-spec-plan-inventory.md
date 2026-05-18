# Workflow Spec/Plan Inventory and Gap Audit

**Written:** 2026-04-27
**Scope:** Inventory of `research/articles/`, `research/evaluations/`, `.agents/workflow/specs/`, and `.agents/workflow/plans/` before enriching the KG-adjacent research synthesis.
**Purpose:** identify domain gaps, logical flaws, and platform-specific bias in the current article evaluations after the workflow/spec tree moved forward.

---

## Inventory

### Research Corpus

- `research/articles/`: 19 article extracts.
- `research/evaluations/`: 5 sibling evaluations before this note: workflow orchestration, agent execution, hooks/platform, skills/rules/graduation, lessons/memory.
- `research/articles-evaluation-kg-and-adjacent.md`: evaluates all 19 article extracts, but its frontmatter and synthesis text still said 16 articles / 14 themes in places.
- New untracked article extracts in this working tree: `akshay_pachaar-build-agents-never-forget.md` and `shivsakhuja-skill-graphs-2.md`.

### Workflow Specs

There are 22 markdown spec/design artifacts under `.agents/workflow/specs/`. The load-bearing groups for this research pass are:

- **KG and graph trust:** `scoped-knowledge-graphs/design.md`, `graph-bridge-contract/design.md`, `kg-command-surface-readiness/design.md`, `go-native-code-graph-analysis/design.md`.
- **Workflow and fanout:** `workflow-parallel-orchestration/design.md`, `planner-evidence-backed-write-scope/design.md`, `loop-agent-pipeline/*`.
- **Non-code generalization:** `app-type-profiles/design.md` adds `write_scope_kind: document | artifact`, citation/document graph backends, profile versioning, and behavior-preservation gates.
- **Skill composition:** `skill-tiering-contract/design.md` already turns the shivsakhuja recommendation into a draft contract with `tier`, `calls`, verifier, review gate, and attendance invariants.
- **Audit and plan sync:** `completed-plan-audit-analysis/design.md` and `project-audit-plan-sync-expansion/design.md` define the spec-vs-implementation audit program that the article evaluations did not yet factor into their priorities.
- **Distribution and config:** `config-distribution-model`, `org-config-resolution`, `external-agent-sources`, `platform-dir-unification`, and plugin/resource specs define platform/package boundaries that make Claude-specific recommendations too narrow unless restated as managed dot-agents invariants.

### Workflow Plans

There are 13 plan directories under `.agents/workflow/plans/`.

- **Active:** `kg-command-surface-readiness` has one pending follow-up, `kg-fresh-build-transaction-fix`, after the main readiness tasks were marked complete.
- **Paused:** `refresh-skill-relink`.
- **Completed but audit-relevant:** `loop-agent-pipeline`, `ci-smoke-suite-hardening`, `graph-bridge-command-readiness`, `planner-evidence-backed-write-scope`, `workflow-parallel-orchestration`, `ralph-fanout-and-runtime-overrides`, `plugin-resource-salvage`, `platform-dir-unification`, `error-message-compliance`, `test-archive-p2`.
- **Metadata-incoherent:** `typescript-port` exists as an empty plan directory, and `test-archive-p2` has empty tasks/metadata. These are plan-sync problems, not implementation signals.

---

## Domain Gaps

1. **Non-code work is now a first-class domain, not an analogy.** Earlier evaluations treated research/writing/design as examples to borrow from. `app-type-profiles` makes them pipeline targets with document/artifact scopes, citation graphs, rubric review, and behavior-preserved verifiers. Recommendations about workflow, hooks, and execution should point at profiles instead of hand-editing code-shaped pipeline assumptions.

2. **Plan audit is missing from the research priority model.** The evaluations propose many new primitives, but the workflow tree already says completed plans may be soft-complete, status-drifted, or evidence-thin. Before adding more primitives, the next workflow work should respect the completed-bundle audit queue and plan-sync rules.

3. **Skill tiering has moved from recommendation to draft spec.** The KG-adjacent doc currently treats `tier: atom | molecule | compound` as a future P0. That is stale. The useful research work now is to stress-test the draft's open decisions: task tier inheritance, self-declaration vs inference, compound attendance, and first rollout scope.

4. **KG trust is not evenly operational.** Graph-bridge readiness is completed, but KG command readiness remains active because fresh isolated builds can still fail in the CRG-backed path. Any research statement that says "warm sqlite + CRG already solves this" needs a caveat: code-lane readiness is improving, context-lane ingestion remains deferred, and fresh-build trust is still being repaired.

5. **Research provenance needs profile-shaped verification.** Article evaluations already added trust gates, but there is no explicit `research` profile verifier chain for citations, freshness, and rubric fit. The app-type profile spec provides the right home for that gap.

---

## Logical Flaws to Correct

1. **"Nightly consolidation" must not be confused with KG staleness.** The scoped-KG spec is explicit: staleness is event-driven, while time-based review nudges are separate. A dream cycle can dedupe, propose links, and raise review nudges, but it must not mark facts stale merely because they aged.

2. **Cross-scope contradiction is metadata, not revocation or staleness.** Several article-derived recommendations say "higher-tier wins" or "contradictions resolve by precedence." The canonical scoped-KG contract is sharper: precedence selects the returned answer; cross-scope disagreement is disclosed in `contradictions`; neither side becomes stale.

3. **`author` is necessary but insufficient.** Durable provenance needs at least author, scope/origin, `derived_from`/`cites`, trust tier, and revocation semantics. `author: human | agent` protects edits, but it does not prove truth, freshness, or authority across repo/user/team/org scopes.

4. **Prose-as-title should not break stable IDs.** Human-readable labels are useful, but plan IDs, task IDs, note IDs, and skill IDs are machine contracts. Prefer `title` / display text / aliases over renaming stable identifiers when link durability matters.

5. **Parallel verification is only safe for independent work.** Running verifier/review in parallel with the next implementation is sound only when the next task is in the same non-conflicting `max_batch` and does not consume the unverified output. Otherwise verifier failure causes cascading re-bundles.

6. **Verifier removal requires behavior evidence.** The sullyai critique of over-verification is useful, but removing verifiers from dot-agents should go through app-type/profile behavior-preservation gates and rejection-rate evidence, not intuition.

---

## Platform-Specific Bias to Reduce

- **Say "managed agent rule surface" instead of `CLAUDE.md` or `agents.md` when the recommendation is cross-platform.** This repo renders rules/config for Claude Code, Cursor, Codex, and GitHub Copilot; platform-specific files are outputs, not the canonical policy surface.
- **Say "hook-enforced where the platform supports it; rule-only fallback elsewhere."** Claude Code's hook model is richer than Cursor/Codex/Copilot. Research recommendations must not imply uniform enforcement across every platform.
- **Treat Discord, Obsidian, Linear, CloudWatch, Cognee, and DKG as deployment choices, not patterns.** The portable patterns are marker protocols, graph-backed retrieval, scheduled triage, provenance, and hybrid graph/vector bridge contracts.
- **Prefer profile/version/package language for reusable verifiers and skills.** The newer specs make verifier evolution, distribution, and non-code profiles explicit; cross-platform recommendations should ride those contracts.

---

## Enrichment Targets

- Update the KG-adjacent synthesis counts and current-stack paragraph.
- Add a spec/plan inventory section to the KG-adjacent doc so recommendations are anchored in current workflow state.
- Add a "corrections after workflow inventory" section that separates domain gaps, logical flaws, and platform-specific bias.
- Add compact addenda to sibling evaluations so their recommendations point at profiles, skill-tiering, plan audit, and scoped-KG event semantics.

---

## Second-pass findings (2026-04-27, after re-reading specs end-to-end)

The first inventory pass updated counts, named the major domain gaps, and patched obvious logical flaws. After reading `scoped-knowledge-graphs/design.md`, `app-type-profiles/design.md`, `skill-tiering-contract/design.md`, and the audit specs end-to-end, several couplings remained latent. These are now reflected in `Part F` of each evaluation.

### Couplings the first pass missed

1. **Verifier evolution governs verifier removal.** Recommendations to drop low-rejection-rate verifiers (agent-execution E.4, KG doc C.6) collide with `app-type-profiles/design.md` §6.1–§6.2: removal is a major bump that must pass the behavior-preservation gate against a stored corpus. Evidence is necessary; gate is sufficient.

2. **Cell vs compound vs molecule changes the audit shape.** `completed-plan-audit-analysis/design.md` prescribes one evidence-precedence ladder; `skill-tiering-contract/design.md` §4 distinguishes cell-tier specs (decisions + done criteria) from compound-tier plans (orchestration + evidence). The audit playbook should split into a contract-audit (cell) lane and an execution-audit (compound) lane.

3. **Reweave (plan graph) and KG derivation propagation are one primitive.** Workflow-orchestration W.5 and scoped-KG §2.6 both walk citation edges and emit tags on reachable entries. Different stores, identical shape. A shared propagation walker, parameterized by edge type and store, is correct.

4. **Same-scope vs cross-scope contradiction is two skills, not one.** the_smart_ape's "contradiction protocol" (KG doc P1 #6) collapses two distinct events: same-scope writes auto-stale the older entry; cross-scope disagreements remain fresh on both sides and surface in metadata. Two prompts, two escalation policies, one shared name.

5. **One trust schema, three projections.** `author`, `tier`, `cites`, `scope`, `derived_from`, `corrective_source` should live in `scoped-knowledge-graphs/design.md` as the canonical `KGNote` schema. Lessons (`.agents/lessons/<name>/LESSON.md`) and Claude-Code auto-memory files become projections. Cursor / Codex / Copilot equivalents project from the same warm-store rows. No per-store schema dialect.

6. **`open_questions:` frontmatter must be additive, not migratory.** Three canonical specs (`scoped-knowledge-graphs` §4 with nine, `app-type-profiles` §10 with six, `skill-tiering-contract` §8 with five lettered D1-D5) carry open questions as load-bearing prose with rationale. A `workflow open-questions` extractor must read frontmatter first and fall back to parsing the prose section heading; it must not require rewriting the prose blocks.

7. **The `public` scope is reserved; ingest must respect it.** Scoped-KG §4.5 defers the `public` backend but requires today's resolver and provenance to cover it. Any KG ingest command must default to `--scope public` for external content, never `repo`.

8. **`cross-app-dependency-impact` is a named-but-empty spec slot.** App-type-profiles' completeness note announces the companion spec for cross-repo profile propagation. That is the destination for multi-agent-memory-dkg / TrustGraph "Context Cores" / cross-plan reweave. The KG doc should reserve a Part A.5 group for it once drafted.

9. **The hook capability matrix needs concrete first rows.** Hooks/platform F.1 in this update fills it out. Recommendations that say "enforce by hook" must declare which row they live on and what the rule-only fallback looks like for the platforms below.

10. **Recursive accountability — apply the `research` profile to the research corpus.** `app-type-profiles/design.md` §3 defines a `research` profile (citation-presence + source-freshness + rubric-check). The evaluation docs claim adoption priorities without running themselves through that chain. They should declare `tier: cell` once skill-tiering ships, and they should be verified by the `research` profile on a quarterly cadence.

### Plan status delta

The `.agents/active/research-evaluation-kg-adjacent-enrichment.plan.md` listed all five tasks as completed after the first pass. This second pass is additive enrichment — Part F sections in all six evaluation docs — not a re-do. The plan should be reopened narrowly with a single follow-on task ("second-pass enrichment: contract-level couplings") and immediately closed once these Part F edits land.

---

*Document status: draft evaluation. No code changes; this is research inventory and synthesis only.*
