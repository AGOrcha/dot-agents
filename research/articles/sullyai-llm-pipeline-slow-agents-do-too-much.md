## Your LLM Pipeline Is Slow Because Your Agents Do Too Much

**Source**: https://x.com/sullyai/status/2044466240597459230
**Author**: Sully.ai (@sullyai) — written by Muratcan Koylan & Amit Kumthekar
**Date**: 2026-04-15
**Method**: Playwright
**Word count**: ~5200 words

---

### Summary

Sully.ai's engineering team describes how decomposing a monolithic clinical AI documentation pipeline into parallel focused specialists reduced p50 latency from 37s to 7.5s and p95 from 100s+ to 16.3s across 100,000+ production encounters — without sacrificing quality (which held or improved). The core insight: context engineering and iteration are substitutes, not complements. When each agent sees only the context for its narrow task, first-pass accuracy is high enough that correction loops become unnecessary. The piece covers their V1 (monolithic draft + judge loop), their V2 (parallel section agents + single QA pass), supporting design choices (uniform agent interface, dynamic output contracts), and a secondary finding that decomposition changes the model selection equation — opening space for smaller, faster models.

---

### Body

When a clinical AI system makes an error, the cost is measured in clinician time. Every fabricated medication or misattributed statement is something the clinician has to find and fix. In a 15-minute visit, the margin for review is thin. If the output requires line-by-line auditing, the system has failed regardless of how fast it appeared on screen.

This makes accuracy the primary design constraint for clinical AI in production. Speed matters too, for reasons specific to clinical workflow. In high-volume clinics, clinicians move between visits with little transition time, and documentation that arrives 60 seconds after a visit ends competes with the next patient for attention. In inpatient settings, timely documentation affects care coordination directly: a specialist's assessment needs to be available before the primary team rounds, and handoff documentation in emergency settings has direct implications for patient safety. Speed is a clinical requirement. But speed without accuracy is worse than slow and correct.

Most clinical AI systems achieve one of these, not both. The most common approach is a single model call: one prompt, one pass, accept whatever comes back. It is fast. It is also unreliable. The model hallucinates medications, misattributes statements to the wrong speaker, drops entire sections. Clinicians learn quickly that they cannot trust the output.

A second approach adds a safety net. Generate a draft, run a judge model to find errors, send the violations to a refinement agent, repeat until the judge passes. This pattern exists, in various forms, in most production systems that care about accuracy. In our system, the loop caught real problems. When we ran a controlled ablation, removing the loop entirely, output quality dropped 11% across clinical dimensions (3.17 → 2.85 on our internal eval scale, n=126 vs n=114). The steepest drops were in clinical safety (+0.58) and evidence reasoning (+0.54), the dimensions where errors carry the highest clinical risk. The loop was doing real clinical work. But each correction cycle added 10-15 seconds of serial computation, and the loop consumed more than half the total pipeline time.

We spent months trying to make the loop both higher quality and faster. Smaller judge models. Tighter prompts. Fewer iterations. The improvements were marginal. The problem was not the speed of the loop. The problem was that we needed the loop at all.

**The question we started asking: why was the first pass unreliable?**

Our hypothesis was that a weak first pass was not a model capability issue. It was a task design issue. A single model generating a full clinical document performs six to eight concurrent cognitive tasks at once: parsing a long transcript, routing information to correct sections, applying section-specific documentation rules, handling specialty logic, avoiding fabrication, producing structured output matching a complex schema. When one model does all of this simultaneously, accuracy on any individual task degrades. The correction loop exists to compensate for that overloaded context.

This is the finding that shaped everything that followed: **context engineering and iteration are substitutes, not complements.** When you decompose a complex task into focused sub-tasks, each component sees a radically narrower context. The model does one thing instead of eight. The first pass becomes reliable. The correction loop becomes unnecessary. And because focused components can run in parallel, the architecture is fast as a structural consequence of being accurate, not the other way around.

We tested this by restructuring our pipeline around parallel specialist components, each responsible for a narrow slice of the document, each seeing only the context relevant to its task. A single quality check replaced the iterative loop.

