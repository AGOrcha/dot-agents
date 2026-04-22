# Articles Evaluation — Skills, Rules & Graduation

**Written:** 2026-04-21
**Scope:** How the articles speak to dot-agents' agent-authorship surfaces: skills (`.agents/skills/<name>/` → `dot-agents skills promote` → shared), rules (`.agents/rules/<ns>/` via proposal/review loop), prompts (shared prompt library), and the graduation pipeline that moves experimental artifacts from project-local to globally-shared. Also covers progressive disclosure design for skills/tools.
**Siblings:** `workflow-orchestration.md`, `agent-execution.md`, `hooks-and-platform.md`, `lessons-and-memory.md`, `../articles-evaluation-kg-and-adjacent.md`.

**Rubric:** Core / Pros / Cons / Risk profile (Failure mode / Evidence / Reversibility / Second-order) / Mapping.

---

## Part A — Per-article evaluation

### thealexker — *Progressive Disclosure*

**Core.** Skills/tools should reveal capability progressively. A skill's initial surface is minimal — a short description, a few key args. Deeper capability is revealed on demand (via help, examples, or escalation). Overloading the agent's context with every skill's full spec up-front is the anti-pattern.

**Pros.**
- Directly matches our `Skill` tool design — the tool surface is a name + args, not a full description dump. Claude decides to invoke a skill based on context; the skill's content loads only when invoked.
- Validates our "skills are markdown, loaded on-demand" pattern.

**Cons.**
- Progressive disclosure requires discoverability: the agent has to know a skill exists to invoke it. Minimal surfaces work only when a "list available skills" mechanism exists.
- Can hide capability: skills that aren't invoked on their index entry will be forgotten.

**Risk profile.**
- *Failure mode:* skill that exists but isn't in the index → agent never discovers it → work duplicated or done badly. Silent.
- *Evidence:* structural; thealexker articulates the principle clearly.
- *Reversibility:* easy (promote/demote skills in the index).
- *Second-order:* encourages terse skill descriptions that force callers to read the actual skill when invoked. Good for quality; bad if descriptions are too opaque to decide whether to invoke.

**Mapping.**
- **[WE-AHEAD]** — our skill system already does progressive disclosure. The `Skill` tool lists available skills in system reminders; full skill content loads only on invocation.
- **[OVERLAP-SHARPEN]** — the *descriptions* in our skill index (`SKILLS.md` or equivalent) are the decision surface. Audit them for "do I know when to invoke this?" Today some descriptions are too abstract. Add an "invoke when:" one-line field to each skill's frontmatter.
- **[GAP-ADOPT — P1]** — a `skill-describe` lint: every skill must have a one-line "invoke when:" description that names a concrete trigger. Catches skills whose discoverability relies on hope.

---

### second-brain-needs-two-authors (kevin) — *Author Field as Graduation Gate*

**Core.** Every note has `author: human | agent` frontmatter. Humans author durable knowledge; agents draft candidates. A PreToolUse hook enforces the distinction: agents can't silently edit human-authored files. Graduation happens when a human reviews an agent-authored draft and flips the author field.

**Pros.**
- Generalizes beyond notes: the same pattern applies to lessons, rules, skills, and plans. Every agent-authored artifact has a drafted → reviewed → graduated lifecycle.
- Gives graduation a single, enforceable primitive. Today our proposal/review loop achieves this for global rules but not for lessons, skills, or plans.

**Cons.**
- Every new artifact-type needs the hook and the policy. Scales with care.
- Author field alone doesn't capture authorship history — only the current state. If an artifact was human-authored then agent-edited, we lose the chain.

