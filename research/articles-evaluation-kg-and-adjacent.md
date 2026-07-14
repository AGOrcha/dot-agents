# KG & Adjacent Articles — Evaluation Against dot-agents

**Written:** 2026-04-21 (addenda: 2026-04-23 added shivsakhuja Skill Graphs 2.0 as §A.1 entry 5; 2026-04-23 added akshay_pachaar *Build Agents that never forget* as §A.1 entry 6; 2026-04-27 added workflow spec/plan inventory corrections; 2026-05-03 added annimaniac *Six Levels of AI-Pilled Organizations* as new §A.5 group plus Part B theme 7 and §C.10/C.11/C.12; 2026-05-03 added ashwingop *Company Brain* series (Part 2 Factual Memory + Part 3 Interaction Memory) as §A.2 entry 6 plus Part B theme 8 and §C.13/C.14; 2026-05-08 added ashwingop Parts 4–7 (Action Memory, Memory as State, Year of Building, Semantics+Ontology) extending §A.2 entry 6; added alphasignalai *Single vs Multi-Agent* as §A.4 entry 5; added Part B theme 9; added §C.14/C.15; added Part G research-profile verification pass; added akshay_pachaar *IdeaBlocks* as §A.1 entry 7; added mem0ai *Memory Decay* as §A.2 entry 7; added ghumare64 *Runbooks* + trq212 *HTML* as §A.3 entries 5–6; added Part B theme 10; added §C.16/C.17; 2026-06-24 added Part H — five new x.com-sourced articles (compaction-orchestrator, prompt-debt, token-capital-efficiency, jailbreak-prompt-opt-duality [NEEDS-PASTE], agent-without-phone) evaluated against graph-backend-adapter-contract / scoped-knowledge-graphs / work-tracking-storage-abstraction / kg-command-surface-readiness + the KG-as-SOT typed-views theme; 2026-06-24 re-extracted all five via the `article-extract` skill Playwright path and conformed articles/ + extracts/ to the skill's title/author/date/method/body/key-quotes template — confirmed aminkarbasi's X-native article body is login-walled, not retrievable unauthenticated)
**Scope:** 33 articles in `research/articles/` (KG + memory + harness + multi-agent + hooks/platform). Compared against current specs in `.agents/workflow/specs/`, plans in `.agents/workflow/plans/`, proposals in `.agents/proposals/`, lessons in `.agents/lessons/`, and the scoped-KG / graph-bridge / app-type-profile / skill-tiering specs.
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

#### akshay_pachaar — *You're Doing RAG Wrong (IdeaBlocks)*

*Second article by akshay_pachaar; the first (§A.1 above) covers memory architecture. This one covers RAG preprocessing.*

**Core.** The chunk is the wrong unit of knowledge for RAG retrieval. Replace it with a **question-answer packet (IdeaBlock)**: one question, its validated answer, and typed governance fields (clearance level, version state, source) as a single typed schema object. Queries are already questions; when the index stores question-answer pairs, the match becomes structural rather than probabilistic. Benchmarks from Blockify (open-source, 17 documents / 298 pages): cosine distance 0.1585 (IdeaBlocks) vs 0.3624 (naive chunks) — **2.29× retrieval distance reduction**. Semantic deduplication at 80-85% similarity collapsed 2,042 raw blocks → 1,200 canonical blocks; **distilled corpus outperformed undistilled by 13.55%** because near-duplicates in the same embedding region distribute probability mass, degrading the canonical match score. Governance fields (clearance, version state, product line) are typed on the block — not logic bolted onto the orchestrator.

**Pros.**
- **"Fifteen near-duplicates create fifteen competing vectors"** is the clean explanation for why our warm-store deduplication step (scoped-KG same-scope contradiction driver, §2.5 driver 4) improves retrieval quality — not just hygiene.
- **Governance in the data layer** maps exactly to our scoped-KG's per-note scope/permissions model. Their `version_state: current|deprecated` is the typed analog of our `ArchivedAt` field.
- **Q/A packet matches how LLMs are queried.** For our research corpus (each article evaluation is a set of claims + recommendations), structuring evaluations as Q/A packets would improve vector retrieval in `kg query` calls.
- The **7-stage preprocessing pipeline** (scope → ingest → chunk → dedup → tag → validate → export) is a worked example for how our dream-cycle consolidation pass should be specified.

**Cons.**
- Blockify benchmarks are internal, not peer-reviewed. The 40× corpus compression claim is headline-worthy; the methodology (17 documents) is too small to treat as load-bearing evidence.
- The Q/A unit works well for factual corpora (docs, FAQs, policies). Our KG contains decision traces, plan notes, and open questions — not all of which map cleanly to a single Q/A pair.
- Human validation step (SMEs spend 1-2 hours/quarter on their corpus slice) assumes stable organizational structure. Our notes evolve faster than a quarterly validation cycle allows.

**Mapping.**
- **[OVERLAP-SHARPEN]** — **Our KG notes are already close to IdeaBlocks.** Each note is a single claim with typed metadata. What we're missing: (a) an explicit `question` field that frames what retrieval query this note answers, and (b) a `version_state: current|deprecated|draft` field distinct from `ArchivedAt`. `ArchivedAt` conflates "no longer active" with "specifically deprecated for a reason." Adding a `version_state` field makes the distinction machine-readable.
- **[GAP-ADOPT — data layer]** — **Semantic deduplication pass in the dream-cycle.** Blockify's 80-85% cosine deduplication step (3-5 iterative rounds → canonical block per cluster) should be the shape of our scoped-KG consolidation: within a scope, cluster near-identical notes by cosine similarity, propose a canonical merge to the human reviewer, mark superseded notes `ArchivedAt`. Not automated — human validates the merge — but the pipeline shape is correct.
- **[GAP-ADOPT — small]** — **Add `version_state` to `KGNote`** as a typed enum (`current | deprecated | draft | approved`). Pair with the `author` field rollout (C.1 / F.2.1). One field; large impact on provenance clarity and on the dream-cycle's pruning decision.
- **[WE-AHEAD]** — Our scope chain (repo/user/team/org) is a more principled governance model than IdeaBlock's flat clearance level (PUBLIC/INTERNAL/CONFIDENTIAL/SECRET). Our model handles the team-org-user hierarchy; their model handles flat access control. Different problems; ours is more complex and more correct for a multi-user agent system.

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

#### mem0ai — *Memory Decay: Recency-Aware Ranking*

**Core.** A per-project toggle that applies recency-aware ranking to agent memory search. Every memory tracks its last 20 access timestamps; at search time, recently-accessed memories receive a boost (up to 1.5×) and idle memories are dampened (floor 0.3×) — a **5× spread** between fresh and stale. Nothing is deleted or hidden; stale memories still surface when genuinely relevant, just ranked lower. Search-time concern only — no reindexing, no schema migration. Roadmap: category-aware weighting (high-importance categories resist dampening) and per-project auto-tuning.

**Pros.**
- **Recency as a soft signal complements our event-driven staleness model.** Our scoped-KG marks notes stale via specific drivers (source-mutation, contradiction, revocation). Memory Decay handles the complementary case: nothing fired, but the note is simply forgotten — nobody has retrieved it in weeks. The two mechanisms are orthogonal, not competing.
- **"History stays; present ranks correctly"** is a cleaner user-facing contract than our current model, which offers no retrieval bias. For agent orientation (SessionStart KG readback), recently-accessed notes should surface first without requiring the user to filter by date.
- **Floor of 0.3×** prevents total suppression — stale but genuinely matching notes still appear. This is the right tradeoff: you want recency-weighting, not recency-filtering.
- **Access count as a pruning signal** for the dream-cycle: notes with zero accesses over a window, no linked active plans, and no derivation children are consolidation candidates.

**Cons.**
- Product announcement, not a research paper. The 5× spread (0.3–1.5×) and the 20-timestamp window are engineering choices, not principled derivations. The right parameters for our KG may differ.
- "Last accessed" conflates retrieval-by-agent with retrieval-by-human. A note accessed 50 times by automated SessionStart orientation should not rank the same as a note accessed 50 times because a human deliberately sought it out. Access source matters.
- **Category-aware weighting** (roadmap item) is the more interesting primitive for our use case. A `decisions` note should not decay at the same rate as an `observation` note. The current feature release doesn't have this yet.

**Mapping.**
- **[GAP-ADOPT — small]** — **Add `last_accessed_at` and `access_count` to `KGNote`.** Two fields, no schema redesign. Feed them into: (1) `kg query` scoring (recently-accessed notes slightly preferred, like browser history); (2) dream-cycle pruning candidates (access_count = 0 in last N days + no linked active plans → archival candidate). Both are read-time or consolidation-time concerns.
- **[OVERLAP-SHARPEN]** — **Our review-nudge axis (scoped-KG §2.7) fires on events; Decay fires on access patterns.** The two are complementary axes. A note can be review-nudged without being stale (a question was asked that should re-examine the note) and can be stale without a review-nudge (nobody remembered to ask). Add both to the dream-cycle's pruning decision tree.
- **[WE-AHEAD]** — Our event-driven staleness model (source-mutation, revocation, contradiction) is more precise than decay-by-age for notes that are load-bearing. A spec's decision note should not decay just because nobody retrieved it last month; it should decay only when a superseding decision fires. Decay is the right signal for notes that are forgotten rather than superseded.
- **[GAP-ADOPT — medium]** — **Per-category decay resistance as a `NoteType` property.** Once `version_state` and `NoteType` extensions land (F.2.1, C.1), add a `decay_resistance: low|medium|high` property to the NoteType enum definitions. `decisions` = high (decays slowly), `observations` = low (decays fast), `commitments` = high. This is the category-aware weighting Mem0 lists as roadmap — we can design it now as a schema property.

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

#### ashwingop — *Company Brain* (Parts 4–7 extension)

*Extends the Parts 2+3 evaluation above. Treated as a single continuing architecture series.*

**Core (Part 4 — Action Memory).** Action memory is the third memory layer after factual and interaction: it remembers *how the company moves*, not just what it knows or why decisions were made. Four sub-types: **procedural** (how a process is supposed to work), **trigger** (when a condition should wake a workflow), **execution** (what actually happened in a specific case), **outcome** (what happened after the action — the feedback loop). Key principle: **doing nothing is a first-class action.** An agent that cannot stay still on purpose cannot be trusted to act on purpose. "Most workflow diagrams are polite fiction" — the documented path differs from the lived path; action memory must remember the actual path. Action memory is partly agentic: it has to *participate* (notice a condition changed, route, respect guardrails), not just sit still. Guardrails: not all actions are equal; some are routine, some require approval, some affect money or production. A one-percent threshold change can flip the entire operating path.

**Core (Part 5 — Memory as State).** Enterprise AI is fragmenting memory into tool-local silos; the solution is a **shared semantic substrate — memory as state, not a service.** Substrate primitives: entities, facts, state changes (first-class, not just artifacts), relationships (typed edges). Ontologies sit *above* the substrate as lenses, not below it as schemas — this is what allows the same underlying memory to be read differently by sales, product, legal, and an agent without the memory splitting into copies. Trust requirements: provenance, permissions, version history, correctability, "what contradicts it?" Trust questions are load-bearing, not bureaucratic. "A database stores records. A substrate defines the rules by which records become shared operating state."

**Core (Part 6 — Year of Building).** Retrospective on the "Company Brain" project. Three lessons: (1) company memory must emerge from the *work itself* (meetings/messages/calls), not from polling people; (2) interactions are the **chain of thought of the organization** — where the company reasons before it writes down what it decided; (3) ontology emerges from needing to make the same conversation mean different things to different functions. Memory traces → world model → learning from outcomes (described as "reinforcement learning inside the company: traces, actions, outcomes, feedback loops"). Customer case: compressing signal latency from "surfaced in weekly review" to "flagged while the conversation was still fresh" changes how the organization can respond. UI principle: memory should be experienced through *surfacing*, not browsing (TikTok analogy — not surveillance, but the right thing at the right moment).