Across more than 100,000 production encounters, quality held or improved: each component's first pass was more accurate than what the overloaded approach produced. Latency dropped from 37 seconds at p50 to 7.5 seconds, and from over 100 seconds at p95 to 16.3 seconds. Not because we found a faster model, but because we eliminated the architectural pattern that made the pipeline slow.

**Why monolithic agents produce weak first passes**

When you ask a single agent to produce a complex, multi-part output, it must perform several competing cognitive tasks within one context window. The more objectives packed into one prompt, the worse each individual objective is executed.

Consider what a single model generating a clinical document is actually doing. A typical note has over a dozen sections: the patient's history, review of systems, medications, physical exam findings, assessment and plan, procedure and diagnosis codes, and more. Each section has its own documentation rules. The model receives all of these instructions simultaneously, alongside a full transcript (often thousands of words), clinical context from the EHR, safety constraints, and an output schema with a key for every section.

It is at least six concurrent cognitive tasks packed into a single prompt:

- Parse a long, unstructured conversation and identify who said what about which problem
- Route each piece of information to the correct section (this symptom goes in HPI, that finding goes in Physical Exam, this medication goes in the medication list)
- Apply section-specific documentation rules that differ for every section
- Handle specialty logic (hormone therapy documentation rules for endocrinology, surgical site specifications for orthopedics)
- Avoid fabrication across all sections simultaneously
- Produce structured JSON output matching a 15-key dynamic schema

Recent research quantifies what we observed in practice. Across 256 models tested, instruction-following accuracy drops from 92% at 200 tokens of instructions to 60% at 4,000 tokens (Gupta et al., 2025). Even the best frontier models achieve only 68% accuracy when following 500 simultaneous instructions (Jaroslawicz et al., 2025). Post-training and reasoning chains do not fix it. It is a fundamental property of how attention works under competing objectives.

We saw the consequences in our judge violations. The judge returned structured error reports: which section had the problem, what text was wrong, what the fix should be. The patterns were consistent. Medications from the transcript appeared in the wrong section. Attribution errors compounded across sections — a symptom the patient reported would be documented as a clinician observation. Sections that were narratively related (HPI and Assessment) contradicted each other because the model processed them independently within a single overloaded context.

The judge caught these errors. But here is what we did not expect: **the refinement agent often made them worse.**

We analyzed ~50 production traces to understand what the refinement loop actually did. It resolved roughly 45% of the violations the judge flagged — primarily quick fixes like ICD-10 coding errors, missing section content, and medication documentation gaps. But it also introduced new violations at nearly the same rate: 15 new clinical accuracy errors, 13 new structural or template compliance errors, and 11 new Assessment and Plan accuracy issues that were not present in the original note. Only 8% of traces were fully resolved after refinement. In 39%, the note showed no improvement at all.

The pattern was consistent: refinement fixed small, well-defined problems (a missing code, an incomplete section) and struggled with — or actively worsened — larger clinical accuracy and structural issues. A section agent omits a medication. The judge flags the omission. The refinement agent adds the medication back, but attributes it to the wrong part of the encounter, or adds a dosage that was never mentioned in the transcript. The note goes from incomplete to fabricated.

Research on self-correction in language models supports this pattern. Intrinsic self-correction, where a model corrects its own output without external feedback, remains largely ineffective. The judge and the generator share the same blind spots. Asking one to fix the other is closer to asking the same person to review their own work after a coffee break than to getting a genuine second opinion.

One detail from the judge ablation surprised us. Section agents performed almost identically whether or not the judge loop was present downstream. With the judge, sections improved the draft by +0.23 points; without it, by +0.33. The section agents were doing their job either way. The quality difference came entirely from whether the judge/refinement loop existed to clean up afterward. This told us the right question was not "how do we make the judge better?" but "how do we make the sections good enough that we don't need it?"

This realization shifted our approach. We stopped trying to make the judge faster. We started trying to make the judge unnecessary.

**Task decomposition as context engineering**

