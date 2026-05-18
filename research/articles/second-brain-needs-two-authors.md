Core Idea: Your Second Brain Needs Two Authors

Most people building personal knowledge systems on top of LLMs make the same mistake. 
They either let the agent write everything llm-wiki style, which works until you realize the system no longer sounds like you and the thinking has just averaged into generic AI prose. 
Or they write everything themselves, which works until they realize they can't keep up, and the system decays into a stale knowledge base that's only useful for historical reference purposes.
Neither approach scales so I spent the last few weeks building a working second brain with two authors that fixes this. 
Here's how it works and what I borrowed to get there.
What I started with

@karpathy's viral LLM Knowledge Bases post and subsequent gist has been the inspiration for many implementations, mine included, as it laid out the core pattern. 
Instead of RAG, where the LLM re-derives knowledge from raw sources on every query, the LLM incrementally compiles source material into a persistent, interlinked collection of markdown articles. 
Ingest once, keep current, query against the compiled wiki. Knowledge accumulates instead of being re-discovered every time you ask a question.
The architecture is three layers:
Raw sources (immutable articles, papers, transcripts)
Wiki layer (the LLM-maintained compilation, one folder per topic)
Config (a CLAUDE.md file that tells the agent how to operate)
Karpathy's framing is the LLM is the programmer, the wiki is the codebase, Obsidian is the IDE. The operations loop is three commands. Ingest, query, lint.
Then @nvk built on Karpathy's pattern with tighter topic isolation and a more structured schema for concept vs entity pages. Both are great starting points, but neither was complete for what I wanted.
The part that was missing

Here's what the gist doesn't address - when you're building a knowledge system around your own thinking, the system needs to distinguish between your voice and everything else. 
Not because your thinking is more important than the sources you're compiling, but because your thinking is the thing that can't be recovered if the agent overwrites it, so it needs to be preserved.
An agent can always re-research a topic, but it cannot re-think your thought.
So I added one rule to the pattern. Every file in the wiki has a frontmatter field: author: kevin or author: agent. 
Files tagged author: kevin are untouchable. Agents can read them, link to them, build supporting material around them, but never modify or overwrite them. 
Agent-authored files are updatable by future research runs, subject to mandatory citation and contradiction flagging.
That's it - one field that changes everything about how the system compounds.
How it actually runs

The wiki lives inside an Obsidian vault with five custom slash commands running on top of Claude Code and accessible via my agent:
/research [topic] spins up 5-8 parallel agents across different angles (academic, practitioner, contrarian, historical, empirical). Each agent does its own web research and drops raw source files into the sources folder. A synthesis pass then compiles wiki pages across all of them.
/ingest [source] processes a single manual source (URL, file, pasted text), discusses with me before filing, then creates or updates wiki pages with mandatory citations.
/wiki-query [question] reads the indexes first, drills into articles, produces cited synthesis with three depth modes (quick, standard, deep).
/wiki-lint runs a health check for broken citations, stale confidence markers, index drift, orphan pages.
/wiki-output [type] generates first-draft articles, briefs, or outlines from wiki content using author: kevin pages as the voice source.
Each topic folder has three subfolders. concepts/ for abstract frameworks. entities/ for concrete things like companies, products, people, papers. synthesis/ for cross-cutting analysis. 
The synthesis folder is where most "author: kevin" files live, because that's where the thinking that matters most happens.
The graduation mechanism

When an agent creates a new synthesis page during a research run, it files as author: agent. If I review it and the content represents my thinking accurately, I edit one line in the frontmatter. The file becomes author: kevin. From that point forward, no agent can touch it.
This is the hidden compounding loop. 
Fresh research continuously adds agent-authored material to the wiki. My review process continuously promotes the best of that material into the protected layer. 
The system gets smarter over time in two directions at once. Broader agent-authored coverage. Deeper human-authored conviction from me.
Most personal knowledge systems lose this. They either never let the agent contribute (static) or never let the human take ownership (generic). The promotion mechanism is the bridge.
Why it matters

A personal knowledge system, aka "second brain", is not a notes app. It's a substrate for your thinking to accumulate against and synthesize topics for future recall. 
If the agent owns everything (as most "llm-wikis" do), your second brain becomes a mirror of the LLM's average view on every topic you care about. That's more like a search engine, not a second brain.
If you own everything, you can't keep up with the world moving faster than you can type.
If you split authorship explicitly, one field with two values and strict rules about who can modify what, you get both. 
Agent-speed ingestion with human-owned interpretation. The part that compounds is the synthesis layer where the two meet.
