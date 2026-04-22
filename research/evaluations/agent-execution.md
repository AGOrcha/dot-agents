# Articles Evaluation — Agent Execution & Delegation

**Written:** 2026-04-21
**Scope:** How the articles speak to dot-agents' runtime layer — the orchestrator, loop-worker, verifier, review pipeline, delegation bundles, write-scopes, parallel orchestration, and the ISP pipeline. Execution is distinct from orchestration: orchestration decides *what* and *in what order*; execution is *how agents run, communicate, and produce output*.
**Siblings:** `workflow-orchestration.md`, `hooks-and-platform.md`, `skills-rules-graduation.md`, `lessons-and-memory.md`, `../articles-evaluation-kg-and-adjacent.md`.

**Rubric:** Core / Pros / Cons / Risk profile (Failure mode / Evidence / Reversibility / Second-order) / Mapping (`[OVERLAP-SHARPEN] / [GAP-ADOPT] / [WE-AHEAD]`).

---

## Part A — Per-article evaluation

### openclaw-hermes — *Supervisor Pattern*

**Core.** One supervisor agent routes work to specialist sub-agents based on declared roles and capabilities. Supervisor owns state and arbitration; specialists own execution. Roles are declared, not negotiated at runtime.

**Pros.**
- Centralizing arbitration in one agent matches our orchestrator's role — it prevents the classic "two agents disagree about who owns task X" failure.
- Declared roles are the right stance: our delegation bundles name the role (`loop-worker`, `verifier`, `reviewer`) up-front, not by inference.

**Cons.**
- Supervisor is a single point of failure. If the supervisor's context degrades, the whole fanout degrades.
- Doesn't address how the supervisor's context stays correct across long runs.

**Risk profile.**
- *Failure mode:* supervisor context bloat → routing errors (wrong agent for task) → silent quality regression. Specialist agents don't know they got the wrong ticket.
- *Evidence:* structural (the pattern is widely replicated); specific to Hermes project.
- *Reversibility:* moderate — if supervisor routing is wrong, rolling back means rewriting the routing logic, not a data rollback.
- *Second-order:* forces role declaration up-front, which is healthy; can rigidify roles so that hybrid tasks (e.g., verify-then-implement-a-fix) are awkward.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our orchestrator is a supervisor; `loop-worker` / `verifier` / `reviewer` are specialists. The hermes pattern validates this. Worth writing one paragraph in `workflow-artifact-model.md` naming the supervisor role explicitly and pointing at our orchestrator as that role.
- **[GAP-ADOPT]** — add a "supervisor context health" check: at iteration-close, the orchestrator should self-audit whether its fanout decisions on the last N tasks were correct (fanout count, conflict rate, verifier-rework rate). Composes with fold-back.

---

### claude-obsidian-ai-employee — *Agent as Employee*

**Core.** Treat the agent as a new employee onboarding into a role. Give it a job description, communication rules, escalation triggers, and a learning channel. The role boundary is explicit — the agent is not a genie, it's an employee with scope.

**Pros.**
- The employee metaphor maps cleanly onto our role-per-subagent pattern (`loop-worker.md`, `verifier.md`, `reviewer.md` as "job descriptions").
- Escalation triggers are an under-specified surface in our stack: when should a loop-worker stop and ping the orchestrator vs. just fail?

**Cons.**
- Employee metaphor leaks when taken too literally — agents don't accumulate tenure or cross-train organically, and the metaphor encourages treating them as if they do.
- Communication-rule enforcement is implicit in the prompt, not enforced by the harness.