**Risk profile.**
- *Failure mode:* missing hook on a platform → agent silently edits human artifact → graduation boundary erodes. Loud the first time a human notices their artifact changed, then silent after.
- *Evidence:* anecdotal (kevin's implementation) + structural reasoning.
- *Reversibility:* easy to add the field; easy to remove; medium-hard to backfill across existing artifacts.
- *Second-order:* forces the team to answer "who owns this artifact" for every artifact-type. Cultural clarity gain.

**Mapping.**
- **[GAP-ADOPT — P0]** — `author: human | agent` on KGNote, lessons, plans, skills, rules. Write a PreToolUse hook that blocks `Write`/`Edit` on `author: human` files without explicit override. Composes with H.3 in the hooks doc.
- **[GAP-ADOPT — P1]** — generalize the proposal/review loop beyond global rules: skills, plans, lessons can all graduate from agent-authored drafts. Today only `~/.agents/proposals/` supports this; project-local proposals exist as markdown with no formal graduation.
- **[OVERLAP-SHARPEN]** — `dot-agents review approve` already does graduation for global rules/skills. Extend its scope to cover the new artifact types.

---

### karpathy-second-brain-pattern — *Wiki / Prose-as-Title*

**Core.** Use the prose of the link as the title of the target. Instead of `[[auth-mw-rewrite]]`, write `[[why auth middleware is being rewritten]]`. Titles are searchable, summarizable, and self-documenting.

**Pros.**
- Makes artifact indexes human-readable without a separate "description" field.
- Matches how Obsidian-native users write.

**Cons.**
- Long titles are awkward as filesystem paths. Some tooling assumes short filenames.
- Renaming is a one-shot migration; harder to enforce on older artifacts.

**Risk profile.**
- *Failure mode:* mixed conventions (some short-id titles, some prose titles) → search/lint gets confused. Loud (breaks grep).
- *Evidence:* anecdotal; Karpathy's personal practice.
- *Reversibility:* easy to rename forward; hard to roll back without losing link integrity.
- *Second-order:* prose titles become small summaries, which replaces some need for description fields — reduces frontmatter bloat.

**Mapping.**
- **[GAP-ADOPT — P0]** — prose-as-title convention for lessons (already P0 in the KG doc). Also applies to skill names: today `article-extract` is good but `workflow-conflict-detection-debug` (a specific lesson) is the right shape.
- **[OVERLAP-SHARPEN]** — our lesson filename convention is `.agents/lessons/<name>/LESSON.md`; the `<name>` slot should carry prose. Update the lessons-convention rule.

---

### arscontexta — *Three-Space Invariant (self / notes / ops)*

**Core.** Every agentic knowledge system has three spaces: **self** (identity: rules, prompts, skills — what the agent *is*), **notes** (knowledge: what the agent *knows*), **ops** (state: what the agent is *currently doing*). Mixing them across folders causes load-path drift.

**Pros.**
- Clean articulation of a split we have partly but not rigorously.
- The distinction helps answer "where does artifact X belong?" — if it's load-bearing for agent identity (rules, skills, prompts), it's self; if it's a fact or observation, it's notes; if it's transient work, it's ops.

**Cons.**
- Rigid application is expensive — some artifacts span two (e.g., a LESSON cites past observation AND is now a durable rule).
- Naming conventions adapted per project in arscontexta's description (`notes/` → `reflections/` → `claims/`) is a tooling nightmare. We should not adopt that part.

**Risk profile.**
- *Failure mode:* artifacts in wrong space → hook/lint/index misfires → agent can't find things. Usually loud.
- *Evidence:* anecdotal + structural argument.
- *Reversibility:* easy to restructure once; painful to do repeatedly.
- *Second-order:* explicit self/notes/ops boundary makes "whose ownership" decisions cheaper (see two-authors pattern above).

**Mapping.**
- **[OVERLAP-SHARPEN]** — our `.agents/` tree is close to three-space:
  - **self:** `rules/`, `skills/`, `prompts/`, `hooks/`
  - **notes:** `lessons/`, `history/`, warm KG store
  - **ops:** `active/`, `workflow/`
  - Plans live in `workflow/plans/` (ops) but impl-results migrate to `history/` (notes) — that's a deliberate transition, not a leak.
- **[GAP-ADOPT]** — articulate the three-space mapping in `workflow-artifact-model.md`. Helps new contributors and makes future artifact-type decisions principled. Do NOT adopt per-project renaming.

---

### claude-obsidian-memory-stack (Nyk) — *Self-Improving Skills Graph*

**Core.** Skills and notes cross-reference each other via wikilinks; the graph of skills ↔ notes improves over time as both layers get rewritten in response to corrections. The stack includes an explicit "rewrite older notes when a new one contradicts them" step.

**Pros.**
- Validates cross-referencing between skills and lessons/notes as a primitive. Our stack treats them as separate trees.
- The "rewrite-older-on-contradiction" step is Reweave at the skill/lesson level — same pattern, different substrate.

**Cons.**
- Nyk's stack is personal-scale; scaling the rewrite step to team-scale is an unsolved problem (contradictions between two humans' authored skills).
- Wikilinks between skills and lessons add load for hook enforcement.