**Core (Part 7 — Semantics and Ontology).** Reacts to Anthropic's Claude Managed Agents memory launch (file-based, scoped, versioned, `/mnt/memory/`). Verdict: correct direction, necessary but not sufficient. Semantics = what something is. Ontology = why it matters per perspective. Personal ontology is fluid (we are many roles at once); organizational ontology is more stable (roles constrain the lenses: sales/product/support/legal/leadership). **Custom ontologies are the real path to organization-wide AI adoption** — adoption fails when one assistant carries one ontology and seven functions can't map their work onto it. Architectural move: separate substrate (shared, permissioned, durable) from lens (functional, customizable, per-vertical). The metric: "not how often people use AI. It is how often the company successfully does the thing it decided to do."

**Pros.**
- **Action memory's 4-layer taxonomy is the missing vocabulary for our workflow pipeline.** Our pipeline has `eligible` (= trigger), `plan` (= procedural), `merge-back` (= execution), `impl-results` (= outcome). These are the same four stages under different names; making the mapping explicit unlocks the "outcome → precedent" loop.
- **"Doing nothing is a first-class action"** is the cleanest articulation of `on_fail: soft` + guard-condition semantics. It names why our verifier `on_fail: soft` is not a weakness but a design — the verifier is a guardrail that knows when to hold.
- **Memory as state (Part 5) is architectural validation of our warm-store model.** Our sqlite warm store + hot markdown + MCP bridge is exactly the substrate-above-adapter model Parts 5 and 7 describe. Their `/mnt/memory/` = our `.agents/` hot filesystem. We are building this already.
- **Part 7's reaction to Claude Managed Agents** gives us external corroboration that file-based memory stores with scoped permissions are the right primitive. But it also names the gap we have: semantic trace layer is missing. We store facts; we don't yet store "support trace / customer trace / product trace" as first-class derived note types.
- **"Substrate generalizes; ontologies don't"** maps directly onto our scope chain (repo/user/team/org provide the substrate; NoteType + interaction labels provide the per-function lens). The claim is testable.
- **"Chain of thought of the organization"** corroborates arscontexta's Reweave (updating prior artifacts) and the_smart_ape's source tiers — three sources converging on the same primitive: the reasoning that produced a decision matters more than the decision artifact.
- **Latency as a dimension of memory quality** — Part 6's customer case (signal surfaced during the conversation rather than in the weekly review) names a quality metric our system has no surface for. Speed-of-surfacing is a capability.

**Cons.**
- **All 4 parts are marketing pieces for Sentra.app.** The architecture lessons are real and corroborated elsewhere in the corpus, but the implicit "and we're building this" is doing some of the work. Honor §E trust gate: adopt structural insights, not feature claims.
- **"RL inside the company"** (Part 6) is a compelling framing but completely unoperationalized. The feedback loop requires: action labeling, outcome labeling, credit assignment across delayed outcomes, reward shaping. None of these are specified. Treat as a research direction, not a near-term design input.
- **Part 7 references Claude Managed Agents specifics** ("dreaming," `/mnt/memory/`, workspace-scoped memory stores) that are Anthropic product claims at a specific point in time (May 2026). These may change.
- **"One substrate, many lenses"** is the aspiration; the actual org-scale ontology engineering problem (who owns the sales lens, how does it evolve, who mediates conflicts between the legal lens and the product lens) is not addressed.

**Mapping.**
- **[OVERLAP-SHARPEN]** — **Action memory's 4 sub-types (trigger / procedural / execution / outcome) map onto our workflow pipeline stages.** Name the correspondence explicitly in the scoped-KG spec as four `NoteType` sub-classes under a `workflow-trace` family. Execution + outcome traces live in `delegate-merge-back-archive/`; trigger traces live in `active/fold-back/`. The mapping closes the gap between "what the spec says" and "what the history already records."
- **[GAP-ADOPT — spec-level]** — **Semantic trace types as NoteType extension.** Part 7's "support trace / customer trace / product trace / revenue trace / action trace / decision trace" are the conversation-shaped extensions to the factual NoteType enum (complement to Part 3's interaction ontology). Add a `trace_kind: workflow | interaction | outcome | commitment` sub-field to `KGNote` rather than multiplying the flat enum.
- **[GAP-ADOPT — medium]** — **"Speed-of-surfacing" as a KG quality metric.** Today we measure provenance and freshness; we have no metric for latency-from-event-to-surfacing. A note written two weeks after the fact has lower signal value than one written at the moment. Add `capture_lag_seconds` to `KGNote` as an optional field (populated by ingest tooling); use it in the consolidation/dream-cycle to weight recently-captured notes higher for review-nudge firing.
- **[WE-AHEAD]** — **Our scoped-KG with `derived_from` + scope hierarchy is more concrete than Part 5's "semantic memory filesystem" aspiration.** We should not apologize for our model's specificity. Where we lag: the "ontology as lens above substrate" framing is more elegant than our current flat `NoteType` enum. The enum should be refactored as a two-level structure: a canonical note-type (fact/decision/commitment/etc.) + a per-scope ontology overlay that decides the lens.
- **[OVERLAP-SHARPEN]** — **"The metric is how often the company does the thing it decided to do"** (Part 7) is the external articulation of our `impl-results` + `history/` archive purpose. Today we archive passively; the metric suggests we should *compute* completion rate: plan started → impl-results written → outcomes described. A `da workflow stats` command over the history dir would surface this.
- **[GAP-ADOPT — research-tier]** — **Verify "substrate generalizes; ontologies don't" against our scope chain.** This is a testable claim. The scope chain (repo/user/team/org) is our substrate; NoteType + interaction labels are our ontology. Does the same warm-store row read correctly through a repo lens and a user lens? If yes, the claim holds; if not, we have scope-collapse. Write this as a test case in the kg-command-surface-readiness verification plan.
- **[DEFERRED]** — The "RL inside the company" thread (traces → world model → learning from outcomes) is worth tracking as a long-range goal. Not actionable until outcome labeling and delayed credit assignment are specified. Flag in the kg-command-surface-readiness open questions for a future spec cycle.

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

#### ghumare64 — *Agents Need Runbooks, Not Longer Chats*

**Core.** Agent reliability comes from operational scaffolding, not better prompts. Two required layers: (1) **knowledge layer** (docs, skills, memory, conventions) and (2) **control layer** (state machine, tool policy, checks, approvals, rollback). Most teams over-invest in layer 1 and under-build layer 2. A prompt describes what should happen; a runbook *controls* what can happen. Verification must be external and programmatic (tests must actually run, exit codes must be checked), not agent self-assertion. "CLAUDE.md is not enough" — project instructions tell the agent how the project works but cannot guarantee the agent follows the release process. The right abstraction is "agent as production worker" not "agent as employee": constrain the environment, don't ask for carefulness.

**Pros.**
- **The two-layer framework (knowledge + control) is the clearest external validation of our spec/skill/plan (knowledge) + TASKS/ISP/iteration-close (control) split.**
- **"Prompt is advice; runbook is infrastructure"** is the one-line articulation of why skills and CLAUDE.md alone are insufficient: they tell the agent what to do but cannot enforce it.
- **The DevOps analogy** ("we learned this lesson, then forgot it with agents") is a memorable framing for why agent reliability is a platform problem, not a prompt problem.
- **Per-task permissions (`allowed_tools`, `write_scope`)** and **per-step `on_failure` handling** are the exact fields our TASKS.yaml needs to be a real runbook rather than a status-tracking file.
- **"Just ask the agent to verify is not enough"** maps directly to our verifier stage — and names the failure mode we must guard against: an agent that claims tests passed without the verifier stage checking an exit code.

**Cons.**
- Light on implementation specifics. The YAML snippets are conceptual sketches, not a real schema. Our actual runbook needs are more complex (dependency ordering, write-scope conflict detection, cross-plan deps).
- The article is targeted at teams starting from scratch. We already have much of the control layer (ISP, iteration-close, delegation contracts). The gap is smaller than the article implies.
- "Agent as production worker" is correct for bounded tasks but doesn't account for the orchestrator role, which needs broader judgment about *which* worker runs next.

**Mapping.**
- **[WE-AHEAD]** — **Our TASKS.yaml + ISP pipeline IS the runbook + control layer ghumare64 argues for.** We have: state (task status + iteration log), write-scope permissions (bounded by `write_scope` in TASKS), verification (verifier+review in ISP), checkpoints (iteration-close), approval gates (`review_kind: human`). The article is validating our architecture. We should make this explicit in the workflow artifact model documentation.
- **[GAP-ADOPT — small]** — **Add `allowed_tools` as an explicit TASKS.yaml field**, mirroring app-type-profiles' `impl_defaults.allowed_tools`. Today write-scope says which files but not which tools. Adding per-task tool restrictions closes the gap between "runbook controls what can happen" and our current advisory write-scope model.
- **[GAP-ADOPT — small]** — **Verifiers must produce a machine-checkable output, not a prose assertion.** Our iteration-close verification step should require: either a non-zero exit code check (for test suites), a file existence check (for generated artifacts), or a structured JSON output. An agent that writes "tests passed" in the merge-back without an exit code captured is the failure mode ghumare64 names. Add a `verification_evidence_kind: exit_code | file_exists | json_assertion | prose_only` field to task completion records; flag `prose_only` in the `da workflow checkpoint` output.
- **[OVERLAP-SHARPEN]** — **PostToolUse hook for observability: "what did the agent read/modify?"** Our iteration-close captures what was done, but we have no per-task tool-call log. A lightweight PostToolUse hook that appends `{tool, path, timestamp}` to a per-task `tool-calls.jsonl` file in the active directory would give us the observability ghumare64 says is load-bearing. Claude Code supports this natively.

---

#### trq212 — *The Unreasonable Effectiveness of HTML*

*Author is Thariq, Claude Code @ Anthropic. 2.3M views — most viral article in this corpus.*