**Risk profile.**
- *Failure mode:* implicit escalation rules mean agents either over-escalate (ping for every ambiguity → supervisor bottleneck) or under-escalate (guess and fail verification).
- *Evidence:* anecdotal; operator-level claim.
- *Reversibility:* easy (prompt edits).
- *Second-order:* explicit escalation rules tighten the fanout/fan-in boundary and make orchestrator load predictable.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our subagent prompts (`loop-worker`, `verifier`) are job descriptions; make that framing explicit in the prompt preamble.
- **[GAP-ADOPT — P1]** — add an explicit "escalation protocol" section to each subagent prompt: "If you hit X, Y, or Z, stop and report rather than proceeding." Today we rely on the agent's judgment. Composes with milksandmatcha's expediter pattern.

---

### codex-multi-agent-swarms-playbook — *Multi-agent Coordination*

**Core.** Swarms of short-lived subagents, each given a bundle that contains goal, context, output contract, and known anti-scope. Coordination happens via the bundle design, not via runtime chatter.

**Pros.**
- Matches our fanout exactly: bundles carry context, not agents carry context across calls.
- The "no runtime chatter" stance is what makes parallelism safe. If agents had to coordinate mid-task, conflicts would be hard to detect.

**Cons.**
- Bundles can get stale if the orchestrator writes them once and the world moves on (e.g., a sibling task finishes mid-fanout and changes the shared state).
- No guidance on partial bundle invalidation.

**Risk profile.**
- *Failure mode:* stale bundles → subagent writes code against an outdated assumption → conflict detection catches it at fan-in, but after work is done (wasted compute). Silent in the sense that individual agents can't detect the staleness.
- *Evidence:* pattern-based; playbook-form article.
- *Reversibility:* easy (bundle-design rule changes).
- *Second-order:* bundle-as-contract encourages decomposing work so bundles are small and independent. Good.

**Mapping.**
- **[OVERLAP-SHARPEN]** — already covered in the workflow doc (W.4: audit bundle-prompt template).
- **[GAP-ADOPT — P2]** — add a bundle invalidation signal: if the orchestrator detects that a sibling task has altered shared state relevant to an in-flight bundle, cancel and re-bundle. Today we rely on write-scope conflict detection at fan-in; we could catch some conflicts mid-flight.

---

### milksandmatcha — *Kitchen Patterns, Parallel Verification*

**Core (execution-focus).** "Gordon Ramsay" parallel verification: the head chef doesn't serialize — verifier tastes one dish while the next is being plated. Applied to agents: verifier runs concurrently with next implementation, gated at fan-in rather than blocking.

**Pros.**
- Directly reduces ISP wall-clock without reducing rigor.
- Verifier running in parallel means its context is fresher — it's checking work near the time it was produced, not after a queue of other work.

**Cons.**
- Parallel verification means partial rollback if the verifier rejects: the next impl has already started against the rejected context.
- Requires dep-graph discipline: the next impl must not have already consumed the rejected work.

**Risk profile.**
- *Failure mode:* verifier rejects but next impl has already branched → cascading re-bundle and retry. Cost is visible (time) but not silent.
- *Evidence:* anecdotal; metaphorical.
- *Reversibility:* moderate — adopting parallel verification requires dep-graph aware fan-in logic that doesn't exist today.
- *Second-order:* forces clean dep-graph declarations; under-declared deps become visible as cascade reworks.

**Mapping.**
- **[GAP-ADOPT — P2]** — parallel verification track in the ISP pipeline. Verifier runs concurrent with next independent task; fan-in gates on verifier result. Covered as W.7 in the workflow doc — belongs to execution design.

---

### intuitiveml — *Three Parallel Review Passes + Architect/Operator*

**Core (execution-focus).** Every PR gets three Claude Opus 4.6 review passes in parallel: quality (logic/perf/maintainability), security (auth boundaries, injection), dependencies (supply chain, license). These are review gates, not suggestions. Engineering org splits into Architect (1–2 people; design SOPs for AI) and Operator (everyone else; assigned tasks by AI-triage).

**Pros.**
- Three lenses in parallel catches more than one generic pass, at the same wall-clock.
- Fixed-lens review produces predictable coverage; generic review produces unpredictable coverage.
- The Architect/Operator split names a real division: designing the harness vs. running tasks in it. Maps onto our "rule/skill author" vs. "loop-worker" division.