The fix was not a better model or a cleverer prompt for the monolithic agent. It was decomposing the task so each agent has a single clear objective.

This is the central finding from our work, and it generalizes far beyond clinical documentation. Andrej Karpathy described context engineering as "the delicate art and science of filling the context window with just the right information for the next step." We arrived at the same conclusion from the production side: when you control what the model sees, you control what it gets right.

In our previous pipeline, an agent generating the History of Present Illness section also saw instructions for Assessment and Plan, Review of Systems, procedure codes, and every other section. It had to route information, apply 15 different rule sets, and produce a 15-key JSON object, all while parsing a 10,000-word transcript. In the new pipeline, the HPI agent sees only HPI instructions, only HPI output keys, and the same transcript. It does one thing.

The transcript is the same. The model can be the same. But what the model sees in its context window is radically narrower.

**What changes in the context**

Every section agent receives two categories of information:

- **Shared context**, identical across all agents: the full transcript, clinical demographics from the EHR, and safety rules (no-fabrication directives, attribution rules, fallback behavior for missing information). This is the raw material of the note.
- **Focused context**, unique per agent: only the instructions for its assigned sections, and a dynamically-generated output schema with only its section keys. An agent assigned Chief Complaint and HPI gets a schema with two keys. An agent assigned Assessment and Plan, CPT codes, and ICD-10 codes gets three. Compare this to the draft agent's 15.

Two keys versus fifteen. The task complexity per agent drops by 5-7x. Fewer objectives means higher per-objective accuracy. Higher first-pass accuracy means no correction loop.

The output schema itself is a form of context engineering. It tells the model what to produce before it reads the transcript. A schema with keys `chief_complaint` and `history_of_present_illness` implicitly scopes the task. The model writes to the schema, not around it.

**What this enables architecturally**

When each agent is independent and focused, they run simultaneously. Wall-clock time becomes the duration of the slowest single agent, not the sum of all stages. In practice, multiple sections can be grouped into a smaller number of parallel specialist calls. The exact grouping matters less than the principle: sections that share context dependencies belong together, and unrelated work should not compete for attention in the same prompt.

In production, this plays out concretely. Across half a million section agent calls, p50 latency per section agent is under 2 seconds. The QA agent, which sees the full assembled note, takes longer — p50 of 4.3 seconds — but it runs once, after all section agents have completed in parallel. The total p50 end-to-end is 7.5 seconds. For shorter transcripts (under 1,000 words), it drops to 4.3 seconds.

A single QA agent reviews the combined note after all section agents complete. It detects and fixes issues inline in one pass. No iteration.

In an evaluation of 131 complex clinical cases, the system achieved an 83% average capture rate for clinical items with only 2% critical issues. Diagnoses were captured at 95.6% accuracy, symptoms at 95.0%, medications at 93.0%. The areas with the most room for improvement — patient instructions and plan items — correspond to sections that require the most cross-transcript synthesis, exactly the task where focused context has the highest leverage. Notably, omissions (43.6% of issues) dominated over fabrications (11.4%), meaning the system errs toward incompleteness rather than hallucination — a safer failure mode in clinical documentation and one that clinician review catches naturally.

**Context engineering and iteration are substitutes**

This is the relationship we want to name explicitly.

You can have broad context and iteration: one agent sees everything, produces a weak first pass, and a judge loop corrects it over multiple rounds. This was our V1. It worked. It was slow.

Or you can have focused context and a single pass: each agent sees only what it needs, produces a reliable first pass, and one lightweight QA check catches the remainder. This is our V2. It is 5x faster because it eliminates serial dependencies.

The judge loop was not wrong. It was solving the right problem — quality assurance — with the wrong lever. Iteration compensated for a context engineering problem. When we fixed the root cause, the symptom disappeared.

Anthropic's research on context engineering for agents reaches a similar conclusion: "context is a finite resource with diminishing marginal returns," and sub-agent architectures that scope each agent's context produce better results than monolithic approaches with longer context windows (Anthropic, 2025). Cursor's research on self-driving codebases found the same pattern: a single agent given too many roles "exhibited pathological behaviors" and "in retrospect, it makes sense it was overwhelmed." Separating roles into focused specialists resolved the problem. (Cursor, 2026)

