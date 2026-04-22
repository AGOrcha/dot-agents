# KG & Adjacent Articles — Evaluation Against dot-agents

**Written:** 2026-04-21
**Scope:** 14 articles in `research/articles/` (KG + memory + harness + multi-agent). Compared against current specs in `.agents/workflow/specs/`, plans in `.agents/workflow/plans/`, proposals in `.agents/proposals/`, lessons in `.agents/lessons/`, and the scoped-KG spec just drafted.
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
- **[GAP-ADOPT]** — an **ingestion pipeline for external content**. Today, transcripts of Slack/meeting/video never reach the KG. A `dot-agents ingest <url|file>` that extracts claims, frameworks, actions and drops them as KG notes (with `derivation: untracked` per scoped-KG §5.8) would close the biggest blind spot in our "what does the agent know?" surface.
- **[WE-AHEAD]** — our warm store with sqlite + typed queries is strictly better than Obsidian-as-database for machine readers. Humans can still open the `.md` files.

---

#### second-brain-needs-two-authors (kevin) — *Two-Author Pattern*

**Core.** Every wiki file has `author: kevin` or `author: agent` frontmatter. `author: kevin` files are *untouchable* by any agent — read-only, link-only, build-around-but-never-overwrite. Agent files are mutable. Graduation mechanism: human reviews an agent file, promotes it to `author: kevin` by editing one field.

**Pros.**
- **Elegantly solves the "agent overwrites my thinking" problem with one frontmatter field.** This is worth adopting verbatim.
- Maps onto our existing `rules/` vs agent-proposed rules distinction: human-authored rules survive `refresh`, agent-proposed ones go through `dot-agents review`.
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

**Cons.** Mostly a repackaging of the patterns in arscontexta, Nyk, and kevin. The cross-platform Skills install is interesting but we already solve platform distribution differently (via `dot-agents refresh`).

**Mapping.**
- **[GAP-ADOPT]** — `dot-agents kg lint` as a command surface that runs: broken-wikilink detection, orphan-note detection, stale-citation detection, author-field presence check, contradiction scan. Today we have `kg fresh/warm/build/bridge` but no lint. Lint is the reweave/hygiene primitive.
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
- **Thoth's dream cycle** (nightly: duplicate merging at 0.93+ similarity → description enrichment → relationship inference → confidence decay on relations older than 90 days) is the most concrete instance of the "graph improves itself" pattern across all 14 articles.
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
- **[GAP-ADOPT]** — **Context Cores as our distribution primitive.** We already bundle skills/rules/hooks via dot-agents refresh; formalizing it as a versioned, rollback-able "context core" bundle aligns naming and gives rollback guarantees we don't currently promise.
- **[OVERLAP-SHARPEN]** — our scoped-KG spec uses "drivers" for staleness; Zep's `valid_at`/`invalid_at` is a simpler surface for the same idea. Consider adding `valid_at` as an explicit note field alongside `IndexedAt` — it becomes the signal that a driver has fired.

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

## Part B — Synthesis against our stack

### Our current stack in one paragraph

dot-agents is a Camp 2 (context substrate) system with: a unified `.agents/` tree separating identity/knowledge/ops partly (specs + rules + skills + prompts are identity-ish, KG + lessons + history are knowledge, active + workflow are ops); a single-process orchestrator with fanout-to-delegation-bundles for write-scoped implementation; a warm sqlite KG + hot markdown notes + code-review-graph (Tree-sitter) connected via MCP; a bridge query surface (`workflow graph query`) that's just hardening into readiness; an ISP pipeline (impl → verify → review → parent); a proposal/review loop for human approval of changes to shared resources; central hook distribution across Claude Code / Cursor / Codex / Copilot. We have scoped-KG spec (draft), graph-bridge-contract (done), kg-command-surface-readiness (in progress), workflow-parallel-orchestration (active). Recent history: planner-evidence-backed-write-scope merged, ralph-fanout-and-runtime-overrides merged.

### Themes across the 14 articles

1. **Camp 2 is the winning direction and the industry is converging on it.** witcheer's two-camp frame; Zep's rebrand "memory → context engineering"; MemSearch's "files are source of truth, vectors are derived"; TrustGraph's Context Cores; Thoth's dream cycle. We're already in Camp 2; we should name it explicitly and let it steer future proposals.

2. **Derivation / provenance is the load-bearing primitive for long-lived systems.** arscontexta's `cognitive_grounding`, kevin's `author:` field, the_smart_ape's source tiers, multi-agent-memory-dkg's cryptographic fingerprints, scoped-KG's `derived_from` cites — five articles independently converge on "every claim must cite its evidence." Our `KGNote` currently has no confidence, no author, no cite field.

3. **Compounding / graph-improves-itself is a nightly process, not an inline one.** Thoth's dream cycle, arscontexta's reweave, claude-obsidian's self-improving graph. All three run consolidation out-of-band. Our scoped-KG spec commits to "propagation is write-time"; we should also have a "consolidation is nightly" lane.