**Cons.**
- Three parallel LLM calls per PR is expensive; only justified at CREAO's throughput (3–8 deploys/day).
- Architect/Operator taxonomy is a social/org claim; the Operator role description ("AI assigns tasks to humans") is extreme and context-specific.

**Risk profile.**
- *Failure mode:* one lens passes while another finds issues → review gate blocks. Loud failure (visible in PR status); good.
- *Evidence:* anecdotal; specific numbers (3–8 deploys/day averaged over 14 days) reported but not externally verified.
- *Reversibility:* easy (toggle passes on/off per plan type).
- *Second-order:* fixed lenses means review prompts get tuned independently; prevents "one prompt to rule them all" drift.

**Mapping.**
- **[GAP-ADOPT — P2]** — three-lens parallel review for review-heavy work (specs, plan changes, proposals). Lenses should compose with planning-lenses from the_smart_ape — suggested lens set: quality, security, dependency, arch-consistency, contrarian.
- **[OVERLAP-SHARPEN]** — our Architect role already exists implicitly (whoever writes the `.agents/rules/` / prompts). Name it explicitly in `agents.md` so the division is legible.
- **[WE-AHEAD]** — our loop-worker is a more structured Operator than "AI assigns tickets to humans" — we keep humans-as-reviewers, not humans-as-ticket-operators.

---

### intuitiveml — *Harness Engineering Principle*