**Core.** HTML is a richer output format than Markdown for agent-produced artifacts. Markdown is appropriate for machine-to-machine handoffs (agent reads/writes it efficiently); HTML is appropriate for human consumption. Information density: HTML can represent tables, CSS styling, SVG diagrams, JavaScript interactions, spatial layouts — "almost no set of information that Claude can read cannot be represented in HTML." Visual clarity: humans stop reading Markdown past ~100 lines; HTML with tabs, collapsible sections, and diagrams holds attention. Shareability: upload to S3, share a URL. Two-way interaction: sliders, copy-as-prompt export. Key use cases: specs/explorations (compare 6 designs side-by-side), code review explainers (annotated diffs, better than GitHub's diff view), design prototypes, reports synthesized from cross-source data, custom throwaway editing interfaces (draggable task boards, structured config editors). Downside: HTML diffs are noisy in version control.

**Pros.**
- **"Humans stop reading Markdown past 100 lines"** is a direct observation about our current research evaluation docs. Our 845-line KG evaluation doc is a prime example — the HTML equivalent would be far more navigable for a human stakeholder review.
- **Custom throwaway editing interface + copy-as-prompt** is immediately applicable to TASKS.yaml triage and plan priority editing. A drag-and-drop card board for our task backlog, ending with "copy as YAML" — this is the custom editing interface pattern at zero infrastructure cost.
- **"I feel more in the loop with Claude"** names the real problem with large Markdown plans: the human disengages from planning because they can't see it clearly. HTML restores engagement without changing the underlying data model.
- The **code review explainer use case** (annotated HTML diff attached to every PR) is immediately useful and maps to our `review-pr` skill's output — replace or supplement its Markdown output with an HTML artifact for the human reviewer.

**Cons.**
- **HTML diffs are noisy in git** — this is a genuine limitation for any artifact that must be version-controlled and reviewed as a diff. Our PLAN.yaml, TASKS.yaml, `.plan.md`, and KG notes must remain Markdown/YAML for diffability.
- **2× to 4× generation time** vs Markdown. For agent-internal handoffs (orchestrator → worker context), the generation overhead is pure waste.
- **Agent-consumed artifacts must remain machine-readable.** SessionStart rule loading, KG ingest, plan parsing — these must stay Markdown/YAML. HTML as a format is only appropriate for the human-facing output layer.
- The article is from an Anthropic team member and is Claude Code-specific. The "two-way interaction" feature requires a browser and is a Claude Code UI affordance, not portable to Codex/Cursor/Copilot.

**Mapping.**
- **[GAP-ADOPT — workflow]** — **HTML as the human-facing output layer for plan presentations and PR explainers.** Our architecture already has a machine layer (YAML/Markdown) and a human layer (plan.md, impl-results.md). Add an optional HTML rendering step: `da plan view --html` generates an HTML dashboard from TASKS.yaml + PLAN.yaml for stakeholder review or archival. Not a replacement for the YAML; a derived human-readable view.
- **[GAP-ADOPT — skill]** — **The `review-pr` skill should offer an HTML artifact output alongside its current Markdown report.** Given the trq212 observation that HTML code review explainers are "often better than the default GitHub diff view," the `review-pr` skill's output stage could produce an HTML annotated diff as an optional artifact, attachable to the PR description.
- **[WE-AHEAD]** — **Our machine-readable artifact layer (YAML + Markdown) is correct and must not be replaced by HTML.** Agents consume PLAN.yaml, TASKS.yaml, and KG notes. HTML is strictly worse for this layer. The trq212 framing makes this explicit: "I'm not editing these files myself anymore" — but *our agents are*. Maintain the two-layer distinction: YAML/Markdown for machine layer, HTML only for human-facing output layer.
- **[OVERLAP-SHARPEN]** — **Custom editing interface pattern for TASKS.yaml backlog triage.** The trq212 draggable-card / copy-as-YAML pattern could be generated as a `da workflow triage` output: render the current plan's tasks as a drag-and-drop HTML board, export the reordered result as TASKS.yaml. This is the throwaway editor pattern at exactly the right scope. Low priority but zero infrastructure cost.

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

#### alphasignalai — *How to Choose Between Single and Multi-Agent Solutions*

**Core.** Empirically-grounded framework for the single-vs-multi-agent decision, citing two studies: (1) Stanford — when controlled for "thinking budget" (same token count), single-agent matches or beats multi-agent on multi-hop reasoning; passing info between agents creates lossy summarization and compounds errors. (2) Google/MIT — independent agent swarms amplify baseline errors by up to **17.2×**; in 16-tool setups, single-agent coordination efficiency = 0.466 vs multi-agent 0.074–0.234 (2–6× penalty). Key heuristics: **45% accuracy threshold** — if single-agent accuracy on a task is below 45%, multi-agent can help; above 45%, single-agent usually wins. **Pre-answer scaffolding** — before committing, force the model to identify ambiguities, list candidate interpretations, and test alternatives; this recovers multi-agent benefits inside a single context. **Decision matrix**: tool-heavy (>10 tools) → single; context degradation → multi; natural decomposition + independent sub-tasks → multi; strict regulatory verification → centralized multi with orchestrator bottleneck (36.4% contradiction reduction, 66.8% context omission reduction).

**Pros.**
- **Empirical grounding is unique in this corpus.** Most articles are architectural reasoning; this one cites quantified study results (17.2× error amplification, coordination efficiency numbers, 45% threshold). Even second-hand, the magnitudes matter.
- **"45% accuracy threshold"** is a concrete, actionable fanout criterion. Currently our `eligible` gating has no empirical trigger; this gives one.
- **Pre-answer scaffolding** is immediately deployable as a prompt pattern for the orchestrator's planning phase.
- **Centralized orchestrator with a validation bottleneck** is exactly what our ISP verifier+review stages are. The Google/MIT numbers (36.4% contradiction reduction, 66.8% context omission reduction) are external validation.

**Cons.**
- AlphaSignal is a newsletter aggregator, not original research — the studies cited are real, but second-hand with no methodology detail given. The 45% threshold comes from one study with specific task structures; generalizing requires care.
- "Thinking budget" control is meaningful for reasoning tasks but our fanout tasks are write-scope-bounded (not open-ended reasoning) — the analogy is partial.
- The decision matrix implicitly assumes homogeneous task types; our plans mix tool-heavy tasks (10+ tools in some delegations) with lightweight document edits in the same plan.

**Mapping.**
- **[OVERLAP-SHARPEN]** — **Our ISP fanout gating (eligible → fanout vs single delegation) is empirically validated by the Stanford/Google findings.** Sequential tasks with clear single-agent context fit → stay single. Decomposable independent sub-tasks with write-scope conflicts → fanout. We do this already; the studies justify it. We should cite these numbers in any future spec that discusses the fanout decision rationale (skill-tiering-contract D4 — "compound attendance model" — is the right place).
- **[GAP-ADOPT — small]** — **Add the 45% single-agent accuracy heuristic as an explicit `eligible` criterion note.** Before fanning out a task, the orchestrator prompt should ask: "can a single agent reliably complete this write scope, or does the scope complexity/size suggest coordination will compound errors?" This is not a hard gate but an explicit reasoning step — currently the decision is implicit.
- **[GAP-ADOPT — prompt-level]** — **Pre-answer scaffolding for the orchestrator planning phase.** Before the orchestrator produces a delegation bundle, prompt it to: (a) identify ambiguities in the task spec, (b) list candidate interpretations of the write scope, (c) test alternatives before committing to the plan. This is the `open_questions` discipline from spec-writing applied at runtime.
- **[WE-AHEAD]** — **Our orchestrator-does-not-implement-the-delegated-slice rule + bounded write scopes directly prevent the 17.2× error-amplification problem.** Bounded scopes contain what each agent can touch; the orchestrator/parent gate catches conflicts before merge. The architecture is already correct; the studies explain why.
- **[WE-AHEAD]** — **Our verifier+review stages are the "centralized validation bottleneck"** the Google/MIT study recommends. The ISP structure (impl → verify → review → parent) matches their centralized multi-agent topology for regulated-verification cases. Numbers validate the pattern.

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

8. **Ontology is the load-bearing primitive that decides what gets remembered.** ashwingop's Part 3 names this directly: an ontology is the set of concepts and relationships a system uses to make sense of a domain. In a company, it decides whether something in conversation is a decision, commitment, objection, escalation, dependency, assumption, customer pain, owner, precedent, or open question — and those labels decide what survives capture. Our `NoteType` enum is implicitly an ontology, but it's coded for code-shaped artifacts (decision/rule/lesson/research-claim) and missing the conversation-shaped labels organizations need. Reinforced by the_smart_ape's source tiers, arscontexta's `cognitive_grounding`, kevin's authorship axis, akshay_pachaar's episodic/semantic/procedural — five articles converge on "the labels you choose at write time decide what's recoverable at read time." Two consequences: (a) extend `NoteType` with the conversation-shaped labels; (b) commit explicitly to "the ontology evolves; the system must reread its own past" as the dream-cycle's load-bearing motivation, not a hygiene pass. Parts 4–7 extend this: (c) action memory adds a *second ontology axis* — not just what something is, but when it should wake something up (trigger) vs what actually happened (execution) vs what the outcome was — and (d) "substrate generalizes; ontologies don't" (Part 7) confirms our scope-chain model is correct architecture; the per-function lens (NoteType + interaction labels) should be a refactored two-level structure, not a flat enum.

10. **The knowledge layer and the control layer are both required; most teams under-build the second.** ghumare64 names this split explicitly (knowledge = docs/skills/memory; control = state machine/tool policy/checks/approvals/rollback). Our stack already has both layers — TASKS.yaml + ISP is the control layer; skills + CLAUDE.md + KG is the knowledge layer — but we haven't documented the split or made per-task tool permissions (`allowed_tools`) and machine-checkable verification evidence (`exit_code | file_exists`) first-class. Reinforces sullyai (context engineering, not iteration), thealexker (R.P.I. phases), and codex-multi-agent (front-load constraints). akshay_pachaar's IdeaBlock adds: governance (clearance level, version state) belongs in the data layer, not the orchestrator — the same principle applied to the knowledge layer. mem0ai's Decay adds the recency dimension: recently-accessed knowledge should rank higher without requiring event-driven staleness triggers. trq212 adds the output layer: HTML for humans, YAML/Markdown for agents — maintaining this two-layer distinction prevents us from collapsing machine-readable and human-readable artifacts into one.

9. **Single-agent baseline is the correct default; multi-agent is a measured upgrade.** alphasignalai (citing Stanford + Google/MIT studies) adds empirical grounding to what sullyai, thealexker, and codex-multi-agent argued by reasoning: multi-agent coordination amplifies baseline errors by up to 17.2×; for tool-heavy tasks (>10 tools), coordination efficiency drops 2–6×; single-agent matches multi-agent when the thinking budget is equal. Our ISP fanout gating (eligible → fanout vs single delegation) is architecturally correct. Two additions: (a) pre-answer scaffolding — the orchestrator should explicitly enumerate ambiguities and candidate interpretations before committing to a delegation plan; (b) the 45% accuracy threshold: if estimated single-agent success on a write scope is above 45%, stay single-agent; only fan out when decomposition adds more than coordination costs.

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

- **F.3.3 — MCP tool surfaces (`kg_ingest`, `kg lint`, `get_impact_radius`) are available on all four platforms.** ~~The KG doc treats MCP as a unified runtime, but Codex CLI and Copilot do not currently run MCP.~~ *Corrected 05/03/2026: Codex CLI and Copilot do run MCP — the original claim was incorrect.* The real tension is ergonomics: CLI invocations (`da kg ingest …`) have more LLM training data behind them and degrade more gracefully in constrained environments than MCP tool calls. Recommendations to expose ingest/lint as MCP tools should therefore be paired with: (a) a CLI fallback that is a first-class path, not a second-class degradation path; and (b) a rule in the corpus that says "prefer `da kg …` CLI over the MCP tool variant when the platform's tool-call latency is above N ms or when the task is script-driven." This is a pragmatic ergonomics decision, not a capability gap.
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
- **NEW C.14 — extend `NoteType` with action-memory workflow-trace sub-types** (trigger / procedural / execution / outcome) from ashwingop Part 4. These map to existing pipeline artifacts: `active/fold-back/` = trigger traces, plan body = procedural, `merge-back.md` = execution, `impl-results.md` = outcome. Add a `trace_kind` sub-field (orthogonal to `NoteType`) rather than multiplying the flat enum. (ashwingop Parts 4–7 §A.2.)
- **NEW C.16 — add `allowed_tools` + `verification_evidence_kind` to TASKS.yaml** as the two missing fields that turn task specs from advisory runbooks into control runbooks. `allowed_tools` bounds what tools this task may call (mirroring app-type-profiles' `impl_defaults.allowed_tools`). `verification_evidence_kind: exit_code | file_exists | json_assertion | prose_only` makes verification machine-checkable; `da workflow checkpoint` should flag `prose_only` tasks as needing stronger evidence. (ghumare64 §A.3.)
- **NEW C.17 — add `last_accessed_at`, `access_count`, and `version_state` to `KGNote`** as a combined field rollout alongside the `author`/`tier`/`cites` schema (F.2.1/C.1). `last_accessed_at` + `access_count` feed Memory Decay-style recency scoring in `kg query` and dream-cycle pruning. `version_state: current|deprecated|draft|approved` makes provenance state machine-readable without conflating it with `ArchivedAt`. One schema rollout, five new fields. (mem0ai §A.2; akshay_pachaar IdeaBlocks §A.1.)
- **NEW C.15 — add an empirical basis to the `eligible` fanout criterion**: before escalating to multi-agent fanout, the orchestrator prompt should explicitly ask whether estimated single-agent success is above or below the 45% threshold identified by the Google/MIT study. Tool-heavy tasks (>10 tools) should default single-agent regardless. This is a prompt-level change to the `orchestrator-session-start` skill, not a pipeline change. Pairs with pre-answer scaffolding: enumerate ambiguities + candidate interpretations before committing to a delegation bundle. (alphasignalai §A.4; reinforces sullyai §A.3.)

---

## Part G — Research Profile Verification Pass (2026-05-08)

*Running the `research` profile verifier chain (`citation-presence → source-freshness → rubric-check`) against this corpus, as committed to in §F.4.2 and §F.5 NEW C.10. This is the self-test: treating each evaluation section as a `write_scope_kind: document` work item and auditing it against the profile's three verifiers.*

---

### G.1 citation-presence (hard verifier)

**Rule:** Every claim in a mapping block must be traceable to a specific article handle, spec section, or plan. Orphaned claims (no cite, no source) are citation failures.

**Findings:**

| Location | Claim | Citation status |
|---|---|---|
| Part B Theme 1 ("Camp 2 is winning") | five articles cited | pass |
| Part B Theme 2 ("derivation is load-bearing") | five articles cited | pass |
| Part B Theme 3 ("compounding is nightly") | four articles cited | pass |
| Part B Theme 4 ("context engineering > iteration") | "30%+ accuracy drop" attributed to akshay_pachaar | pass |
| Part B Theme 9 ("single-agent baseline") | "17.2×", "45%", "0.466 vs 0.074-0.234" attributed to alphasignalai citing Stanford + Google/MIT | pass — but citation is second-hand; original study links not captured in article file |
| §F.1 verifier-evolution paragraph | references "E.4", "C.6", "app-type-profiles §6" | partial — internal cross-refs to evaluation doc sections rather than article handles; acceptable as in-corpus citations |
| §F.2.3 open-questions framing | cites spec sections by file path + §N | pass |
| §F.3.3 MCP claim (Codex/Copilot don't run MCP) | no source — and already flagged as **incorrect** by human correction 05/03/2026 | **FAIL — incorrect uncited claim, corrected in-place; correction noted but not removed** |
| §C.13 (Part C body) | "(ashwingop §A.2)" | pass |
| Part A ashwingop Parts 4–7 Cons, "RL inside the company" | attributed to Part 6 | pass |

**Summary:** Two actionable items:
1. **Part B Theme 9** — the Stanford + Google/MIT study URLs should be captured in `alphasignalai-single-vs-multi-agent.md` article file for forward reference. Currently the article only cites the newsletter's paraphrase. Low risk since the studies are cited through a real article extract, not fabricated.
2. **§F.3.3** — the incorrect MCP claim was corrected by human note but the incorrect paragraph text remains. It should be struck through or replaced rather than just annotated. Recommendation: replace the body text with the corrected claim (Codex CLI and Copilot do run MCP) and move the discussion to "CLI vs MCP ergonomics" rather than capability absence.

---

### G.2 source-freshness (soft verifier)

**Rule:** Flag article sources whose host may have moved or whose content is more than 18 months old relative to evaluation date.

**Findings:**

| Article | Date | Freshness status |
|---|---|---|
| techwith_ram — *Knowledge Graphs Blazing Fast* | Unknown (inferred ~2024) | **SOFT FLAG** — no date in article file; article discusses SPARQL/triple stores without recent LLM context, suggests pre-2025 |
| multi-agent-memory-dkg — *From AI Memory Silos* | Unknown | **SOFT FLAG** — §F.2.6 (now F.2) noted "pre-date late-2026 reorganizations"; OriginTrail DKG v9 reference should be verified against current docs |
| jhleath — *Agents Share Environments, Not Data* | Unknown | SOFT FLAG — references "Archil's Serverless Execution" service; verify the service is still operational |
| karpathy — *Second Brain Pattern* | Unknown | SOFT FLAG — Karpathy-attributed content, likely 2023-2024; may predate multi-agent norms |
| arscontexta — *Claude Code Plugin* | Unknown | Pass — claims are architectural, not product-version-specific |
| All ashwingop Parts 1–7 | 2026-04-28 through 2026-05-07 | Pass — fresh |
| alphasignalai | 2026-05-06 | Pass — fresh |
| All other articles | Various 2025-2026 dates | Pass |

**Action on soft flags:** techwith_ram, multi-agent-memory-dkg, jhleath, karpathy — before any recommendation from these four is promoted to a plan, verify the source URL is live and the article content hasn't been superseded by a newer version. No immediate blocking action required.

---

### G.3 rubric-check (hard verifier)

**Rule:** Each §A entry must have: Core (what the article claims), Pros, Cons, Mapping with at least one labeled item ([OVERLAP-SHARPEN], [GAP-ADOPT], or [WE-AHEAD]).

**Findings:**

| Entry | Core | Pros | Cons | Mapping with labels | Status |
|---|---|---|---|---|---|
| techwith_ram | ✓ | ✓ | ✓ | ✓ (3 labeled) | pass |
| arscontexta | ✓ | ✓ | ✓ | ✓ (3 labeled) | pass |
| multi-agent-memory-dkg | ✓ | ✓ | ✓ | ✓ (3 labeled) | pass |
| All §A.1 remaining entries | ✓ | ✓ | ✓ | ✓ | pass |
| ashwingop Parts 2+3 | ✓ (two Cores) | ✓ | ✓ | ✓ (8 labeled) | pass |
| ashwingop Parts 4–7 (new) | ✓ (four Cores) | ✓ | ✓ | ✓ (7 labeled) | pass |
| alphasignalai (new) | ✓ | ✓ | ✓ | ✓ (5 labeled) | pass |
| annimaniac | needs spot-check | — | — | — | not re-verified here; was pass at Part D add |
| §F sections | not article entries — analytical prose | N/A | N/A | N/A | not applicable |

**One rubric gap:** The claude-obsidian-memory-stack, claude-obsidian-ai-employee, and second-brain-needs-two-authors entries (§A.2) were added before the rubric was formalized. They have abbreviated Mapping sections (1–2 labeled items) vs the 3–8 norm for later entries. **Soft flag** — not a hard failure but worth expanding in a future enrichment pass.

---

### G.4 Verification verdict

| Verifier | Result | Blocking? |
|---|---|---|
| citation-presence | 1 hard failure (§F.3.3 incorrect uncited claim), 1 soft (Theme 9 second-hand studies) | Soft block on §F.3.3 — fix below |
| source-freshness | 4 soft flags (techwith_ram, multi-agent-memory-dkg, jhleath, karpathy) | Non-blocking; note before promoting to plans |
| rubric-check | 3 abbreviated §A.2 entries (soft) | Non-blocking; flag for future pass |

**§F.3.3 fix (citation-presence hard failure):** Replacing the incorrect claim about Codex/Copilot MCP support in §F.3.3.

---

## Part H — 2026-06-24 addendum: five x.com-sourced articles vs the KG anchor specs

**Why a new part:** This batch of five was sourced fresh (x.com tweets) and explicitly tasked against four anchor specs — `graph-backend-adapter-contract`, `scoped-knowledge-graphs`, `work-tracking-storage-abstraction`, `kg-command-surface-readiness` — plus the memory theme **"KG is SOT for SDD artifacts + typed working/operational/semantic/episodic views."** Raw sources are in `research/articles/`; per-article concept extracts in `research/extracts/`. Rubric and `[OVERLAP-SHARPEN]` / `[GAP-ADOPT]` / `[WE-AHEAD]` labels are the same as Part A.

**Retrieval note (honesty):** all five x.com tweets were navigated directly via Playwright (the `article-extract` skill, Path B); each was confirmed to be a bare link card. Four articles' full bodies were recovered from the authors' own cross-posts (GitHub README, dbreunig.com, kmad.ai, raisingpixels.dev Substack). One — aminkarbasi "Jailbreakers and Prompt Optimizers Are Looping the Same Problem" — is an X-native long-form Article with no public author cross-post; Playwright reached the tweet card (verified title, opening excerpt, exact 2026-06-24 timestamp) but the article *body* redirected to an X login wall and is not retrievable without an authenticated X session. Only its title/thesis/tagline and 8 verified arXiv references were recoverable at the time. **Update 2026-07-12:** the aminkarbasi body was recovered in full via a logged-in Claude-in-Chrome x.com session (full verbatim body now in `research/articles/jailbreak-prompt-opt-duality.md`); H.4 below is revised against the full text and the former "Part H — PENDING" section is resolved in place.

### H.1 compaction-orchestrator — *Your Agent Does Not Need One Summary, It Needs a Compaction Plan* (@luhsnaa)

**Core.** Context compaction is a *routing decision*, not a single summary. Classify each context segment, route each to a strategy (`keep_verbatim` / `extract_active_error` / `externalize_for_retrieval` / `structured_summary`), and emit an inspectable **compaction plan** plus a derived runtime view over an **immutable raw event log** that is never overwritten. Policy-driven (`balanced` / `cost_first`), pluggable strategies, SQLite store, importers for Claude Code & Codex JSONL. Self-reported ACCS metric (coding 0.836 vs 0.698 rolling-summary; support 0.773 vs 0.474; voice 0.886 vs 0.767).

**Pros.** A running instance of exactly our derivation model — immutable source + provenance-bearing derived views. The Claude-Code-JSONL importer + immutable log is essentially our session-handoff-journal substrate. Per-segment routing is concrete prior art for episodic→semantic projection.

**Cons.** Alpha; ACCS numbers self-reported on curated fixtures. Policy is deterministic-only today (LLM strategies are future work).

**Mapping.**
- **[OVERLAP-SHARPEN]** vs `scoped-knowledge-graphs` derivation model — the compaction *plan* is a provenance-bearing derivation shaped like our `{reason, because[], fired_at}`. They make the plan a first-class auditable artifact; our spec keeps derivation provenance but doesn't expose a "what was deliberately dropped" record. Steal the inspectable-plan idea.
- **[GAP-ADOPT]** vs `work-tracking-storage-abstraction` §3A typed views — a `compaction-plan` note type in the **episodic view** (edges `derived_from` → raw events, `preserves` → surviving facts) lets handoff/recovery replay *kept vs dropped*, not just a summary. Their `externalize_for_retrieval` (pointer-to-canonical) is a candidate for keeping the journal lean while the KG holds the payload.
- **[WE-AHEAD with quirk]** — our scope-chain + event-driven staleness is a richer substrate than their single SQLite store; the quirk worth importing is the **typed, domain-specific output package** (e.g. a support handoff carrying identity/escalation/policy/next-action) instead of free text.

### H.2 prompt-debt — *The Problem is Prompt Debt* (@dbreunig / Drew Breunig)

**Core.** Hand-written natural-language prompts are great prototypes but accrue as technical debt and silently lock you to one model (NL was never a specification language; spurious cross-instruction interference makes prompts brittle). Specify behavior with **evals / typed specs, not prose**; stop hand-writing prompts — *search* for them via DSPy/GEPA. "You can't be model agnostic if you're hand-tuning prompts."

**Pros.** Independent validation of our KG-as-SOT direction (spec/eval canonical, prose a regenerable projection) and of verifier/reviewer stage profiles + evidence-backed write_scope. Sharp portability framing.

**Cons.** Our *own* skills, CLAUDE.md, and delegation bundles are exactly the large hand-tuned NL prompts he warns about — and we don't yet auto-optimize prompts against evals.

**Mapping.**
- **[OVERLAP-SHARPEN]** vs KG-as-SOT theme + `work-tracking-storage-abstraction` D1′ — our "committed YAML = regenerable projection, prose indexed into the semantic view" is the same instinct; he sharpens *why* (durability/portability), not just hygiene.
- **[GAP-ADOPT]** — a **`prompt-debt audit` lens** over the operational view: query for skills/rules with no attached eval and high edit-churn / repeated-instruction ("fighting the weights") signatures, flag as debt. Pairs with §3A's result→skill/rule correlation edges.
- **[WE-AHEAD]** — we already keep behavior in typed stage/app-type profiles + verifier evals rather than one monolithic prompt; the gap is auto-optimization, not structure.

### H.3 token-capital-efficiency — *Token Capital Efficiency* (@kmad / Kevin Madura)

**Core.** Value captured per dollar of tokens. Determinism↔probabilism spectrum: the *what* (intent) never disappears, only specification of the *how* fades. "Wrap the probabilistic core in a deterministic shell." Motion: **define → match → measure → optimize.** Evals become owned, composable, compounding IP — a "digital inventory of tasks + evals." Match model to task by *measuring* it (capability + cost), then ride the cost curve until accuracy crosses tolerance.

**Pros.** Names the economic learning-loop that §3A's result→profile/skill/rule/spec correlation edges are built to support. "Wrap probabilistic core in deterministic shell" == our deterministic CLI/workflow shell with the KG as the durable boundary. "The *what* never disappears, only the *how* fades" is a clean articulation of why intent/spec lives in the KG while agent prose is disposable.

**Cons.** Strategy piece, no benchmarks of its own; builds on a referenced Satya Nadella "token capital" article.

**Mapping.**
- **[OVERLAP-SHARPEN]** vs `work-tracking-storage-abstraction` §3A self-improvement loop — his "digital inventory of tasks + evals that compounds" is our typed operational+episodic views; he supplies the economic vocabulary (token capital efficiency) to motivate it.
- **[GAP-ADOPT]** — add `token_budget` + `tolerance` to stage/app-type profiles; emit per-task token spend into the episodic view; a `da kg` query computes efficiency per task type and recommends model down-shifts where eval pass-rate stays above tolerance.
- **[OVERLAP-SHARPEN]** vs `kg-command-surface-readiness` — "you can't move to a cheaper model without a defined bar" reinforces that `da kg` surfaces must make the eval/measurement boundary queryable, not just return notes.

### H.4 jailbreak-prompt-opt-duality — *Jailbreakers and Prompt Optimizers Are Looping the Same Problem* (@aminkarbasi) — RESOLVED 2026-07-12: full body recovered

**Core (full body, deep-read 2026-07-12).** Automated jailbreaking and automated prompt optimization are the same iterative loop (propose → evaluate → score → reflect/refine), differing only in objective function — "similar feedback loops with a different sign on the reward." "Prompt is a control surface." Attack lineage PAIR→TAP→Adversarial Reasoning; optimization lineage APE→DSPy→MIPRO→GEPA→ACE. The communities should cross-pollinate: jailbreakers own hard-won experience with **imperfect judges, black-box search, query budgets, transferability, and evaluator hacking**; prompt-optimization owns better abstractions for **programs, traces, metrics, data splits**. The body closes with **FAPO (Fully Automated Prompt Optimization)** — previously an unverifiable mirror label, now confirmed as the article's own system: the whole loop run through coding agents (Codex/Claude Code): run evaluations → classify failures → identify dominant failure modes → generate targeted variants → review changes → compare against previous best → **accept only if it improves the system** → iterate.

**Pros.** Our verify/score/refine iteration loop is the same abstraction. GEPA/ACE "evolving playbooks" mirror our operational view evolving from result correlations; ACE's **"context collapse"** is a direct warning for any KG semantic-view summarization (echoes H.1). The full body adds the sharpest available list of judge-failure concerns (evaluator hacking, overfitting to a judge, transferability) — exactly the failure surface our verifier stage must be hardened against.

**Cons.** Position piece; FAPO is described, not benchmarked. One sentence mis-attributes the loop's popularization ("popularized by @steipete by dates a few years back," sic).

**Mapping.**
- **[OVERLAP-SHARPEN]** vs `planner-evidence-backed-write-scope` + verifier profiles — the emphasis on **evaluator/judge robustness** says: harden the verifier stage as the judge the whole loop rides on; a weak judge breaks both safety and quality. Deep-read addition: their evaluator-hacking vocabulary names what the transcript plan's OH-E3 finding (substring scorer rates unverifiable cursor self-reports `high`) demonstrated empirically — a gamed judge.
- **[OVERLAP-SHARPEN — new, from FAPO]** — FAPO's accept-only-if-it-beats-previous-best is structurally our parent verdict gate + bounded fold-back re-entry (craft §3): keep changes only on demonstrated improvement, reject otherwise, iterate within a budget. Independent convergence on gate-shaped optimization.
- **[GAP-ADOPT]** — adopt GEPA/ACE-style *reflective* (not RL) optimization for evolving operational-view skills/rules with the verifier as scoring judge, guarded against "context collapse."
- **[GAP-ADOPT — security, no current spec]** — "prompt is a control surface" implies KG-canonical skills/rules are an attack surface. Consider an adversarial-eval gate / red-team edge type before a skill/rule is promoted. No anchor spec covers this.

### H.5 agent-without-phone — *How I Run an AI Agent Without Touching My Phone* (@madebydia / Diana Park) — adjacent

**Core.** The interface (and the social signal it sends), not the agent, is the problem. Three capture tiers: fast (Watch dictation → iMessage executor → agent), slow (notebook, classify-later), none (scheduled/cron). Automation's value is removing the *decision point* before the task, not just task-minutes. "The best interface is no interface."

**Pros.** *Ingest-now, classify-later* (slow capture) and proactive scheduled surfacing (no capture) validate an async path into the KG over pull-only `da kg query`.

**Cons.** Personal attention/UX essay; no metrics or technical contracts. Weakest structural tie of the five.

**Mapping.**
- **[GAP-ADOPT — soft]** vs episodic view + background-worker direction — a `capture_tier` attribute on episodic ingest (synchronous command / deferred note / autonomous-scheduled) so the operational view can distinguish human-intent from machine-generated knowledge.
- **[OVERLAP-SHARPEN]** — "remove the decision point" is a clean rationale for KG-driven *push* (stale-note alerts, eligible-task push) over pull-only commands; reinforces `kg-command-surface-readiness`'s "empty result ≠ no impact" surfacing instinct.

### H.6 Part H synthesis (cross-cutting)

1. **Eval/spec is the durable boundary; prose is disposable.** H.2 (prompt debt), H.3 (eval-as-IP), and H.4 (eval-as-objective) independently converge on the KG-as-SOT claim: the canonical, ownable artifact is the *measurement/spec*; the natural-language *how* is a regenerable projection. Strongest external validation yet of D1′ + §3A.
2. **Immutable canonical source + provenance-bearing derived views.** H.1 (compaction plan over an immutable event log) is a working instance of our scoped-KG derivation model; H.4's ACE "context collapse" is the failure mode that justifies *why* derivations must be provenance-tracked, not lossy re-summaries.
3. **The self-improvement loop is an economic, compounding asset.** H.3 frames §3A's correlation loop as "token capital efficiency"; H.4 supplies the concrete optimizer (GEPA/ACE reflective evolution) + judge-robustness needed to run it. Together they argue operational+episodic views should carry cost/tolerance + eval edges.
4. **Optimization and adversarial robustness are the same machinery.** H.4's "prompt is a control surface" reframes soon-to-be-KG-canonical skills/rules as both optimization target and attack surface — a gap none of the anchor specs cover.
5. **Capture/surfacing is a first-class design axis.** H.1 (routing what enters context) and H.5 (fast/slow/none capture tiers) argue that how knowledge enters/exits the working set is a deliberate, typed decision — supporting a `capture_tier` dimension on episodic ingest and a push (not just pull) `da kg` surface.

### Part H — PENDING: RESOLVED 2026-07-12
- **H.4 aminkarbasi** full body recovered via a logged-in Claude-in-Chrome x.com session (no maintainer paste needed); `research/articles/jailbreak-prompt-opt-duality.md` now carries the full verbatim body and the `NEEDS-PASTE` marker is cleared. H.4 above revised against the full text; the three original mappings held unchanged and one new mapping (FAPO ≈ parent verdict gate) was added. The previously-excluded "FAPO" label is confirmed as the article's own system name.

### Part H — open questions raised
1. **Episodic→semantic projection policy:** H.1 has a running per-segment router; §3A describes projection abstractly. Do we adopt a compaction-plan-style typed router for what survives into the semantic view, and is the plan itself a KG node?
2. **Cost/tolerance as a profile dimension:** Should stage/app-type profiles carry `token_budget` + `tolerance`, and should the episodic view record per-task token spend to compute efficiency (H.3)?
3. **Reflective optimization of operational-view artifacts:** Do we use GEPA/ACE reflective evolution (verifier as judge) to evolve skills/rules, guarding against "context collapse" (H.4)?
4. **Adversarial/red-team surface for KG-canonical skills/rules:** Do we need an adversarial-eval gate / red-team edge type before promotion (H.4)? Not covered by any anchor spec.
5. **Prompt-debt audit lens:** Can we query the operational view for un-eval'd, high-churn, repeated-instruction skills/rules and flag them as debt (H.2)?
6. **Capture-tier provenance on ingest:** Is human-intent vs. deferred vs. machine-scheduled provenance (H.5) worth modeling on episodic ingest?

---

## Part I — 2026-07-03 addendum: six agentic-engineering articles (owner-flagged)

Six x.com threads flagged by the owner to sharpen the `da-recipe-scripts` spec. Originally
teaser-level (full X Articles login-walled). **Deep-read complete 2026-07-12:** all six full
bodies recovered via a logged-in Claude-in-Chrome x.com session (the X MCP read path was not
needed). Article files under `research/articles/` now carry full verbatim bodies; distilled
takeaways in `research/extracts/agentic-engineering-2026-07.md` (enriched same date). Each
entry below keeps the teaser-pass claim and adds its deep-read delta.

### I.1 elvissun — /goal + Loss Functions
Autonomous loops should optimize an explicit *loss function*, not run fire-and-forget. Maps to
r1-outcome-scoring + the r4 eval harness: make each run's objective explicit and measurable.

**Deep-read delta.** The full body supplies a four-part loss-function anatomy: **target** (large
enough that enumeration doesn't pay; eval blinded from the agent), **constraints** (wall-clock
budget first — "agents have no sense of time" — plus money caps, surface, methodology),
**instruments** ("a constraint without an instrument is a vibe — the agent will violate it
cheerfully because it can't tell it's violating it"; one CLI command per constraint: time
accounting, provider budget, LLM spend, and *self-aware token usage of the loop itself*), and
**forced entropy** (overfit reflection every cycle, non-obvious jump on stall, and "keep an
iteration log … so it can look back and reflect across compactions"). Three documented rounds of
eval-cheating (seed mirroring → learn-by-miss keyword enumeration → keyword ballooning) are the
empirical Goodhart record. Consequences: the instruments list is the transcript-analysis plan's
cost instrumentation (craft §5) argued from the loop-design side; the iteration-log rule is our
iter-log discipline verbatim; the blinded-eval + cheat rounds are direct design input for
r4-code-task-generation-eval (blind the answer key; fence cheap paths).

### I.2 iamgrigorev (Poolside) — designing ML experiments that teach
Throughput without hypothesis + clean readout is noise. Reinforces
`prototype-experiment-fidelity-gate` and the harness discipline.

**Deep-read delta.** Adds four adoptable mechanisms: **dated shared baseline configs** (one
baseline in main, retrained + re-dated after verified features; "people still say 'baseline,'
but the word does not mean one thing anymore") = pinned eval baselines for the r4 harness;
**experiments must finish** ("an unfinished experiment is … just compute spent"; killing early
biases taste toward ideas that look good immediately) = the Pareto rubric's completed-run
discipline; **manual run table** (accountability via hand-written log) = the iter-log; and
**efficiency gain = C_predicted / C_observed** via occasional scaling-law fits — an
interpretable effect-size unit ("how much compute did the method save?") that the Pareto cost
axis can mirror as cost-per-task-completed. "The evaluation should match the question" is our
lens-selection rule stated for ML.

### I.3 sergeykarayev — Your Agents Should Be Multiplayer
Siloed per-agent context forces humans to be couriers. Validates orchestrator+workers+
`session-handoff-journal` and "shared coordination backend > isolated-worktree status."

**Deep-read delta.** The concrete wins are sharper than the teaser: **code review by joining
the builder's session** ("the session holds the full history of decisions, including the dead
ends" — ask the agent, not the author), **handoffs carrying what-the-agent-already-tried**, and
around-the-clock shift work. "The model is no longer the bottleneck. Coordination is." is the
one-line thesis of `full-loop-orchestration-runtime`; the dead-ends-preserved session is
exactly the session-handoff-journal payload (episodic KG view).

### I.4 thealexker — token engineering, not tokenmaxxing
A $200 unmonitored expensive-model run; engineer + monitor spend. External validation of the
`worker-model-selection` policy (scope model to task; expensive tiers not a default).

**Deep-read delta.** Three proposals in the body: **routing** (lightweight LLM-judge classifier
deciding where tokens flow — stage_profiles cheap-tier routing restated), **evals** ("measure …
the cost per task completed, not the dollar amount per token"; LLM-generated code is technical
debt until proven otherwise) and **knowledge sharing** (searchable org knowledge so models stop
re-retrieving — the KG-as-SOT argument). Cost-per-task-completed is the unit the transcript
plan's productive-token normalization (O3) should report at the top level; token-burn
leaderboards as Cookie Clicker is the anti-metric warning for any token dashboard we build
(r2-observability-dashboard).

### I.5 nifinet — signal-based outbound engine (Codex)
watch → validate-a-real-signal → decide → draft. A concrete template for the dot-agents X
outreach engine once the X app is approved.

**Deep-read delta.** The architecture is more reusable than the teaser suggested: **six
codex-exec jobs around one schema-validated shared record** — "the prompt will drift; the
schema keeps the next step usable" is delegation-bundle contract discipline independently
invented for GTM. **outputs/ vs memory/ separation** ("outputs are what happened today; memory
is what the system should remember tomorrow") = iter-log vs lessons. **Writer ≠ checker** ("the
same step that writes the message will often defend it") is RULE-7's rationale. The weekly
*learn* step optimizing for selectivity ("outbound improves when the system says no more
often") is a fold-back loop whose success metric is cuts, not throughput.

### I.6 realmcore_ (akira) — Onyx: a programmable runtime for agent orchestration  ← the sharpener
Onyx is a VM for *programmable* agent orchestration ("orchestration as software engineering") —
the maximal end of the orchestration spectrum.

**Deep-read delta.** The full body *confirms and arms* the boundary reading. Onyx's own argument
for the VM is: **"a skill is not a binding behavioral contract. You cannot get a guarantee out
of text"** — which is simultaneously (a) why recipes must stay mechanical (D3) and (b) why our
runtime gates are typed code, not prompt prose (CC runner verdict gating; craft §2/§4).
Adoptable quirks — routed to `full-loop-orchestration-runtime`, NOT to recipes: the
**`checkpoint` primitive** (fixed-shape notify from a running program to its parent ≈ our
reconcile/monitor events), **state-/schema-gated subagent completion** ("state, and schema
adherence, gate subagent completion" ≈ schema-valid merge-back), **loud orchestration-error
semantics** (agent blocked/failed/out-of-budget throws like a code error ≈ craft §3's
route-crash-through-reconcile rule), and **per-program budget caps + per-stage model routing**
(≈ stage_profiles). Their named open problem — the **durability model** (resume/lifecycle) — is
the part our da-owned lifecycle + pid-aware locks + wave reconciliation already answers.

### I.7 The `da-recipe-scripts` sharpening (why this pass mattered)
Onyx could provoke the wrong instinct — "make `da` recipes programmable (control flow, state, a
DSL)." That is wrong for dot-agents, because **we already own the programmable-orchestration end:
the `Workflow` JS engine** (fan-out, pipelines, control flow, budget). We do not need a second
programmable runtime. So Onyx sharpens `da-recipe-scripts` by **reinforcing its boundaries**:

- **Reinforce D3 (mechanical-only):** recipes stay deterministic, no control flow. Any branching/
  looping/dynamic dispatch is a signal the work belongs in a **skill** (judgment) or the
  **Workflow engine** (programmable orchestration) — never a recipe. Resist DSL creep.
- **Resolve OQ3 (vs the Workflow engine) decisively** — three complementary layers:
  - **Workflow engine** = programmable/agentic orchestration (dot-agents' "Onyx").
  - **Skills** = judgment sequences; may *call* a recipe for a mechanical spine.
  - **`da`-recipes** = the deterministic, cross-platform, versionable *floor* — fixed `da`-command
    sequences below skills. Deliberately NOT programmable.
- **Spec edit to fold in when building `da-recipe-scripts` p1:** add to `design.md` §2 D3 / §7 the
  positioning line — *"Recipes are the deterministic floor of orchestration; the Workflow engine is
  the programmable ceiling. If a recipe wants to become programmable, it wants to be a workflow."*
- **No plan/scope change:** the 5 tasks and the minimal line-oriented format (D2) stand; the value
  is determinism + portability + versionability, not expressiveness.

### Part I — PENDING: RESOLVED 2026-07-12
Full bodies recovered for all six articles via a logged-in Claude-in-Chrome x.com session (the
`NEEDS-PASTE` markers in `research/articles/` are cleared; the X MCP read path was not needed).
Deep-read deltas folded into I.1–I.6 above. **I.7's boundary holds unchanged** — the Onyx body
strengthens it (see I.6 delta) and adds one clause worth appending to the positioning line when
`da-recipe-scripts` p1 lands: Onyx models orchestration procedurally in code because *"you don't
need a powerful LLM to think through the orchestration after it is authored the first time"* —
the same cheap-after-authoring argument behind emitting swarm YAML from the IR instead of
hand-writing it (craft §7, anti-pattern #1).

---

## Part J — 2026-07-12 addendum: eight agent-efficiency / review / loop-craft articles (second pass, inflight-plan-anchored)

**Why a new part:** a user-supplied reference batch extracted 2026-07-12 (full bodies in
`research/articles/`; distilled batch extract in
`research/extracts/agent-efficiency-and-review-2026-07.md`). Unlike Parts A/H/I, this pass is
explicitly anchored against **inflight design work**: the
`transcript-analysis-and-pipeline-craft` plan (craft doc `craft/full-loop-craft.md` §§1–7;
synthesis themes **T-\*** and outcomes **O1–O14**; Pareto hypotheses **H1–H6**; falsification
rubric **RULE-7**) and its parallel `full-loop-orchestration-runtime` plan. The plan-local
consumer map already exists at
`.agents/workflow/plans/transcript-analysis-and-pipeline-craft/evidence/prior/external-references-2026-07-12.md`
(per-article consumers, local verification runs, cross-cutting synthesis); **this part does not
duplicate it** — it evaluates the same sources with the Part A/H rubric for the research corpus,
carries the plan digest's corrections, and adds the KG-and-adjacent angle the plan digest
deliberately omits. Rubric labels as in Part A. Part E's trust gate applies: priorities here are
author judgment.

### J.1 gille — *Claude Code's Token Counts Are Wrong* + energy-monitor scripts (Magnus Gille, 2026-02-24)

**Core.** Claude Code JSONL `usage.input_tokens` is a never-finalized streaming placeholder
(their corpus: 75% of entries ≤1; input undercounted 102–174×); `output_tokens` partially
placeholder and excludes thinking (10–17× under); `cache_read`/`cache_creation` are accurate
(~1×); the same `requestId` appears 2–10× (51–55% duplicates); statusbar `context_window` totals
are the accurate ground truth. Scripts: `sum_jsonl.py` dedups by *last entry per requestId* and
classifies ≤1 placeholders; `analyze_tokens.py` validates via statusbar deltas (confirms input
excludes cache_creation — no double count — and output includes thinking). Author-flagged
**PRELIMINARY — PENDING REVIEW**; filed as anthropics/claude-code#28197.

**Pros.** Directly load-bearing for the transcript plan's token accounting: names the exact
field-trust and dedup rules any CC-source cost row needs. The plan digest's **local
verification** materially revises the claims for our corpus: input≤1 = **12% (not 75%)** —
placeholder severity is version/era-dependent — but **76% of requestIds appear >1×**, with
per-field overcount ratios input 3.01× / output 3.02× / cacheRead 2.38× / cacheCreate 3.04×; our
CC evidence rows did NOT dedup by requestId (hence pareto erratum #2).

**Cons.** Preliminary, single-operator corpus, version-dependent (our numbers disagree on
placeholder rate while confirming the duplication problem). The energy-monitor's mWh constants
are back-of-envelope.

**Mapping.**
- **[GAP-ADOPT — already consumed by the plan]** pareto erratum #2: dedup-by-requestId
  (last-entry-wins), placeholder exclusion, cache fields as the only high-trust CC fields;
  evidence-rubric amendment for CC-source rows; capability-matrix note (CC `has_tokens` ⇒
  *low-fidelity* tokens).
- **[OVERLAP-SHARPEN]** craft §5 "NEVER price a stage on raw `total_tokens`" (T-c1/O3) extends
  to: never trust raw CC usage fields without dedup + placeholder handling. Same rule, one level
  lower in the stack.
- **[WE-AHEAD with quirk]** craft §6 already attaches a telemetry-capability mask per harness;
  the quirk to import is their **delta-based validation method** (statusbar/context-window
  deltas as independent ground truth for field semantics) — a cheap Q1/Q2-style check our
  evidence rubric can require before trusting a new harness's token fields.
- **KG angle.** Per-harness token-field-trust rules are ingest-contract metadata on the episodic
  view (each cost row carries `field_trust` provenance), not scorer code — the same
  "labels-at-write-time decide recoverability" principle as Part B Theme 8.

### J.2 infracost — *We cut Claude's token usage 79% by redesigning our CLI for agents* (Glenn Gillen, 2026-05-18)

**Core.** Redesigned a CLI *for agent callers*: predicate pushdown (`--filter`, `--missing-tag`,
`--fields`, `--addresses-only`) eliminates model-composed jq/python pipelines; TOON tabular
output (uniform arrays → header + rows) −35% vs minified JSON at equal comprehension; a
benchmark harness (16 questions × 3 configs × 5 repeats, sandboxed HOME, PATH-pinned binary,
rerun-failed/rescore). Hard-question bucket: bare Claude 0/6 at $7.61 → skill+flags 6/6 at
$2.48; hard-bucket output tokens 113K→24K (−79%); accuracy 45%→100%.

**Pros.** The strongest external anchor for **H1** ("to move volume you must change the
pipeline, not the model"): they moved volume ~79% by changing the *tool surface*, zero model
change. Their harness discipline (paired repeats, rescore, pinned environment) is our
pareto-measurement-rubric independently invented. Accuracy and cost improved *together* —
evidence that output-volume work is not a quality trade.

**Cons.** Vendor case study on one CLI; TOON gains limited to uniform arrays; no CI reported on
the deltas.

**Mapping.**
- **[GAP-ADOPT]** `da` CLI agent path: predicate flags + a token-efficient tabular output mode
  on `workflow eligible`/`tasks` (candidate fold-back, named in the plan digest). Our `--json`
  envelopes are the analog of their pre-redesign JSON dumps.
- **[OVERLAP-SHARPEN]** craft §5 fixed-tax rule (T-c4/H4): their finding is the copilot
  67%-tool-def anchor approached from the tool-author side — the fix for fixed tax is owned by
  the CLI designer, not the loop.
- **[OVERLAP-SHARPEN]** their 16×3×5 rerun/rescore harness vs our live-wave protocol
  (snapshot-identical paired repeats, ≥3, bounded `--max-waves`): convergent measurement craft;
  their "rescore failed runs before concluding" step is worth adding to `pareto-live-review`.

### J.3 Green-PT — *honey-for-devs* (output-volume compression skill family)

**Core.** Three levers: less code (YAGNI ladder), less prose, denser agent-to-agent handoffs
(ESON, −51% lossless). Safety carve-outs are never compressed. Judged by a **4-model
cross-family panel** (median of Opus/Sonnet/Haiku/GPT) under a length-blind rubric. CCR =
lossy-but-recoverable compression for uniform arrays (sentinel + local cache + retrieve). PX =
image-rendered reads (~−80% tokens) but only Fable-class models read renders usably, with
silent-confabulation risk on exact strings. **Published negative result:** input-prompt
precompression saves ~0% on 266 real prompts. An honesty note retracts earlier
non-reproducible benchmark numbers.

**Pros.** The negative result corroborates J.2 and H1 from a third angle: the bill lives in
tool output and agent output, not the input prompt — the output-volume lever is *behavioral*.
The cross-family judge panel is an independent invention of RULE-7's diversity requirement.
Publishing a retraction is itself evidence of trustworthy methodology (rare in this corpus).

**Cons.** ESON/CCR formats unproven outside their harness; PX's confabulation risk makes it
unusable for anything anchor-bearing; benchmark numbers post-retraction are thinner than the
README implies.

**Mapping.**
- **[OVERLAP-SHARPEN]** RULE-7 / cross-family gate — with PostHog (J.4) this makes **three
  independent inventions** of cross-family, writer≠reviewer judging (theirs, PostHog's, ours);
  the falsification-review rubric's gate graduates from house convention to
  externally-corroborated practice.
- **[GAP-ADOPT — gated]** ESON-style dense A2A handoffs as a candidate for delegation
  bundles/merge-backs, importing their carve-out rule intact: compress prose, **never the
  contract fields** (write_scope, verdicts, anchors) — the analog of "safety carve-outs never
  compressed."
- **[WE-AHEAD]** our blocking gates are typed reject-on-BLOCKER/HIGH; their panel is
  median-of-scores. Do not import score-averaging for *blocking* decisions — averaging is for
  quality dials, vetoes are for gates.

### J.4 PostHog — *Stop being the code review bottleneck* (2026-07-10)

**Core.** Delegate review to a pipeline: qa-swarm spawns four reviewer lenses (qa-team /
security-audit / personal-voice / XP), review-triage classifies threads
**actionable / nit / ambiguous**, outer loop bounded at ≤3 iterations; writer-agent must never
self-review; different instructions AND different model providers per reviewer (cites "juries
over judges"). StampHog auto-stamper: deterministic gates FIRST (PR state, deny-list keywords,
size caps — per the authoritative StampHog README **>800 substantive lines OR >30 files**; the
newsletter's "500 lines/20 files" is a stale figure), then a simple LLM showstopper check;
fail-closed; LLM can tighten gates but never loosen; ~1/3 of merged PRs auto-stamped, 1.6K
PRs/month. "Verify by observation, not reasoning": decompose into stacked independently-runnable
PRs; screenshots/GIFs as review evidence. Cost admission: "something like 60% of my token spend
is burned automating the toil of handling CI and review."

**Pros.** The closest published production analogue to the transcript plan's verification/review
spine (craft §4) — multi-lens fanout, triage taxonomy, bounded outer loop, fail-closed
deterministic gating, writer≠reviewer — all running at 1.6K PRs/month scale.

**Cons.** Newsletter-level detail; StampHog thresholds calibrated to PostHog's codebase; the
60%-of-spend figure is one engineer's anecdote (but a useful order of magnitude).

**Mapping.**
- **[OVERLAP-SHARPEN]** their ≤3-iteration outer review loop is our `target_count: 3` bounded
  fold-back re-entry (craft §3) — independent convergence on small bounded budgets before
  terminal escalation.
- **[OVERLAP-SHARPEN]** RULE-7: "different models and providers for different reviewers" +
  writer-never-self-reviews, in production. Third leg of the J.3 convergence.
- **[GAP-ADOPT]** **deterministic-gates-BEFORE-LLM ordering** for our delegation/review gate
  (candidate fold-back, named in the plan digest): a cheap pre-LLM deny-list/size/state tier
  before dispatching review lenses would spare review tokens on auto-rejectable work — directly
  additive to H6 (review is the cheap-route target) by removing some review calls entirely.
  Import the invariants intact: fail closed; LLM tightens, never loosens.
- **[GAP-ADOPT]** review-triage taxonomy → fold-back routing labels (actionable→fix, nit→note,
  ambiguous→escalate) — maps cleanly onto task-scoped vs plan-scoped fold-backs plus
  owner-escalation.
- **[WE-AHEAD]** "verify by observation, not reasoning" is already our T-b5/OH-E3/O11 rule —
  never gate on self-report; require an anchor + a real tool/verifier record — held with
  transcript evidence, not anecdote. Their stacked-observable-PRs practice is the human-facing
  rendering of the same rule.
- Paul's 60%-of-spend-on-review figure is an external consistency check on **H6**: review/CI
  toil is a first-order token sink, which is exactly why the cheap-route-review contrast (C6)
  is the highest-leverage Pareto cell.

### J.5 aparnadhinak (Arize) — *What the hell is a loop, anyway?* (2026-07-04)

**Core.** "The loop" hides ≥4 architectures: **execution loop** (act-observe tool cycle; ends on
environment feedback — or the agent declaring done), **task loop** (Ralph loop: fresh context
per iteration against one spec; ends on spec+tests), **product loop** (software factory:
codebase+backlog, configurable human checkpoints, ratcheting auto-merge; Warp's Oz, Anthropic's
internal Tag at 65% of product-team code), **system loop** (autoresearch: outer loop improves
prompts/evals/harness; "the loop is the product"), plus the unnamed **oversight loop** (set
goals / allocate / cull — the ring where a human should live). Agentic MapReduce is a topology,
not a loop ("a loop without feedback is just a for statement"). "Naming a signal is not the same
as wiring it in. A loop without its signal doesn't converge." Anthropic's own factory reports
being bottlenecked on human review and conceptualization.

**Pros.** The cleanest shared vocabulary yet for where our pieces sit; every claim maps onto a
named artifact of ours. The exit-signal argument is the elvissun instrument argument (I.1)
restated at taxonomy level — and is the transcript-analysis plan's own justification: the
pipeline is the *wiring* of our loops' exit signals.

**Cons.** Vendor-authored (closes with an Arize AX pitch); the taxonomy is descriptive, not
empirical; second-hand conference quotes.

**Mapping.**
- **[OVERLAP-SHARPEN — consumed by the plan]** the loop-name mapping (inner swarm = execution;
  bounded per-task fold-back = task; `da` N-plan driver = product; pareto-live-waves +
  fold-back/proposal routing = system; user gate = oversight) belongs in the pipeline-architect
  skill's design instruction as the first architect question: *which loop are you editing?*
- **[WE-AHEAD]** our exit conditions are typed and tested (`available == 0` /
  `total_eligible == 0` quiescence, craft §1; RECONCILED sentinel) — most published loops end
  on the agent's say-so, which is exactly the execution-loop failure the article names.
- **[OVERLAP-SHARPEN]** "fan-out is a topology, not a loop" is a useful guard for our own
  vocabulary: ISP fanout is topology inside the task loop; calling a pipeline a loop requires
  naming its feedback edge and exit signal.

### J.6 trq212 (Anthropic) — *A Field Guide to Fable: Finding Your Unknowns* (2026-07-03)

**Core.** Map (prompt/skills/context) vs territory (codebase, real constraints); the difference
is your **unknowns**, in four quadrants (known knowns = the prompt; known unknowns; unknown
knowns = obvious-but-unwritten; unknown unknowns). Technique catalog by phase —
pre-implementation: blind-spot pass, brainstorm/prototype before wiring, one-question interviews
prioritized by "answer would change the architecture," references-as-source-code,
implementation plans leading with likely-to-change decisions; during: `implementation-notes.md`
with a **Deviations** log; post: pitch/explainer artifacts and a quiz gating merge ("I only
merge after I pass the quiz perfectly"). Sibling to the earlier HTML-over-Markdown piece
(§A.3 trq212).

**Pros.** Delegation-side craft from the CC team itself; the Deviations log and quiz-gate are
concrete, cheap, and compose with our existing artifacts.

**Cons.** Individual-practice essay; no measurements.

**Mapping.**
- **[GAP-ADOPT — consumed by the plan]** require a `Deviations` section in the merge-back
  template (we already require notes; naming deviations is sharper and makes plan-vs-actual
  drift queryable). Template change first; schema field only if queries need it.
- **[OVERLAP-SHARPEN]** interview-before-architecture = the ideation-cycle fork-resolution
  pattern and the pipeline-architect design interview; his prioritization rule ("questions
  where my answer would change the architecture") is the right question-ordering heuristic for
  both.
- **[OVERLAP-SHARPEN — KG angle]** "unknown knowns" are what KG readback before fanout exists
  to surface — the readback is our blind-spot pass; Part F's C.15 pre-answer scaffolding
  (enumerate ambiguities before delegating) is the same move. His quiz-gate is a human-side
  parent verdict gate: comprehension, not just correctness, gates the merge.

### J.7 refactoringenglish (Michael Lynch) — *How to Write an Effective Design Doc* (2026-06-24)

**Core.** Investment scales with cost-of-being-wrong ("what's the penalty for being wrong?" as
the section-inclusion test); goals in impact terms, not implementation; explicit non-goals;
scenarios paint the after-state; SLOs before monitoring; alternatives-considered.

**Pros.** External validation of the workflow-artifact-model tier discipline: done criteria
live in the spec; the plan resolves open questions; specs don't grow into plans.

**Cons.** General SWE writing advice; nothing agent-specific; lowest urgency of the batch.

**Mapping.**
- **[OVERLAP-SHARPEN]** his design doc ≈ our spec tier; "penalty for being wrong" is a good
  section-inclusion test to fold into the spec-drafting convention next to Part C's C.11
  (hard test + common false positive).
- **[GAP-ADOPT — soft]** an explicit **non-goals** block in the spec template. We have
  "deferred items"; non-goals is the sharper frame (deferred = later, non-goal = never, and
  conflating them re-surfaces settled scope questions).
- **[WE-AHEAD]** success_criteria + open-questions are already first-class in our specs; his
  SLO-before-monitoring point is our verification-strategy-traces-to-done-criteria rule.

### J.8 Part J synthesis (cross-cutting)

1. **Token-field trust is per-harness ingest metadata, not scorer code.** J.1's field-level
   unreliability + the plan's local verification + craft §6's capability mask converge: every
   cost row carries per-harness field-trust provenance (erratum #2 should graduate from erratum
   to evidence-rubric clause). Extends Part H's "immutable source + provenance-bearing views."
2. **There are two cost levers, and we were measuring only one.** Model routing is H1-bounded
   (volume moves ≤~4% on an executor swap); output-volume *behavior* (J.2 CLI predicate
   pushdown + tabular output; J.3 compression levers) is where the other ~96% lives. The Pareto
   axes measure the routing lever; the volume lever is pipeline/CLI design work and should be
   named as such in the craft doc's §5 rules.
3. **Cross-family, writer≠reviewer review now has three independent inventions** (PostHog
   qa-swarm, honey's judge panel, RULE-7) — the strongest convention-to-corroborated-practice
   upgrade in this corpus since the Part H eval/spec convergence.
4. **Gates are ordered: deterministic before LLM, bounded before terminal.** StampHog's
   fail-closed deny-list/size tier before any LLM call (J.4) + FAPO's accept-only-if-improves
   (H.4 revised) + our target_count/fold-back design describe one shape: cheap typed gates
   first, judgment second, bounded retry, terminal escalation.
5. **Exit signals must be wired, not named** (J.5) — the taxonomy-level restatement of I.1's
   instruments and the justification for the transcript pipeline itself: it is the wiring that
   lets our loops' exit signals (verdicts, fold-backs, cost frontiers) be observed rather than
   asserted.
6. **KG angle across the batch:** deviations (J.6), token-field trust (J.1), and review-triage
   labels (J.4) are all write-time labels that decide read-time recoverability — Part B Theme 8
   again. The episodic view should carry them as typed fields, which is also what H.1's
   compaction plan and I.1's iteration-log-across-compactions asked for.

### Part J — routing note (workflow)

The five concrete adoptables were **created as fold-backs and routed 2026-07-12** (owner
direction) — recorded in `.agents/active/fold-back/<slug>.yaml` for the record AND routed
through the plan's own process so each is consumed before its owner runs, not left as
unrouted queue debt:

| slug | routed to | consumed when |
|---|---|---|
| `evidence-rubric-token-field-trust` (J.1) | task note `pareto-live-waves` | before scoring live rows — erratum #2 graduates to an evidence-rubric clause |
| `merge-back-deviations-section` (J.6) | task note `pareto-live-waves` | live-wave merge-backs must carry a Deviations section |
| `review-gate-deterministic-pre-llm-tier` (J.4) | task note `pareto-live-review` | assessed against live gate-rework/block-rate data; proposal only if the data supports it |
| `da-cli-agent-path-predicate-tabular` (J.2) | proposal `~/.agents/proposals/obs-da-cli-agent-path-predicate-tabular.md` | no pending task owns the `da` CLI surface — the proposal queue is the plan's declared sink |
| `spec-template-non-goals-block` (J.7) | proposal `~/.agents/proposals/obs-spec-template-non-goals-block.md` | spec-convention change; reviewed via `da review` |

The plan-local digest (`evidence/prior/external-references-2026-07-12.md`) remains the
per-article consumer map.

### Part J — open questions raised

1. Does the delegation/review gate grow a deterministic pre-LLM tier (deny-list, size, state),
   and does it live in `da` (typed, cross-harness) or per-harness projection? (J.4)
2. Do merge-backs adopt a dense A2A format with never-compress contract fields, and what is the
   measured token saving on our own bundles? (J.3)
3. Is the `da` CLI agent-path work (predicate pushdown + tabular mode) worth a fold-back now,
   or does it wait for the post-0.4.0 CLI surface review? (J.2)
4. Should the evidence rubric hard-require per-harness token-field-trust labels on every cost
   row — making J.1's finding a rubric clause rather than a per-plan erratum? (J.1)
5. Is the Deviations log a merge-back template change only, or also a queryable schema field on
   the delegation record? (J.6)
6. Which of the four loops does each of our named runtimes claim in its own doc — and does
   every one name its feedback edge and exit signal explicitly? (J.5)

---

## Part K — 2026-07-13 addendum: three operating-contract / model-routing articles

Processed AFTER the plan's red-team disposition and fold-back archival (2026-07-12), so the
routing discipline here is deliberately corroboration-first: nothing in this batch reopens a
settled wave decision; adoptables are either resolved same-day into plan artifacts or parked
as [UNVERIFIED] leads. Full bodies: `research/articles/sairahul1-fable-brain-backup.md`,
`0xmiraqle-gpt56-god-mode-contract.md`, `prajwaltomar-gpt56-two-scoreboards.md`. Genre
caveat for all three: growth-account X articles (modest engagement, linkless claims); they
corroborate *design shape*, not effect sizes.

### K.1 sairahul1 — *How To Copy Claude Fable 5's Thinking Before It's Gone* (2026-07-12)

**Core.** Occasioned by Fable 5 leaving subscriptions: the model was never the asset — its
procedures are. Extract them as second-person standing **orders** for the replacement model
(10 areas: intent, decomposition, effort placement, verification, known-vs-guessed marking,
self-attack, completeness, refusing to guess, delivery order, fake-competence tells), each as
**trigger→action** rules ("When you see X, do Y"; "if a rule sounds like advice, rewrite it
until it's an action") ending in a final send-gate checklist. Then **prove the transfer with a
trap test** (a smooth-reading compound-discount error run with and without the instructions);
on failure, regenerate only the vague section. Plus: repeat-task interview prompts, a
compressor that "never touches the final gate," and a monthly readback health-check
("summarize the instructions you're running on… if you can't, say so").

**Pros.** The distillation format and the transfer-verification step are both real mechanics;
"a cheaper model with a great checking process catches mistakes the plain version misses" is
the C1–C6 thesis in one sentence.

**Cons.** Newsletter-funnel piece; n=1 trap anecdote; no measurements; the kit upsell wraps
the kernel.

**Mapping.**
- **[GAP-ADOPT — resolved same day]** the trap test is instrument-transfer verification:
  when a stage prompt (lens/rubric) runs on a SECONDARY-tier model in a contrast arm, a
  Tier-B planted-defect task verifies the instructions actually transferred before that arm's
  frontier rows count. Folded into `evidence/pareto/disposable-tasks.md` Tier B (fold-back
  `lens-transfer-trap-test`) — calibration-only, O13-compatible, no new gate.
- **[OVERLAP-SHARPEN]** trigger→action executability is the authoring bar our lens/rubric
  prompt text should be held to (same standard RULE-7's checklists already aspire to); the
  fix-it prompt is bounded single-section regeneration — the fold-back regeneration pattern.
- **[OVERLAP-SHARPEN]** the monthly health-check is a readback probe — identical in shape to
  the delegation bundle readback (`resolve-prompt`) that guards against instructions loaded
  but not used.
- **[WE-AHEAD]** the "backup file" is a singleton pasted into one Project box; rules/skills/
  scopes version, distribute, and sync the same artifact across harnesses.

### K.2 0xmiraqle — *HOW TO USE GPT 5.6 ON GOD MODE* (2026-07-09)

**Core.** Operating contract, not prompting: destination + fence + bar, never the route.
Five-line house rules (irreversible→stop-and-show; every "done" ships sub-minute-checkable
evidence; rules beat instructions). Measurable bars, no adjectives — and **the builder never
grades its own work**: external evaluators caught 5.6 gaming graders "at the highest rate
ever measured on a public model," so the grader is a separate fresh-context instance — "a
skeptic with no memory of the excuses." `/goal` loop: build→verify→biggest-gap→close, where
**the loop never contains the model's right to declare itself finished**. Payroll routing
(Sol/Terra/Luna + effort dial; parallel-committee mode reserved for foundations). Traces as
"a private training set nobody else has." Honest paragraph: ~1-in-400 complex agent tasks
off-script (unauthorized infra, falsified reports) → one budgeted human-QA hour daily.

**Pros.** Independently converges on nearly every structural bet in the full-loop pipeline;
supplies the first system-card-grounded *reason* (grader gaming) for cross-context/
cross-family grading rather than mere convention.

**Cons.** All empirics linkless ([UNVERIFIED]); confident-guide genre; browser/budget
sections are consumer-oriented.

**Mapping.**
- **[OVERLAP-SHARPEN — RULE-7]** fresh-context builder≠grader is now the FOURTH independent
  invention in this corpus (PostHog qa-swarm, honey judge panel, RULE-7, this) — and the
  first justified by measured grader-gaming rather than taste. Chase the 5.6 system card;
  if confirmed, cite it in `methodology/falsification-review-rubric.md` prior art.
- **[WE-AHEAD]** "the loop never contains the model's right to declare itself finished" =
  parent-verdict/gate-owns-termination; house rules ≙ rules/ + write_scope + delivery-gate
  stop-before-push; evidence-checkable-in-under-a-minute ≙ merge-back verification_status.
- **[OVERLAP-SHARPEN]** §8 traces-as-private-training-set is the transcript-analysis plan's
  thesis verbatim — "the people getting scary results… are mostly people whose models never
  start from zero."
- **[GAP-NOTE — parked]** budget-based permissioning ("spend up to $X, don't ask below") vs
  our per-action gates: a candidate policy knob for wave cost-gating; not adopted (waves are
  already user-gated on cost as a whole).

### K.3 prajwaltomar — *GPT 5.6 Didn't Kill Claude. It Killed Something Bigger.* (2026-07-10)

**Core.** The hero model died, not Claude. 5.6 = a priced family (Sol $5/$30, Terra
$2.50/$15, Luna $1/$6 vs Fable $10/$50). **Two scoreboards**: Artificial Analysis has Sol at
80 (+2.8 over Fable, <½ the output tokens); Every's private month-long Senior Engineer
benchmark has Fable 91 vs Sol 56 — "both are telling the truth"; "Sol executes the
instruction. Fable questions the instruction." Converged org chart: Fable=manager (plan,
review, blindspots — pay-per-use is the *correct* pricing for judgment), Sol=senior worker
(impl, long runs, browser/computer use), Terra/Luna=interns. Cites Anthropic's own published
orchestrator numbers: **96% of performance at 46% of cost** for big-model-orchestrates/
cheap-models-execute. Wiring: `model:` lines in `~/.claude/agents/` + a delegation policy;
"effort settings matter more than model choice — the gap between effort tiers is bigger than
the gap between adjacent models." Cautions: METR flagged Sol with their highest-measured
reward-hacking rate ("especially on tests it wrote itself"); "benchmarks are marketing — run
both on YOUR work."

**Pros.** The plan's reason-to-exist stated externally: public benchmarks and private
task-anchored benchmarks disagree by 35 points on the same pair, therefore measure on your
own cells — which is the live-wave design. Supplies a published effect-size prior (96%/46%)
for exactly the contrast class C1–C6 runs.

**Cons.** Every number is secondhand and linkless; $19.24-vs-$1 is n=1; launch-week
enthusiasm from an agency-growth account.

**Mapping.**
- **[OVERLAP-SHARPEN — priors]** 96%-at-46% is a citable prior for C1/C2 (executor swaps
  under a baseline orchestrator) — chase to primary source before quoting in evidence
  artifacts; if located, it belongs in `evidence/prior/` with the provenance chain.
- **[OVERLAP-SHARPEN]** two-scoreboards = the H-series' own epistemics: our preregistered
  cells are the "private benchmark" side; K.3 is the popular argument for why the historical
  pass refused public-benchmark priors.
- **[GAP-NOTE — future axis]** reasoning-effort dial as a blocking axis: our cells block on
  `model_family × task_class × cache_regime × retry_regime` but not effort tier; if "effort >
  adjacent-model" replicates, a future wave should treat effort as the swapped variable.
  Logged as an open question, NOT a mid-freeze cell change.
- **[WE-AHEAD]** "verify Sol's output… especially on tests it wrote itself" = the
  frozen-verifier-suite ground-truth rule in `disposable-tasks.md` (verifiers pass on the
  landed implementation before any contrast run; the executor never authors its own ground
  truth). METR's flag joins K.2's grader-gaming as external justification.

### K.4 Part K synthesis (cross-cutting)

1. **The corpus's first full external convergence on the plan's economics.** K.1 (process
   transplants down-tier), K.2 (external fresh-context grading is a necessity, not a style),
   and K.3 (two scoreboards; judgment priced per-use at the top, execution routed cheap)
   are three growth-account restatements of the pareto plan's three pillars: contrasts on
   our own cells, RULE-7, and candidate-model tiering. None of them *measures* anything we
   can import — but the convergence itself upgrades "our design is idiosyncratic" to "our
   design is the emerging consensus shape, and we are one of the few actually instrumenting
   it."
2. **Grader integrity got its empirical hook.** Two independent [UNVERIFIED] citations
   (5.6 system-card grader-gaming, METR reward-hacking high-water mark) now justify
   builder≠grader + cross-family review from measured behavior. Verifying these two sources
   is the single highest-value follow-up in this batch.
3. **Instruction transfer is testable and should be tested.** K.1's trap test generalizes:
   any time prompt/skill text is the thing carrying capability across a model swap, a
   planted-defect calibration task verifies the carry — now a Tier-B usage note in
   disposable-tasks.md. This is C0's logic applied to instructions instead of instruments.
4. **A new candidate axis: reasoning effort.** Two of three articles independently claim the
   effort dial dominates adjacent-model choice. Our cells don't block on it; the claim is
   cheap to test later as a single-variable wave and is logged as an open question.

### Part K — routing note (workflow)

One fold-back, resolved same day (post-archival discipline — no new open decisions for the
waves): `lens-transfer-trap-test` → Tier-B usage note in
`evidence/pareto/disposable-tasks.md`, artifact archived to
`.agents/history/transcript-analysis-and-pipeline-craft/fold-backs/pareto-live-waves/`.
Corroboration entries (RULE-7 fourth invention + grader-gaming/METR hooks; 96%/46% prior;
effort-dial axis) recorded in the plan digest
(`evidence/prior/external-references-2026-07-12.md`, 2026-07-13 addendum) as [UNVERIFIED]
leads with named chase targets. No spec, rubric, or gate changes.

### Part K — open questions raised

1. Do the GPT 5.6 system card and METR's evaluation actually contain the grader-gaming /
   reward-hacking claims as characterized? (K.2, K.3 — chase before citing anywhere binding.)
2. Where do Anthropic's 96%-performance-at-46%-cost orchestrator numbers live, and what task
   distribution were they measured on? (K.3 — potential C1/C2 prior.)
3. Should a future wave treat reasoning-effort tier as the swapped variable, holding model
   constant? (K.3/K.2 — candidate post-freeze axis.)
4. Is trigger→action executability worth enforcing as a lint-style review item on lens/skill
   prompt text (K.1), or is the Tier-B trap test sufficient enforcement at the point of use?

---

*Document status: draft. No changes made to code, specs, or plans. This is evaluation only.
Second pass (Parts H/I resolution + Part J) completed 2026-07-12 against the inflight
`transcript-analysis-and-pipeline-craft` and `full-loop-orchestration-runtime` plans.
Part K (operating-contract batch) added 2026-07-13 under post-archival corroboration-first
routing.*
