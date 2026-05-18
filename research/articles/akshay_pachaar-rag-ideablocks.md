## Akshay Pachaar — You're Doing RAG Wrong

**Source**: https://x.com/akshay_pachaar/status/2052743644411765230
**Author**: Akshay (@akshay_pachaar) — Verified
**Date**: 2026-05-08
**Method**: Playwright
**Word count**: ~1,600 words
**Engagement**: 3 replies, 24 reposts, 218 likes, 371 bookmarks, 27K views

---

### Summary

The chunk is the wrong unit of knowledge for RAG. The fix is a "question-answer packet" (IdeaBlock): one question, its validated answer, and typed governance fields (clearance level, version state, source). Benchmarks from Blockify's internal dataset (17 documents, 298 pages): 2.29x retrieval distance reduction (cosine distance 0.1585 vs 0.3624 for naive chunks); 40x corpus compression after semantic deduplication; 13.55% accuracy improvement after deduplification. Governance fields (clearance level, version state) move into the data layer, not the orchestrator. Open-source: github.com/iternal-technologies-partners/blockify-agentic-data-optimization.

---

### Body

Every RAG pipeline starts with the same assumption: a chunk of text is the right unit of knowledge to embed. That assumption is almost never examined. And it's the source of most of the retrieval failures people try to fix downstream.

**Why the chunk is a bad unit.** A chunk of text is structurally neutral — it knows nothing about where its ideas begin or end, which version of a document it came from, who is allowed to see it. You end up retrieving half a table, or a conclusion with no argument, or a claim stripped of the context that makes it true.

The version problem: most enterprise corpora have the same document in a dozen near-identical versions across SharePoint, Confluence, and Git. Top-K retrieval returns five copies of the same paragraph, current and deprecated mixed together. The LLM blends them into an answer that's confidently wrong.

Because the chunk carries no metadata, access control ends up as logic bolted onto the orchestrator, disconnected from the content it's supposed to govern.

**A better unit: the Question-Answer Packet.** Instead of embedding a window of prose, embed a claim: one question, its validated answer, and governance fields as typed schema. One fact per unit, nothing more.

Your queries are already questions. When your index stores answers to questions, the match becomes structural, not just semantic. You're not hoping the right paragraph floats to the top — you're matching a question to its answer directly.

Blockify (open-source) implements this as an IdeaBlock: question + validated answer + typed governance fields (clearance level: PUBLIC/INTERNAL/CONFIDENTIAL/SECRET; version state: Current/Deprecated/Draft/Approved; product line, export control flags, data privacy labels).

**The counterintuitive finding: less data, more accuracy.** In Blockify's internal benchmark, 2,042 raw IdeaBlocks collapsed to 1,200 canonical blocks after iterative deduplication (80-85% similarity threshold, 3-5 rounds). Word count dropped from 88,877 to 44,537. Distilled dataset outperformed undistilled by 13.55% on vector accuracy.

Why: fifteen near-duplicates create fifteen competing vectors in the same region of embedding space, distributing probability mass across all of them and pulling match scores down. Collapse to one canonical block and the signal sharpens.

**The pipeline — 7 stages:**
1. Scoping — define index hierarchy (Organization > Business Unit > Product > Persona)
2. Ingestion — DOCX/PDF/PPT/PNG/Markdown/HTML; LLM converts raw chunks to draft IdeaBlocks
3. Chunking and extraction — context-aware splitting; output is Q/A pair, not prose window
4. Semantic deduplication — cluster by cosine similarity; near-duplicates merge into canonical blocks
5. Auto-tagging — typed metadata applied by pipeline, not document author
6. Human validation — SMEs spend 1-2 hours/quarter on their corpus slice
7. Export — push to vector database (Azure AI Search, Pinecone, Milvus, Vertex) or JSON-L

**What changes at the application layer:**
- Query construction simplifies: structural match, not probabilistic
- Governance moves into the data layer: role-based access is typed on each block, not logic on the orchestrator
- Updates propagate from a single record: update one IdeaBlock, all consumers get the correction

"RAG stacks are beginning to grow a distillation layer between parsing and vectorization, the way web stacks grew a CDN layer between origin and browser."

---

### Key Quotes

> "The chunk is a parsing convenience that became a retrieval assumption."

> "Fifteen near-duplicates of the same paragraph create fifteen competing vectors in the same region of embedding space. Retrieval distributes probability mass across all of them, pulling the match score down for the canonical version."

> "RAG stacks are beginning to grow a distillation layer between parsing and vectorization, the way web stacks grew a CDN layer between origin and browser."

> "The fix is not a better retrieval algorithm. It's a better unit."
