# KG & Adjacent Articles — Evaluation Against dot-agents

**Written:** 2026-04-21 (addenda: 2026-04-23 added shivsakhuja Skill Graphs 2.0 as §A.1 entry 5; 2026-04-23 added akshay_pachaar *Build Agents that never forget* as §A.1 entry 6; 2026-04-27 added workflow spec/plan inventory corrections; 2026-05-03 added annimaniac *Six Levels of AI-Pilled Organizations* as new §A.5 group plus Part B theme 7 and §C.10/C.11/C.12; 2026-05-03 added ashwingop *Company Brain* series (Part 2 Factual Memory + Part 3 Interaction Memory) as §A.2 entry 6 plus Part B theme 8 and §C.13/C.14)
**Scope:** 19 articles in `research/articles/` (KG + memory + harness + multi-agent + hooks/platform). Compared against current specs in `.agents/workflow/specs/`, plans in `.agents/workflow/plans/`, proposals in `.agents/proposals/`, lessons in `.agents/lessons/`, and the scoped-KG / graph-bridge / app-type-profile / skill-tiering specs.
**Rubric per article:** core idea → pros → cons/tradeoffs → mapping to our stack with one of three labels:
- **[OVERLAP-SHARPEN]** — we do it, they do it better or differently in a way we should learn from
- **[GAP-ADOPT]** — we don't do it, worth adding
- **[WE-AHEAD]** — we do it better, but they have a quirk worth noting

---

## Part A — Per-article evaluation

### A.1 Group: KG foundations & architecture

#### techwith_ram — *Knowledge Graphs Blazing Fast*

**Core.** KG query = subgraph matching; exponential blowup (6-hop at k=50 = 15.6B paths). Controlled by indexes (six-permutation SPO/SOP/etc.), bitmaps, delta-compressed adjacency lists, BFS/DFS/Dijkstra/A*, bidirectional search (trades d→d/2), Leapfrog Triejoin (worst-case-optimal), cardinality estimation via characteristic sets, subgraph caching, materialized transitive closures + neighborhood summaries, TransE+FAISS for fuzzy lookup, Bloom filters for existence pruning, community-vs-predicate partitioning, federated SPARQL.

**Pros.** Authoritative taxonomy of every knob we'd ever reach for if our KG grows past sqlite-on-disk. Bidirectional search and materialized closures are cheap wins that don't require changing data model.

**Cons.** All of this assumes a formal triple store. Our warm store is row-per-node, not SPO triples — we'd have to restructure to get most of these benefits. Also: these optimizations matter at 10⁶–10⁹ nodes; we have ~34K nodes in CRG. Premature optimization risk is high.

**Mapping.**
- **[WE-AHEAD with quirk]** — we skip the SPARQL ceremony entirely and query via typed Go functions against sqlite. Simpler, plenty fast at our scale. The quirk worth stealing: **materialized neighborhood summaries per node** — precompute "N LinkKinds adjacent, M symbols adjacent, K derivation children" at write time, cache on the node row. Makes `get_impact_radius` near-instant without walking the graph. This costs one integer column per node and maps cleanly onto the §2.6 derivation-propagation machinery in the scoped-KG spec.
- **[GAP-ADOPT — small]** — Bloom filters per scope to skip empty-scope lookups in the resolver. When a query walks repo→user→team→org, most hops return nothing; a 256-byte per-scope bloom on node ids eliminates the backend round-trip entirely.
- **[GAP-ADOPT — longer-term]** — **bidirectional impact search** in `get_impact_radius`. Today it fans out one direction; for "does X affect Y" queries with a named target, meet-in-the-middle cuts work 4 orders of magnitude. Probably not load-bearing yet but the algorithm is small.

---

#### arscontexta — *Claude Code Plugin for Agentic Knowledge Systems*

**Core.** Three-space invariant (`self/`, `notes/`, `ops/`) with per-project naming variation. Six-R pipeline (Record → Reduce → Reflect → Reweave → Verify → Rethink) where each phase spawns a fresh subagent. `/ralph` orchestrator. Four hooks (orient, write-validate, auto-commit, session-capture). 249 research claims with `cognitive_grounding` links.

**Pros.**
- The three-space invariant is the cleanest articulation I've seen of the "identity / knowledge / state" split. Maps well onto what we already do implicitly (`prompts/` + `rules/` = self, KG + notes = knowledge, `active/` + `workflow/` = ops).
- **Reweave** (backward pass that updates prior context after new findings) is the pattern our derivation-propagation machinery needs at the *skill* level, not just the KG.
- `cognitive_grounding` is the concrete shape of "derivation cites" (§5.8 of the scoped-KG spec) — every claim links to the research that grounds it.

**Cons.**
- Conversational setup (20-minute interview → generates architecture) works for personal second brains but is overhead for a CLI/library like dot-agents. We already have `init` and project scaffolding.
- Naming adapts per domain (`notes/` → `reflections/` → `claims/`) — that's a nightmare for cross-project tooling. We should keep naming invariant.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our `.agents/` tree is already close to three-space but the boundaries leak. Plans live in `workflow/plans/` (ops-ish), but impl-results live in `history/` (knowledge-ish). A stricter split (identity/knowledge/ops) would make agent orient hooks cheaper.
- **[GAP-ADOPT]** — **Reweave as an explicit pipeline phase.** Today, when a plan completes, `impl-results.md` gets written and life moves on. Reweave says: walk backward, update prior plan docs, flag decisions whose assumptions no longer hold. This is derivation-propagation applied to plans, not KG notes. Could live as `/reweave` skill or as a loop-close step.
- **[GAP-ADOPT]** — **cognitive_grounding at the lesson/skill level.** Every lesson should cite the specific incident/PR/commit that produced it. Today `LESSON.md` files describe the rule but rarely link to evidence. This is the same pattern as the scoped-KG spec's `derived_from` cites.

---

#### multi-agent-memory-dkg — *From AI Memory Silos to Multi-Agent Memory*

**Core.** OriginTrail DKG v9 as shared verifiable KG across orgs. Five inversions: isolation→collaboration, trust→verification (cryptographic fingerprints), retrieval→reasoning (SPARQL), closed→interoperable (any HTTP-capable agent), rented→owned (wallet-based publishing). Context Oracles resolve conflicts via consensus rather than authority. Claims 60% faster wall-clock, 40% cheaper tokens vs markdown handoffs.

**Pros.** The verifiability idea — cryptographic fingerprint + publisher identity per fact — is the answer to the "where did this come from, can I trust it" question our provenance model hand-waves. Context Oracles as a consensus protocol for contradictions is conceptually elegant.

**Cons.**
- Blockchain/wallet-based publishing is overkill for a team-scale KG. The honest motivation is cross-org coordination where no one trusts each other, which is not our problem.
- 60% / 40% claims are from one coding-swarm benchmark with no methodology published. Treat as directional, not load-bearing.
- SPARQL as the interface is a tax — tooling, learning curve, and our current sqlite store can't serve it.

**Mapping.**
- **[GAP-ADOPT — conceptual only]** — **content-addressed notes**: store a hash of the note's canonical content alongside the note id. Not for blockchain — for deduplication, integrity, and detecting silent edits. Cheap (one column), maps onto the scoped-KG `source-hash` driver directly.
- **[GAP-ADOPT — borrow the pattern]** — **publisher identity per note**: the scoped-KG spec already says every note carries its origin scope. Add "origin agent identity" (which loop-worker produced this? which human?) as a second axis. This becomes useful the moment two agents publish into the same scope and we need to attribute.
- **[WE-AHEAD]** — our `contradictions` field (scoped-KG §3.2) does the same work as Context Oracles without needing a consensus protocol. Precedence + explicit contradiction surfacing > voting.
- **[WE-AHEAD]** — "share the environment, not the data" (covered next in jhleath) is a better answer than DKG for most agent-handoff scenarios. DKG is solving the wrong layer of the problem for us.

---

#### the_smart_ape — *Research Skill Graph*

**Core.** 20-file folder: `index.md` (command center with execution instructions), `methodology/` (frameworks, source-evaluation 5-tier trust system, synthesis-rules, contradiction-protocol), `lenses/` (6 forced angles: technical, economic, historical, geopolitical, contrarian, first-principles), `projects/`, `sources/`, `knowledge/`. Compound mode: open-questions from one project become the next project's index.

**Pros.**
- **The 6-lens forced re-thinking pattern is directly transplantable.** We already have verifier-prompt variants (`unit`, `batch`, `streaming`, `ui-e2e`, `api`) — these are *methodology* lenses. What we're missing is *judgment* lenses for planning (e.g., a contrarian lens that asks "what if this plan is wrong?", a first-principles lens for spec review).
- **Source-evaluation tiers** is the pattern for structured provenance we need. Every claim in the KG should have a tier/confidence. Today `KGNote` has no confidence field.
- **Contradiction protocol as a first-class step, not an afterthought** — "document, don't resolve" is the right default, and maps exactly onto the scoped-KG `contradictions` field.
- **Compound mode** (open-questions become next index) is the same pattern arscontexta calls Reweave and the scoped-KG spec calls derivation propagation — three articles converging on the same idea means it's real.

**Cons.**
- The system is designed for a human operator running a single-question research project. Scaling it to continuous agent work where the "question" is a codebase change is non-obvious.
- 6 lenses is probably 2-3 too many for code tasks. The right number for engineering is probably: *implementation-correctness*, *contrarian* ("what if this bug is a symptom of a deeper problem?"), *first-principles* ("is the abstraction correct?").

**Mapping.**
- **[GAP-ADOPT]** — **planning lenses.** Extend `prompts/verifiers/` (or a new `prompts/lenses/`) with judgment lenses invoked during plan review. Our `self-review` skill is one lens; we're missing contrarian and first-principles. This is a small addition and compounds.
- **[GAP-ADOPT]** — **source-tier / confidence field on `KGNote`.** Add `confidence: high | medium | low` (or a tier enum), track whether a claim came from primary evidence (test result, commit, CI log) vs inference (LLM-derived). Today's `NoteType` field conflates the kind of claim with its trust.
- **[OVERLAP-SHARPEN]** — our `contradictions` idea in scoped-KG is good; what we're missing is a **contradiction-protocol** — the *procedure* for how an agent handles a contradiction when it sees one. The article's 4-step protocol (check basics → find root → document → upgrade to open-questions) is a ready-made skill definition.

---

#### shivsakhuja — *Skill Graphs 2.0 (Atoms / Molecules / Compounds)*

**Core.** Direct response to `the_smart_ape` and arscontexta. Flat skill graphs break past a few hops of dependency depth (agents don't reliably dispatch through dense chains; circular deps make it worse). Replace with explicit three-tier composition: **atoms** (single-purpose primitives, ~deterministic, don't call other skills), **molecules** (2–10 atoms chained by explicit in-skill instructions, minimal runtime agent judgment), **compounds** (orchestrators of molecules where the agent actually gets autonomy). "Brain-RAM" leverage argument: a human context-switching between 5 agents gets ~5 atomic tasks at the atom tier vs ~500 atomic units of work at the compound tier for the same budget. Author's implementation at gooseworks.ai names them capabilities / composites / playbooks. Anecdotal ceiling: ~8–10 molecules per compound before reliability degrades.