**Risk profile.**
- *Failure mode:* uncorrected stale skill content → agent follows out-of-date procedure. Loud (work output is wrong) or silent (work is suboptimal).
- *Evidence:* anecdotal; single operator.
- *Reversibility:* easy to add cross-references; easy to remove.
- *Second-order:* linked skills-and-lessons form a feedback loop — lessons point at skills they updated, skills point at lessons that taught them. Compounds over time. Matches arscontexta's cognitive_grounding pattern at the skill layer.

**Mapping.**
- **[GAP-ADOPT — P1]** — explicit `lessons: [...]` and `skills: [...]` cross-reference fields in lesson and skill frontmatter. A lesson that updated a skill cites the skill; a skill that was born from a lesson cites the lesson.
- **[GAP-ADOPT — P2]** — "rewrite on contradiction" pass in the lesson/skill pipeline. When a new lesson contradicts an older one, flag for review (not auto-rewrite). Composes with the contradiction protocol from the_smart_ape.

---

### intuitiveml — *Architect Designs the SOPs*

**Core.** "The Architect: one or two people. They design the standard operating procedures that teach AI how to work. They build the testing infrastructure, the integration systems, the triage systems. They decide architecture and system boundaries. They define what 'good' looks like for the agents." This role requires "deep critical thinking. You criticize AI. You don't follow it."

**Pros.**
- Names the role behind our skill/rule/prompt authoring work. The Architect is the author of the harness.
- "Criticize AI, don't follow it" is the correct posture for the Architect role and matches how proposals should be reviewed.

**Cons.**
- CREAO's claim that one Architect can serve 100 Operators is aspirational. In practice the Architect is a bottleneck; staffing it right is hard.
- Operator role in CREAO is "AI assigns tickets, human investigates" which is thinner than our loop-worker / reviewer split.

