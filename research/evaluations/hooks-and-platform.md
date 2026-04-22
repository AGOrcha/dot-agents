# Articles Evaluation — Hooks & Platform Distribution

**Written:** 2026-04-21
**Scope:** How the articles speak to dot-agents' hook layer (PreToolUse, PostToolUse, SessionStart, SessionEnd, UserPromptSubmit) and our multi-platform distribution surface — generating and refreshing configs for Claude Code, Cursor, Codex, and GitHub Copilot from a single source in `.agents/`. Also covers scheduled jobs and cross-machine/shared-environment concerns.
**Siblings:** `workflow-orchestration.md`, `agent-execution.md`, `skills-rules-graduation.md`, `lessons-and-memory.md`, `../articles-evaluation-kg-and-adjacent.md`.

**Rubric:** Core / Pros / Cons / Risk profile (Failure mode / Evidence / Reversibility / Second-order) / Mapping.

---

## Part A — Per-article evaluation

### claude-code-hooks-automation — *Hooks as Guardrails*

**Core.** Use Claude Code's hook surface (PreToolUse, PostToolUse, UserPromptSubmit, Stop, SessionStart) to enforce invariants: block writes to forbidden paths, require tests after edits, auto-commit after session, validate prompts against a style guide. Hooks run deterministically — the agent can't skip them.

**Pros.**
- Hooks as guardrails is the correct mental model: the harness, not the prompt, enforces invariants. Matches the harness-engineering stance from the execution doc.
- PreToolUse hooks catch bad actions *before* they happen; cheaper than post-hoc correction.

