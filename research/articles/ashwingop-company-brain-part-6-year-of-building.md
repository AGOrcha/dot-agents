## ashwingop — Company Brain, Part 6: What Building a Company Brain over the Last Year Taught Me

**Source**: https://x.com/ashwingop/status/2052051032566530216
**Author**: Ashwin Gopinath (@ashwingop) — CEO of Sentra.app; former MIT Prof; 2x founder
**Date**: 2026-05-06
**Method**: Playwright
**Word count**: ~1,400 words
**Engagement**: 7 replies, 53 reposts, 499 likes, 1,400 bookmarks, 55.8K views

---

### Summary

Part 6 of the series is a retrospective on a year of building Company Brain / Sentra. It traces the evolution from naive agent-polling ideas to the insight that company memory must emerge from work itself, introduces the concept of organizational "chain of thought," and describes real customer examples where compressed signal latency changed how organizations operated.

---

### Body

We did not start by calling it Company Brain. Our first name for what we were building was Enterprise General Intelligence. The thesis was that some version of AGI would become real over the next few years, and that intelligence itself would start to get commoditized. If every enterprise eventually has access to similar intelligence, what makes one company better than another? My answer was: the way the company works.

The project did not start as category theory. It started with founder pain. I wanted to understand what was actually happening inside the company without relying on status updates, secondhand summaries, and my own ability to chase context across meetings, messages, documents, and people. Every founder knows some version of this problem. You have a sense of what is going on, but the detailed reality is always scattered.

Our first instinct was naive: what if an agent could check in with everyone, call around, ask what was happening, and give me a live picture of the organization? That idea broke quickly because people do not want to report to an agent every day. More than that, company truth is not created by polling people. The truth is already being produced in the work itself: in the meeting where a customer risk becomes real, the Slack thread where a workaround gets accepted, the email where a commitment is made, the call where a product decision changes, and the ticket where all of that gets reduced into a few fields. The first lesson was simple, though it took a while to internalize: company memory has to emerge from the work itself.

Meetings, messages, emails, and calls are the undistilled version of company reality. Documents, tickets, dashboards, and workflows matter, but they are usually downstream. The decisions, objections, tradeoffs, commitments, complaints, and handoffs happen before the artifact is cleaned up. I have started to think of these interactions as the chain of thought of the organization. Not in the literal model sense, but in the human sense. They are where the company reasons before it writes down what it decided.

That changed the problem from transcription to memory. We were not trying to record more words; we were trying to understand what should be remembered. A transcript tells you what was said, but memory has to understand why it mattered, who it mattered to, what changed because of it, and what should happen next.

That is how ontology became central for us. We did not get there by wanting a cleaner data model. We got there because the same conversation kept meaning different things to different parts of the company.

A customer call is a good example. A salesperson may hear renewal risk, a PM may hear a roadmap signal, a support lead may hear an escalation, and a lawyer may hear an obligation. The artifact is the same, but the useful memory depends on the perspective. LLMs can help understand what was said; ontology determines what it means inside the company.

Organizational memory is different from personal memory. It is still complex, but it is more tractable. Companies have roles, functions, customers, products, risks, commitments, owners, workflows, and decisions. A CEO, PM, lawyer, support lead, salesperson, manager, and IC may see the same artifact differently, but the set of meaningful perspectives is not infinite. That boundedness is one reason company memory is buildable.

The second lesson was that you cannot solve this by dumping everything into one store. That only moves fragmentation down a layer. If the memory substrate cannot support different ontologies over the same underlying data, the company ends up with separate memories for sales, product, legal, support, leadership, and agents. The interface may look unified while the memory has already split.

The substrate has to be universal without forcing one universal interpretation. That sentence took us a long time to arrive at. A useful Company Brain needs a single memory substrate that can hold the same underlying artifacts while allowing different valid perspectives on them. The CEO looking at a customer email should not see the same thing as the support lead, the PM, or the account owner. They should all be looking at the same memory, but through different ontological lenses.

This is also why context graphs are not static objects. The graph depends on the ontology. The same email may create a risk trace, a commitment trace, a decision trace, an action trace, or several of those at once. The ontology determines which relationships matter, which state changes should be remembered, and which future actions the memory should inform.

Memory traces matter because they let the system begin to build a world model of the company. By world model, I do not mean anything mystical. I mean a learned representation of how work tends to move: which signals precede risks, which commitments usually slip, which handoffs fail, which decisions create downstream work, and which actions resolve problems.

