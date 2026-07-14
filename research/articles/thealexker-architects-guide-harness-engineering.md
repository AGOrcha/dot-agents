# The Architect's Guide to Harness Engineering

**Author:** Alex Ker (@thealexker)
**Source:** https://x.com/thealexker/status/2076741193367761378
**Published:** July 13, 2026
**Engagement:** 17.3K views · 110 likes · 255 bookmarks

---

## Relevance to dot-agents

**[GAP-NOTE parked + GAP-ADOPT small + OVERLAP-SHARPEN]** (eval Part L.6). Essay-grade, no
measurements, and cites L.5 LangChain (so the harness-profile cluster is ~1.5 independent
sources, not 2). Its **8 diagnostic properties** are a ready-made self-assessment rubric for
dot-agents-as-a-harness — we score well on most (context/state via ISP subagents, memory via KG
typed views, MCP support, observability via iteration-log + verifier evidence, model-selection
via `model:` routing, hackability = composable); weak axes worth a parked docs/positioning pass
are remote access and self-repair/rollback observability. Small adoptable: **cache-aware
routing** — routing to a different model mid-session incurs a cache miss, so a router should
prefer the warm-cache model when quality permits (a task-note for `model:`-per-agent routing in
the r-series / full-loop-runtime). "Standard adherence: open conventions vs proprietary
structure" is the sharpest external framing of config-v2's portability/lock-in goal.

"We shape our tools, and thereafter our tools shape us." – McLuhan. The next generation of trillion-dollar opportunities will come from choosing the best model for your harness, and the best harness for your model. Models and harnesses need **co-optimization and co-evolution**.

Choosing or customizing a harness is personal and situational; it's often a topic of endless debate amid choice paralysis. This guide starts from the practical question of which harness fits your role, then discusses from first principles the must-have primitives, presenting a framework to evaluate whether a vanilla option suffices or it's worth building your own.

## Buy, customize, or build?

**Non-Engineers (GTM, Ops, most knowledge workers):** You probably shouldn't customize your harness most of the time. Focus on finding and buying the best AI-native application (Harvey for Legal, Clay for GTM, Descript for Video). Out-of-the-box, less-customized experiences provide strong guardrails against degenerate behaviors in sensitive use cases, and maintaining your own evals is infeasible as an end-user. Your real job is to be a good **context engineer** — populate the existing harness with the right context.

**Engineers (incl. non-technical tinkerers):** Bring-your-own-model lets you mix frontier open-source, closed, or self-hosted models to run harnesses that are performant and cost-effective. Spin up subagents in parallel for both high-quality deep work and frequent, routine, shallow work. Highest-leverage actions:
- Document error cases / rough edges in a file so the harness doesn't fall into the same pitfall + recovery loop (a cause of frequent compaction and the "dumb zone").
- Encode the structure of a huge codebase into a `.md` file at the root and instruct the harness to look there first, avoiding repeated grep/exploration.
- Disconnect unused tools to save context window.
- Further: if you have user trajectories + evals of what "good" looks like, consider post-training / fine-tuning the harness *jointly* with your model (Cursor, OpenCode do this). But this is the frontier — exhaust out-of-the-box and customization approaches first.

## Three categories of harness (least → most composable)

- **Frameworks and SDKs** — for *building* harnesses (Django gives building blocks, not an app). Vercel AI SDK, Anthropic Agent SDK, Mastra.
- **Extensible** — ship with few/no built-in features beyond the base tool-use loop (like vanilla emacs/vim). Pi and Deep Agents.
- **Turnkey** — batteries-included, lots of features, often tied to proprietary APIs. OpenCode, Codex, Claude Code, Cursor agent.

## Two components of the decision

- **Harness-task fit.** Is it post-trained with the models and tools you need? Right features and level of abstraction?
- **Fluency.** Is it a harness you actually know how to drive?

## The eight diagnostic properties (for harness-task fit)

