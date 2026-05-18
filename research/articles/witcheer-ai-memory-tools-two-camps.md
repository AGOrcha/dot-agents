## I Went Through Every AI Memory Tool I Could Find. There Are Two Camps.

**Source**: https://x.com/witcheer/status/2044456778843238689
**Author**: witcheer (@witcheer), Head of Growth @YariFinance, Founder @Broad_Land
**Date**: 2026-04-15
**Method**: Playwright
**Word count**: ~2500 words

---

### Summary

witcheer surveyed 450+ "agent-memory" and 460+ "context-management" GitHub repos while running a 24/7 agent setup on a Mac Mini M4, and found two fundamentally different paradigms. Camp 1 (memory backends) extracts facts into vector databases for recall — Mem0, MemPalace, Supermemory, Honcho. Camp 2 (context substrates) maintains structured, human-readable files that accumulate and compound across sessions — OpenClaw, Zep, Thoth, TrustGraph, MemSearch. The author argues Camp 2 is where serious continuous-agent infrastructure is heading, and predicts "context engineering" will replace "memory" as the dominant term within 6 months.

---

### Body

there are 450+ repos tagged "agent-memory" on github and 460+ tagged "context-management." me and my agentic best friends went through them.

what I expected to find: 40 tools doing roughly the same thing with different APIs.

what I actually found: two fundamentally different paradigms, almost no one drawing the line between them, and a category that doesn't have a name yet.

I run a 24/7 agent setup on a Mac Mini M4. every session compounds on the last. that setup is the reason I noticed the split — most memory tools couldn't power what I'm doing, and the ones that could weren't being talked about as memory tools at all.

**The Two Camps**

**Camp 1: memory backends** — these tools extract facts from your conversations, store them in vector databases, and retrieve relevant ones when you ask. automated note-takers. they file things away and pull them back when needed.

**Camp 2: context substrates** — these maintain structured, human-readable context that accumulates across sessions. nothing gets "extracted." the context is the files. your agent reads them, works within them, writes back to them, and the whole thing compounds over time.

camp 1 asks: "what should the AI remember?"
camp 2 asks: "what context should the AI work inside?"

most of the space (and most of the github stars) sit in camp 1. but camp 2 is where the architectures that actually scale to continuous, multi-session, multi-project work are emerging.

**Camp 1: The Memory Backends**

**Mem0 — 53.1k stars**: the category leader by adoption. four operations: add, search, update, delete. extracts facts from conversations, stores them at three levels (user, session, agent), retrieves them via hybrid search. dead simple to integrate. the limitation: memories are flat entries. no relationships between them. a fact from january sits next to a fact from april with no notion that one might supersede the other.

**MemPalace — 46.2k stars**: local-first verbatim memory. instead of extracting facts, MemPalace stores conversations word-for-word and organises them into wings (entities), rooms (topics), and drawers (original content). searches them with ChromaDB. the benchmark numbers are the highest in the space: 96.6% retrieval recall on LongMemEval using raw semantic search alone, no API calls, no LLM. 98.4% with hybrid pipeline. 99%+ with LLM reranking. the limitation: verbatim storage scales linearly. no compression, no synthesis. if your problem is "find the thing I said three weeks ago," this is the best tool. if your problem is "give me the current state of my work across five projects," it's the wrong tool.

**Supermemory — 21.8k stars**: positions itself explicitly as "memory is not RAG." the differentiator is temporal awareness: say "I just moved to San Francisco" and it supersedes your old city. expired facts get forgotten automatically. connectors for google drive, gmail, notion, onedrive, github. the closest camp 1 gets to thinking about state, not just storage.

**Honcho — 2.4k stars**: treats both humans and agents as "peers" in a unified model. an async reasoning service runs in the background, deriving psychological insights about each peer from their sessions. PostgreSQL + pgvector required. the closest thing in camp 1 to caring about entity evolution rather than just fact storage.

**The rest of camp 1**: Cognee (15.4k) combines vector search with graph databases for relational reasoning. Memori (13.3k) intercepts LLM API calls to capture execution context, hits 81.95% on LoCoMo using only 4.97% of full-context tokens. AgentScope, MemOS, EverOS, MIRIX, SimpleMem, Memobase — all variations on the same loop.

**What Camp 1 Tools Have In Common**

Every tool above runs the same fundamental loop:

conversation happens → system extracts facts or stores content → facts go into a database (vector, graph, or both) → next conversation, relevant facts get retrieved and injected

The intelligence is in the extraction and retrieval. the human interacts with the agent. the memory system works behind the scenes. you never touch the memory directly and you trust the system to remember the right things and surface them at the right time.

this works. the benchmarks prove it. but it's solving one specific problem: fact recall. "what did I say about X?" "what does the user prefer?"

there's a different problem none of these tools address.

**Camp 2: The Context Substrates**

