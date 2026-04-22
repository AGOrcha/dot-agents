## claude code + obsidian + skill graph. how to build a local research engine?

**Source**: https://x.com/the_smart_ape/status/2043262727922053128
**Author**: The Smart Ape (@the_smart_ape)
**Date**: 2026-04-12
**Method**: Playwright
**Word count**: ~5000 words

---

### Summary

A practical guide to building a "research skill graph" — a folder of 20 interconnected markdown files where each file is one knowledge node connected via `[[wikilinks]]`. When Claude is pointed at this folder, it follows the links, applies 6 research lenses (technical, economic, historical, geopolitical, contrarian, first-principles), and synthesizes multi-angle analysis. The author deployed this for 4 clients (1 media company, 2 consulting firms, 1 independent analyst), cutting research costs by ~60%. The piece walks through every file in the structure: index.md as the command center, research-frameworks.md for picking an approach, source-evaluation.md with a 5-tier trust system, synthesis-rules.md for combining cross-lens findings, contradiction-protocol.md for resolving disagreements, the 6 lens files, and the compound-effect knowledge base that makes the system improve over time. The most powerful mode uses Claude Code pointed at a local Obsidian vault for fully autonomous context compounding.

---

### Body

i built a research system for 4 clients in the past 6 months. one media company (well-known, can't name them), two consulting firms, and one independent analyst.

combined, they cut their research costs by about 60%. one of them fired 3 junior researchers and replaced them with this system + one senior editor who reviews the output.

no fancy tools. no $500/mo subscriptions. no 47 open chrome tabs. just a folder of .md files, one ai agent (claude), and a system that takes one research question and produces a multi-angle analysis that would normally take a team 2 weeks.

this article is the full breakdown. every file, every link, every template. by the end you'll have a working research engine you can build in 1 hour.

**what is a research skill graph?**

most people use ai for research like this: open chatgpt, type "research X for me", get a surface-level summary that sounds like a wikipedia intro, then spend 3 hours verifying it and filling the gaps yourself. that's not research. that's just a google search with extra steps.

the problem isn't the ai. it's that you're giving it zero structure. no methodology, no evaluation criteria, no framework for thinking about the topic from different angles. you're hiring a genius with amnesia every single time.

a research skill graph fixes this. it's a folder of interconnected markdown files where each file is one "knowledge node" — one piece of your research system's brain. inside each file you use `[[wikilinks]]` to reference other nodes.

when you point claude at this folder and give it a research question, it doesn't just google stuff. it follows the links, reads your methodology files, applies your source evaluation criteria, then analyzes the topic through 6 completely different lenses before synthesizing everything.

the difference: a single prompt gives you a summary. a skill graph gives you a research department.

**why 6 lenses instead of one big prompt?**

this is the core idea. instead of asking "research topic X", you force the ai to rethink the same question 6 times from fundamentally different angles:

- **technical**: what do the numbers actually say? look at data only
- **economic**: follow the money. who pays, who profits, what incentives drive behavior
- **historical**: what patterns repeat? what's been tried before? what context is everyone forgetting
- **geopolitical**: zoom out to the global chessboard. which countries, which power dynamics
- **contrarian**: what if the consensus is wrong? who benefits from the current narrative
- **first principles**: forget everything. rebuild from fundamental truths only

each lens produces findings that often contradict the others. and that tension between lenses is where the real insight lives.

when i ran "why are birth rates collapsing globally" through this system, the technical lens said "crisis: the math is brutal", while the contrarian lens said "japan has had low fertility for 50 years and hasn't collapsed." neither is wrong. the truth lives in the tension.

**the folder structure**

```
/research-skill-graph
├── index.md
├── research-log.md
├── methodology/
│   ├── research-frameworks.md
│   ├── source-evaluation.md
│   ├── synthesis-rules.md
│   └── contradiction-protocol.md
├── lenses/
│   ├── technical.md
│   ├── economic.md
│   ├── historical.md
│   ├── geopolitical.md
│   ├── contrarian.md
│   └── first-principles.md
├── projects/
│   └── (one subfolder per research topic)
├── sources/
│   └── source-template.md
└── knowledge/
    ├── concepts.md
    └── data-points.md
```

20 files. 6 folders. that's your entire research department.

**file 1: index.md (the command center)**

This is the single most important file. Every research starts here. It's not a table of contents — it's a briefing document that tells the ai who you are, what system you're using, and exactly how to execute.

```markdown
# Research Skill Graph — Command Center

## 1. Mission
Deep research system that takes ONE question and produces a multi-angle analysis
no single Google search or ChatGPT prompt could ever match.

Research Question: [PASTE YOUR QUESTION HERE]
Scope: [DEFINE BOUNDARIES — what's in, what's out]
Time Horizon: [how far back and forward are we looking?]
Output Goal: [what decision does this research inform?]

## Prior Research (optional — for compound mode)
Check [[research-log]] for previous research that may connect to this question.

## 2. Node Map
### Methodology
- [[research-frameworks]] — how to approach different types of questions
- [[source-evaluation]] — criteria for judging if a source is worth trusting
- [[synthesis-rules]] — how to combine findings across lenses
- [[contradiction-protocol]] — what to do when sources disagree

### Lenses (the core engine)
- [[technical]] — data, numbers, mechanisms
- [[economic]] — follow the money
- [[historical]] — patterns, precedent
- [[geopolitical]] — power dynamics
- [[contrarian]] — what if the consensus is wrong?
- [[first-principles]] — rebuild from fundamentals

### Outputs
- [[executive-summary]] — 500 words max
- [[deep-dive]] — full analysis organized by lens
- [[key-players]] — people, organizations, countries
- [[open-questions]] — what we still don't know

### Knowledge Base
- [[concepts]] — key terms, definitions, mental models
- [[data-points]] — hard numbers, statistics with source attribution

## 3. Execution Instructions
1. Read this file completely
2. Read [[research-frameworks]] to pick the right approach
3. Read [[source-evaluation]] for evidence standards
4. For EACH lens: read its file, research through that lens only, record findings
5. Read [[contradiction-protocol]] — resolve or document disagreements
6. Read [[synthesis-rules]] — combine everything
7. Produce all 4 output files

CRITICAL RULE: each lens must RETHINK the question, not just add more information.
The technical lens and the contrarian lens should feel like two different researchers
who disagree with each other. That tension is where insight lives.
```

notice: the node map gives context with every link. not just `[[technical]] — data` but `[[technical]], how does it work mechanically? what do the numbers actually say?`. that extra context helps the agent make decisions without opening every file for every task.

**file 2: research-frameworks.md (pick your approach)**

```markdown
# Research Frameworks

Framework Selection:

### Type 1: "Is X true?" (Verification)
- Start with [[technical]] lens to establish what the data actually says
- Then [[contrarian]] to stress-test the claim
- Then [[historical]] for precedent
- Best for: fact-checking, debunking, validating assumptions
- Example: "Is nuclear fusion viable by 2035?"

### Type 2: "Why is X happening?" (Causal Analysis)
- Start with [[historical]] to trace the roots
- Then [[economic]] to find incentive structures
- Then [[technical]] for mechanism
- Then [[geopolitical]] for systemic forces
- Then [[contrarian]] to challenge your causal chain
- Example: "Why are birth rates collapsing globally?"

### Type 3: "What happens if X?" (Scenario Planning)
- Start with [[first-principles]] to establish base assumptions
- Then [[technical]] for constraints
- Then [[economic]] for incentives
- Then [[geopolitical]] for power dynamics

### Type 4: "What should I do about X?" (Decision Support)
- Start with [[executive-summary]] of existing knowledge
- Then run all 6 lenses in parallel
- Rank options by lens agreement (5/6 lenses same way = high confidence)

## Research Depth Levels
### Level 1: Quick Scan (30 min) — 3 lenses max, top 5 sources
### Level 2: Standard Research (2-3 hours) — all 6 lenses, 15-25 sources
### Level 3: Deep Dive (1-2 days) — all 6 lenses, 50+ sources, primary data

## Source Collection Strategy (per lens)
1. Start with the BEST single source
2. Find the source that DISAGREES most with #1
3. Find primary data that lets you judge between them
4. Record everything in [[data-points]] with attribution
```

**file 3: source-evaluation.md (trust tiers)**

This is what stops your research from being garbage-in-garbage-out. A 5-tier system for evaluating every source before using it.

```markdown
# Source Evaluation

## Source Tier System

### Tier 1: Primary Data (highest trust)
- Raw datasets (UN, World Bank, national statistics offices)
- Peer-reviewed studies with methodology visible
- Financial filings, government records
- Direct measurements and observations
- USE FOR: [[data-points]], hard claims, base assumptions

### Tier 2: Expert Analysis
- Reports from domain-specific research institutions
- Books by recognized authorities in the field
- Long-form investigative journalism with cited sources
- Conference papers and working papers
- USE FOR: interpretation, causal claims, framework building

### Tier 3: Informed Commentary
- Expert blog posts and newsletters
- Quality podcasts with domain experts
- Think tank reports (check funding sources)
- Industry publications
- USE FOR: angles you hadn't considered, hypothesis generation

### Tier 4: General Media
- Major news outlets
- Wikipedia (good for overview, never for final claims)
- Popular science writing
- USE FOR: initial orientation only. always verify upstream

### Tier 5: Social/Anecdotal (lowest trust)
- Twitter threads, Reddit posts
- Personal anecdotes
- Viral content
- USE FOR: signal detection only. "people are talking about X" ≠ "X is true"

## Red Flags (downgrade any source by 1 tier if present)
- No cited sources or methodology
- Author has financial incentive in the conclusion
- Published by organization with known agenda on the topic
- Cherry-picked time frames or geographies
- Conflates correlation with causation
- Uses emotional language instead of evidence

## Evaluation Checklist
For every key claim, ask:
1. What tier is this source?
2. Can I find the same claim in a Tier 1 or Tier 2 source?
3. Who funded this research or publication?
4. What would the author lose if they were wrong?
5. Is this the BEST available evidence, or just the first I found?
```

**file 4: synthesis-rules.md (combine without flattening)**

This is the hardest part. Most people skip synthesis and just stack facts. This file forces actual thinking.

```markdown
# Synthesis Rules

## The Synthesis Process

### Step 1: Lens Summary
After completing all 6 lenses, write a ONE paragraph summary per lens:
- What is this lens's main finding?
- Confidence level: high / medium / low
- What surprised you from this angle?

### Step 2: Agreement Map
- If 4+ lenses point the same direction → high confidence finding
- If 3 lenses agree → moderate confidence, worth stating with caveats
- If only 1-2 lenses support a claim → hypothesis only, flag as uncertain

### Step 3: Tension Map
Identify where lenses DISAGREE:
- [[technical]] says X but [[economic]] says Y → this tension IS the insight
- Don't resolve by picking a winner. document both positions
- Ask: "under what conditions is each lens correct?"

### Step 4: Second-Order Insights
The best findings come from COMBINING lenses:
- "The technical data shows declining fertility, but the economic lens reveals
   that financial incentives haven't reversed it anywhere — which means the
   [[first-principles]] lens's argument about cultural shifts might be the
   dominant factor"
- These cross-lens insights are what make this system more powerful than
   reading 50 articles

### Step 5: Confidence Calibration
For each major finding, state:
- CLAIM: what you believe is true
- EVIDENCE: strongest supporting data (from [[data-points]])
- CONFIDENCE: high / medium / low
- WHAT WOULD CHANGE MY MIND: the specific evidence that would reverse this

## Output Rules
- Never present a single-lens finding as a conclusion
- Always show the tension between lenses
- Separate "what the data shows" from "what I interpret"
- [[open-questions]] is as important as [[executive-summary]]
- Prefer "it seems likely that X because Y and Z, though A complicates this"
   over "X is true"

## Anti-Patterns
- Confirmation bias: only searching for evidence that supports your initial hunch
- Narrative fallacy: making a clean story out of messy, contradictory data
- Recency bias: overweighting the latest article over 30 years of data
- Authority bias: believing something because an impressive person said it
- Anchoring: letting the first number you found define your mental range
```

**file 5: contradiction-protocol.md (where the real insights hide)**

Most research buries contradictions. This system surfaces them on purpose. Contradictions are features.

```markdown
# Contradiction Protocol

## When Two Sources Disagree

### Step 1: Check the basics
- Are they talking about the same thing? (different geographies, timeframes, definitions)
- Are they using the same data? (same dataset, different interpretation?)
- Is one source a higher tier than the other?

### Step 2: Find the root of disagreement
Usually one of:
- Different data: they're looking at different datasets → find out which is more complete
- Different interpretation: same data, different conclusions → examine reasoning chain
- Different scope: one is global, one is country-specific → both might be right in context
- Different timeframe: short-term vs long-term trends can look opposite
- Different incentives: one has reason to spin the data → check funding, affiliations

### Step 3: Document, don't resolve
In [[deep-dive]], write it as:
"Source A argues [X] based on [data]. Source B argues [Y] based on [data].
The disagreement likely stems from [root cause]. Under conditions [C1], A is
probably right. Under conditions [C2], B is probably right."

### Step 4: Upgrade to [[open-questions]] if unresolvable
- This IS a finding. "We don't know whether X or Y" is valuable intelligence
- Add it to [[open-questions]] with the specific evidence needed to resolve it
- This often becomes the most interesting part of the research

## When Two Lenses Disagree
This is expected and healthy. Different lenses SHOULD produce tension.

## Confidence Adjustment
- 2 lenses agree, 1 disagrees → investigate the disagreeing lens deeper
- All lenses agree → be suspicious. you might have confirmation bias
- No lenses agree → your research question might be too broad. narrow scope
```

**files 6-11: the 6 lenses**

Every lens file follows the same structure: core questions, how to research through that angle, output format, voice, and connections to other lenses. Here is the technical lens as the detailed example — the other 5 follow the exact same pattern, just swap the angle.

```markdown
# Lens: Technical

Strip away opinions and narratives. What do the numbers actually say?
What mechanisms are at work?

## Core Questions
1. What does the DATA show? (not what people say about the data)
2. What are the measurable inputs and outputs?
3. What mechanisms drive the phenomenon?
4. What are the hard constraints (physical, biological, mathematical)?
5. What metrics matter most and how are they measured?
6. Where is the data incomplete or poorly measured?

## How to Research Through This Lens
- Start with Tier 1 sources ONLY: raw datasets, peer-reviewed studies
- Look for: time series, geographic comparisons, demographic breakdowns
- Quantify everything. replace "declining" with "declined by X% between Y and Z"
- Identify measurement problems: how is this data collected? what's excluded?
- Find the base rates. what's "normal"? what's the historical range?

## Output Format
For each finding:
- METRIC: [what you measured]
- VALUE: [the number]
- SOURCE: [Tier 1 or 2 source with date]
- TREND: [direction and rate of change]
- CAVEAT: [measurement limitations]

## Voice
Clinical. precise. no emotional language.
"fertility rate dropped from 2.1 to 1.6" not "fertility is collapsing."
let the numbers speak.

## Connects To
- [[source-evaluation]] — only high-tier sources for this lens
- [[data-points]] — all numbers go here
- [[economic]] — technical data often explains economic outcomes
- [[first-principles]] — technical constraints define what's possible
```

For the other 5 lenses, same structure, different core questions. The author's verbatim one-line hints:

- **economic**: who pays, who profits, what incentives, what policies tried, what ROI?
- **historical**: when has this happened before, what conditions, what was tried, what worked?
- **geopolitical**: which countries most affected, what power shifts, what alliances change?
- **contrarian**: what's the mainstream narrative, what's the strongest argument against it, who benefits from the consensus?
- **first principles**: what are the absolute base-level facts, what's the simplest model that explains 80% of what we observe?

---

### Synthesized Lens Templates (not in original — derived by applying the technical-lens template to the author's one-line hints for lenses 2–6)

> **Note:** The five templates below are NOT verbatim extraction. The original article gave only the technical lens in full and one-line descriptions of the others. These templates apply the same structure (Core Questions / How to Research / Output Format / Voice / Connects To) to each hint so the skill graph can be built without additional lookup. Treat them as a reasonable starting scaffold, not canonical author content.

```markdown
# Lens: Economic

Follow the money. Who pays, who profits, what incentives drive behavior,
what markets move, what policies have been tried?

## Core Questions
1. Who bears the cost of this phenomenon and who captures the value?
2. What incentive structures cause the observed behavior?
3. What policies or market mechanisms have been tried, and what ROI did they produce?
4. What are the flows of capital, labor, goods, and rents involved?
5. Where does the market fail (externalities, information asymmetry, public goods)?
6. What would change if the prices or subsidies shifted?

## How to Research Through This Lens
- Tier 1 sources preferred: central bank reports, national accounts, industry
  financials, academic economics papers
- Follow the money: whose P&L changes if this phenomenon continues or reverses?
- Identify revealed vs. stated preferences (what people do vs. what they say)
- Check whether stated rationales match the incentive structure
- Look for natural experiments (policy changes that created before/after data)

## Output Format
For each finding:
- AGENT: [who is acting]
- INCENTIVE: [what they gain / avoid]
- EVIDENCE: [data showing the behavior]
- POLICY TRIED: [interventions attempted, with outcomes]
- ROI / ELASTICITY: [how responsive is behavior to price/policy?]

## Voice
Follow-the-money skeptic. No moralizing. "The subsidy increased uptake by X%
and cost Y per additional unit" beats "the policy helped."

## Connects To
- [[technical]] — technical data often explains economic outcomes
- [[historical]] — economic patterns repeat under similar incentive structures
- [[geopolitical]] — national economic interests drive state behavior
- [[contrarian]] — "who benefits from the current narrative?" is an economic question
```

```markdown
# Lens: Historical

What patterns repeat? What's been tried before? What context is everyone forgetting?

## Core Questions
1. When has this phenomenon (or a close analog) occurred before?
2. What were the initial conditions each time?
3. What was tried — policy, technology, cultural — and what worked / failed?
4. What is the longest relevant time series, and what does it show?
5. What does everyone forget about the previous cycles?
6. What's the base rate — how often does the claimed outcome actually happen?

## How to Research Through This Lens
- Extend time series as far back as the data allows
- Find at least 3 prior episodes that rhyme with the current one
- For each episode: what conditions preceded it, what was tried, what was the outcome?
- Look for survivorship bias in the received wisdom
- Read the contemporary writing from each era, not just modern retrospectives

## Output Format
For each finding:
- EPISODE: [when and where it happened]
- INITIAL CONDITIONS: [what set it up]
- INTERVENTIONS: [what was tried]
- OUTCOME: [what actually happened]
- DIFFERENCE FROM NOW: [why this analogy might fail]

## Voice
Long-horizon, comparative. "Between 1873 and 1896, the same pattern produced..."
Resist the present-tense urgency of current narratives.

## Connects To
- [[first-principles]] — long time series reveal which "laws" actually hold
- [[economic]] — most historical patterns have economic drivers
- [[contrarian]] — the strongest contrarian arguments often come from historical precedent
- [[open-questions]] — where history and the present diverge is the most useful finding
```

```markdown
# Lens: Geopolitical

Zoom out to the global chessboard. Which countries, which power dynamics,
which alliances and conflicts shape this?

## Core Questions
1. Which countries are most affected — positively and negatively?
2. What power shifts does this phenomenon cause or reflect?
3. Which alliances strengthen or fracture under this pressure?
4. What are the state-level incentives (security, legitimacy, resources)?
5. What are the plausible state-level countermoves?
6. How does this look from the perspective of each major actor (not just your home country)?

## How to Research Through This Lens
- Read sources from at least 3 different national perspectives, in translation if needed
- Check military, trade, and treaty data from Tier 1 state/institutional sources
- Identify revealed state preferences via votes, treaties, spending
- Map the dependency structure (energy, semiconductors, food, capital)
- Ask: who has asymmetric power in this situation and how are they using it?

## Output Format
For each finding:
- ACTOR: [country or bloc]
- INTEREST: [what they are optimizing for]
- LEVERAGE: [what instruments they have]
- CONSTRAINT: [what limits their action]
- LIKELY MOVE: [plausible countermove or escalation path]

## Voice
Dispassionate, multi-perspective. No home-country framing.
"From Beijing's vantage point..." "From Brasília's vantage point..."

## Connects To
- [[economic]] — trade, capital, and resource flows drive state behavior
- [[historical]] — power transitions follow recognizable patterns
- [[technical]] — technology transfers redraw geopolitical maps
- [[contrarian]] — the home-country narrative is usually the weakest frame
```

```markdown
# Lens: Contrarian

What if the consensus is wrong? Who benefits from the current narrative?
What's nobody saying?

## Core Questions
1. What is the mainstream narrative, stated as precisely as possible?
2. What is the strongest steelmanned argument AGAINST that narrative?
3. Who benefits financially, politically, or reputationally from the consensus holding?
4. What evidence would the consensus have a hard time explaining?
5. Where is the consensus making a prediction that will soon be testable?
6. What is the most embarrassing possibility nobody is publicly raising?

## How to Research Through This Lens
- Actively seek out dissenters: heterodox economists, minority-view papers,
  disfavored scientists, emerging-market analysts
- Apply the "who funded the consensus?" check to everything
- Look for disconfirming data the mainstream has explained away
- Take the opposite side seriously enough to argue it in writing
- Rate the counter-argument's STRENGTH honestly — "weak" is a valid rating

## Output Format
For each finding:
- CONSENSUS: [precisely what the mainstream claims]
- STEELMANNED COUNTER: [best argument against, stated in good faith]
- COUNTER-EVIDENCE: [specific data the consensus has trouble with]
- INCENTIVES FOR CONSENSUS: [who gains from it being true / believed]
- STRENGTH RATING: [weak / moderate / strong / devastating]

## Voice
Steelman, don't strawman. No "well actually" smugness.
Every contrarian finding should be something the author would stake reputation on.

## Connects To
- [[source-evaluation]] — "who funded this?" applies double here
- [[historical]] — consensus has been spectacularly wrong before; pattern-match
- [[first-principles]] — the strongest counters rebuild from base facts
- [[open-questions]] — if the counter is moderate or stronger, flag it as testable
```

```markdown
# Lens: First Principles

Forget everything you think you know. Rebuild from fundamental truths only.
What's the simplest model that explains 80% of what we observe?

## Core Questions
1. What are the absolute base-level facts — physics, biology, math, logic — that constrain this?
2. Strip every claim. Which ones are definitionally true vs. empirically observed vs. conjectured?
3. What is the simplest model that explains 80% of the observations?
4. What would have to be true for the phenomenon to NOT exist?
5. Where does the intuitive / received explanation rely on an unstated assumption?
6. What prediction does the simplest model make that differs from the received model?

## How to Research Through This Lens
- Start with zero context. Define every term from scratch before using it.
- Separate: (a) physical/biological/mathematical constraints, (b) empirical regularities,
  (c) cultural conventions, (d) path-dependent accidents
- Rebuild the phenomenon from the ground up without referencing any authority
- Test the simplest model against 3+ specific observations
- Ask: if I were explaining this to a sharp 12-year-old, which steps do I skip?

## Output Format
For each finding:
- BASE FACT: [unambiguously true statement]
- SOURCE OF CERTAINTY: [definition / physics / observation / convention]
- ASSUMPTION DEPENDED ON: [if any; flag it]
- SIMPLEST MODEL: [most compressed explanation of the 80%]
- WHERE IT BREAKS: [remaining 20% the model can't explain]

## Voice
Naive, rigorous, childlike. "Wait — why is that true?" repeated until you hit bedrock.
Resist jargon; if you can't say it in plain words, you don't understand it yet.

## Connects To
- [[technical]] — first-principles constraints bound what the technical data can show
- [[contrarian]] — both lenses strip consensus; they often agree
- [[historical]] — base-level constraints stay fixed across eras; use history to check the model
- [[data-points]] — every base fact should cite a Tier 1 source
```

**file 12: source-template.md**

Copy this for each major source you process during research.

```markdown
# Source Template

## Source: [Title]
- Author: [who]
- Date: [when published]
- URL: [link]
- Tier: [1-5, from [[source-evaluation]]]
- Lens: [which lens found this source]

## Key Claims
1. [claim with page/section reference]
2. [claim]
3. [claim]

## Data Extracted
[any numbers → also add to [[data-points]]]

## Methodology Notes
[how did they arrive at their conclusions? any red flags?]

## Connections
[what other sources or concepts does this relate to? use [[wikilinks]]]

## My Assessment
[is this trustworthy? what's strong, what's weak?]
```

**the compound effect (why this gets better over time)**

This is what makes the system fundamentally different from chatgpt conversations or google research.

`knowledge/concepts.md` and `knowledge/data-points.md` accumulate across ALL your research projects. After 5 projects, your ai starts with a base of 200+ verified data points and 50+ defined concepts.

`research-log.md` tracks every project with key findings and connections. Your 10th project doesn't start from zero — it starts from everything you've already learned.

Even better: the open-questions from one research become the index.md of the next. When i researched birth rates, one open question was "will ai automation offset labor shortages fast enough?" — that's an entire research project that already has context from the demographic data.

And if you want a clean slate? Just upload only `methodology/` and `lenses/` to a fresh claude project. No `knowledge/`, no research-log. Same system, fresh brain.

**how to use it**

**Method 1: Claude Projects (easiest)** — create a new project, upload all files, give it a topic with "follow the execution instructions in index.md."

**Method 2: paste context** — copy index.md + relevant lens files into any ai chat. Less powerful but works anywhere.

**Method 3: Claude Code + Obsidian (most powerful)** — point Claude Code at your local vault. The agent reads and writes files directly. The graph evolves itself, updating knowledge files, adding new concepts, refining lenses based on output quality. Fully autonomous.

**visualize with obsidian**

Download obsidian (free) from obsidian.md. Open your research-skill-graph folder as a vault. You immediately see a graph view showing how all your nodes connect. index.md sits at the center. The 6 lenses radiate out. Methodology files connect to lenses. Knowledge files connect to everything.

Two things this gives you: you can see which nodes are disconnected (research gaps) and spot unexpected connections between projects (compound insights).

Obsidian is optional — the ai reads markdown regardless. But the visual graph makes the system tangible and helps you debug it.

**why this beats traditional research**

Traditional research: open 50 tabs, read 20 articles that all say the same thing, miss the contrarian view, miss the historical pattern, get lost in confirmation bias, produce a summary that sounds smart but has no structure.

Skill graph research: one question goes through 6 forced angles, each angle has evaluation criteria, contradictions are documented not hidden, sources are tiered, findings compound across projects.

The biggest difference is the contrarian lens. Traditional research almost never challenges its own findings. This system has a built-in devil's advocate that asks "what if everything i just found is wrong?", and rates the strength of the counter-argument honestly.

**build it in 1 hour**

1. Create the folder structure. 20 files, 6 folders. Takes 5 minutes.
2. Fill index.md first: this defines everything else
3. Fill the 6 lens files with the core questions and output formats
4. Fill methodology/ with source evaluation tiers and synthesis rules
5. Upload everything to a claude project
6. Give it a topic and test
7. Iterate. Update the lens files based on output quality. Add concepts to knowledge/ after each project

The system gets better every time you use it. That's the whole point.

---

### Key Quotes

> "the difference: a single prompt gives you a summary. a skill graph gives you a research department."

> "each lens produces findings that often contradict the others. and that tension between lenses is where the real insight lives."

> "you're hiring a genius with amnesia every single time."

> "CRITICAL RULE: each lens must RETHINK the question, not just add more information."

> "the biggest difference is the contrarian lens. traditional research almost never challenges its own findings. this system has a built-in devil's advocate that asks 'what if everything i just found is wrong?'"

> "the open-questions from one research become the index.md of the next."

> "the system gets better every time you use it. that's the whole point."
