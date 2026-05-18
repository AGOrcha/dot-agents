## Everyone wants to be AI-pilled. Most Companies Are Still Level 1

**Source**: https://x.com/annimaniac/status/2050225284277026990
**Author**: Ann Miura-Ko 🦖 (@annimaniac) — VC at Floodgate; pre-seed/seed in SmarterDX, Lyft, Okta, Hebbia, Nooks, Terradot
**Date**: 2026-04-30 (X Article, long-form)
**Method**: Playwright
**Word count**: ~2,100 words
**Engagement**: 50 replies · 175 reposts · 959 likes · 2,997 bookmarks · 468.9K views

---

### Summary

Miura-Ko argues that "AI-pilled" is being used as a binary identity tag when in practice companies sit on a six-level autonomy spectrum (L0–L5) modeled on the AV self-driving levels. She offers a four-question lens — *what can AI see, what can AI do, who can extend the system, how has the org changed* — and claims most scaled companies are stuck at L1 (personal productivity) while their leadership talks like they are at L4. Each level has a "hard test" (concrete proof of capability) and a "common false positive" (the failure mode that gets mistaken for the real thing). L5 is explicitly aspirational: organizations whose core operating loops sense, synthesize, decide, act, escalate, and update shared memory — with humans governing strategy, taste, risk, and exceptions rather than running the loops.

---

### Body

Over the last few weeks I've expanded our office visits beyond AI-pilled startups to scaled companies — most recently Ramp, a 1,500-person organization. The earlier visits showed me what AI-native looks like at 8 or 50 people. At a tiny startup, it is easy to say the company is AI-native because the founders are. Everyone sits close to the customer. Everyone builds. Everyone experiments. The operating system is mostly the people. At a scaling company, the bar is much higher. AI can no longer be a personality trait of the founding team. It has to become part of the company's DNA.

Questions around what is truly AI-native reminds me of the debates we used to have about the levels of autonomy in AVs. For years, everyone in AV was chasing Level 5 self-driving. The levels mattered because they forced precision. Cruise control was not autonomy. Lane keeping was not autonomy. Driver assistance was not the same thing as self-driving.

Something similar is happening with AI-pilled organizations.

Right now, "AI-pilled" is being used as though it were binary. You either are or you aren't. In practice, companies differ both in **intensity** (how deeply AI is embedded into daily work across the organization) and in **technical capability** (what AI is actually allowed to see, do, and change).

A company where employees use ChatGPT to summarize meetings is not in the same category as a company where agents can query systems of record, take bounded action, propagate workflows across teams, and improve the way future work gets done. Both may describe themselves as AI-forward. They are not operating at the same level.

So the better question is not: *Is this company AI-pilled?* The better question is: **What level of autonomy has the organization actually achieved?**

To put a finer point on it:

- **What can AI see?** Is the work of your company legible to a machine or does it live in someone's mind, undocumented meetings, and SaaS tools the AI can't read?
- **What can AI do?** Can it act on systems of record (e.g. open PRs, update CRMs, reconcile invoices) or can it only summarize what humans already wrote down?
- **Who can extend the system?** Are non-engineers shipping production internal tools, or is every workflow held together by a few power users whose work walks out the door when they leave?
- **How has the organization changed?** Or are you running 2023's org chart with better autocomplete?

The answers cluster into six levels.

#### L0: AI as theater

- **What can AI see?** Nothing structured. Knowledge lives in people's heads, undocumented meetings, and SaaS tools AI can't read.
- **What can AI do?** Nothing of consequence. Maybe summarize a meeting if a human pastes the transcript.
- **Who can extend the system?** No one. AI is a personal tool, ungoverned, unintegrated.
- **How has the org changed?** It hasn't. Same chart, same hiring plan, same handoffs, same dependence on managers as routers.

**Hard test:** Can AI complete *any* recurring business process end-to-end?

**Common false positive:** A CEO who gives an excellent speech about AI transformation while still running the company through the same executive staff meetings, status updates, reporting lines, and headcount plans. Announcements ≠ adoption.

#### L1 — Personal productivity

- **What can AI see?** Each individual's personal AI sees only what that person feeds it. Saved prompts, scratch files, private knowledge bases. No org-level visibility.
- **What can AI do?** Help individuals draft, summarize, brainstorm, code. No action on systems of record.
- **Who can extend the system?** Each user reinvents independently. Power users are heroes; their workflows leave with them.
- **How has the org changed?** It hasn't. Same chart. Maybe a "Head of AI" hire that has budgetary influence and has purchased some AI products for the company.

**Hard test:** If your best AI user left tomorrow, would their workflow remain in the company?

**Common false positive:** "80% of employees use AI weekly!" — probably true and also meaningless.

#### L2 — Team workflow

- **What can AI see?** Teams have shared context like a `claude.md` per team, shared prompts, function-specific MCP integrations. AI sees within team boundaries.
- **What can AI do?** Functional workflows. AI for sales prospecting, support tier-1 triage, eng code review. Bounded actions within a team's domain.
- **Who can extend the system?** Within a team, non-engineers can tap into shared workflows. Across teams, not really. Each function rebuilds the same thing privately.
- **How has the org changed?** Functional efficiency within roles. A CSM with AI handles 200 accounts vs. 50. Hiring slows but org shape unchanged. Role boundaries intact.

**Hard test:** Does this workflow cross team boundaries, or is every function building its own private AI stack?

**Common false positive:** "We have AI workflows in every department." But the workflows don't connect, so the company is a collection of AI-enhanced silos rather than an AI-native organization.

#### L3 — Organizational infrastructure