**OpenClaw — 358k stars**: plain markdown files: MEMORY.md for long-term storage, daily notes (YYYY-MM-DD.md) for running context, DREAMS.md for consolidation summaries. the line from their docs that defines the philosophy: "the model only 'remembers' what gets saved to disk, there is no hidden state." no vector database. no extraction pipeline. files the agent reads and writes to.

The most interesting feature is **dreaming**: a background process that consolidates daily notes into long-term memory in three phases:

- light sleep — screens daily notes, groups nearby lines into coherent chunks
- REM — weighted recall promotion, frequently-accessed information becomes "lasting truths"
- deep sleep — replay-safe promotion into MEMORY.md, reconciles rather than duplicates

Only entries passing all threshold gates get promoted: minimum score 0.8, minimum recall count 3, minimum unique queries 3. Six weighted signals score every candidate: relevance (0.30), frequency (0.24), query diversity (0.15), recency (0.15), consolidation (0.10), conceptual richness (0.06).

**Zep — 4.4k stars**: Zep recently rebranded their entire positioning from "memory" to "context engineering." That one move is the strongest market signal in this entire landscape. A funded company with 4.4k stars looked at where the space was going and decided "memory" was the wrong word for what they were building. Under the hood, Zep uses a temporal knowledge graph (their Graphiti framework). Facts include `valid_at` and `invalid_at` timestamps. Sub-200ms retrieval. SOC2 Type 2 and HIPAA compliant. Zep sits between the two camps architecturally — it still extracts and retrieves — but the rebrand is the tell.

**Thoth — 145 stars**: tiny project, deepest architecture I found in the entire landscape. Thoth builds a personal knowledge graph with 10 entity types connected by 67 typed directional relations. FAISS vector search with one-hop graph expansion before every LLM call. the standout is the dream cycle, a nightly four-phase process: duplicate merging at 0.93+ similarity threshold → description enrichment → relationship inference → confidence decay on relations older than 90 days. Three anti-contamination layers prevent cross-entity fact bleed. it's sitting at 145 stars because it requires you to take the camp 2 thesis seriously enough to set up a knowledge graph for your own context. worth watching.

**TrustGraph — 2.0k stars**: introduces "Context Cores" — portable, versioned bundles that contain domain schemas, knowledge graphs, vector embeddings, evidence sources, and retrieval policies. treats context like code: version it, test it, promote it, roll it back. every camp 1 tool treats memory as a side effect of conversations. TrustGraph treats context as a first-class artifact with identity, versioning, and a lifecycle. you can hand a Context Core to a new agent and it inherits the full operational context. the conceptual model is the right one.

**MemSearch by Zilliz — 1.2k stars**: markdown-first memory from the team behind Milvus. memories are .md files, human-readable, editable, version-controllable. Milvus runs as a "shadow index" derived from the files, fully rebuildable. the files are the source of truth. the vector search is just an access layer on top. what's notable is that this came from Zilliz, a vector database company. they shipped a memory system where their own product is downstream of the files. that's a meaningful concession about where the source of truth actually lives.

**What Camp 2 Tools Have In Common**

the loop is different:

agent reads structured context before working → agent works within that context → agent (or background process) writes back to the structured context → next session, the context is richer than before

the intelligence is in accumulation. the context is the memory. and because it's files (markdown, knowledge graphs, context containers), a human can read it, edit it, correct it, and understand exactly what the agent knows.

- camp 1 optimises for **recall**: can the system find the right fact?
- camp 2 optimises for **compounding**: does the system get better over time?

**Where This Is Heading**

the pattern from running a 24/7 agent setup is clear. memory and context aren't the same problem. my agent doesn't need to "remember" that I prefer dark mode. it needs to operate inside a context that includes my active projects, the people I work with, recent decisions, and what happened yesterday. and that context needs to be richer tomorrow than today.

memory backends solve recall. 96%+ accuracy, sub-200ms latency, drop-in APIs. if you need a chatbot to remember user preferences, Mem0 or MemPalace will do it.

but if you're running an agent continuously — one that works while you sleep, reads from the same knowledge base your other tools write to, and gets meaningfully better over weeks and months — the context substrate approach is what makes that work.

my prediction is that within 6 months, "context engineering" replaces "memory" as the default term for what serious agent infrastructure does.

---

### Key Quotes

> "camp 1 asks: 'what should the AI remember?' camp 2 asks: 'what context should the AI work inside?'"

> "Zep recently rebranded their entire positioning from 'memory' to 'context engineering.' That one move is the strongest market signal in this entire landscape."

> "every camp 1 tool treats memory as a side effect of conversations. TrustGraph treats context as a first-class artifact with identity, versioning, and a lifecycle."

> "camp 1 optimises for recall: can the system find the right fact? camp 2 optimises for compounding: does the system get better over time?"

> "my prediction is that within 6 months, 'context engineering' replaces 'memory' as the default term for what serious agent infrastructure does."