The pattern holds outside clinical AI. It holds for any structured generation task where the output has independently-addressable sections and the input is long enough to dilute attention. Document generation, report assembly, code generation across multiple files, multi-section compliance reviews. **If your pipeline has a correction loop, ask whether the loop is compensating for an overloaded context.**

**A few supporting design choices**

The core finding here is decomposition, not framework design. Still, a few supporting choices made it possible to test that idea quickly and safely in production.

**Uniform agent interface**

Every agent in our system presents the same interface to the orchestrator. The orchestrator does not know what kind of agent it is calling. It sends typed input, receives typed output, and records execution metadata.

This matters because the transition from our old pipeline to the new one required zero changes to the agent layer. Same agents, different wiring. We removed the draft stage, removed the judge loop, added a QA agent, and rewired the topology. The agents themselves were untouched. When you are testing a hypothesis about pipeline architecture, you do not want to conflate the results with changes to the agents. The uniform interface made that separation clean.

The same pattern applies to any multi-agent system. If your agents are tightly coupled to a specific orchestration topology, every architectural experiment becomes a rewrite. If they are interchangeable, you can swap topologies in a day.

**Dynamic output contracts**

Each agent declares what it will produce — a structured schema generated per-request from the input. A template with 8 sections produces a schema with 8 keys. A single-section agent produces a schema with 1 key. The schema is built at request time, not hardcoded.

This serves two purposes. For the model, the schema guides generation. A model writing to a 2-key schema stays focused in a way that a model writing to a 15-key schema does not. For the system, the schema guarantees parseable output. Combining results from parallel agents is deterministic because each agent's output slots into a known structure. Fan-out is easy. Fan-in is where parallel systems break. Dynamic contracts make fan-in reliable.

**A secondary finding: decomposition changes the model selection equation**

When we benchmarked models on the monolithic pipeline versus the decomposed pipeline, we found something we did not expect: the architecture changed which models were viable.

**The quality gap narrows on focused tasks**

On the full note generation task, larger models consistently outperformed smaller ones. The gap was significant and stable. On section-level tasks, smaller models closed that gap substantially. A well-prompted model at a fraction of the parameter count matched or exceeded larger models on focused extraction and structuring.

To quantify this, we ran an experiment comparing a frontier-class model against a smaller open-source model on the same decomposed architecture, evaluated by the same LLM judge on a shared evaluation set. This was an exploratory comparison, not a production decision. The goal was to understand where decomposition changes the model selection equation.

We learned that the accuracy gap is not uniform, and that is what makes the finding useful. Diagnoses, symptoms, and medications show small gaps (under 5%). The largest drops cluster in sections that require cross-transcript synthesis: follow-ups, vitals, plan items, patient instructions. These are the sections where focused context and targeted prompt engineering have the highest leverage, which means the gap is narrow without switching to a larger model.

The experiment showed that decomposition opens a region of the quality-speed tradeoff that monolithic pipelines cannot access. A model that is too slow for end-to-end generation becomes viable when it is one of several parallel components. The architecture does not force a single model choice, and that flexibility is where the practical value lies.

This is consistent with broader research. A fine-tuned 1B model matched GPT-4.1 at 99% accuracy on a focused classification task, with 18x throughput improvement (arXiv:2510.21970). Microsoft found that a fine-tuned small model outperformed GPT-4o on search relevance while being 17x faster and 19x cheaper (Kang et al., 2026). In the clinical domain specifically, a fine-tuned 8B model achieved human-level accuracy (90% exact match) on clinical information extraction across four datasets, using a single desktop GPU (Liu et al., Nature Scientific Reports, 2025).

**Task complexity, not model capability, determines output quality. When you simplify the task, smaller models become viable. Decomposition is a model selection strategy, not just a latency strategy.**

**The latency-quality frontier shifts**