- **What can AI see?** The whole organization is queryable. Cross-functional context accessible. Core systems of record exposed via CLI / MCP / well-defined APIs and integrated into a view on which agents can act and not just observe.
- **What can AI do?** Agents act *across* systems. They update CRMs, open PRs, route tickets, run analyses, draft customer communications, reconcile invoices. Cross-functional but still bounded.
- **Who can extend the system?** Non-engineers don't just consume shared skills — they *author* them. Sales rep packages call analysis as a shareable skill. CX engineer packages a ticket investigation pattern. Skills move horizontally across functions.
- **How has the org changed?** The org chart looks materially different from a 2023 equivalent. Specific shape varies — zero-PM teams, PM-as-agent-orchestrator and product curator, or pure role convergence into "builders". The unifying signal: the company has made an explicit structural choice about how AI changes who does what, and the choice is visible. Token-maxing over headcount-maxing running uncomfortable API bills.

**Hard test:** Can an agent answer, across systems: *what shipped last sprint, who asked for it, what broke after launch, what customers said, and what the company should do next* — without convening a cross-functional meeting?

**Common false positive:** A landfill of meeting transcripts and dashboards with no synthesis. Capture is not legibility. An inert archive is not an operating system.

#### L4 — Compounding operating system

- **What can AI see?** Not just *what* happens but the *relationships* between what happens. The system maintains its own context so that agents update agents, skills marketplaces propagate wins and remove duplicate efforts, the system learns what to surface. Capture + synthesis + query are continuous.
- **What can AI do?** Agents have policy-driven decision authority within scoped domains. Security agents detect then validate then fix then open PR with human review at the merge step. Custom internal harnesses purpose-built for the work the company does most. Active removal of software blockers that prevent agents from being useful.
- **Who can extend the system?** Non-engineers ship production internal tools. A finance person builds an automated contract reviewer. An AE shipped a sales tool in under an hour. *None of them are engineers.* They didn't file a ticket. They found their own pain, prototyped a fix, and pulled engineering in only when it was time to go to production.
- **How has the org changed?** Hierarchy collapses toward "channel managers" of agent workflows. New archetypes emerge. Compensation/promotion explicitly tied to AI proficiency. Customer signal-to-ship measured in hours.

**Hard test:** Show me a workflow that got better because the *system* learned from prior runs, not because one heroic person manually improved it. Plus: show me three production tools shipped by non-engineers in the last quarter.

**Common false positive:** Agent sprawl. A hundred brittle automations don't equal a compounding operating system. L4 requires *managed* compounding (lifecycle, observability, evaluation), not chaotic proliferation. Without compaction discipline, the factory clogs.

#### L5 — Virtually self-driving organization

A clean operational definition (with the caveat that I realize L5 does not exist yet so I'm describing what I think it might look like): **an L5 organization is one where the core operating loops can sense reality, diagnose issues, initiate work, execute within delegated authority, update shared memory, and improve future behavior — with humans governing strategy, taste, risk, values, and exceptions rather than running the loops themselves.**

The six L5 markers:

- The system **notices** something important without being asked.
- The system **synthesizes** across multiple sources of context.
- The system **decides** whether action is warranted.
- The system **acts** within delegated authority.
- The system **escalates** when uncertainty or consequence exceeds its authority.
- The system **updates shared memory** so future behavior improves.

Through the four-question lens:

- **What can AI see?** Generative — the system asks its own questions, identifies gaps in its own knowledge, proposes investigations and runs them.
- **What can AI do?** Delegated authority for novel decisions, not just configured policies. The L4→L5 leap: at L4, the system improves because humans direct it to. At L5, because it notices it should.
- **Who can extend the system?** Non-engineers contribute directly to the customer-facing product itself, *or* the product is reshaped so anyone can extend it without writing code. The boundary between "internal tool" and "product feature" dissolves.
- **How has the org changed?** Truly fluid. Agents are organizational members with meaningful delegated authority. The org self-modifies or proposes role changes, team boundary shifts. Onboarding becomes system-driven. Institutional knowledge survives transitions perfectly because it lives in the system, not in any individual.

**Hard test:** What important thing did the company *notice, decide, act on, and learn from* recently without a human initiating the process? Not a threshold alert; not a configured automation; not an agent summarizing what people surfaced. Something the system synthesized that humans hadn't framed as a question yet.

**Common false positive:** The "fake autonomy" pattern. The company claims self-driving behavior, but the system is only executing preconfigured rules or surfacing threshold-based alerts. Humans are still doing all the noticing. Distinguishing real generative behavior from glorified observability is the open challenge at this level.

---

Interestingly, a company rarely answers all four questions at the same level but the asymmetry tells you where the next intervention should focus. Sometimes AI sees a lot but can't do much. AI might do a lot but only engineers can really extend it. The org chart could have changed but the substrate is thin.

Steve Blank once said that a startup is not a small version of a large company. Similarly an AI-pilled company is not simply an AI-assisted version of an old company. They are organizations rebuilt around a new operating model. We are still learning what this looks like but those who are curious and engaged will have a compounding advantage as they make their way quickly up the stack and see real world impact in their business operations and hopefully margins.

---

### Key quotes

> "AI-pilled is being used as though it were binary. You either are or you aren't. In practice, companies differ both in intensity and in technical capability."

> "L4 requires *managed* compounding (lifecycle, observability, evaluation), not chaotic proliferation. Without compaction discipline, the factory clogs."

> "At L4, the system improves because humans direct it to. At L5, because it notices it should."

> "The system notices, synthesizes, decides, acts, escalates, updates shared memory."

> "Distinguishing real generative behavior from glorified observability is the open challenge."

> "A company rarely answers all four questions at the same level but the asymmetry tells you where the next intervention should focus."