**Pros.**
- **Concrete answer to the skill-graph-depth problem** we already flagged for `the_smart_ape`. The fix is not to abandon composition, it's to make composition tiers explicit and push as much composition as possible *into* the skill (reducing runtime judgment).
- **Converges cleanly with sullyai's "decomposition over iteration"** (§A.3): atoms and molecules keep determinism; compounds are the single place judgment is handed off, and compounds are supposed to be few and human-driven. Same principle, different vocabulary.
- **Converges with milksandmatcha's parallel-scope model** (§A.3): the brain-RAM argument *is* the parallelism-of-scope argument reframed around leverage. Three articles now converge on "decompose scope so the human stays at the highest tier their RAM supports."
- **Ready-made vocabulary** (capabilities / composites / playbooks) and a tier contract (atoms=deterministic, molecules=explicit, compounds=autonomy) that is cheap to retrofit — frontmatter and lint, not refactoring.

**Cons.**
- Single-operator report (gooseworks.ai founder, pre-product). The 8–10 molecules/compound ceiling is a guess, not a measurement. Honor §E trust gate.
- The leverage math (5 compounds × 10 molecules × 10 atoms = 500 atomic units) assumes each tier composes losslessly — which is the exact failure mode the article also acknowledges. Treat the 100× number as directional, not a claim.
- "A human probably needs to drive compounds" conflicts partially with our ISP pipeline, which is designed to run compounds mostly unattended with verifier/review gates. We have a stronger story for unattended compound execution than the article gives credit for.

**Mapping.**
- **[GAP-ADOPT — small, P0-tier]** — **tier field on skill frontmatter**. Add `tier: atom | molecule | compound` to every skill in `.agents/skills/` and `~/.agents/skills/`. Expectation per tier: atoms declare no downstream skill calls; molecules declare their called atoms; compounds may invoke molecules with agent judgment. Makes dispatchability and expected determinism legible to both the orchestrator and the human reader. This is a rename + lint pass, comparable in cost to the prose-as-title pass in P0.3.
- **[GAP-ADOPT — rule, P0-tier]** — **"push composition into the skill, minimize runtime decision-making"** as a written principle in `AGENTS.md` / agent rules. This is the same principle sullyai names as "decomposition over iteration" and milksandmatcha calls "scope before parallelism"; Shiv's framing makes the implementation guidance explicit (if a molecule needs runtime judgment to pick between atoms, consider promoting it to a compound or tightening the molecule's instructions).
- **[OVERLAP-SHARPEN]** — our ISP pipeline (orchestrator → impl → verify → review → parent) is a **compound**. Our delegation bundles with pinned write-scope are **molecules**. Today these tiers are implicit; labeling them explicitly tightens the contract: a delegation bundle shouldn't need a compound-tier verifier if the bundle is structured as a molecule. The `tier` metadata above should extend to plan tasks, not just skills.
- **[WE-AHEAD]** — compound-tier reliability: we have explicit verify + review + parent-gate stages that Shiv's article doesn't discuss. The article's claim "a human probably needs to drive compounds" is where our pipeline goes further — verifier/review replace a human reviewer at the compound tier for a bounded class of tasks. Worth stealing back: compounds with >N molecules should be flagged for split, echoing his 8–10 heuristic — but as a lint, not a hard rule.
- **[DEFERRED]** — the leverage math / 100× claim. Do not cite it downstream; the converging *direction* (stay at the highest reliable tier) is the load-bearing part.

---

#### akshay_pachaar — *Build Agents that never forget*

**Core.** A first-principles walk from stateless LLM → Python list → markdown files → vector search → graph+vector hybrid. Each layer fixes the previous pain but reveals a deeper one. Cognitive-science frame (Lilian Weng 2023): `Agent = LLM + Memory + Planning + Tool Use`; long-term memory splits into **episodic** (specific events), **semantic** (facts/concepts), **procedural** (skills/workflows), bridged by **memory consolidation** (repeated specifics → reusable rules). Quantitative evidence: "lost in the middle" drops accuracy >30% when relevant info sits mid-context, so bigger context windows don't fix the problem — structure does. Load-bearing failure case: "Was Alice's project affected by Tuesday's outage?" — three facts `Alice→Atlas`, `Atlas→PostgreSQL`, `PostgreSQL→outage`; vector search ranks the endpoints but the connecting fact mentions neither Alice nor Tuesday and never surfaces. Fix: three-store architecture (relational=provenance / vector=semantics / graph=relationships) with every graph node carrying a corresponding embedding so queries *enter through vectors and exit through graph* (or reverse). Author promotes Cognee (SQLite+LanceDB+Kuzu embedded, Postgres+Qdrant+Neo4j prod, four-call API: `add`/`cognify`/`memify`/`search`) whose `memify()` is an RL-inspired pass that strengthens used paths, prunes stale nodes, auto-tunes edge weights, and adds derived facts.

**Pros.**
- **The Weng three-system taxonomy (episodic/semantic/procedural) is the cognitive-science grounding our `NoteType` enum is missing.** Our decision/rule/lesson/research-claim split is ad-hoc; mapping it onto episodic (lessons, incident logs, impl-results) ↔ semantic (rules, decisions, spec rationales) ↔ procedural (skills, playbooks) gives the enum a principled backbone and makes **consolidation** (episodic→semantic via graduation) a named operation rather than a vibes-based proposal loop.
- **"Lost in the middle" — 30%+ accuracy drop mid-context — is the quantitative citation** our scoped-KG spec has been missing when justifying why the resolver returns ranked, scope-filtered subsets instead of naively unioning everything applicable. Every resolver-contract argument should anchor on this stat.
- **The three-store diagnosis is an honest articulation of what we already half-do.** Our warm sqlite handles node+edge (graph-ish) and some provenance; CRG provides structural code graph; we **deliberately do not embed**. Naming the three dimensions makes our non-choice explicit and prevents drift into Camp 1.
- **The Alice/Atlas/PostgreSQL multi-hop example is the cleanest single illustration of why CRG+KG beats vectors-only.** This should be the motivating worked example in the scoped-KG spec's §2 or in kg-bootstrap design.
- **`memify()`'s four operations (strengthen / prune / auto-tune / add-derived) are near-identical vocabulary to scoped-KG's four drivers** (source-mutation / derivation-mutation / revocation / contradiction / environmental). Converges with Thoth's dream cycle and arscontexta's reweave — three independent articles on the same consolidation primitive.
- **"Enter through vectors, exit through graph"** is the clean one-liner for our future KG+CRG bridge contract. Our current `get_impact_radius` does graph-only; a future vector-in entry point would close the hybrid loop.

**Cons.**
- **Single-operator report with a commercial plug.** Akshay is effectively marketing Cognee. The architectural lessons (three stores, consolidation, multi-hop failure mode) are solid and supported elsewhere in the corpus; the specific Cognee recommendation is not load-bearing. Honor §E trust gate — adopt the diagnosis, don't adopt the tool without independent evaluation.
- **The progression argument (list → files → vectors → graph+vectors) is tidy but glosses over the Camp-1-vs-Camp-2 fork** (witcheer). Akshay treats "add vector search" as an inevitable intermediate step; we deliberately skip that step. Our trajectory is list → files → sqlite warm store + structured graph. Reading his progression literally would nudge us toward adding a vector layer we don't need yet.
- **`memify()`'s "auto-tune edge weights based on real usage"** is untestable from the article — no eval methodology, no ablation. Treat the compound claim ("graph develops its own sense of relevance") as directional.
- **Conflates "consolidation" (episodic→semantic distillation) with "optimization" (edge-weight tuning).** These are separate operations; the article bundles them under `memify()`. Our consolidation primitive should keep them split — distillation is a human-reviewed graduation (kevin's two-author), optimization is a nightly stats pass (Thoth's dream cycle).

**Mapping.**
- **[GAP-ADOPT — spec-level, small]** — **Add episodic/semantic/procedural framing to scoped-KG §2** as the grounding for `NoteType`. One paragraph plus a mapping table (lesson→episodic, rule/decision→semantic, skill→procedural). Makes the graduation pattern (episodic→semantic via human review) a named operation rather than an implicit proposal-loop behavior. Reinforces Part B theme 2 (derivation/provenance) by giving the taxonomy a cognitive-science floor.
- **[GAP-ADOPT — spec-level]** — **Cite "lost in the middle" (30%+ accuracy drop mid-context) in scoped-KG §2.8 (resolver purity) and §3.2 (query behavior)** as the justification for ranked, scope-filtered results over naive applicability union. Pairs with Part B theme 4 (context engineering > iteration).
- **[GAP-ADOPT — spec-level, naming-only]** — **Make the "no embeddings" non-choice explicit** in scoped-KG (alongside §4.6 which already defers semantic propagation). State: "Vector similarity is out of scope; we are a Camp-2 substrate; any future vector layer would be a separate bridge, not an expansion of scoped-KG." This is a one-sentence commitment that prevents drift.
- **[OVERLAP-SHARPEN]** — **`memify()` vocabulary converges with our drivers + Thoth's dream cycle.** Action: in the (future) dream-cycle / consolidation spec (C.4 below), use the four operations (strengthen / prune / auto-tune / add-derived) as the operation vocabulary. Cite all three sources (Akshay, Thoth-via-witcheer, arscontexta) as converging.
- **[GAP-ADOPT — worked example]** — **Steal the Alice/Atlas/PostgreSQL multi-hop example as a motivating fixture** in scoped-KG §2 or kg-bootstrap §1. It's the cleanest single illustration of why we bridge CRG+KG and why flat retrieval fails. Use it verbatim (with attribution) rather than inventing a new one.
- **[GAP-ADOPT — future bridge]** — **"Enter through vectors, exit through graph" as the contract for a future hybrid bridge.** Not for this spec cycle (we have no vector store), but name it in kg-command-surface-readiness or scoped-KG §4 deferred section as the shape of a future read path, so when it comes up in proposals we already know which contract it should satisfy.
- **[WE-AHEAD]** — our Camp-2 architecture + central `.agents/` tree already does what Akshay's "markdown persistence layer" was describing as a problem. The "500K+ tokens on disk, 128K context window" problem doesn't bite us because our warm sqlite + hot markdown split is exactly the "retrieval isn't" answer. What we're missing is the ingestion command (Nyk's gap) — which Akshay reinforces independently.
- **[DEFERRED — trust gate]** — the Cognee endorsement itself, all per-operation claims about `memify` performance, and the specific 14-retrieval-modes architecture. Treat the article as a diagnostic resource, not a shopping list.

---

### A.2 Group: Memory / context substrates

#### claude-obsidian-memory-stack (Nyk) — *3-Layer Memory*

**Core.** Three compounding layers: session memory (`CLAUDE.md` + auto-memory), KG (Obsidian vault + smart-connections/qmd MCPs), ingestion pipeline (`brain-ingest` for video/audio/transcripts). "Prose-as-title" (notes named as claims, not categories) + "wiki-link-as-prose" (links read as sentences). Cowan's 4-chunk active attention limit → KGs compensate for context-window bloat.