A model that takes 15 seconds for full note generation becomes viable when it is one of several parallel components. You pay the latency of one call, not five sequential ones. The architecture opens up a region of the speed-quality tradeoff that monolithic pipelines cannot access.

Parallel agents use more total tokens than a single call but each call is smaller (focused instructions, smaller schema), which partially offsets the duplication. And because we run on our own inference infrastructure (Nvidia, 2026), the cost equation is compute-hours rather than per-token API pricing.

**Model diversity across agents is practical**

Different agents have different requirements. Section agents performing focused extraction benefit from fast inference speed. The QA agent performing cross-section reasoning benefits from stronger analytical capability. Our architecture supports assigning different models to different agent roles independently.

Our research team explored this principle in a different domain last year. The Consensus Mechanism (Kumthekar et al., 2025) applied specialist decomposition to medical reasoning: a triage model classifies the query, expert models reason from their specialty's perspective in parallel, and a consensus model synthesizes the outputs. The ensemble beat OpenAI's O3-high by 3.4-8.2% across four medical benchmarks. The architecture enabled a different thing there — improved accuracy through specialist depth rather than latency through parallelism — but the underlying insight is the same. Decompose the task. Assign each piece to the right model. Combine the results.

If you are hitting quality limits on smaller models, the answer might not be a bigger model. It might be a simpler task per agent.

**How we validated this**

We did not replace the old pipeline with the new one. We ran them side by side on the same production traffic for four months.

We could vary pipeline topology, model choice, and prompts independently, which let us isolate one variable at a time without risking patient care. Every agent call was traced end to end: model, prompt version, token counts, latency, retries, success or failure. We did not need synthetic benchmarks. Production traffic was the experiment, and the observability layer was the eval harness.

As of this writing, the new pipeline has processed over 100,000 production notes. The production numbers tell the story:

Latency scales gracefully with transcript length. Short encounters (under 1K words) complete at p50 of 4.3 seconds. Long encounters (5K+ words) take 11 seconds at p50. The relationship is roughly linear, not exponential — the architecture does not have a complexity cliff.

Earlier controlled experiments confirmed that quality holds at the latency target, and provider benchmarks showed that infrastructure choice shifts latency by 2-3x at the same concurrency — confirming that infrastructure matters as much as architecture.

The research-to-production cycle was days, not quarters. We started the architectural research in late 2025. By early 2026, the new pipeline was serving the majority of production traffic. That speed of iteration came from being able to test architectural changes directly on live traffic without spinning up a separate deployment or evaluation stack.

The cost question deserves an honest answer. The new pipeline uses more total tokens per note because multiple components process the same transcript. That trade-off is real. We accepted it because the quality and latency improvements were substantial, and because our infrastructure lets us manage that trade-off differently than a per-call API model would.

**Trade-offs and what we don't know yet**

The quality and speed improvements are real, measured across more than 100,000 production notes. But they come with trade-offs we are still navigating.

**The speed only counts if quality holds.** We run continuous evaluation across every customer and specialty. Quality regressions trigger alerts. The architecture comparison we described — removing the judge loop in favor of focused specialists — produced a quality improvement alongside the latency reduction. Clinicians have noticed: customer satisfaction scores increased meaningfully after the transition. But maintaining that quality line requires ongoing investment.

In our model comparison experiments, the gap between frontier and smaller models was largest in the highest-complexity sections — plan items, instructions, follow-ups — which is exactly where prompt engineering and decomposition strategy have the most leverage. Per-agent model routing means we can assign stronger models to the sections that need them, and the architecture supports that without changing anything else.

We are also experimenting with small language models fine-tuned through reinforcement learning for specific section types, with the goal of further improving both speed and accuracy on focused extraction tasks.

**When single-pass QA is not enough.** The iterative judge loop caught cross-section inconsistencies that a single QA pass sometimes misses. Section A contradicts Section B. A medication listed in the plan was not mentioned in the history. The QA agent sees the full combined note and catches many of these, but it operates in one pass. The iterative judge had multiple chances to find subtle contradictions.