**Core.** "The primary job of an engineering team is enabling agents to do useful work. When something fails, the fix is never 'try harder.' The fix is: what capability is missing, and how do we make it legible and enforceable for the agent?" (OpenAI's harness engineering framing, picked up by CREAO.)

**Pros.**
- Names the correct mental model for our work: we build the harness, not the agent. Every failure is a harness deficiency.
- Aligns with thealexker's R.P.I. and sullyai's context-engineering-over-iteration — all three articles converge on the same substrate-first stance.

**Cons.**
- Over-applied, it discourages investing in better agents/models — sometimes the model really is the bottleneck.

**Risk profile.**
- *Failure mode:* none directly (it's a principle). Risk of misapplication: blaming the harness for a model-capability failure that no amount of context engineering can fix.
- *Evidence:* structural (supported by OpenAI's framing); anecdotal implementation (CREAO).
- *Reversibility:* N/A (principle, not code).
- *Second-order:* a team that internalizes harness engineering invests in rules, skills, prompts, and evaluation — exactly the work our stack is built for.

**Mapping.**
- **[GAP-ADOPT — P0]** — name harness engineering as our stance in `workflow-artifact-model.md` alongside Camp 2 (from witcheer). One paragraph. Filters future proposals toward harness-improvements.

---

### sullyai — *Pipeline Simplification*

**Core (execution-focus).** Every agent in a pipeline that doesn't carry its weight should be removed. Specifically, verifier agents often compensate for under-decomposed work upstream; if decomposition is right, verification collapses.

**Pros.**
- Challenges the default assumption that "more verifiers = more quality." Forces a pipeline audit.
- Pairs with the context-vs-iteration substitution framing: a verifier loop is an iteration loop in disguise.

**Cons.**
- Under-applied, it tempts teams to skip verification. Some verification is genuinely load-bearing (security, interface contracts).
- No concrete rubric for "is this verifier earning its cost."

**Risk profile.**
- *Failure mode:* removing a verifier that was actually catching real issues → silent quality regression; only visible weeks later via bug reports or downstream failures.
- *Evidence:* anecdotal (sullyai's pipeline experience).
- *Reversibility:* easy if we measure what the removed verifier was catching; painful if we don't.
- *Second-order:* forces per-verifier justification, which is healthy; risks political fights over "my verifier matters."

**Mapping.**
- **[GAP-ADOPT — P2]** — add a per-verifier audit: track what each verifier rejected over the last N plans; verifiers with near-zero rejection rate get reviewed for removal. Composes with the fold-back count signal from the workflow doc.
- **[OVERLAP-SHARPEN]** — our verifier variants (`unit`, `batch`, `streaming`, `ui-e2e`, `api`) are already differentiated; that's good. Add a per-variant rejection-rate dashboard.

---

### arscontexta — *Fresh Subagent per Phase*

**Core (execution-focus).** Each phase of the Six-R pipeline spawns a *fresh* subagent with clean context. No state leaks between phases. The article names this as the single most important execution discipline for long-running agent pipelines.

**Pros.**
- Matches our existing pattern: each `loop-worker` / `verifier` / `reviewer` is a fresh subagent with the bundle as its total context.
- Prevents the "agent poisoned by its own prior mistakes" failure mode that plagues long-running conversations.

**Cons.**
- Fresh-per-phase means the bundle *must* carry all necessary context, raising the cost of bundle design errors.
- Subagent spawn has overhead (cold-start token load, cache misses). Not free.

**Risk profile.**
- *Failure mode:* if bundle is under-specified, the fresh subagent asks clarifying questions or guesses. Loud (it shows up as verifier rejection or a clarifying escalation).
- *Evidence:* structural — widely observed in long-running agent chats that state leaks cause degradation.
- *Reversibility:* N/A (we already do this).
- *Second-order:* encourages investment in bundle prompts; the bundle is the harness per subagent.

**Mapping.**
- **[WE-AHEAD]** — we already do fresh-subagent-per-phase. Worth naming this as a principle in `agents.md` so it doesn't silently drift.

---

### thealexker — *Harness Thinking*

**Core (execution-focus).** The harness is everything the agent can see and act on — tools, rules, file surfaces, output formats. Improving the harness beats improving the prompt for the same agent.

**Pros.**
- Same idea as intuitiveml's harness engineering; reinforcement from an independent source.
- Concrete framing: "the agent's capability at task T is upper-bounded by the harness's legibility for T."

**Cons.**
- Doesn't address the tradeoff between harness size (more tools, more legibility) and context cost.

**Risk profile.**
- *Failure mode:* over-tooling — adding every possible MCP/tool just in case. Bloated context, slower agents, more distraction. Silent.
- *Evidence:* structural + anecdotal.
- *Reversibility:* moderate — removing tools people have started to rely on is a rule fight.
- *Second-order:* a team focused on harness improves rules, skills, and prompts in a compounding way.

**Mapping.**
- **[OVERLAP-SHARPEN]** — already covered above under intuitiveml's harness engineering. Cite thealexker as independent confirmation.

---

## Part B — Synthesis against our stack

### Execution patterns our runtime should internalize

1. **Harness engineering is our stance.** intuitiveml + thealexker + arscontexta all converge: the team's job is building the harness, not the agent. Every "agent failed" event should trigger a "what harness capability was missing" analysis.
2. **Fresh subagent per phase is already ours; keep it.** arscontexta + our own pattern. Name it explicitly.
3. **Explicit escalation protocols reduce supervisor load.** claude-obsidian-ai-employee + openclaw-hermes. Today our subagent prompts don't say "stop and escalate if X."
4. **Fixed-lens parallel review beats generic review.** intuitiveml. Apply to review-heavy plans.
5. **Parallel verification reclaims wall-clock.** milksandmatcha. Gated at fan-in, not serial.
6. **Verifiers need per-stage justification.** sullyai. Measure what each verifier catches.
7. **Bundles are contracts; keep them small and independent.** codex-multi-agent-swarms + milksandmatcha.

### What we do well (runtime)

- **Write-scope declared per task → deterministic conflict detection.** No one in this set has a formal equivalent.
- **Fanout + dep graph at the orchestrator level.** Already strictly ahead of openclaw-hermes's declarative routing.
- **Fresh subagent per role, per task.** Already matches arscontexta's strongest execution principle.
- **Role separation (loop-worker / verifier / reviewer / orchestrator).** Cleaner than most supervisor-pattern articles.

### What we miss (priority-ordered)

**P0:**
- Name "harness engineering" as our stance (intuitiveml + thealexker). One paragraph in `agents.md`.

**P1:**
- Explicit escalation protocols per subagent prompt (claude-obsidian-ai-employee + openclaw-hermes). Today each subagent guesses when to stop.

**P2:**
- Three-lens parallel review for review-heavy plans (intuitiveml). Quality + security + dependency + arch-consistency + contrarian.
- Parallel verification track (milksandmatcha). Verifier concurrent with next impl; gated at fan-in.
- Per-verifier rejection-rate audit (sullyai). Find verifiers that no longer earn their cost.
- Bundle mid-flight invalidation signal (codex-multi-agent-swarms). Cancel + re-bundle when sibling task alters shared state.

**P3:**
- Supervisor context-health audit at iteration-close (openclaw-hermes). Orchestrator self-checks its fanout decisions.

### What we do better than them

- **vs. openclaw-hermes:** our write-scope + dep-graph is stricter than their declared-role supervisor; we have structural guarantees they get only by convention.
- **vs. codex-multi-agent-swarms:** our orchestrator owns state; their coordination is ad-hoc.
- **vs. intuitiveml:** our Architect role has more structure (rules / skills / prompts / proposals) than their informal SOP design. Borrow their Operator/Architect naming as communication aid, not taxonomy.
- **vs. milksandmatcha:** our orchestrator is the expediter with formal ticket tracking; theirs is prose.

---

## Part C — Recommended next steps (execution layer)

**Immediate:**
- **E.1** — Name harness engineering as stance in `agents.md` (one paragraph; cites intuitiveml + thealexker + arscontexta). Filters future proposals.
- **E.2** — Rename each subagent prompt preamble to name the role as a "job description." One-line preamble edit per subagent.

**Short-term (one plan):**
- **E.3** — Explicit escalation protocol section in every subagent prompt: "If you encounter X, Y, Z, stop and report; do not proceed." (claude-obsidian-ai-employee + openclaw-hermes).
- **E.4** — Per-verifier rejection-rate tracking. Emit a per-plan `verifier_stats.json` at close; summarize in `history/`. Feeds a quarterly audit. (sullyai).

**Medium-term:**
- **E.5** — Parallel verification track on the ISP pipeline (milksandmatcha). Requires dep-graph-aware fan-in — stacks on the existing orchestration work.
- **E.6** — Three-lens parallel review for review-heavy plans (intuitiveml). Stacks with planning-lenses from `the_smart_ape` / KG doc.
- **E.7** — Bundle mid-flight invalidation signal (codex-multi-agent-swarms). Orchestrator detects shared-state mutation by in-flight sibling tasks and cancels + re-bundles.

**Deferred:**
- Supervisor context-health audit (openclaw-hermes). Useful at larger scale; our current fanout depth doesn't justify it.
- Adopting Operator/Architect as explicit org roles (intuitiveml). Their social context isn't ours.

---

## Part D — Trust gate (read before acting on any P0/P1 above)

Priority labels above are author judgment, **not validated evidence**.
Per-article Risk profile blocks report Evidence strength (structural /
anecdotal / pattern / measured). Recommendations built on single-operator
anecdotes should be treated as *directional*, not *load-bearing*, until
a second source confirms or an internal pilot validates.

Before turning any P0/P1 here into a plan:
1. Re-tier the underlying evidence. Demand a second source for anecdotes.
2. Check for converging sources (see Part B's cross-article themes).
3. Prefer recommendations with trivial `Reversibility` first.
4. Always cite Evidence + Reversibility when pitching a recommendation.

See `workflow-orchestration.md` Part D for the full trust-gate exposition.

---

*Document status: draft evaluation. No changes made to code, specs, or plans.*