**Pros.**
- **Prose-as-title** is a small convention with outsized effect. Our plan files are well-named (`ralph-fanout-and-runtime-overrides`) but our KG notes and lessons files aren't. `LESSON.md` tells me nothing; `LESSON-never-mock-the-database.md` tells me whether to read it.
- **MEMORY.md as a routing document under 200 lines** — we follow this rule (our auto-memory MEMORY.md is 4 lines).
- The three-layer split maps onto our existing stack: session memory (`~/.claude/.../memory/` + `CLAUDE.md`), KG (warm store + hot notes), ingestion (we don't have this).

**Cons.**
- The Obsidian-specific tooling (smart-connections, qmd) is a sidecar ecosystem we don't want to depend on. Point-of-view binds to one editor.
- Wiki-link-as-prose requires human authorship to hit the graceful reading; agent-generated prose rarely clears the bar.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our lessons and note files should adopt prose-as-title. This is a five-minute convention change.
- **[GAP-ADOPT]** — an **ingestion pipeline for external content**. Today, transcripts of Slack/meeting/video never reach the KG. A `da ingest <url|file>` that extracts claims, frameworks, actions and drops them as KG notes (with `derivation: untracked` per scoped-KG §5.8) would close the biggest blind spot in our "what does the agent know?" surface.
- **[WE-AHEAD]** — our warm store with sqlite + typed queries is strictly better than Obsidian-as-database for machine readers. Humans can still open the `.md` files.

---

#### second-brain-needs-two-authors (kevin) — *Two-Author Pattern*

**Core.** Every wiki file has `author: kevin` or `author: agent` frontmatter. `author: kevin` files are *untouchable* by any agent — read-only, link-only, build-around-but-never-overwrite. Agent files are mutable. Graduation mechanism: human reviews an agent file, promotes it to `author: kevin` by editing one field.

**Pros.**
- **Elegantly solves the "agent overwrites my thinking" problem with one frontmatter field.** This is worth adopting verbatim.
- Maps onto our existing `rules/` vs agent-proposed rules distinction: human-authored rules survive `refresh`, agent-proposed ones go through `da review`.
- The **graduation mechanism** is the exact pattern our proposal→review loop implements at the rule level. Kevin's innovation is applying it per-file in the KG.

**Cons.**
- Requires discipline: humans must review and promote, or the agent layer accumulates stale "agent-authored" files.
- One field per file is cheap; enforcement (agents must respect it) is where it breaks. A hook that blocks `Write`/`Edit` on `author: human` files would enforce it.

**Mapping.**
- **[GAP-ADOPT — high priority]** — add `author: human | agent` (or `authority: canonical | derived`) to `KGNote`, to lessons, to plan files. This is the cheapest, most durable provenance primitive in any of these articles.
- **[GAP-ADOPT]** — **PreToolUse hook that blocks edits to `author: human` files** unless explicitly overridden. One Python script in `.claude/hooks/`. Closes the enforcement loop that Kevin's article leaves as trust-based.
- **[OVERLAP-SHARPEN]** — our proposal system is the graduation mechanism at the rule level. We should generalize it: *any* agent-authored artifact should have a graduation path (lessons graduate when approved, plans graduate when archived, KG notes graduate when human-reviewed).

---

#### karpathy-second-brain (Nick Spisak) — *LLM Wiki Pattern*

**Core.** Three layers (raw sources / wiki / schema), three operations (ingest / query / lint). Installed as an Agent Skill that works across 40+ agents.

**Pros.** The ingest/query/lint triad is the minimal command surface for a durable KG. We have query (kg bridge, MCP tools). We don't have first-class ingest or lint.

**Cons.** Mostly a repackaging of the patterns in arscontexta, Nyk, and kevin. The cross-platform Skills install is interesting but we already solve platform distribution differently (via `da refresh`).

**Mapping.**
- **[GAP-ADOPT]** — `da kg lint` as a command surface that runs: broken-wikilink detection, orphan-note detection, stale-citation detection, author-field presence check, contradiction scan. Today we have `kg fresh/warm/build/bridge` but no lint. Lint is the reweave/hygiene primitive.
- **[OVERLAP-SHARPEN]** — our MCP surface exposes query; it does not expose ingest. Add `kg_ingest` as an MCP tool so agents can persist discoveries during a session without going through a human-facing command.

---

#### claude-obsidian-ai-employee (Fraser) — *AI Employee for Business Ops*

**Core.** Same 3-layer pattern as Nyk, applied to business operations (Slack/Gmail/Calendar/Drive via MCP). Client roster + action tracker auto-updated from meeting transcripts.

**Pros.** Illustrates that the pattern is domain-independent. Reinforces MCP-as-source-of-ingestion.

**Cons.** No new architectural pattern. Worth reading for the ingestion-via-MCP-connector angle.

**Mapping.** **[WE-AHEAD]** — dot-agents is strictly more principled about MCP management (centralized distribution, platform-specific rendering) than Fraser's ad-hoc setup. Nothing to adopt.

---

#### witcheer-two-camps — *Memory Backends vs Context Substrates*

**Core.** 450+ memory repos cluster into Camp 1 (fact extraction → vector DB → retrieval: Mem0, MemPalace, Supermemory) and Camp 2 (markdown/graph substrate that compounds: OpenClaw, Zep, Thoth, TrustGraph, MemSearch). "Camp 1 optimizes recall; Camp 2 optimizes compounding." Author predicts "context engineering" replaces "memory" as dominant term in 6 months.

**Pros.**
- **The taxonomy itself is the payload.** Naming the two camps forces clarity about what dot-agents' KG is: we are Camp 2 (compounding substrate). Any proposal that drifts us toward Camp 1 (extract-to-vector-DB) should be treated as a category error unless we're intentionally crossing camps.
- **Thoth's dream cycle** (nightly: duplicate merging at 0.93+ similarity → description enrichment → relationship inference → confidence decay on relations older than 90 days) is the most concrete instance of the "graph improves itself" pattern across the memory/context articles. Post-inventory caveat: in dot-agents this should produce `review_due` / proposed links, not clock-based staleness.
- **TrustGraph's "Context Cores"** — portable, versioned bundles of {domain schemas, KGs, embeddings, sources, retrieval policies}, treated like code (versioned, testable, rollback-able) — this is the right mental model for what dot-agents distributes today and could formalize.
- **MemSearch** (Zilliz-owned, markdown as source-of-truth, vector as derived index) validates our architecture — a vector DB company concluded files are canonical.
- **Zep's `valid_at`/`invalid_at` temporal model** provides driver event candidates for the scoped-KG spec (§2.5) with real-world precedent.

**Cons.**
- The "Camp 1 is wrong" framing is overstated. Memory backends solve a real recall problem; we'd want Camp 1 behavior for e.g. "what did the user say about X three months ago" even if our primary is Camp 2.

**Mapping.**
- **[WE-AHEAD — but formalize]** — we are Camp 2 but haven't named it. Putting the taxonomy in a rule or CLAUDE.md clarifies design direction. "dot-agents is a context substrate, not a memory backend" is a load-bearing one-liner.
- **[GAP-ADOPT]** — **a dream cycle / nightly consolidation job.** Thoth's four phases map directly onto scoped-KG maintenance:
  - duplicate merging (content-hash dedup)
  - description enrichment (summarize a cluster of related notes)
  - relationship inference (propose new `NoteSymbolLink` rows based on co-occurrence)
  - confidence decay on stale relations
  
  Pair this with the scoped-KG spec's "review-nudge" axis: the dream cycle is the process that fires review-nudges and gathers candidate cleanups.
- **[GAP-ADOPT]** — **Context Cores as our distribution primitive.** We already bundle skills/rules/hooks via `da refresh`; formalizing it as a versioned, rollback-able "context core" bundle aligns naming and gives rollback guarantees we don't currently promise.
- **[OVERLAP-SHARPEN]** — our scoped-KG spec uses "drivers" for staleness; Zep's `valid_at`/`invalid_at` is a simpler surface for the same idea. Consider adding `valid_at` as an explicit note field alongside `IndexedAt` — it becomes the signal that a driver has fired.

---

#### ashwingop — *Company Brain* (Parts 2 + 3)

A series by Ashwin Gopinath (CEO, Sentra.app; ex-MIT). Part 1 (memory framing), Part 2 (Factual Memory), Part 3 (Interaction Memory), Part 4 announced as Action Memory. Treated together because Parts 2 and 3 are reciprocal halves of a single architecture.

**Core (Part 2 — Factual Memory).** A real Company Brain is built from the individual outward, not as a central repository. Three categories of question it must answer cleanly: *what exists* (provenance, ownership, freshness), *what changed* (lineage, supersession), *what's connected* (relationships between artifacts). Permission-aware semantic memory grows when individual work crystallizes into team and institutional memory. Durable factual memory is proactive (offers context before asked), role-aware (CEO sees a different layer than IC), and grounded in attribution. Rejects "central archive" as a category error — people work in docs/Slack/meetings/tickets/local notes, and the brain has to meet them there.

**Core (Part 3 — Interaction Memory).** Interaction memory is the layer that turns meetings, messages, calls, and emails into structured organizational reasoning. Where factual memory remembers *artifacts* (what), interaction memory remembers *what happened between people before the artifact existed* — why, what constraint mattered, which assumption was fragile, what was left unsaid. **Interaction is the chain of thought of the organization.** A transcript or summary is insufficient — the hard problem is **interpretation**. The system needs an **ontology**: a set of concepts (decision, commitment, objection, escalation, dependency, assumption, customer pain, owner, precedent, open question) that decides what gets remembered and what gets dropped. The ontology evolves; therefore **the company has to be able to reread its own past**. A casual customer objection becomes evidence of churn risk three months later because the ontology grew. This feeds a **context graph** linking people / teams / customers / projects / commitments / decisions / risks / assumptions / dependencies / time. Goal: proactive surfacing of patterns no individual is holding in their head — same objection across three calls, two teams using conflicting metric definitions, decisions reopened because the original tradeoff was never recorded clearly.

**Pros.**
- **Ontology as a load-bearing primitive is the missing vocabulary in our scoped-KG spec.** Our `NoteType` enum (decision/rule/lesson/research-claim) is ad-hoc; Part 3's ontology — decision/commitment/objection/escalation/dependency/assumption/customer-pain/owner/precedent/open-question — is the conversation-shaped extension that organizational interaction needs. Five of those (decision, commitment, dependency, assumption, open-question) map cleanly onto things our specs/plans already track informally; the rest (objection, escalation, customer-pain, owner, precedent) name organizational-interaction shapes our system has no slot for.
- **"The company has to be able to reread its own past"** is the cleanest single-sentence articulation of the dream-cycle / reweave / managed-compounding thread. It names *why* derivation propagation matters: meaning is not stored once; it's re-derived as the ontology evolves. This validates and sharpens our existing spec's §2.6 propagation work.
- **The multi-lens reading of one sentence** ("we can ship Friday if legal signs off and Acme is okay with the beta limitation" → product/legal/customer/sales/action/executive lenses) is a working illustration of cross-scope contradiction (scoped-KG §3.2): the same fact carries different precedence and meaning per scope, and the resolver has to surface that asymmetry.
- **Permission/boundary tension as central, not detail** is exactly the privacy-boundary-on-traces decision our architecture note §6.9 names but punts on. Part 3 raises the stakes: "if you get the product wrong, it feels like surveillance." Our trace storage and promotion gate must surface this same tension explicitly.
- **The "too little / too much" memory spectrum** is the impact-score gating problem named in plain terms. "Too little forgets, too much surveils" is a one-liner test: every threshold we set should be defensible against both ends.
- **Proactive pattern surfacing** ("notice when the same objection appears in three customer calls", "notice when an owner is implied but never named") is the operational L5 hard test (annimaniac §A.5). The two articles converge on the same behavior from different framings.
- **Permission-aware crystallization** (Part 2: individual → team → institutional) maps directly onto our scope chain (user → team → org) and our promotion-gate dual-motion model.
- **"Provenance, ownership, freshness, relationships" as the four anchors of factual memory** validates our scoped-KG `derived_from` + `IndexedAt` + `contradictions` design choices and names the missing fourth axis explicitly: *ownership* (which team has authority on this fact?). Today our scope chain implies ownership; we should make it a first-class field.

**Cons.**
- **Marketing piece for Sentra.app** — the architecture lessons are real and corroborated elsewhere in the corpus, but the implicit "and we're building this" is doing some of the work. Honor §E trust gate.
- **Conversational/prose framing without a worked schema.** The ontology vocabulary is named but not formally defined; the context graph is described but not specified. Useful as design *grounding*, not as design *contract*. Treat as directional.
- **The "permissions are not a detail" warning is correct but unactionable** — the article says privacy is hard but doesn't name how to operationalize it. Our architecture note must do more than restate the tension.
- **Conflates *capture* with *interpretation*.** Part 3 says "a transcript is not enough; interpretation is the hard problem" — true, but the article doesn't separate the two phases cleanly. For us, capture (ingestion) and interpretation (ontology mapping) are different command surfaces; treating them as one risks shipping a single command that does both badly.
- **No quantitative evidence.** "Most decisions are made in conversation" is plausibly true and unmeasured. Our adoption of the ideas should not depend on this claim being precisely correct.

**Mapping.**
- **[GAP-ADOPT — spec-level]** — **Extend `NoteType` (or a sibling `interaction_label` field) with the Part-3 ontology** in scoped-KG §2 / §3.1: decision, commitment, objection, escalation, dependency, assumption, customer-pain, owner, precedent, open-question. Name them as additive to our existing enum (lesson, decision, rule, research-claim) — the existing entries become a *subset* of the broader ontology. Cross-cite with kevin's two-author pattern (author + ontology = full attribution).
- **[GAP-ADOPT — spec-level]** — **Make "ownership" a first-class scoped-KG field**, distinct from `derived_from` (which is provenance-of-derivation). Today ownership is implicit in scope; promoting it to a named field clarifies who can revise vs who can read. Pairs with the synthesis-spec promotion-gate's "reviewer standing per scope" decision (§6.3 of the architecture note).
- **[OVERLAP-SHARPEN]** — **"Reread the past" is our dream-cycle's load-bearing motivation, not a side-effect.** When we draft the consolidation spec (C.4), it should *open* with this framing rather than treating reweave as a hygiene pass. Update C.4 narrative accordingly. Cross-cite with arscontexta (reweave) and akshay_pachaar (memify) — three converging articles on the same primitive.
- **[OVERLAP-SHARPEN]** — **The cross-lens contradiction (one sentence, six lenses) is the worked example our scoped-KG §3.2 is missing.** Steal verbatim (with attribution) as the motivating fixture for cross-scope precedence + contradiction surfacing.
- **[GAP-ADOPT — architecture-note]** — **Add a "memory boundary" axis to the impact-score formula in §3 of the architecture note.** Today the formula gates on consumers + scope_weight + contradictions + provenance_tier. Part 3's "too little forgets, too much surveils" suggests an *attention/exposure* axis: how many readers will this fact be exposed to via auto-promotion? Higher exposure → higher review threshold. This is structurally similar to scope-weight but distinct — scope-weight is about *authority*, exposure is about *reach*. Worth naming separately.
- **[GAP-ADOPT — architecture-note]** — **Privacy boundary at scope-promotion** is no longer deferable. Part 3's "people will not trust it with the most important context" if permissions get the product wrong escalates §6.9 from "decide later" to "decide before any cross-tool ingest ships." Specifically: traces from `user`-scope cannot auto-propagate to `team`/`org` without a redaction pass; the redaction contract is part of the synthesis spec.
- **[GAP-ADOPT — research-tier]** — **Capture vs interpretation as two distinct command surfaces.** Capture = `kg ingest <source>` (writes raw notes with `derivation: untracked`). Interpretation = a separate pass that applies the ontology and may write new notes with `derivation: ontology-pass:<rules>`. Today we'd be tempted to bundle both into ingest; Part 3's framing argues for separation so the ontology can evolve without re-ingesting raw sources.
- **[OVERLAP-SHARPEN]** — **"Proactive surfacing of patterns no individual holds in their head"** is the L5 hard test (annimaniac §A.5) re-stated. Reinforces C.12 (rename to "managed compounding") and L5's behavioral acceptance criterion. The two articles converge — we are not over-claiming when we say this is the goal.
- **[WE-AHEAD]** — our scope chain (user → team → org) + promotion gate is a more concrete operationalization than Part 2's prose-level "individual outward" framing. We should stop apologizing for our model's specificity.
- **[DEFERRED — not now]** — the agent-trace ingestion thread ("agents matter, agent traces will matter more over time"). Real but not load-bearing for our current synthesis. When the execution-telemetry pillar (§1.6 of the architecture note) lands, agent traces become a first-class scope; until then, defer.

---

### A.3 Group: Execution / harness

#### thealexker — *Harnesses Are Everything*

**Core.** Three levers for harness quality: lean .md files via progressive disclosure (skills loaded by name+description, full body only when relevant), R.P.I. prompting (Research → Plan → Implement as disciplined phases), subagent patterns (fan-out for breadth, pipeline for depth). "Instruction budget" → LLMs hit a "dumb zone" past a few hundred instructions.

**Pros.**
- **"Instruction budget" is the frame we should adopt for CLAUDE.md design.** We already lean progressive disclosure on skills; we're sloppy on CLAUDE.md.
- R.P.I. maps onto our spec→plan→implementation lifecycle almost perfectly. Worth naming explicitly.
- MCP tool search on Claude Code reducing context by 85% validates that we should push MCP tools to be search-discoverable rather than loaded-at-startup.

**Cons.**
- The article is prescriptive about "human-written > LLM-generated" for system prompts, citing 20% perf degradation from LLM-written prompts. That directly contradicts arscontexta's conversational-setup approach. Our stack leans neither way — rules/skills are usually LLM-drafted then human-edited, which probably gets most of the benefit without most of the cost.

**Mapping.**
- **[OVERLAP-SHARPEN]** — audit our project's `CLAUDE.md` (currently includes 4 .md files in instructions) against the instruction-budget principle. Some of that is load-bearing; some is leakage.
- **[GAP-ADOPT]** — **explicit R.P.I. pattern in `agent-start` or a new `rpi` skill**: when starting a non-trivial task, force the three-phase structure. Our plan-mode default gets us partway there; R.P.I. formalizes it.
- **[GAP-ADOPT]** — our MCP tools are already dynamically loaded via `ToolSearch`, but our *skill* descriptions are only as good as we write them. A lint check on skill descriptions (keyword-rich, specific) would make search more reliable.

---

#### sullyai — *Your LLM Pipeline Is Slow Because Your Agents Do Too Much*

**Core.** In a 100K+ production deployment, they replaced a monolithic draft+judge+refine loop with parallel focused section agents + single QA pass. p50: 37s → 7.5s, p95: 100s+ → 16.3s. Quality held or improved. **Core claim: context engineering and iteration are *substitutes*, not complements.** If your pipeline has a correction loop, ask whether the loop is compensating for an overloaded context.

**Pros.**
- **Production-validated at scale.** The strongest empirical evidence in the set.
- **"Context engineering and iteration are substitutes"** is a principle worth tattooing. Our loop-worker + verifier + orchestrator pattern leans on iteration where we could lean on decomposition.
- Uniform agent interface (orchestrator doesn't know what kind of agent it's calling) + dynamic output contracts (per-request schemas) are two design patterns we could reuse.
- Ablation finding: "Sections improved the draft by +0.23 with the judge, +0.33 without it" — the judge was often making things worse. This validates skepticism about our own review/verifier layers.

**Cons.**
- Decomposition granularity is "empirical, not derived from a formal framework" — they admit they don't know how to pick it principled. We'd have the same problem.
- Their fan-in is easy because sections don't overlap. Code tasks often do overlap (two tasks touching the same file). Our `workflow-parallel-orchestration` plan already handles conflict detection, which theirs doesn't need.

**Mapping.**
- **[OVERLAP-SHARPEN]** — audit our verifier + review + parent-gate stages per the "is this loop compensating for overloaded context?" lens. The ISP (implement → verify → review → parent) pipeline is exactly the shape sullyai replaced. We should be prepared to answer: would a well-decomposed fanout eliminate the verifier stage?
- **[GAP-ADOPT]** — **dynamic output contracts per delegation bundle**. Today our bundles have free-form `write_scope` and `verification_required`. Adding a per-task output schema (what sections/files the task must produce, typed) would make fan-in deterministic and enable automated merge-back.
- **[WE-AHEAD]** — our `impl-agent` / `verifier` / `review-agent` already present a uniform interface to the ISP skill. We have that pattern.
- **[GAP-ADOPT]** — **the diagnostic question itself** codified as a rule: "If a plan has an iteration loop, the first review asks: is this compensating for an overloaded context? Split the task before adding another iteration."

---

#### milksandmatcha — *Single-Agent AI Coding Is a Nightmare*

**Core.** Five restaurant-kitchen patterns for multi-agent coding:
1. **Prep Line** — fan-out parallel variations, human picks best (design exploration)
2. **Dinner Rush** — swarm: each agent owns a distinct file/module (no shared writes)
3. **Courses in Sequence** — waves where each wave depends on the previous; within a wave, parallel (our model)
4. **Prep-to-Plate Assembly** — sequential pipeline, state in files + task queues (also our model)
5. **Gordon Ramsay** — verifier agents (code reviewer + visual/functional tester) run parallel to the builder; flag issues back

Benchmark: single-agent 36.5min/12 interventions/100% fail vs multi-agent 5.2min/2 interventions/first-try success.

**Pros.** Our `workflow-parallel-orchestration` is literally Courses in Sequence. Our ISP pipeline is Prep-to-Plate. We've independently discovered two of these five patterns.

**Cons.** The Gordon Ramsay pattern separates builder from code reviewer; we have a verifier + review stage but they run *after* the builder, not *in parallel with*. That's a real difference.

**Mapping.**
- **[WE-AHEAD]** — Courses/Waves and Prep-to-Plate are exactly our existing patterns; we've already named them `wave` and `fanout`.
- **[GAP-ADOPT — biggest one from this article]** — **parallel verification (the Gordon Ramsay pattern).** Today our ISP runs: impl → verifier → review sequentially. Running verifier and review *in parallel* with the next task's impl, gated by their completion before fan-in, could cut wall-clock without changing correctness guarantees. This is a small change to the ISP skill and a potentially big speedup.
- **[GAP-ADOPT]** — **Prep Line pattern for design exploration.** Today when we have a judgment call (which architecture? which approach?), we discuss with the human once and commit. Prep Line says: spawn N candidates in parallel, present all N, human picks. For exploratory work (lens-style reviews, naming decisions, schema shapes), this is a better fit than sequential deliberation. Could be a `/explore` skill.

---

#### codex-multi-agent-swarms — *Swarm Playbook Lvl 1*

**Core.** Ambiguity is the enemy. Swarm Waves (one subagent per unblocked task, waves bounded by dep map) vs Super Swarms (total parallelism, let orchestrator resolve conflicts). Front-load subagent context with a full template ([ID], description, acceptance, validation, instructions). Use large models for orchestration.

**Pros.** We already do Swarm Waves (via `workflow eligible` + fanout). The article's template for subagent prompts is a checklist we can audit our own bundle prompts against.

**Cons.** Super Swarm pattern assumes conflict resolution is "adept"; in reality this is where our merge-back machinery earns its keep. We're more principled about conflicts.

**Mapping.**
- **[WE-AHEAD]** — our dependency graph + `workflow eligible --json` does exactly what the article calls out as missing from most swarm setups. Our conflict-detection in `tests/test-workflow-conflict-detection.sh` is more principled than Super Swarm's "orchestrator handles it."
- **[OVERLAP-SHARPEN]** — audit our delegation bundle prompts against the article's template. Specifically check: "Related tasks: [tasks that depend on or are depended on by this task]" — do our bundles include both upstream and downstream task context, or just upstream?

---

### A.4 Group: Automation / coordination

#### openclaw-hermes — *Supervisor Pattern*

**Core.** Two bots (work agent + supervisor) in a dedicated Discord channel. Four intent markers ([STATUS_REQUEST], [REVIEW_REQUEST], [ESCALATION_NOTICE], [ACK]) with strict rules: one marker per message, one @mention, ACK is terminal, max 3-message chains. Supervisor never generates work content, only verifies and routes.

**Pros.**
- **Strict termination logic prevents infinite loops.** The [ACK]-is-terminal rule is the single most important design choice in the article.
- The supervisor-never-does-work constraint prevents role drift — Hermes starts generating content alongside OpenClaw otherwise.
- Freeing human attention from "ops mode" is the real win. Maps onto our agent-as-operator ambition.

**Cons.** Discord-channel-as-protocol is a deployment choice, not a pattern. The pattern is the marker protocol + termination rules.

**Mapping.**
- **[OVERLAP-SHARPEN]** — we have an analogous protocol in `workflow eligible` / `fold-back create` / `checkpoint` / `advance`, but it's not a bounded conversation pattern. An explicit **intent-marker analog for agent-to-agent handoff** within our stack (not Discord) would formalize the existing implicit protocol.
- **[GAP-ADOPT]** — the supervisor-never-does-work constraint as an explicit rule for our orchestrator. Today the orchestrator in ISP/orchestrator-session-start has broad latitude; naming and enforcing "orchestrator does not implement the delegated slice" (which the orchestrator-session-start skill already says) via a hook would tighten the separation.

---

#### claude-code-hooks-automation — *8 Automation Hooks*

**Core.** PreToolUse and PostToolUse hooks for: auto-format, block dangerous commands, protect sensitive files, run tests on edit, block PR without tests, scan secrets, lint, log tool calls. Exit code 2 blocks execution on PreToolUse.

**Pros.** Concrete patterns. We already manage hooks centrally via dot-agents.

**Cons.** Entirely mechanical; the article doesn't teach anything we don't already know.

**Mapping.**
- **[WE-AHEAD]** — our central hook distribution is strictly better than per-project hook config. Nothing to adopt architecturally.
- **[GAP-ADOPT — small]** — the **"block edits to `author: human` files" hook** (from the second-brain-two-authors pattern) would slot into this framework directly. Also: a **"block write to archived plans"** hook would prevent accidental edits to `.agents/history/` (we've had bugs from this).

---

#### jhleath — *Agents Share Environments, Not Data*

**Core.** Agents that work on large contexts shouldn't pass data via S3 uploads (the 2015–2025 pattern). Instead, share the disk/filesystem as a server (`diskId` → bash tool anywhere in the world). The environment includes specialized binaries, documents, SQLite tables, context files. Hand-off in constant time regardless of location.

**Pros.**
- Correctly identifies that agent context is not a blob to copy but an environment to share.
- For a future dot-agents where team members run the same plan from different machines, sharing `.agents/` as a mounted environment is cleaner than syncing via git every step.

**Cons.**
- Requires infrastructure (Archil's Serverless Execution) we don't own. Self-hosting the pattern is expensive.
- Our current scale — one person, one machine, git as handoff — doesn't hit the pain.

**Mapping.**
- **[GAP-ADOPT — future, not now]** — worth naming as a future architectural lane. "When dot-agents needs team-scale agent handoff, consider shared-environment (worktree + shared disk) over shared-data (git sync)." Not a near-term spec.
- **[OVERLAP-SHARPEN]** — our `isolation: "worktree"` pattern for Agent tool invocations is a tiny instance of this idea at the local level. We could make it more first-class.

---

#### intuitiveml — *AI-First Strategy*

**Core.** 25-person company, 99% of production code AI-written, 3-8 deploys/day. Monorepo unified so AI can see everything. Six-phase CI/CD (Verify → Build → Test Dev → Deploy Prod → Test Prod → Release). Claude does three parallel PR review passes (quality/security/dependencies). Self-healing loop: every 9 AM, Claude queries CloudWatch, triages errors, auto-generates Linear tickets. Architect vs Operator engineer roles.

**Pros.**
- **"Monorepo = legible to AI"** is the principle we've internalized with our single `.agents/` tree.
- **Self-healing loop with per-day cadence** is the pattern our `autonomous-loop-dynamic` sentinel gestures at but hasn't been deployed for.
- The Architect/Operator split maps onto orchestrator/loop-worker in our stack.

**Cons.** A 25-person startup willing to burn senior engineers is not our deployment context. The pattern is real; the social cost it names (senior engineers questioning their value) is a warning.

**Mapping.**
- **[WE-AHEAD]** — our spec/plan/tasks/history hierarchy is more disciplined than CREAO's ad-hoc Linear-based triage.
- **[GAP-ADOPT]** — **a scheduled auto-triage job** that runs over `.agents/active/fold-back/` observations, clusters them, and proposes plan updates. This is the agentic analog of the CREAO 9 AM health check, applied to our workflow artifacts. Maps onto our existing `schedule` skill.

---

### A.5 Group: Adoption maturity / organizational frame

#### annimaniac — *Six Levels of AI-Pilled Organizations (L0–L5)*

**Core.** Ann Miura-Ko (Floodgate) argues "AI-pilled" is being used as a binary identity tag when in practice companies sit on a six-level autonomy spectrum (L0–L5) modeled on the SAE AV self-driving levels. A four-question lens — **what can AI see, what can AI do, who can extend the system, how has the org changed** — produces stable answers per level. L0 is theater (announcements ≠ adoption); L1 is personal productivity (heroes whose workflows leave with them); L2 is team workflow (silos that don't connect); L3 is organizational infrastructure (cross-system queryable, non-engineers *author* skills); L4 is a compounding operating system (agents update agents, skills marketplaces propagate wins, **managed compounding** with lifecycle/observability/evaluation); L5 is virtually self-driving (notice → synthesize → decide → act → escalate → update shared memory; aspirational, doesn't exist yet). Each level has a hard test (proof of capability) and a common false positive (the failure mode that gets mistaken for the real thing). The killer line: "At L4, the system improves because humans direct it to. At L5, because it notices it should."

**Pros.**
- **The L0–L5 ladder is the cleanest external articulation of the maturity gradient our four-spec stack is implicitly building toward.** Their L3 description — "Core systems of record exposed via CLI / MCP / well-defined APIs and integrated into a view on which agents can act and not just observe" — is verbatim what `scoped-knowledge-graphs` + `graph-bridge-contract` + `kg serve` are building. Their L4 description — "agents update agents, skills marketplaces propagate wins and remove duplicate efforts" — is verbatim what the agent-context-resolution architecture note describes. Their L5 six markers (notice/synthesize/decide/act/escalate/update-shared-memory) are the literal control flow our dispatch contract specifies.
- **The four-question lens is a usable audit framework** — applicable to dot-agents itself ("what can our agents see at repo scope vs team scope?", "who can author skills, not just consume them?", "how has the workflow changed?"). Maps directly onto our scope chain × tier dispatch matrix.
- **L4's "managed compounding (lifecycle, observability, evaluation), not chaotic proliferation"** is external validation of our promotion-gate / dual-motion (auto + peer review) approach. The exact failure mode she names — "agent sprawl, the factory clogs without compaction discipline" — is what our crystallization protocol prevents.
- **"Asymmetric maturity across the four questions"** is a real insight. A company can have great visibility (high "see") and weak action authority (low "do") — the asymmetry tells you where the next intervention focuses. For dot-agents this maps onto: scoped-KG read API can be at L4 while skill-tiering enforcement is still at L2. We can ship per-axis.
- **The "common false positive" pattern is a drafting tool, not just description.** Each spec we write should declare its hard test AND its common false positive. The false positive list catches the kind of plausible-looking failure that wouldn't be flagged by our success-criteria field today.

**Cons.**
- **VC-essay framing** — written for a portfolio audience, optimized for shareability over operational precision. The level definitions are evocative but not crisp enough to use as acceptance criteria without translation work.
- **The AV analogy strains at L4–L5.** SAE AV levels are about *vehicle* autonomy with a single driver-attention contract. Organizational autonomy has many concurrent decision loops at different scopes — the linear ladder doesn't capture that. Risk: importing the AV framing too literally pushes us toward "is dot-agents at L3 yet?" when the more honest answer is "different surfaces are at different levels."
- **L5 is admittedly speculative** ("the caveat that I realize L5 does not exist yet"). The six markers are fine as a research target; treating them as a roadmap commits us to a destination nobody has reached.
- **"Non-engineers ship production tools" as the L4 marker** is a deployment-context claim, not architectural. For a CLI library like dot-agents, the analog is "non-author humans can promote skills/lessons through the proposal system" — which we already half-have.
- **No methodology or evidence for the level boundaries.** Levels are illustrative not measured; the false positives are claimed not surveyed.

**Mapping.**
- **[OVERLAP-SHARPEN]** — **adopt the four-question lens as a self-audit framework** for the agent-context-resolution architecture note. Each anchor spec (scoped-KG, skill-tiering, app-type-profiles, dispatch contract) declares which of the four questions it answers and at what target level. Concretely: scoped-KG owns "what can AI see" at L3+ (cross-system queryable). Skill-tiering owns "what can AI do" at L3 (bounded action) and gates the L3→L4 transition (delegated authority). App-type-profiles owns "how has the org changed" by naming the verifier chain per app type. Dispatch contract owns the L4→L5 leap (the system *notices* it should improve).
- **[GAP-ADOPT — spec-drafting convention]** — **add "hard test" + "common false positive" fields to spec frontmatter or §0**. Specs already have `success_criteria`; adding the *false positive* (what looks like success but isn't) is what makes the criterion auditable. This is a one-line drafting rule, big effect on review quality. Apply retroactively to scoped-KG, skill-tiering, app-type-profiles before they graduate to plans.
- **[GAP-ADOPT — synthesis spec acceptance criterion]** — **L5's hard test as the acceptance criterion for `agent-context-resolution`**: "What important thing did the system notice, decide, act on, and learn from recently without a human initiating the process?" If the dispatch contract is doing its job, the orchestrator should be able to point at one such event. This is a stricter criterion than "tests pass" — it's a behavioral acceptance test.
- **[GAP-ADOPT — positioning]** — **dot-agents documents its target levels per surface.** Today our docs don't say "this is what L3 looks like in our stack." Adding a one-section table to the top-level README — "scoped-KG read = L3 target, skill graduation = L3 today / L4 target, agent dispatch = L2 today / L4 target" — gives users a map. The framing is borrowed; the mapping is ours.
- **[OVERLAP-SHARPEN]** — **"managed compounding"** is a better phrase than our current "promotion gate / crystallization." Replace the in-spec terminology in `agent-context-resolution` with "managed compounding" because (a) it's the term the broader industry is starting to use, (b) it foregrounds the *lifecycle* aspect (observe, evaluate, retire) rather than just the gate aspect.
- **[OVERLAP-SHARPEN]** — **the L4→L5 leap framing ("at L4 system improves because humans direct it; at L5 because it notices it should")** sharpens our escape-hatch language. The escape hatch is not just "agent pauses for human review"; it's the specific signal that distinguishes L4 from L5. For our dual-motion gate: auto-promotion within threshold = L4 behavior; promoting because the system *noticed* a contradiction without being asked = L5 behavior. We commit to L4 as the immediate target and name L5 as the deferred axis.
- **[WE-AHEAD]** — **the proposal/review system is a real instance of L4 "managed compounding."** Most companies described in the article have nothing like a structured async review for knowledge changes. We have it for global rules and are extending it for KG promotion. Worth citing in the agent-context-resolution note as evidence that the architecture is feasible, not just aspirational.
- **[DEFERRED — trust gate]** — the specific level *boundaries* are not load-bearing. We borrow the *framework* (four questions, six levels, hard test + false positive) and define our own boundaries against our scope chain × tier matrix. Don't try to reverse-engineer "is dot-agents at L3" without independent measurement.

---

## Part B — Synthesis against our stack

### Our current stack in one paragraph

dot-agents is a Camp 2 (context substrate) system with: a unified `.agents/` tree separating identity/knowledge/ops partly (specs + rules + skills + prompts are identity-ish, KG + lessons + history are knowledge, active + workflow are ops); a single-process orchestrator with fanout-to-delegation-bundles for write-scoped implementation; a warm sqlite KG + hot markdown notes + code-review-graph (Tree-sitter) connected via MCP; bridge query surfaces whose product contract is completed but whose KG command readiness still has one active fresh-build transaction fix; an ISP pipeline (impl → verify → review → parent); a proposal/review loop for human approval of changes to shared resources; central config/hook/rule/skill distribution across Claude Code, Cursor, Codex, and GitHub Copilot. Current workflow specs now include scoped-KG (canonical draft), graph-bridge-contract (completed), app-type-profiles (non-code/profile versioning draft), skill-tiering-contract (draft born from this research), completed-plan-audit-analysis, and project-audit-plan-sync-expansion. Recent plans moved workflow-parallel-orchestration, planner-evidence-backed-write-scope, ralph-fanout-and-runtime-overrides, and graph-bridge-command-readiness to completed, but the audit specs warn that completed does not automatically mean verified-complete.

### Themes across the 19 articles

1. **Camp 2 is the winning direction and the industry is converging on it.** witcheer's two-camp frame; Zep's rebrand "memory → context engineering"; MemSearch's "files are source of truth, vectors are derived"; TrustGraph's Context Cores; Thoth's dream cycle. We're already in Camp 2; we should name it explicitly and let it steer future proposals.

2. **Derivation / provenance is the load-bearing primitive for long-lived systems.** arscontexta's `cognitive_grounding`, kevin's `author:` field, the_smart_ape's source tiers, multi-agent-memory-dkg's cryptographic fingerprints, scoped-KG's `derived_from` cites — five articles independently converge on "every claim must cite its evidence." Our `KGNote` currently has no confidence, no author, no cite field.

3. **Compounding / graph-improves-itself is a nightly process, not an inline one.** Thoth's dream cycle, arscontexta's reweave, claude-obsidian's self-improving graph, and now akshay_pachaar's `memify()` (strengthen used paths / prune stale nodes / auto-tune edge weights / add derived facts) — four converging articles. Our scoped-KG spec commits to "propagation is write-time"; we should also have a "consolidation is nightly" lane.

4. **Context engineering > iteration.** sullyai's core finding, echoed in thealexker's R.P.I., milksandmatcha's decomposition patterns, codex-multi-agent's front-loading, shivsakhuja's skill-tiering ("push composition into the skill, minimize runtime decision-making"), and now akshay_pachaar's "lost in the middle" quantitative evidence (30%+ accuracy drop when relevant info sits mid-context). Six independent sources converge on the same principle: the first review of any correction loop asks "is this compensating for overloaded context?" Shiv's contribution is the concrete vocabulary — atoms/molecules/compounds — for *where* in a composition tree a given task should live; Akshay supplies the citable stat.

5. **Human-agent authorship boundary is a durable, simple primitive.** kevin's one field. Our proposal/review loop is this pattern at the rule level; we should generalize it per-artifact.

6. **Cognitive-science taxonomy (episodic / semantic / procedural) is a better floor for memory-type enums than ad-hoc categories.** Akshay via Weng 2023, reinforced by the "consolidation = episodic→semantic distillation" pattern that appears in Thoth's dream cycle, arscontexta's reweave, and our existing proposal/review loop (which is consolidation-via-human-review for rules). Our `NoteType` enum should be re-grounded on this taxonomy; our graduation mechanism (lesson→rule) is already the episodic→semantic bridge, just unnamed.

7. **Maturity ladders + four-question lenses are the right shape for declaring our target.** annimaniac's L0–L5 + see/do/extend/changed lens, paired with shivsakhuja's atom/molecule/compound (artifact-level autonomy) and arscontexta's three-space invariant (identity/knowledge/ops), gives us three axes that compose: scope (where), tier (how autonomous), and adoption level (how integrated). Our four anchor specs — scoped-KG, skill-tiering, app-type-profiles, agent-context-resolution — map onto these axes and should declare their target level explicitly. The L4 phrase "**managed compounding** (lifecycle, observability, evaluation), not chaotic proliferation" is the cleanest external articulation of our promotion-gate / crystallization design and is worth importing as the in-spec terminology.

8. **Ontology is the load-bearing primitive that decides what gets remembered.** ashwingop's Part 3 names this directly: an ontology is the set of concepts and relationships a system uses to make sense of a domain. In a company, it decides whether something in conversation is a decision, commitment, objection, escalation, dependency, assumption, customer pain, owner, precedent, or open question — and those labels decide what survives capture. Our `NoteType` enum is implicitly an ontology, but it's coded for code-shaped artifacts (decision/rule/lesson/research-claim) and missing the conversation-shaped labels organizations need. Reinforced by the_smart_ape's source tiers, arscontexta's `cognitive_grounding`, kevin's authorship axis, akshay_pachaar's episodic/semantic/procedural — five articles converge on "the labels you choose at write time decide what's recoverable at read time." Two consequences: (a) extend `NoteType` with the conversation-shaped labels; (b) commit explicitly to "the ontology evolves; the system must reread its own past" as the dream-cycle's load-bearing motivation, not a hygiene pass.

### What we do well (and should keep)

- **Unified artifact tree with lifecycle tiers** (`workflow/specs/` → `workflow/plans/` → `active/` → `history/`). arscontexta's three-space is a cleaner articulation but we have the bones.
- **Central hook/rule/skill distribution across platforms** — strictly ahead of per-project setup.
- **Workflow-aware orchestration** — dep graphs + conflict detection + fanout > Super-Swarm-style "orchestrator figures it out."
- **sqlite + typed queries for KG** — strictly better than Obsidian-as-DB for machine readers. Humans can still open files.
- **Proposal/review as a first-class mechanism** — the graduation idea applied to global resources.

### What we miss (priority-ordered gaps)

**P0 — smallest change, biggest leverage:**
1. **`author` / `authority` field on KGNote, lessons, plan files** (kevin). One frontmatter field. PreToolUse hook to enforce. Immediate effect.
2. **Decomposition-over-iteration rule** (sullyai, reinforced by shivsakhuja). Written into `self-review` or `iteration-close`: any pipeline with a correction loop must first answer "is this compensating for overloaded context?"
3. **Prose-as-title convention for lessons and KG notes** (Nyk). A rename pass.
4. **Close the `skill-tiering-contract` draft instead of re-proposing tiering from scratch** (shivsakhuja). The draft already covers `tier`, `calls`, verifier/review gates, and attendance; the remaining research value is resolving D1-D5 and making the first rollout reversible.

**P1 — meaningful new primitives:**
5. **Dream cycle / scheduled consolidation job** (Thoth via witcheer). Dedup, relationship inference, description enrichment, and review-nudge firing. Must preserve scoped-KG's contract: time creates `review_due`, not `stale`.
6. **Contradiction protocol as a skill** (the_smart_ape). Explicit 4-step procedure for agents when they see two notes disagree. For scoped KG, same-scope contradiction can become stale; cross-scope disagreement remains `contradictions` metadata.
7. **Ingest command for external content** (Nyk). `da kg ingest <url|file>` that extracts claims and creates untracked-derivation notes.
8. **`kg lint` command** (karpathy). Broken wikilinks, orphan notes, missing authors, stale cites, contradictions. Reweave automation.

**P2 — structural:**
9. **Planning lenses** (the_smart_ape). Extend verifier/review profiles with contrarian + first-principles lenses for plan review.
10. **Dynamic output contracts on delegation bundles** (sullyai). Per-task typed output schemas; deterministic fan-in. This should land through app-type/profile contracts, not ad-hoc bundle prose.
11. **Parallel verification (Gordon Ramsay)** (milksandmatcha). Verifier + review run in parallel only for independent tasks in the same non-conflicting batch; never against work that consumes unverified output.
12. **Materialized neighborhood summaries on node rows** (techwith_ram). One int column per node. Near-instant `get_impact_radius`.

**P3 — named futures, not now:**
13. **Shared environment handoff** (jhleath). For a team-scale dot-agents, revisit worktree + shared disk over git sync.
14. **Formalize Context Cores as distribution primitive** (TrustGraph via witcheer). Our refresh mechanism becomes a versioned, rollback-able bundle.

### What we do better than them — but they have a quirk worth noting

- **vs. techwith_ram:** we skip SPARQL, but we should steal materialized neighborhood summaries and per-scope Bloom filters.
- **vs. arscontexta:** we have a stricter lifecycle (specs → plans → history); they have reweave as a first-class phase which we lack.
- **vs. multi-agent-memory-dkg:** precedence + contradictions > consensus voting; but content-addressed hashing (not blockchain) is a cheap integrity primitive.
- **vs. Obsidian-family:** sqlite warm store is strictly better for machine readers; prose-as-title + wiki-link-as-prose still worth adopting as human-readability conventions.
- **vs. codex swarms:** our dep graph + conflict detection is more principled; their subagent prompt template is a checklist to audit our bundle prompts against.

---

## Part C — Recommended next steps

Three levels of commitment. All are optional; none should be bundled.

### Immediate (a session or two each)

- **C.1** Add `author: human | agent` to `KGNote`, lesson files, and plan files. Write a PreToolUse hook that blocks `Write`/`Edit` on `author: human` files without explicit override. (Derived from kevin's two-author pattern, §A.2.)
- **C.2** Rename lessons and KG notes to prose-as-title. One-time pass over `.agents/lessons/` and the warm store.
- **C.3** Add a one-line rule to `self-review` or `iteration-close`: "if this work introduced or relied on a correction loop, first answer: is this compensating for an overloaded context?" (sullyai, §A.3; reinforced by shivsakhuja §A.1.)
- **C.3b** Resolve `skill-tiering-contract` D1-D5 and draft its implementation plan; do not create a second tiering proposal. (shivsakhuja, §A.1; current spec: `.agents/workflow/specs/skill-tiering-contract/design.md`.)
- **C.10** Adopt the **four-question lens** (`see / do / extend / changed`) as a self-audit framework on each anchor spec's §0 or frontmatter. Each spec declares which question it owns and at what target adoption level (L0–L5). Concrete first pass: scoped-KG owns "see" at L3+; skill-tiering owns "do" at L3 and the L3→L4 gate; app-type-profiles owns "changed" via verifier chain selection per app type; agent-context-resolution owns the L4→L5 leap. (annimaniac, §A.5.)
- **C.11** Add **hard test + common false positive** to spec drafting convention. Each spec's §0 or success-criteria block lists (a) one concrete behavioral test that proves the spec's contract is met and (b) the failure mode that *looks* like success but isn't. Apply retroactively to scoped-KG, skill-tiering, app-type-profiles before they graduate to plans. One-line drafting rule, large auditability gain. (annimaniac, §A.5.)

### Short-term (one plan each)

- **C.4** Draft a spec for a nightly consolidation pipeline (dream cycle). Pair with the scoped-KG review-nudge axis (§2.7 of that spec). The pipeline fires review-nudges, runs content-hash dedup, proposes `NoteSymbolLink` additions from co-occurrence.
- **C.5** Draft a `kg ingest` + `kg lint` spec. Ingest accepts URL/file, produces KG notes with `derivation: untracked`. Lint walks the graph for hygiene issues.
- **C.6** Audit our ISP pipeline (orchestrator → impl → verify → review → parent) through the "context engineering vs iteration" lens. Does a well-decomposed fanout eliminate the verifier stage for most task shapes? Even a negative finding is load-bearing.
- **C.6b** Apply `app-type-profiles` to this research workflow: define a `research` profile with document-scoped write scopes, citation/rubric verifiers, and citation-graph review before turning these evaluations into reusable workflow tasks.
- **C.12** Rename the in-spec terminology from "promotion gate / crystallization" to **managed compounding** in `agent-context-resolution-architecture.md` and the future synthesis spec. The phrase foregrounds the lifecycle (observe → evaluate → retire), aligns with the broader industry vocabulary, and names what the dual-motion (auto + peer review) gate is *for*. L5's hard test ("what did the system notice/decide/act/learn-from without a human initiating?") becomes the synthesis spec's behavioral acceptance criterion. (annimaniac, §A.5.)
- **C.13** **Extend `NoteType` (or sibling `interaction_label`) with the conversation-shaped ontology** from ashwingop Part 3: `decision`, `commitment`, `objection`, `escalation`, `dependency`, `assumption`, `customer_pain`, `owner`, `precedent`, `open_question`. Additive to existing labels. Land in scoped-KG §3.1 alongside the existing enum. Adds an axis the synthesis spec needs for cross-scope contradiction surfacing (one sentence, six lenses). (ashwingop §A.2.)
- **C.14** **Add a `memory_boundary` axis to the impact-score formula** in agent-context-resolution-architecture.md §3 and a **redaction-at-scope-promotion** decision to §6.9 (privacy boundary). Two changes that operationalize the "too little forgets, too much surveils" tension and the "people will not trust it with the most important context" warning. Privacy decision moves from "deferred" to "blocking before any cross-tool ingest ships." (ashwingop §A.2; reinforces annimaniac §A.5 escape-hatch contract.)

- **C.7** Fold the content-hash source mutation driver, Zep-style `valid_at`, and per-scope Bloom filters into the scoped-KG plan (not the spec — these are how-to, not decisions).
- **C.8** Generalize the proposal/review loop from "global rules" to any agent-authored artifact (plans, lessons, notes). Single graduation pipeline.
- **C.9** Run the completed-bundle audit queue before treating completed workflow plans as stable research evidence. The audit specs make this a prerequisite for trusting plan status in downstream analysis.

### Explicitly deferred (naming so they don't re-surface as proposals)

- Cryptographic fingerprints / blockchain KG (DKG). Overkill for our scale.
- SPARQL / Leapfrog / distributed partitioning. Premature at 34K nodes.
- Embedding-similarity propagation (also deferred in scoped-KG §4.6).
- Shared-disk environment handoff (jhleath). Revisit at team scale.

---

## Part D — Workflow Spec/Plan Inventory Corrections

*Added 2026-04-27 after inventorying `workflow/specs/` and `workflow/plans/`; see `research/evaluations/workflow-spec-plan-inventory.md`.*

### Inventory readback

- `research/articles/` contains 19 article extracts, and this file now evaluates all 19.
- `research/evaluations/` contains five sibling evaluations plus the 2026-04-27 cross-cutting inventory note.
- `.agents/workflow/specs/` contains 22 markdown artifacts. The new load-bearing specs for this synthesis are `app-type-profiles`, `skill-tiering-contract`, `completed-plan-audit-analysis`, and `project-audit-plan-sync-expansion`.
- `.agents/workflow/plans/` contains 13 plan directories. `kg-command-surface-readiness` is active with `kg-fresh-build-transaction-fix` pending; `refresh-skill-relink` is paused; several completed plans still require audit before their status should be used as strong evidence.

### Corrections to the synthesis

- **Domain gap:** non-code work is now a profile target, not a borrowed analogy. Recommendations about research, writing, design, or résumé work should route through `app-type-profiles` (`write_scope_kind: document | artifact`, citation/document graph backends, rubric/citation verifiers), not through code-shaped verifier assumptions.
- **Domain gap:** plan audit is now part of the workflow domain. The research docs should stop adding only new primitives and also prioritize spec-vs-implementation audits for completed bundles whose evidence is soft or status-drifted.
- **Logical correction:** scheduled consolidation is allowed, but KG staleness is event-driven. A dream cycle may dedupe, propose links, and raise `review_due`; it must not mark facts stale because of age alone.
- **Logical correction:** cross-scope disagreements are not stale. The scoped-KG contract says same-scope contradiction is a write-time driver; cross-scope disagreement is read-time `contradictions` metadata with precedence selection.
- **Logical correction:** `author` is one provenance axis, not the provenance model. Durable trust also needs scope/origin, cites/derived_from, tier/confidence, revocation semantics, and behavior around human-approved graduation.
- **Logical correction:** prose titles should improve human readability without renaming machine-stable IDs. Prefer `title`, aliases, or display text when an ID participates in schemas, links, or plan dependencies.
- **Platform correction:** cross-platform recommendations should target the managed dot-agents rule/config/profile surface first, then describe platform-specific enforcement. Claude Code hooks are one implementation path; Cursor, Codex, and Copilot need graceful rule-only or capability-limited fallbacks.
- **Execution correction:** parallel verification is a batch-scheduling optimization, not a universal replacement for serial verification. It is safe only when the next task does not depend on the unverified output and write scopes are non-overlapping.

---

## Part E — Trust gate (read before acting on any P0/P1/P2/P3 above)

*Added 2026-04-22 in response to adversarial review.*

Priority labels (P0/P1/P2/P3) above are **author judgment**, not validated
evidence. Most underlying articles are single-operator reports; several
recommendations ride on anecdote even when the mapping label is
`[GAP-ADOPT]`.

Before turning any P0/P1 here into a plan:

1. **Re-tier the underlying evidence.** If the article body is one
   operator's report, treat the recommendation as *directional*, not
   *load-bearing*. Demand a second independent source, a small internal
   pilot, or a written rationale that does not depend on the anecdote.
2. **Check for converging sources.** A recommendation is stronger when
   multiple articles arrive at it independently (Part B flags these).
   Converged items are safer to prioritize.
3. **Prefer reversible adoption.** Start with items whose rollback cost
   is trivial (rule edits, template changes). Defer items with
   infrastructure-scale rollback cost until a specific internal need
   pulls them.
4. **Caveat communication.** When pitching any recommendation from this
   doc, cite the underlying Evidence strength and Reversibility so the
   decision-maker is not misled by the priority label.

The sibling evaluation docs in `research/evaluations/` apply the same
trust gate and report Risk profile (Failure mode / Evidence / Reversibility
/ Second-order) per article. This doc's per-article blocks predate that
rubric — when in doubt, read the sibling docs for the same article to
see its Risk profile before deciding.

---

## Part F — Second-pass enrichment (2026-04-27)

*Added after re-reading the load-bearing specs (`scoped-knowledge-graphs/design.md`, `app-type-profiles/design.md`, `skill-tiering-contract/design.md`, `completed-plan-audit-analysis/design.md`, `project-audit-plan-sync-expansion/design.md`) and the active plan (`kg-command-surface-readiness`). Part D captured inventory; Part F captures contract-level couplings the synthesis was still missing.*

### F.1 Domain gaps the inventory pass did not name

- **`cross-app-dependency-impact` is a named-but-empty spec slot.** `app-type-profiles/design.md` §"Completeness note" announces a companion spec that "describe[s] how changes to one profile or one repo propagate through the dependency graph to affected repos, sharing the profile vocabulary defined here." This is exactly the architectural slot for: multi-agent-memory-dkg's cross-org coordination, witcheer's TrustGraph "Context Cores" (versioned distribution), and the Camp 2 graph-improves-itself thread. The synthesis should stop discussing those articles in the abstract and instead route their adoptable parts (content-hashing, publisher identity, versioned bundles, behavior-preservation gates) into that empty spec slot. Action: when the spec is drafted, add Part A.5 group "Cross-app / cross-repo propagation" pulling in DKG, TrustGraph, MemSearch, and the §F.2.4 reweave-as-propagation thread below.

- **The `public` scope is already designed; "external ingest" must respect it.** Scoped-KG §4.5 defers the `public` backend but requires today's resolver, provenance, and contract surfaces to *cover* it ("model it from day one so provenance and resolver cover it; first implementation plan does not build a public-scope backend"). The KG doc's P1 #7 (`da kg ingest <url|file>`) and P2 (Cognee-style "enter through vectors, exit through graph" hybrid bridge) must enter the system as `public`-scope writes — not as untyped notes in `repo`. Otherwise an ingested article becomes a "fact" with repo authority, polluting the precedence chain. Action: the ingest spec must declare `--scope public` as the only legal target for external content unless the user explicitly imports to a different scope, and must enforce the scoped-KG §3.1 legacy-config diagnostic before writing.

- **Verifier evolution is the missing governance layer for "remove a verifier" recommendations.** The agent-execution evaluation's E.4 (per-verifier rejection-rate audit → remove zero-yield verifiers) and the KG doc's C.6 ("would a well-decomposed fanout eliminate the verifier stage?") both describe verifier *retirement*. App-type-profiles §6 already specifies how that is allowed to happen: tightening the accept set is a **major** version bump that **must** pass the §6.2 behavior-preservation gate against a stored corpus of prior task runs. Removal counts as tightening. The synthesis treated verifier audit as a measurement problem; it is also a contract problem.

- **Cell vs compound vs molecule changes the audit shape.** `completed-plan-audit-analysis/design.md` §3 prescribes one evidence-precedence ladder for every plan ("PLAN.yaml → spec → code/tests → CI → narrative"). Skill-tiering-contract §4 says specs are T3 cells (decisions + done criteria; no ordering) and plans are T2 compounds (orchestration with verifier+review gates). These imply *different* audit shapes:
  - **Cell-tier audit (spec):** does the spec's done criteria still cover the live behavior? Are open questions (§10 in the spec) resolved in plans? Is the contract still authoritative or has implementation drift made the contract a fiction?
  - **Compound-tier audit (plan):** do the tasks correspond to shipped behavior? Does the merge-back archive carry sufficient evidence?
  
  Today's audit playbook collapses both into "spec-vs-implementation audit." The deeper cut: separate the **contract audit** from the **execution audit**. A spec can be verified-complete while its plans are evidence-thin, and vice versa.

### F.2 Logical flaws the inventory pass touched but did not finish

- **F.2.1 — `author`, `tier`, `cites`, `scope`, `derived_from` must be ONE schema across THREE stores.** The lessons/memory addendum says "design these together so semantics don't drift." The deeper reality: lessons live at `.agents/lessons/<name>/LESSON.md` (committed git), Claude-Code auto-memory lives at `~/.claude/projects/<hash>/memory/*.md` (per-user, NOT in repo, Claude-Code-only), and `KGNote` rows live in warm sqlite (per-scope per `kg.scopes`). Three stores, three lifecycle policies, three replication models. The right shape is to define the canonical trust-fields schema **inside `scoped-knowledge-graphs/design.md`** as the note schema, then make `LESSON.md` a *projection* (frontmatter that round-trips to KGNote) and auto-memory files an *adapter view* (read-only mirror of warm-store rows scoped to `user`). This eliminates the multi-store drift the addendum warned about.

- **F.2.2 — Same-scope vs cross-scope contradiction protocol is two different skills, not one.** The_smart_ape's "contradiction protocol" (KG doc §A.1, P1 #6, C.5) is currently described as one 4-step procedure. Scoped-KG §2.5 driver 4 + §5.12 split this into two distinct events:
  - **Same-scope contradiction:** detected at write time; fires a driver on the older entry; the older entry returns `stale: { reason: "contradiction", because: [<new-id>] }`. Procedure is *write-time* and *automatic*.
  - **Cross-scope disagreement:** detected at read time; precedence selects the answer; both sides remain fresh; surfaces in `contradictions` metadata. Procedure is *read-time* and *advisory*.
  
  These need different prompts and different escalation policies. The skill should be `/contradiction` with two modes (`--same-scope`, `--cross-scope`); the same-scope mode acknowledges the auto-stale and asks "do we revoke the new write or accept the stale tag?", while cross-scope mode asks "is the disagreement legitimate (drift between team and repo) or a precedence misconfiguration?" Conflating them produces wrong defaults.

- **F.2.3 — "Open questions" frontmatter (W.3) collides with prose-heavy specs.** Workflow-orchestration W.3 proposes `open_questions:` as structured frontmatter on specs. But `scoped-knowledge-graphs/design.md` §4 has nine open questions in prose with sub-bullets, `app-type-profiles/design.md` §10 has six in prose, `skill-tiering-contract/design.md` §8 has five lettered (D1-D5). Migrating these to YAML lists loses the rationale and the proposed-default. The fix: `open_questions:` is *additive metadata* — frontmatter wins for new specs, prose §"Open questions" is the canonical body, and a `workflow open-questions` extractor reads frontmatter first then falls back to parsing the prose section heading. Don't force a rewrite of three multi-page open-question prose blocks for a query convenience.

- **F.2.4 — Reweave (plan-graph) and derivation propagation (KG-graph) are the same primitive on different stores.** Workflow-orchestration W.5 (`/reweave` skill: walk the plan graph backward at plan close) and scoped-KG §2.6+§5.6 (write-time propagation along `derived_from` edges) are independent ideas in the synthesis. They should share a common shape: **when an entry is mutated, walk outward along stored `derived_from`/`derives_to` edges up to a bounded depth and stamp reachable entries with `review_due` (not `stale`).** For specs/plans, the edge is "this plan's decisions cite that spec's contract." For KG notes, the edge is `NoteSymbolLink` + `derived_from`. One propagation primitive, two stores. Diverging implementations would be debt.

- **F.2.5 — A "dream cycle" that writes is itself a write event under scoped-KG.** KG doc P1 #5 (nightly consolidation: dedup, link inference, description enrichment) describes a job that *writes* into the KG. Under scoped-KG §3.3 every write names a target scope. A cross-scope dedup that merges repo-scope and team-scope notes is *not allowed* — it would erase scope provenance. Inferring a new `NoteSymbolLink` between a repo note and a team note must write into one specific scope (with `derived_from` cites pointing at both), not into a virtual "merged" surface. The recommendation needs to specify: dream-cycle writes target the **most-local scope that has authority over both inputs**, with explicit cross-scope writes blocked. A naive implementation deletes provenance.

- **F.2.6 — Materialized neighborhood summaries are a four-backend migration, not a column add.** KG doc P2 #12 (techwith_ram-derived: "one int column per node"). Scoped-KG §2.2: each scope has its own backend (`sqlite | postgres | http`). A summary column means a coordinated migration on sqlite (repo, user) + postgres (team, org) + skipped (`http` is read-only). The recommendation should either (a) state explicitly that summaries are sqlite-only at first and degrade for team/org until a postgres migration lands, or (b) move from §2.6 derivation-propagation (which already runs at write time) to a derived metadata field stored *per scope*, not per node. Treating it as a quick win was wrong.

### F.3 Platform-specific bias the inventory addendum identified but did not concretize

- **F.3.1 — The hook capability matrix needs a concrete first row.** Hooks-and-platform addendum says "platform-specific files are outputs, not the canonical policy surface" and "enforcement degrades by platform." The KG doc's recommendations to gate `author: human` writes via PreToolUse, ingest via SessionStart, etc., must declare per-platform enforcement explicitly:
  - **Claude Code:** PreToolUse + SessionStart + Stop hooks, full enforcement.
  - **Cursor:** rule-only (`.cursorrules` is advisory). Enforcement reduces to "violation visible at PR review."
  - **Codex CLI:** rule-only via the codex agent surface. No PreToolUse equivalent.
  - **GitHub Copilot:** rule-only via project instructions. Weakest enforcement.
  
  Recommendations should label which mechanism applies on which platform and explicitly say "best-effort on Cursor/Codex/Copilot; full enforcement on Claude Code." Otherwise we're shipping invariants only one platform respects.

- **F.3.2 — Auto-memory recommendations don't apply uniformly across platforms.** Lessons/memory P0 says "add `author: human | agent` to auto-memory files." Claude-Code auto-memory at `~/.claude/projects/<hash>/memory/` is platform-private. Cursor's nearest equivalent is Project Rules; Codex CLI uses a different memory surface; Copilot has no equivalent at all. The recommendation collapses across these. The right framing: define the trust schema (F.2.1) on the canonical scoped-KG `user`-scope note. Each platform's memory adapter projects from the warm store. The auto-memory surface becomes a *cache*, not a source of truth.

- **F.3.3 — MCP tool surfaces (`kg_ingest`, `kg lint`, `get_impact_radius`) are Claude-Code + Cursor-class clients only.** The KG doc treats MCP as a unified runtime, but Codex CLI and Copilot do not currently run MCP. Recommendations to expose ingest/lint as MCP tools must be paired with: a CLI fallback (`da kg ingest …`) and a rule that declares "MCP-backed surfaces auto-degrade to CLI invocations on platforms without MCP, with platform-specific render guidance in the rule corpus." This is the same pattern config-distribution-model uses for verifier maps.
  - Human correction 05/03/2026 - 14:01 (EST)
    Codex CLI and Copilot do indeed run mcp, this is a false or stale claim.
    However, mcp vs cli tool debate is on my mind and do see benefit of cli usage as there's more training data on how to use a cli tool.
### F.4 Recursive accountability — apply our recommendations to this corpus

Article evaluations are themselves Camp-2-substrate research artifacts. Three commitments fall out of the corpus that this corpus has not yet honored:

- **F.4.1 — These docs should declare `tier: cell` once skill-tiering ships.** Each evaluation document is a self-contained contract surface that names decisions about adoption (which articles → which recommendations). That matches skill-tiering-contract §3's T3 cell definition. Frontmatter migration is cheap; do it as part of the rollout.

- **F.4.2 — These docs should ride the `research` profile from app-type-profiles §3, not freelance verifier-shaped checks.** The KG doc currently asserts adoption priorities (P0/P1/P2/P3) without running the citation-presence / source-freshness / rubric-check chain. Applying the `research` profile to this corpus would catch: missing citations on five of the §A entries, two URL references whose host moved (multi-agent-memory-dkg, jhleath both pre-date late-2026 reorganizations), and rubric-fit scoring on the convergence-of-N-articles claims. The "we recommend a profile we don't yet test ourselves against" is the same drift `completed-plan-audit-analysis` warns about for code plans.

- **F.4.3 — Architect ≠ orchestrator.** Skills/rules/graduation evaluation maps `intuitiveml`'s Architect role to "the person(s) who author rules, skills, prompts, and the proposal/review loop." The agent-execution evaluation maps it to the runtime orchestrator. Both can't be right. Skill-tiering-contract resolves it: cell-tier specs are *human-authored* (or agent-proposed-human-approved); the orchestrator runs compounds. The Architect is the cell author. Update agent-execution.md §B/§C accordingly.

### F.5 Updated next-step deltas (replaces, does not append to, Part C)

These supersede the C.* labels they reference. C.* items not listed here stand.

- **C.1+ (author/tier/cites)** — land the schema in `scoped-knowledge-graphs/design.md` as the canonical note shape; lessons + auto-memory become projections. Single rollout, three projections, no per-store schema drift. Replaces the L.2 / S.4 / C.1 fragment.
- **C.4+ (dream cycle)** — must declare target scope per write per F.2.5; cross-scope merging is a schema error. Replaces C.4's silence on scope routing.
- **C.5+ (kg ingest)** — defaults to `--scope public`; refuses to write to `repo` for external content; emits scoped-KG §3.1 legacy diagnostic before any write. Replaces C.5's unscoped framing.
- **C.6+ (verifier audit)** — runs the §6.2 behavior-preservation gate against a stored corpus before any verifier removal; rejection-rate evidence is necessary but not sufficient. Replaces the agent-execution E.4 framing.
- **NEW C.10 — apply the `research` profile to `research/`** as a self-test; treat each article-evaluation doc as a `write_scope_kind: document` work item; its verifier chain is `citation-presence + source-freshness + rubric-check`. Pairs with the C.6b mention in the original Part C.
- **NEW C.11 — split the contradiction skill** into same-scope (write-time, auto-stale-acknowledgment) vs cross-scope (read-time, precedence audit). One name, two modes.
- **NEW C.12 — unify reweave (plan graph) and derivation propagation (KG graph)** into one propagation primitive, parameterized by store. Edge types differ; the walk and the bound do not.
- **NEW C.13 — produce a per-platform enforcement matrix** as a precondition for any rule that says "blocked by hook." Without the matrix, recommendations ship as Claude-Code-only invariants disguised as cross-platform contracts.

---

*Document status: draft. No changes made to code, specs, or plans. This is evaluation only. Part F adds contract-level couplings to the existing inventory addendum (Part D) and trust gate (Part E).*