**Risk profile.**
- *Failure mode:* Architect undersupplied → skills/rules go stale → agents drift. Slow but inevitable degradation.
- *Evidence:* anecdotal (CREAO's org structure).
- *Reversibility:* social, not technical. Hard.
- *Second-order:* naming the Architect role legitimizes investment in rule/skill/prompt authorship as first-class work, not "overhead."

**Mapping.**
- **[OVERLAP-SHARPEN]** — name "Architect" role explicitly in `agents.md`: the person(s) who author rules, skills, prompts, and the proposal/review loop. Our proposal/review mechanism is their workflow primitive.
- **[WE-AHEAD]** — our Operator equivalent (loop-worker + human reviewer) is stronger than CREAO's "humans as Operators of AI-assigned tickets."

---

## Part B — Synthesis against our stack

### Patterns the skill/rule/graduation layer should internalize

1. **Progressive disclosure is load-bearing and we already do it.** thealexker. Name it explicitly; audit skill descriptions for "invoke when" legibility.
2. **Author field is the graduation primitive, generalizable across artifact types.** second-brain-two-authors. Today it's implicit in the proposal/review loop for rules; should be explicit and extended to lessons, plans, skills, notes.
3. **Three-space invariant maps our tree with one exception (plans).** arscontexta. Worth articulating in `workflow-artifact-model.md`.
4. **Skills and lessons should cross-reference.** claude-obsidian-memory-stack. Today they're parallel trees; linking closes a feedback loop.
5. **Prose-as-title applies to lessons and skills, not just notes.** karpathy. Extends the KG doc's P0 recommendation.
6. **Architect is a first-class role.** intuitiveml. Legitimizes investment in the authorship side of the stack.

### What we do well

- **Multi-tier artifact system** (rules / skills / prompts / lessons / hooks) distributed centrally and loaded per-platform.
- **Proposal/review loop for global rules.** Cleanest graduation primitive in this set.
- **Progressive disclosure via `Skill` tool.** Already correct.
- **Project-local vs global distinction** for proposals (via `proposal-routing.md` rule). Prevents over-globalization.

### What we miss (priority-ordered)

**P0:**
- `author: human | agent` field on lessons, plans, skills (kevin). Hook enforcement on write. Same item as the lessons doc and KG doc — coordinate once, land everywhere.
- Three-space articulation in `workflow-artifact-model.md` (arscontexta).
- Prose-as-title convention extended to skills and lessons (karpathy + KG doc).

**P1:**
- Generalize proposal/review loop to cover lessons / plans / skills as graduation targets — not just global rules. Today only `dot-agents review approve` exists, and it targets global rules. Extend to project-local artifacts when CLI support lands (noted in `proposal-routing.md` as a future extension).
- Cross-reference fields (`skills:`, `lessons:` in frontmatter) between skills and lessons (claude-obsidian-memory-stack).
- `skill-describe` lint: every skill must have "invoke when:" one-liner (thealexker).

**P2:**
- "Rewrite on contradiction" pass for lessons and skills (claude-obsidian-memory-stack). Flag, don't auto-rewrite. Composes with contradiction protocol skill from KG doc's §C.5.

### What we do better than them

- **vs. thealexker (progressive disclosure):** already matched by our `Skill` tool. We ship it; they describe it.
- **vs. second-brain-two-authors:** our proposal/review loop is a formal graduation mechanism that their implicit author-field flip doesn't reach.
- **vs. arscontexta (three-space):** our tiered lifecycle (spec → plan → tasks → history) is stricter than their three-space; we keep both.
- **vs. claude-obsidian-memory-stack (Nyk):** our skill/lesson trees are machine-queryable (sqlite warm store for KG, structured metadata); his are pure markdown.
- **vs. karpathy:** our slash-command / `Skill` tool invocation is more structured than Obsidian wikilinks for agent use.
- **vs. intuitiveml (Architect/Operator):** our proposal/review loop *is* the Architect's workflow primitive; they don't have one.

---

## Part C — Recommended next steps (skills / rules / graduation layer)

**Immediate:**
- **S.1** — Three-space paragraph in `workflow-artifact-model.md`: name self / notes / ops and map our tree (arscontexta).
- **S.2** — "Architect role" paragraph in `agents.md`: the author of rules/skills/prompts/proposals (intuitiveml; complements harness-engineering stance from execution doc).
- **S.3** — `skill-describe` lint: CI or pre-commit check that every skill has an "invoke when:" one-liner in frontmatter (thealexker).

**Short-term (one plan each):**
- **S.4** — `author: human | agent` field + write-block hook, rolled across lessons + plans + skills + KG notes + rules in one plan. Coordinated with KG doc's C.1 and lessons doc's equivalent (kevin). Single graduation pipeline for all artifact types.
- **S.5** — Cross-reference fields (`skills: [...]` on lessons, `lessons: [...]` on skills) + lint that broken cross-refs are flagged (claude-obsidian-memory-stack).
- **S.6** — Prose-as-title rename pass over `.agents/lessons/` and `.agents/skills/` (karpathy + KG doc).

**Medium-term:**
- **S.7** — Generalize project-local proposals: `dot-agents review approve` covers project-local artifacts (skills, lessons, plans), not just global rules. Depends on CLI extension referenced in `proposal-routing.md`.
- **S.8** — "Rewrite on contradiction" skill/hook: when a new lesson or skill contradicts an older one, flag both for review. Composes with contradiction protocol from the_smart_ape (KG doc C.5).

**Explicitly deferred:**
- Per-project skill/artifact renaming (arscontexta's "notes/ → reflections/ → claims/"). Bad idea for cross-project tooling.
- Wholesale adoption of Operator role as "AI assigns tickets" (intuitiveml). Our loop-worker / reviewer split is better.

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