**Cons.**
- Per-platform hook surfaces differ (Claude Code has rich hooks; Cursor / Codex / Copilot have thinner surfaces). Enforcement degrades on thinner-surfaced platforms.
- Hook misfires (false positives) teach agents to request overrides, which erodes the guardrail (same pattern as intuitiveml's deterministic-pipeline-needs-no-overrides).

**Risk profile.**
- *Failure mode:* false-positive hooks → agents stuck → human-added override → invariant no longer enforced. Silent drift.
- *Evidence:* structural (the mechanism is well-understood); anecdotal at scale.
- *Reversibility:* easy to add a hook; painful to remove a widely-deployed one without regressions.
- *Second-order:* hooks shift invariant ownership from rules (advisory) to code (enforced). This is the right direction when the invariant is clear.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our `hooks/` directory already contains PreToolUse, PostToolUse, SessionStart hooks. Articulate a design principle in `agents.md`: "if an invariant is enforceable by a hook, prefer a hook over a rule."
- **[GAP-ADOPT — P1]** — add hooks for invariants we currently express only as rules: `author: human` write-block hook, write-scope enforcement hook (already partially built), prose-as-title rename hook.
- **[GAP-ADOPT]** — per-platform hook capability matrix documented in `agents.md`: which invariants are enforceable on Claude Code vs Cursor vs Codex vs Copilot. Where unavailable, fall back to rule-only with a "best effort" note.

---

### arscontexta — *Four Hooks (orient, write-validate, auto-commit, session-capture)*

**Core.** Four concrete hooks cover the lifecycle: **orient** at session start (load project context), **write-validate** on every edit (schema/lint check), **auto-commit** at key milestones (never lose work), **session-capture** at session end (harvest lessons + observations). The article claims these four are sufficient for 95% of agent ergonomics.

**Pros.**
- Clean minimal set. Most teams over-hook; four is a useful ceiling.
- `session-capture` is a hook we underuse — we have fold-back observations, but they're opt-in rather than harvested at session end.

**Cons.**
- "orient" as a single hook hides enormous variance — different projects need wildly different context loaded at session start. A single hook makes this expensive.
- `auto-commit` without review creates noise in git history.

**Risk profile.**
- *Failure mode:* auto-commit at the wrong granularity → git history is either too noisy (every edit) or too sparse (whole-session commits). Visible but annoying.
- *Evidence:* anecdotal with specific implementation detail.
- *Reversibility:* easy (each hook is opt-in).
- *Second-order:* a standard minimal hook set makes projects portable — an agent coming from one dot-agents project to another can expect the same four hooks to be present.

**Mapping.**
- **[OVERLAP-SHARPEN]** — we have orient, write-validate (bits of), session-capture (fold-back). Harmonize on the arscontexta four-hook set as our "minimum ergonomics baseline."
- **[GAP-ADOPT — P1]** — harvest lessons/observations automatically at session-end via SessionEnd hook. Today fold-back is opt-in; automate the "write an observation if the session had corrections" trigger. Composes with the corrections-tracking idea from the lessons doc.
- **[WE-AHEAD]** — we have more hooks than the minimum four (orient, pre-tool guards, post-tool checks, session-start, session-end). We should *keep* them but document the four-hook baseline as the floor.

---

### claude-obsidian-memory-stack — *Memory-Refresh Hooks*

**Core.** SessionStart and UserPromptSubmit hooks that refresh the agent's memory view — inject recent notes, recent fold-back, recent plan state. The hook transforms the agent's "cold start" into a "warm start."

**Pros.**
- Directly addresses the "agent re-derives context every session" cost.
- Pairs cleanly with our auto-memory pattern (MEMORY.md is loaded into context at session start).

**Cons.**
- Injecting too much at SessionStart bloats the cold-start context; injecting too little defeats the purpose.
- UserPromptSubmit hook that rewrites the user's prompt can confuse the user about what the agent saw.

**Risk profile.**
- *Failure mode:* over-injection → context budget exhausted before work starts; under-injection → agent re-derives state anyway. Both are silent (the agent doesn't know what it should have seen).
- *Evidence:* anecdotal (Nyk's specific stack).
- *Reversibility:* easy (hook edits).
- *Second-order:* forces the project to make "what does the agent need to know at session start" an explicit design decision. Good.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our auto-memory system already injects `MEMORY.md` at session start. Add a budget: MEMORY.md index is truncated at 200 lines (already a rule) and per-memory-file content is only loaded on demand.
- **[GAP-ADOPT — P2]** — a structured "project state pulse" injection at SessionStart: current active plans, top 5 recent fold-back observations, current branch status. One small block, not a full dump.

---

### intuitiveml — *9 AM Self-Healing Loop (Scheduled Claude Job)*

**Core.** Every 9 AM UTC, a Claude Sonnet 4.6 job runs: queries CloudWatch for overnight errors, clusters them across 9 severity dimensions, auto-generates Linear tickets with sample logs / affected users / suggested investigation paths. Dedupes against open tickets; reopens regressions. When a fix deploys, the same job verifies resolution and auto-closes.

**Pros.**
- Demonstrates scheduled-agent patterns at production scale. Informs our own use of `CronCreate` and autonomous-loop skills.
- Close-the-loop auto-closing on verified fixes is a structural improvement over manual ticket hygiene.

**Cons.**
- Requires external systems (CloudWatch, Linear, Teams). We don't have an equivalent pipe to run this against.
- "9 errors clustered" is their specific rubric; each team has to build its own.

**Risk profile.**
- *Failure mode:* job misfires → spams Linear with duplicate tickets or misses a real regression. Scale-dependent; loud at low volume, silent at high volume if dedup fails.
- *Evidence:* anecdotal; single-team numbers.
- *Reversibility:* easy (pause the cron).
- *Second-order:* scheduled agents create a "sun never sets" expectation; team must staff triage review to prevent drift.

**Mapping.**
- **[GAP-ADOPT — P1]** — a scheduled auto-triage job over `.agents/active/fold-back/` observations. Clusters, scores by severity/frequency, proposes plan updates or new specs. Uses our existing `ScheduleWakeup` / `CronCreate` primitives.
- **[OVERLAP-SHARPEN]** — our `autonomous-loop-dynamic` sentinel gestures at this. Formalize one concrete scheduled job (fold-back triage) as the first production use.
- **[WE-AHEAD conceptually]** — our spec/plan/tasks discipline produces cleaner inputs than error logs; we don't need CloudWatch-scale noise reduction before scheduling helps.

---

### jhleath — *Share Environments, Not Data*

**Core.** For agents operating on large context, don't copy data between agents (the S3 upload pattern). Share the disk: `diskId` → any bash tool in any location sees the same filesystem. Handoff is constant-time regardless of size.

**Pros.**
- Correctly identifies that agent context is an environment, not a blob. For team-scale coordination, sharing a mounted disk is strictly better than syncing via git at every step.
- Maps onto our `isolation: "worktree"` Agent tool pattern as a tiny instance of the idea.

**Cons.**
- Requires infrastructure (Archil's Serverless Execution or similar) we don't own.
- For a single-user / single-machine stack, the pain this solves doesn't exist yet.

**Risk profile.**
- *Failure mode:* if we adopted a shared-disk pattern prematurely, agents on different machines could race on the same files without git's serialization. Concurrency bugs.
- *Evidence:* product marketing + informed argument; no independent benchmarks.
- *Reversibility:* painful if we build the pattern into skills; easy if we just name it as a future lane.
- *Second-order:* adopting shared-disk changes the unit of collaboration from "commit" to "worktree state," which has cascading implications for merge/review.

**Mapping.**
- **[GAP-ADOPT — P3 / deferred]** — name as a future architectural lane in `workflow-artifact-model.md`. "When dot-agents needs team-scale agent handoff, consider shared-environment over shared-data." Not a plan today.
- **[OVERLAP-SHARPEN]** — our `isolation: "worktree"` Agent-tool pattern is the local instance. Document it more prominently as the "share environment, not data" primitive at single-user scale.

---

## Part B — Synthesis against our stack

### Hook + platform patterns to internalize

1. **Hooks enforce; rules advise.** Our own lesson, reinforced by claude-code-hooks-automation and arscontexta. When an invariant is clearly enforceable, it belongs in a hook; rules are backup when the platform can't.
2. **Minimum four-hook baseline.** arscontexta's orient / write-validate / auto-commit / session-capture is a usable floor; we already meet it with variations. Naming it as a baseline makes portability explicit.
3. **SessionStart is cheap context; don't overload it.** claude-obsidian-memory-stack. Our MEMORY.md system is the right shape.
4. **Scheduled agents work when the input is structured.** intuitiveml. Our fold-back observations are structured-enough to run a triage job.
5. **Share environment, not data — but only at team scale.** jhleath. Named future lane, not current work.

### What we do well

- **Single-source platform distribution** (`.agentsrc.json` + `refresh` + per-platform generators). intuitiveml calls out "monorepo so AI can see everything"; we apply the same principle across agent platforms.
- **Hook distribution across Claude Code / Cursor / Codex / Copilot** with graceful degradation where platforms have thinner hook surfaces.
- **SessionStart memory injection via `MEMORY.md`.** Standard and low-budget.
- **`CronCreate` / `ScheduleWakeup` as scheduled-agent primitives.** Named but underused.

### What we miss (priority-ordered)

**P0 — rule-level / one-paragraph:**
- Design principle: "prefer hooks over rules where the invariant is hook-enforceable" in `agents.md`.
- Per-platform hook capability matrix documented in `agents.md`.

**P1 — small new primitives:**
- Add hooks for rule-only invariants: `author: human` write-block; prose-as-title enforcement on KGNote / lesson writes (depends on C.1/C.2 from KG doc).
- SessionEnd hook that auto-harvests fold-back observations when the session had corrections (arscontexta).
- First production scheduled job: fold-back triage (intuitiveml-style cadence; our primitives). Composes with W.3 (open-questions) and the reweave skill.

**P2 — structural:**
- Structured "project state pulse" block at SessionStart: active plans + top-N fold-back + branch/worktree state (claude-obsidian-memory-stack).

**P3 — named futures:**
- Shared-environment handoff (jhleath). Only when dot-agents becomes team-scale.

### What we do better than them

- **vs. claude-code-hooks-automation:** our hooks are cross-platform; theirs are Claude-Code-only. Our `.agentsrc.json` + `refresh` layer generates platform-appropriate hook configs automatically.
- **vs. arscontexta:** our hook surface is broader than their four; we should *baseline* at their four and allow richer extensions per project.
- **vs. intuitiveml:** our fold-back + spec/plan pipeline produces cleaner inputs than error logs; scheduled agents work earlier in our stack without 100M-row tables.
- **vs. jhleath:** our worktree-isolation-per-agent handles the shared-state concern at single-user scale without requiring custom disk infrastructure.

---

## Part C — Recommended next steps (hooks & platform layer)

**Immediate:**
- **H.1** — Add "hooks enforce, rules advise" principle + per-platform hook capability matrix to `agents.md` (claude-code-hooks-automation + arscontexta).
- **H.2** — Document the arscontexta four-hook baseline (orient / write-validate / auto-commit / session-capture) as the minimum ergonomics floor for every dot-agents-managed project.

**Short-term (one plan each):**
- **H.3** — Add PreToolUse hook for `author: human` write-blocking (depends on KG doc's C.1 adopting the field).
- **H.4** — SessionEnd auto-harvest of fold-back observations when the session had corrections. Stacks with the lessons/memory doc's recommendations.
- **H.5** — First production scheduled job: fold-back triage. Clusters observations, scores by frequency/severity, proposes plan updates. Uses `CronCreate`. (intuitiveml-inspired; our primitives.)

**Medium-term:**
- **H.6** — "Project state pulse" SessionStart block: active plans + top fold-back + worktree state (claude-obsidian-memory-stack). One small block, budget-aware.

**Explicitly deferred:**
- Shared-disk / diskId pattern (jhleath). Named as P3 future lane in `workflow-artifact-model.md`; not a plan today.
- Blocker-level `--no-verify` equivalent for hooks. Our global rule already forbids this without explicit user request.

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