Score your candidate; the gaps tell you whether vanilla suffices or you need to build. They cluster into context management, connection to the outside world, and production behavior.

1. **Context and State (within a task).** Stateful context across turns? How does it handle interruptions/continuations? Compaction mechanism over long horizons — how lossy, can you mitigate? How does it spin up subagents to fight context rot? Can it parallelize workstreams (interwoven or independent)?
2. **Memory (across tasks).** What persists between sessions, and how — local cache, database, remote? When is it retrieved and used?
3. **MCP/Tool Support.** Extensible MCP integration? Can it be enriched with Clay data, output to Grafana, ingest from Notion? How faithfully does it interact with other apps? Compatible with your desired tool calls?
4. **Standard Adherence.** Open spec vs. proprietary structure. OpenCode uses an SQLite store + open conventions; Claude Code uses a Claude-specific structure (`claude.md`, dedicated directory, its own session-history file format). Open standards = portability, less lock-in; proprietary = tighter integration but harder migration if you want to share context across harnesses.
5. **Model Selection Flexibility.** How easy to try new model endpoints? Override settings/config/env to point at non-native endpoints? Can you try new open-source models in your existing setup? Do all models work equally well?
6. **Remote Access.** Access sessions remotely? Built-in (Claude Code's `/remote-control`) or third-party wrapper? Accessible after you close your laptop?
7. **Observability/Debugging Surface.** How is logging/tracing tracked? Can you catch failure modes? Early signal when heading wrong? Roll back to an earlier state and course-correct — or better, can it fix itself if it lands in a bad state?
8. **Hackability Spectrum** (opinionated vs. composable). The key question for a technical decision-maker choosing for a team. More opinions = less friction, easily adopted in enterprise (Copilot). Not opinionated + highly malleable = many ways to push potential, but also pitfalls (security vulns, extreme token consumption) — OpenClaw. In a mature org, an opinionated option with guardrails is the way. The sweet spot: a harness where the troughs of failure aren't critical but the ceiling of productivity is as uncapped as possible. Composable/hackable empowers *you* to smooth out what's not working instead of waiting for the maintainer to maybe fix it upstream.

## The future

The hallmark of the shift into production is a harness that can **support, route, and orchestrate many models.** It comes down to two linked decisions: how you route and how you serve.

**Routing across subagents.** Karpathy's "jagged edges of intelligence": models are superhuman in some domains and fumble in others, so **specialization becomes dominant** — employ multiple models and route each task to the appropriate one using independent subagents. A common strategic pattern: a lightweight classifier decides whether to route to good open-source defaults (save cost), a fine-tuned model (maximize latency/throughput), or a frontier closed model (review pass). Keep an eye on **cache reuse** — within a multi-turn session, routing to different models can incur a cache miss; the harness architecture should be **cache-aware** so requests hit the warm cache for a given model whenever possible. For read-only tasks (investigations, research), subagents can use smaller, faster models; spin up many copies for focused parallelizable work. Subagents are the OOP-of-agents pattern — encapsulation + clean context to fight context rot.

**Serving for performance.** Once routing matches each workload to the right intelligence, model performance is tied directly to UX, determined by the infrastructure around inference. Choose the right model for agentic use too (GLM 5.2 cited as a strong quality+performance candidate). BYO-model or a proxy gives flexibility to pick the best model-harness combo (references LangChain's Harness Profiles). If you need guaranteed rate limits, burst handling, and uptime, host your own open/custom models — dedicated infra lets you control caching, batching, and disaggregation.

## Final thoughts

Harnesses are not one-size-fits-all. Rather than concrete recommendations that age poorly, choose your **non-negotiables** (e.g. strong compaction for huge contexts, operability across multiple models, remote access) and become **fluent** at one particular harness. With fluency come the ways to fork, modify, or command the harness to do precisely what you want. The best harness properties will standardize and eventually commoditize; routing intelligence to the right models + performant inference will fold into the harness itself as table stakes.