4. **Context engineering > iteration.** sullyai's core finding, echoed in thealexker's R.P.I., milksandmatcha's decomposition patterns, codex-multi-agent's front-loading. The single most important principle for our loop-worker / verifier / review pipeline to internalize: the first review of any correction loop asks "is this compensating for overloaded context?"

5. **Human-agent authorship boundary is a durable, simple primitive.** kevin's one field. Our proposal/review loop is this pattern at the rule level; we should generalize it per-artifact.

### What we do well (and should keep)

- **Unified artifact tree with lifecycle tiers** (`workflow/specs/` → `workflow/plans/` → `active/` → `history/`). arscontexta's three-space is a cleaner articulation but we have the bones.
- **Central hook/rule/skill distribution across platforms** — strictly ahead of per-project setup.
- **Workflow-aware orchestration** — dep graphs + conflict detection + fanout > Super-Swarm-style "orchestrator figures it out."
- **sqlite + typed queries for KG** — strictly better than Obsidian-as-DB for machine readers. Humans can still open files.
- **Proposal/review as a first-class mechanism** — the graduation idea applied to global resources.

### What we miss (priority-ordered gaps)

**P0 — smallest change, biggest leverage:**
1. **`author` / `authority` field on KGNote, lessons, plan files** (kevin). One frontmatter field. PreToolUse hook to enforce. Immediate effect.
2. **Decomposition-over-iteration rule** (sullyai). Written into `self-review` or `iteration-close`: any pipeline with a correction loop must first answer "is this compensating for overloaded context?"
3. **Prose-as-title convention for lessons and KG notes** (Nyk). A rename pass.

**P1 — meaningful new primitives:**
4. **Dream cycle / nightly consolidation job** (Thoth via witcheer). Dedup, relationship inference, description enrichment, review-nudge firing. Maps onto scoped-KG §2.7 review-nudges directly.
5. **Contradiction protocol as a skill** (the_smart_ape). Explicit 4-step procedure for agents when they see two notes disagree. Fleshes out scoped-KG §3.2 `contradictions`.
6. **Ingest command for external content** (Nyk). `dot-agents kg ingest <url|file>` that extracts claims and creates untracked-derivation notes.
7. **`kg lint` command** (karpathy). Broken wikilinks, orphan notes, missing authors, stale cites, contradictions. Reweave automation.

**P2 — structural:**
8. **Planning lenses** (the_smart_ape). Extend `prompts/verifiers/` with contrarian + first-principles lenses for plan review.
9. **Dynamic output contracts on delegation bundles** (sullyai). Per-task typed output schemas; deterministic fan-in.
10. **Parallel verification (Gordon Ramsay)** (milksandmatcha). Verifier + review run in parallel with next impl, gated before fan-in.
11. **Materialized neighborhood summaries on node rows** (techwith_ram). One int column per node. Near-instant `get_impact_radius`.

**P3 — named futures, not now:**
12. **Shared environment handoff** (jhleath). For a team-scale dot-agents, revisit worktree + shared disk over git sync.
13. **Formalize Context Cores as distribution primitive** (TrustGraph via witcheer). Our refresh mechanism becomes a versioned, rollback-able bundle.

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
- **C.3** Add a one-line rule to `self-review` or `iteration-close`: "if this work introduced or relied on a correction loop, first answer: is this compensating for an overloaded context?" (sullyai, §A.3.)

### Short-term (one plan each)

- **C.4** Draft a spec for a nightly consolidation pipeline (dream cycle). Pair with the scoped-KG review-nudge axis (§2.7 of that spec). The pipeline fires review-nudges, runs content-hash dedup, proposes `NoteSymbolLink` additions from co-occurrence.
- **C.5** Draft a `kg ingest` + `kg lint` spec. Ingest accepts URL/file, produces KG notes with `derivation: untracked`. Lint walks the graph for hygiene issues.
- **C.6** Audit our ISP pipeline (orchestrator → impl → verify → review → parent) through the "context engineering vs iteration" lens. Does a well-decomposed fanout eliminate the verifier stage for most task shapes? Even a negative finding is load-bearing.

### Medium-term (subsume into scoped-KG plan)

- **C.7** Fold the content-hash source mutation driver, Zep-style `valid_at`, and per-scope Bloom filters into the scoped-KG plan (not the spec — these are how-to, not decisions).
- **C.8** Generalize the proposal/review loop from "global rules" to any agent-authored artifact (plans, lessons, notes). Single graduation pipeline.

### Explicitly deferred (naming so they don't re-surface as proposals)

- Cryptographic fingerprints / blockchain KG (DKG). Overkill for our scale.
- SPARQL / Leapfrog / distributed partitioning. Premature at 34K nodes.
- Embedding-similarity propagation (also deferred in scoped-KG §4.6).
- Shared-disk environment handoff (jhleath). Revisit at team scale.

---

## Part D — Trust gate (read before acting on any P0/P1/P2/P3 above)

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

*Document status: draft. No changes made to code, specs, or plans. This is evaluation only.*