Our judge ablation quantifies what the loop contributed beyond section agents: +0.27 on template compliance, +0.29 on instruction compliance, +0.23 on template instruction violations. These are the compliance dimensions — exactly the cross-section consistency checks that a single QA pass has one shot to catch. For complex multi-problem encounters across certain specialties, the jury is still out. We monitor these cases closely and retain the ability to use the older pipeline where it remains the safer option.

**Prompt quality becomes load-bearing.** This is the trade-off that changed how we work. Without the judge loop as a safety net, the quality of each agent's prompt directly determines output quality. A mediocre section instruction in the old pipeline got corrected by the refinement agent. In the new pipeline, it ships.

This is both the biggest risk and the biggest motivation for investing in prompt engineering as infrastructure. We invest heavily in structured, versioned prompt blocks assembled per-agent and per-organization. When a section agent produces a consistent error pattern, we trace it to the specific instruction block that caused it and fix that block.

**We do not have a principled decomposition framework.** Which sections should be grouped together? One agent per section? Related sections combined? The optimal granularity depends on section complexity, information dependencies between sections, and specialty. Chief Complaint and HPI are grouped because they share a narrative thread. Assessment and Plan are grouped with procedure and diagnosis codes because clinical reasoning needs coherence across those outputs.

These groupings are empirical, and the data tells us where to focus next. Across all our evaluations, Assessment and Plan is the section where decomposition has the most impact — it consistently concentrates the largest share of improvement opportunities (33-46% of flagged items across experiments), regardless of model or pipeline variant. HPI is the second most complex section at 15-17%. This is expected: these sections require the most cross-transcript synthesis. It tells us that decomposition granularity matters most where synthesis complexity is highest, and that prompt engineering investment in A&P yields disproportionate returns.

We test different configurations per template and per specialty. The choices are informed by clinical workflow knowledge and validated by production metrics. But they are not derived from a formal framework.

None of what we have shared here is universal truth. These are the patterns that worked for our system, on our data, for our clinical documentation task. Your decomposition boundaries will differ. Your optimal granularity will differ. But if your pipeline has an iterative correction loop and your first pass is unreliable, the diagnostic question is the same: **is the iteration compensating for an overloaded context?**

**What's next**

The finding that prompt quality is load-bearing has changed how we invest in clinical AI.

When a clinician's documentation depends on each agent's first pass being reliable, the instructions that guide those agents become clinical infrastructure. Different specialties have different documentation requirements. Different organizations have different workflows. A prompt that works for an endocrinology visit may miss what matters in an orthopaedic follow-up. We are building systems that make these instructions composable, versioned, and customizable per clinical context without requiring code changes.

The second investment is in continuous improvement. When clinician feedback indicates a consistent error pattern, we need to trace it to the specific instruction that caused it and fix that instruction, not rewrite the entire prompt. We are building a closed-loop system where clinician feedback drives targeted, evidence-grounded improvements to the specific components that need them. The goal is prompt quality that improves weekly, not quarterly.

The architecture described in this paper was the first step. Keeping quality high at speed is a context engineering problem. Keeping quality improving over time, across specialties and organizations, is where the harder and more clinically impactful work lies.

---

### Key Quotes

> "Context engineering and iteration are substitutes, not complements."

> "The judge loop was not wrong. It was solving the right problem — quality assurance — with the wrong lever. Iteration compensated for a context engineering problem. When we fixed the root cause, the symptom disappeared."

> "Two keys versus fifteen. The task complexity per agent drops by 5-7x. Fewer objectives means higher per-objective accuracy."

> "If your pipeline has a correction loop, ask whether the loop is compensating for an overloaded context."

> "Instruction-following accuracy drops from 92% at 200 tokens of instructions to 60% at 4,000 tokens (Gupta et al., 2025)."

> "Task complexity, not model capability, determines output quality. When you simplify the task, smaller models become viable. Decomposition is a model selection strategy, not just a latency strategy."

> "Fan-out is easy. Fan-in is where parallel systems break. Dynamic contracts make fan-in reliable."