This is the deeper reason memory matters for enterprise AI. Without memory, agents can act only from the context they are given in the moment. With memory traces, decision traces, and action traces, the system can begin to learn from outcomes over time. If a certain escalation pattern repeatedly leads to churn, if a handoff keeps failing, or if a type of customer request usually predicts a roadmap change, the system should learn. In plain terms, this is learning from outcomes — the beginning of reinforcement learning inside the company: traces, actions, outcomes, and feedback loops.

Testing this with customers made the idea less theoretical. In one organization, a customer success person was speaking with a customer when something that looked like churn risk surfaced in the conversation. The system did not wait for a weekly account review or a cleaned-up CRM note. It flagged the CTO while the conversation was still fresh, and the CTO started investigating immediately. The interesting part was not that the CTO learned something they never would have learned otherwise. The interesting part was latency. A signal that normally would have moved through a chain of summaries, meetings, and escalations showed up almost as it happened.

We saw a related version of this in project work. People working on the same initiative can have different understandings of the goal, the timeline, or what has already been decided. A Company Brain can notice some of that earlier. But then the product question becomes more delicate: should it tell everyone they disagree? Should it ask a manager to intervene? Should it simply show the conflicting traces and let the humans work it out? These are not technical questions alone. They are human questions that become visible earlier because memory compresses the time between a signal appearing and someone noticing it.

For a founder or CEO, this means answering the question almost nobody can answer well: what is actually happening inside the company? Not the sanitized version or the dashboard version, but the operational reality. For leadership, it means seeing whether strategy is turning into work. For managers, it means understanding blockers, commitments, and handoffs without constantly asking for updates. For ICs, it means context comes with the task.

That boundary matters. Company memory has to be explicit about ontology, provenance, permissions, and scope. The goal is not to make everyone feel watched. The goal is to make work legible, creditable, and easier to act on.

Over the last year, testing these ideas with teams and design partners has made one thing clearer: memory is useful almost everywhere, but it should not show up everywhere in the same way. Sometimes it should summarize, sometimes it should warn, sometimes it should connect two things nobody realized were related, and sometimes it should stay quiet until the right condition is met.

One of the harder questions from a large enterprise pilot was how to show the value of memory at all. The default visual answer is a knowledge graph. There is a place for that, especially when explaining the architecture. But I do not think a graph is how most people should experience company memory day to day. The right experience is closer to the system helping you notice what matters and make the next good decision.

TikTok is a strange but useful analogy. TikTok does not show you a giant map of all your interests. It understands enough about what you care about to surface the next thing you are likely to engage with. Company memory should not work like consumer recommendations, but the product lesson is real. The value of memory is often experienced through surfacing, not browsing.

That is what makes the substrate so important. If the memory layer is flexible, the surfaces can be different. A CEO surface, manager surface, IC surface, and agent surface can all use the same underlying company memory without becoming the same product.

The interface question matters more than I expected. I do not think Company Brain should feel like another person inside the company. If it behaves too much like a copilot or an employee, it can quietly take agency away from the humans who still own the decision. It should feel more like a tool, a cockpit, or a memory instrument. It can surface reality, show disagreement, and prepare the work, but humans need to remain responsible for judgment.

We started with the ambition of Enterprise General Intelligence. A year later, I think the clearer primitive is Company Brain. The ambition did not get smaller. The path became clearer: the memory substrate has to come first.

The version of enterprise AI I believe in is not built around tool-local memory. It is built around shared semantic company state: interactions becoming memory, memory becoming a world model, and the world model helping humans and agents act with context.

---

### Key Quotes

> "The first lesson was simple, though it took a while to internalize: company memory has to emerge from the work itself."

> "I have started to think of these interactions as the chain of thought of the organization. Not in the literal model sense, but in the human sense. They are where the company reasons before it writes down what it decided."

> "LLMs can help understand what was said; ontology determines what it means inside the company."

> "The substrate has to be universal without forcing one universal interpretation."

> "I do not think Company Brain should feel like another person inside the company. It should feel more like a tool, a cockpit, or a memory instrument."

> "The version of enterprise AI I believe in is not built around tool-local memory. It is built around shared semantic company state: interactions becoming memory, memory becoming a world model, and the world model helping humans and agents act with context."
