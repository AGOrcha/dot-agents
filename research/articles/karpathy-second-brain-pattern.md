# How to Build Your Second Brain: The LLM Wiki Pattern

**Author:** Nick Spisak (@NickSpisak_)
**Source:** https://x.com/NickSpisak_/status/2040448463540830705
**Published:** April 4, 2026
**GitHub:** https://github.com/NicholasSpisak/second-brain
**Based on:** Andrej Karpathy's LLM Wiki pattern (https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f)

---

@karpathy dropped a post describing how he uses AI to build personal knowledge bases. The idea is simple: instead of keeping notes scattered across apps, you dump everything into one folder, let the LLM compile it into a structured wiki, and browse it in Obsidian.

Nick Spisak built a working implementation as an Agent Skill, installable in one command across Claude Code, Codex, Cursor, Gemini CLI, and 40+ other agents.

## Karpathy's LLM Wiki Pattern

Most RAG systems retrieve raw documents at query time, rediscovering knowledge repeatedly. The LLM Wiki pattern differs: the LLM **incrementally builds and maintains a persistent wiki** — a structured, interlinked collection of markdown files positioned between you and raw sources.

When adding a new source, the LLM reads it, extracts key information, and integrates it into the existing wiki — updating entity pages, revising topic summaries, noting contradictions, and strengthening synthesis. The wiki is a persistent, compounding artifact. The cross-references are already there. The contradictions have already been flagged.

### Three Layers

1. **Raw sources** — Immutable curated documents (articles, papers, images, data). The LLM reads but never modifies them.
2. **The wiki** — LLM-generated markdown files. Summaries, entity pages, concept pages, comparisons, synthesis. The LLM owns this entirely.
3. **The schema** — Configuration document (e.g., CLAUDE.md) telling the LLM how the wiki is structured, conventions, and workflows.

### Three Operations

| Operation | What Happens |
|-----------|-------------|
| **Ingest** | Drop a new source, LLM reads it, discusses takeaways, writes summaries, updates index, revises 10-15 files in one pass |
| **Query** | Ask questions against the wiki. LLM reads index, pulls relevant pages, synthesizes answers with wikilink citations |
| **Lint** | Health-check for contradictions, stale claims, orphan pages, missing cross-references, data gaps |

## Nick's Implementation: second-brain

```bash
npx skills add NicholasSpisak/second-brain
```

This installs four skills into your AI agent:

| Skill | What It Does |
|-------|-------------|
| `/second-brain` | Set up a new vault (guided wizard) |
| `/second-brain-ingest` | Process raw sources into wiki pages |
| `/second-brain-query` | Ask questions against your wiki |
| `/second-brain-lint` | Health-check the wiki |

### Vault Structure

```
your-vault/
├── raw/                    # Your inbox — drop sources here
│   └── assets/             # Images and attachments
├── wiki/                   # LLM-maintained wiki
│   ├── sources/            # One summary per ingested source
│   ├── entities/           # People, orgs, products, tools
│   ├── concepts/           # Ideas, frameworks, theories
│   ├── synthesis/          # Comparisons, analyses, themes
│   ├── index.md            # Master catalog of all pages
│   └── log.md              # Chronological operation record
├── output/                 # Reports and generated artifacts
└── CLAUDE.md               # Agent config (varies by agent)
```

### Indexing

**index.md** — Content-oriented catalog of everything. Each page with a link, one-line summary, optional metadata (date, source count). Organized by category. Updated on every ingest. The LLM reads it first when answering queries.

**log.md** — Append-only chronological record. Entries start with consistent prefixes (e.g., `## [2026-04-02] ingest | Article Title`) making it parseable with unix tools.

## Why This Works

"The tedious part of maintaining a knowledge base is not the reading or the thinking — it's the bookkeeping."

Humans abandon wikis because maintenance burden grows faster than value. LLMs don't bore, don't forget cross-references, and touch 15 files simultaneously. The wiki stays maintained because maintenance cost approaches zero.

This connects directly to the [Claude + Obsidian memory stack](claude-obsidian-memory-stack.md) pattern — giving agents externalized memory so they can compound knowledge across sessions instead of starting from zero.

## Connection to dot-agents

The LLM Wiki pattern reinforces several themes from the [agent-as-operator research](../AGENT_AS_OPERATOR_RESEARCH.md):

1. **Agents should manage their own infrastructure.** The LLM is the librarian. You're the curator. You don't maintain the wiki — the agent does.

2. **Persistent artifacts compound value.** Each ingest makes the wiki more valuable. This is the same principle behind persisting workflow state (checkpoints, verification results, lessons) rather than reconstructing it every session.

3. **Schema is critical infrastructure.** The CLAUDE.md/AGENTS.md that tells the agent how to maintain the wiki is the same kind of canonical configuration that dot-agents manages. Without disciplined schema, the wiki degrades.

4. **Cross-agent compatibility.** Nick's implementation works across Claude Code, Codex, Cursor, and 40+ other agents via the Agent Skills standard — the same multi-platform problem dot-agents solves for config and rules.

The pattern also echoes [Ars Contexta's approach](arscontexta-agentic-knowledge-systems.md) but with a different entry point: Ars Contexta starts from conversation and derives architecture; the LLM Wiki pattern starts from raw sources and builds incrementally.

## Optional Tools

- **Obsidian Web Clipper** — Convert web articles to markdown for quick ingestion
- **summarize** — Summarize links, files, and media from the CLI
- **qmd** — Local search engine for markdown (hybrid BM25/vector search, all on-device)
- **agent-browser** — Browser automation for web research

## Quick Start

1. Install the skills: `npx skills add NicholasSpisak/second-brain`
2. Run the wizard: `/second-brain` in your AI agent
3. Install Obsidian Web Clipper, configure to save to `raw/`
4. Open vault in Obsidian
5. Clip your first article to `raw/`, run `/second-brain-ingest`
6. Browse wiki in Obsidian — follow `[[wikilinks]]`, explore graph view
7. `/second-brain-query` to ask questions, `/second-brain-lint` to health-check
