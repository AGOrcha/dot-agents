# Articles Evaluation — Lessons, Memory & Self-Improvement

**Written:** 2026-04-21
**Scope:** How the articles speak to dot-agents' learning surfaces — `.agents/lessons/<name>/LESSON.md` (mistake-driven durable rules), the proposal/review loop for graduating lessons into rules, and Claude Code's **auto-memory** system at `~/.claude/projects/<project-hash>/memory/` (the per-user MEMORY.md index + typed memory files for user/feedback/project/reference). Also covers trust tiers, corrections-driven learning loops, and contradiction handling between memories.
**Siblings:** `workflow-orchestration.md`, `agent-execution.md`, `hooks-and-platform.md`, `skills-rules-graduation.md`, `workflow-spec-plan-inventory.md`, `../articles-evaluation-kg-and-adjacent.md`.

**Rubric:** Core / Pros / Cons / Risk profile (Failure mode / Evidence / Reversibility / Second-order) / Mapping.

**Note on "memory" usage in this doc.** Two related systems are in scope and shouldn't be conflated:
1. **Auto-memory** (Claude Code feature): typed markdown files under `~/.claude/projects/<hash>/memory/`; instance-level, persists across conversations for this repo+user.
2. **Project lessons** (dot-agents feature): `.agents/lessons/<name>/LESSON.md`; repo-level, committed to git, graduates to global rules via proposal/review.

---

## Part A — Per-article evaluation

### second-brain-needs-two-authors (kevin) — *Author Boundary as Memory Primitive*

**Core (memory-focus).** Memory entries carry an authorship mark — `author: human | agent`. Human-authored memories are durable and trusted. Agent-authored memories are drafts: useful, but provisional until a human reviews and "graduates" them. The author field is the smallest primitive that makes this distinction machine-enforceable.

**Pros.**
- Our auto-memory system today has no author marking. Every entry looks the same whether I wrote it or Claude wrote it. For lessons this matters even more — a lesson drafted by an agent after a correction is not yet canon.
- One field. One hook. Immediate leverage.

**Cons.**
- Auto-memory is *supposed* to be Claude-authored — that's the feature. Applying author-marking to auto-memory is about distinguishing "Claude wrote this because user taught me X" (stable) vs "Claude wrote this because it inferred X" (provisional).
- For committed artifacts (lessons), the feature is stronger; for auto-memory it's subtler.

**Risk profile.**
- *Failure mode:* without author marking, an agent-inferred memory becomes treated as gospel on the next session. Silent; compounds over time.
- *Evidence:* anecdotal (kevin's personal practice) + structural argument.
- *Reversibility:* easy — field is additive.
- *Second-order:* forces the team to draw the human↔agent authorship boundary explicitly for every memory/lesson surface. Cultural clarity gain, same as noted in the skills/graduation doc.

**Mapping.**
- **[GAP-ADOPT — P0]** — `author:` field on:
  - `.agents/lessons/<name>/LESSON.md` (human vs agent draft)
  - auto-memory files (distinguish user-taught from agent-inferred)
  - `corrective_source:` sub-field for lessons — link to the user message / PR / incident that drove the lesson
- **[OVERLAP-SHARPEN]** — our `LESSON.md` convention already implies human review, but it's not enforced. A PreToolUse hook that blocks agent writes to `author: human` lessons locks the invariant.

---

### the_smart_ape — *Source-Evaluation Trust Tiers*

**Core (memory-focus).** Five-tier trust system for information sources: **Tier 1** primary/verified (original documents, personal observation), **Tier 2** reliable-secondary (official docs, peer-reviewed), **Tier 3** informal-secondary (blog posts, tweets), **Tier 4** claim-without-evidence, **Tier 5** known-unreliable. Memory entries and claims carry a tier. Search and synthesis weight tiers differently.

**Pros.**
- Makes stale/contradicted/weakly-sourced memories explicit. Today our lessons have no confidence or tier field.
- Composes with the corrections-tracking idea: a lesson born from an actual user correction (Tier 1-ish) is stronger than an agent-inferred one.
- Pairs with the contradiction protocol — when two memories contradict, the higher-tier wins unless the lower-tier cites newer evidence.

**Cons.**
- Tier assignment is itself a judgment. Requires consistent authorship discipline.
- Five tiers is a lot; three is probably enough for our scale (user-taught / user-confirmed-agent-draft / agent-inferred).

**Risk profile.**
- *Failure mode:* without tiers, a weakly-sourced memory has the same weight as a user-taught one; agents follow both identically. Silent; bad decisions accumulate.
- *Evidence:* pattern-based (research-practice-derived).
- *Reversibility:* easy (additive field).
- *Second-order:* tier assignment forces authors (human or agent) to think about "where did I learn this?" — that's the same discipline as `derived_from` cites in the scoped-KG spec, applied to memory.

**Mapping.**
- **[GAP-ADOPT — P1]** — 3-tier trust field on lessons and auto-memory:
  - `tier: 1 (user-taught)` — the user corrected me, or I transcribed their direct instruction
  - `tier: 2 (agent-drafted-user-confirmed)` — agent drafted from observation; human has since confirmed
  - `tier: 3 (agent-inferred)` — agent's own inference; unconfirmed
- **[GAP-ADOPT — P1]** — pair tiers with the author field (S.4 from skills/graduation doc): a `tier: 1` memory must be `author: human` or cite a specific user message.

---

### arscontexta — *Cognitive Grounding (249 Linked Claims)*

**Core (memory-focus).** Every claim in the system links to the evidence that grounds it. arscontexta reports 249 `cognitive_grounding` links across their research corpus. A claim without grounding is treated as unsupported.

**Pros.**
- Concrete implementation of "cite your sources" at agent scale. Our lessons have rationale but rarely cite the specific incident, PR, or commit.
- Enables an automated "stale citation" check: when a cited source changes or is deleted, the claim is flagged.

**Cons.**
- Enforcing grounding on every claim is high-friction; agents skip when they can't locate evidence easily.
- "Evidence" is overloaded — could be a PR link, a tweet, a commit, or a peer file. No canonical shape.

**Risk profile.**
- *Failure mode:* ungrounded lessons → can't re-derive why the rule exists → lesson drift when the reason is forgotten. Silent; the lesson looks fine until someone challenges it.
- *Evidence:* structural + anecdotal (arscontexta's 249-link corpus).
- *Reversibility:* easy to add `cites:` field; hard to backfill across existing lessons.
- *Second-order:* forces every author to name the grounding incident, which is good discipline. Also enables "why does this rule exist" audits.

**Mapping.**
- **[GAP-ADOPT — P1]** — `cites:` array on lessons and auto-memory: links to specific messages / PRs / commits / notes that grounded the claim. Required field when `author: human` and `tier: 1`; optional but encouraged otherwise.
- **[OVERLAP-SHARPEN]** — our `LESSON.md` structure has a "why" section but no structured citation. Rename to `rationale:` + `cites:` in frontmatter; keep the prose too.
- **[WE-AHEAD]** — scoped-KG spec's `derived_from` field is the same pattern at the KG layer. Landing it at the lesson layer is consistent; same design, different tree.

---

### claude-obsidian-memory-stack (Nyk) — *3-Layer Memory Stack*

**Core.** Three memory layers with different decay/refresh cadences:
1. **Session memory** — loaded at SessionStart, discarded at end.
2. **Warm memory** — recent work, refreshed on relevance.
3. **Cold memory** — durable, refreshed rarely but via scheduled reweave.
Each layer has its own hygiene pass (session cleanup, warm reindex, cold consolidation).

**Pros.**
- Clean layered cache model. Our auto-memory is a single flat directory; Nyk's stack suggests structure.
- The scheduled cold-memory reweave is the same pattern as the dream cycle from the KG doc. Validates it from a second source.

**Cons.**
- Three layers is opinionated; our scale may not need the distinction.
- "Refresh on relevance" is a vague trigger — Nyk's stack uses manual notes; we'd need an automated signal.

**Risk profile.**
- *Failure mode:* without layering, all memory is at the same priority and SessionStart injection can bloat. Loud (token budget exhausted) or silent (agent ignores relevant entries).
- *Evidence:* anecdotal; one operator.
- *Reversibility:* moderate (restructures the memory directory).
- *Second-order:* layered memory forces an explicit refresh policy per layer, which makes it easier to reason about "what does the agent know at T+N days."

**Mapping.**
- **[OVERLAP-SHARPEN]** — our auto-memory at `~/.claude/projects/<hash>/memory/` already has `MEMORY.md` as the "index" (session-level pulse) and per-memory files (warm). Formalize:
  - **`MEMORY.md`** = session-layer index (loaded every session, truncated at 200 lines)
  - **`memory/*.md`** = warm layer (loaded on demand when MEMORY.md cites them)
  - **archived memories** = cold layer (moved out of directory after N months of non-access; reweave brings them back when contradicted or re-cited)
- **[GAP-ADOPT — P2]** — nightly or weekly cold-memory consolidation: dedup, relationship inference between memory files, staleness audit against current git state. Same pattern as the KG dream cycle; reuses the same infrastructure.

---

### claude-obsidian-ai-employee — *Employee Learns From Corrections*

**Core (memory-focus).** An AI employee that forgets yesterday's correction is a liability. The stack must have an explicit corrections-capture pipeline: when the user corrects the agent, the correction is recorded as a lesson/memory, not just as a one-off prompt adjustment.

**Pros.**
- Matches our `LESSON.md` mechanism directly. Validates the pattern from a second source.
- The framing ("liability if it forgets") is a good argument for enforcing the corrections→lesson pipeline rather than leaving it advisory.

**Cons.**
- Auto-capture risks converting every user correction into a lesson, bloating the lessons directory with noise.
- Some corrections are context-specific (today's one-off), not rules. Auto-capture needs a filter.

**Risk profile.**
- *Failure mode:* missed corrections → same mistake repeated → user frustration spikes. Loud for the user, silent for the agent unless explicitly instrumented.
- *Evidence:* anecdotal; matches our own global rule ("after ANY correction, update LESSON.md").
- *Reversibility:* easy (hook-based).
- *Second-order:* auto-capture changes the user's behavior — they phrase corrections more carefully knowing they'll be durable. Generally good.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our global rule "after ANY correction, update LESSON.md" already exists. Enforce it via a SessionEnd hook: if the session contained a detected correction (user pushback pattern), flag for lesson creation. Composes with H.4 in the hooks doc.
- **[GAP-ADOPT — P1]** — "correction detector" pattern in SessionEnd: detect phrases like "no, don't", "stop doing", "instead", "you're wrong" → emit a fold-back observation suggesting a LESSON. Filter out one-off corrections (no durable pattern) vs durable ones.

---

### intuitiveml — *Don't Fire the Engineer, Improve the Process*

**Core (memory-focus).** "We don't fire an engineer because they introduced a production bug. We improve the review process. We strengthen testing. We add guardrails. The same applies to AI." The article treats every bug/failure as a signal for process improvement, not individual blame — and applies the same stance to AI errors.

**Pros.**
- The philosophical match for our "every correction produces a lesson" rule. Reinforces that mistakes are raw material for durable improvement, not noise to suppress.
- Applying it to AI means: if the agent makes a mistake, the fix is a rule/skill/hook update, not just a prompt retry.

**Cons.**
- The stance is easy to state, hard to practice. Requires discipline.
- Can create an over-reaction spiral: every one-off becomes a new rule, rule set bloats, system ossifies.

**Risk profile.**
- *Failure mode:* over-application → every minor issue spawns a rule; rule set grows without shrinking; agent context bloats. Silent but compounding.
- *Evidence:* anecdotal; philosophical.
- *Reversibility:* N/A (stance, not code); the over-application failure is easy to fix by reviewing rules periodically.
- *Second-order:* a team that internalizes this stance invests in process, not heroes. Healthy long-term.

**Mapping.**
- **[OVERLAP-SHARPEN]** — our stance is already this. Articulate it alongside harness-engineering in `agents.md`: every AI error is a harness deficiency; every user correction is a candidate rule.
- **[GAP-ADOPT — P2]** — periodic rule/lesson pruning pass. Every N months, review all lessons for still-applicable vs superseded. Matches the "rewrite on contradiction" pattern from the skills doc + the dream cycle from the KG doc.

---

## Part B — Synthesis against our stack

### Patterns the lessons/memory layer should internalize

1. **Author + tier + cites are the three minimal fields for durable learning.** kevin's author + the_smart_ape's tier + arscontexta's cognitive_grounding combine into one coherent primitive: "where did this memory come from, and how much should I trust it."
2. **Corrections should be captured, not just observed.** claude-obsidian-ai-employee + our own global rule. The rule exists; enforcement via SessionEnd hook is the missing step.
3. **Memory layers with explicit decay cadences.** claude-obsidian-memory-stack (Nyk) + the dream cycle from the KG doc. The cold layer needs nightly/weekly consolidation.
4. **Process over blame extends to AI.** intuitiveml. Reinforces our existing stance; pairs with harness engineering.
5. **Rule pruning is as important as rule creation.** Implicit in all three sources (kevin, Nyk, intuitiveml). Today our lessons grow; they don't shrink.

### What we do well

- **`LESSON.md` per-lesson directory + `lessons.md` index.** Structured at-rest; human-editable.
- **Global rule: "after ANY correction, update LESSON.md."** Already in CLAUDE.md. Enforcement is the missing half.
- **Auto-memory typed (user/feedback/project/reference).** Stronger taxonomy than Nyk's flat stack.
- **MEMORY.md as a truncated-at-200-lines index.** Correct budget-aware design.
- **Proposal/review loop for lesson→rule graduation.** Exists, used.

### What we miss (priority-ordered)

**P0:**
- `author: human | agent` + `corrective_source:` on lessons and auto-memory files (kevin). Same as S.4 in skills/graduation doc — one change, multi-surface.

**P1:**
- `tier: 1|2|3` trust field on lessons and memories (the_smart_ape).
- `cites: [...]` structured field on lessons (arscontexta). Required for `tier: 1`.
- SessionEnd correction-detector hook: detect pushback patterns → emit fold-back observation → suggest lesson creation (claude-obsidian-ai-employee). Composes with H.4.

**P2:**
- Cold-memory consolidation pass: nightly/weekly job over auto-memory + lessons; dedup, staleness audit, contradiction flag, archive cold entries (Nyk + KG doc's dream cycle + arscontexta's reweave).
- Periodic lesson pruning pass: every N months, review lessons for still-applicable vs superseded (intuitiveml process stance). Emit a review-due fold-back.

**P3:**
- Memory-layering formalization (session / warm / cold) as explicit directory structure. Only if P2 pass reveals the flat directory is the wrong shape.

### What we do better than them

- **vs. kevin (two-authors):** our proposal/review loop is a formal graduation mechanism that his implicit field-flip doesn't capture. We have the field *and* the graduation primitive.
- **vs. the_smart_ape (trust tiers):** we have no tiers today; that's a gap. But our sqlite warm store + citations-via-links (for KG notes) is a stronger substrate once tiers are added.
- **vs. arscontexta (cognitive grounding):** we lack the grounding field; their 249-link practice is ahead. Gap to close.
- **vs. claude-obsidian-memory-stack (Nyk):** our auto-memory is typed (user/feedback/project/reference); his is flat.
- **vs. claude-obsidian-ai-employee:** our global rule "after ANY correction" exists and is explicit; his is implicit.
- **vs. intuitiveml:** our lesson + proposal/review pipeline *is* the process-over-blame mechanism at the rule level.

---

## Part C — Recommended next steps (lessons & memory layer)

**Immediate:**
- **L.1** — Name "process over blame" stance in `agents.md` alongside harness engineering and Camp 2. One paragraph (intuitiveml).

**Short-term (one plan each):**
- **L.2** — `author: human | agent` + `corrective_source:` + `cites: [...]` + `tier: 1|2|3` fields on lessons and auto-memory files. Single schema addition covering the three converging primitives (kevin + the_smart_ape + arscontexta). Coordinate with S.4 (skills/graduation doc) and C.1 (KG doc) as one multi-tree rollout.
- **L.3** — SessionEnd correction-detector hook: pushback-pattern detection → fold-back observation → lesson-creation suggestion (claude-obsidian-ai-employee). Composes with H.4 from hooks doc.

**Medium-term:**
- **L.4** — Cold-memory / cold-lesson consolidation job: dedup, staleness audit against git state, contradiction flag, archive entries with no access in N months. Pairs with the KG dream cycle and `kg lint`; single cron job covers three surfaces (Nyk + arscontexta + KG doc C.4).
- **L.5** — Periodic lesson pruning review: every quarter, `workflow lessons review` lists lessons sorted by age × access-recency × contradiction count. Emits a fold-back observation for each candidate. (intuitiveml process stance formalized.)

**Deferred:**
- Formal layer separation (session / warm / cold) as directory structure. Only if L.4 reveals the flat directory is genuinely insufficient.
- Auto-rewrite-on-contradiction. Too risky without a human review step; the flag-for-review pattern from skills doc S.8 is safer.

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

This doc's domain is memory and trust — applying these rules to our own
recommendations is the minimum viable version of the_smart_ape's tier
system. The irony is intentional.

See `workflow-orchestration.md` Part D for the full trust-gate exposition.

---

## Part E — 2026-04-27 Inventory Addendum

Memory recommendations now need to align with the canonical scoped-KG contract:

- **Age is not staleness.** L.4 can dedupe, audit, and raise `review_due`, but it must not expire facts by TTL. Staleness requires a driver event.
- **Contradiction behavior depends on scope.** Same-scope contradiction can stale the older entry at write time. Cross-scope disagreement stays fresh on both sides and appears as read-time `contradictions` metadata.
- **Trust fields should be one schema, not separate conveniences.** `author`, `tier`, `cites`, `corrective_source`, `scope`, and `derived_from` should be designed together so lesson/memory/KG trust semantics do not drift apart.
- **Correction capture should produce audit evidence.** SessionEnd correction detection should emit fold-back observations or draft lessons with provenance, so later completed-plan and lesson audits can inspect the evidence trail.

---

## Part F — Second-pass enrichment (2026-04-27)

*Added after re-reading `scoped-knowledge-graphs/design.md` (note schema + drivers + provenance), `app-type-profiles/design.md` (verifier chain + behavior preservation), and `skill-tiering-contract/design.md` (tier invariants for non-composition artifacts).*

- **F.1 — One schema, three projections — owned by scoped-KG.** Part E said "trust fields should be one schema, not separate conveniences" but did not assign ownership. The right home is `scoped-knowledge-graphs/design.md` as the canonical `KGNote` schema: `author`, `scope`, `derived_from`, `tier`, `cites`, `corrective_source`, plus the existing `IndexedAt` / staleness drivers / `contradictions` metadata. From that one schema:
  - **`.agents/lessons/<name>/LESSON.md` is a projection.** Frontmatter mirrors the canonical fields; lessons round-trip into the warm store as `KGNote` rows of `note_type: lesson`. Edits flow either direction; `da kg sync` handles the round-trip.
  - **Auto-memory files are an adapter view.** `~/.claude/projects/<hash>/memory/*.md` is a Claude-Code-platform cache of `user`-scope notes, read at SessionStart and write-mirrored on session end. Cursor / Codex equivalents project from the same `user`-scope rows.
  - **No per-store schema.** The synthesis previously implied three independent conventions; this collapses to one canonical shape with two projections.

- **F.2 — Tiers compose with `on_fail`, not replace it.** the_smart_ape's 3-tier (user-taught / user-confirmed-agent-draft / agent-inferred) is *source quality*. App-type-profiles §3 verifier `on_fail: hard | soft` is *consequence*. Author field is *authorship authority*. These compose orthogonally:
  - A `tier: 1` (user-taught) `author: human` lesson failing a `on_fail: hard` rubric still blocks. Source quality is high; rubric failure is structural; high-source claims can be wrong.
  - A `tier: 3` (agent-inferred) `author: agent` lesson failing a `on_fail: soft` rubric warns and proceeds.
  
  Refined L.2: tier and author land together as schema fields; the `verifier_chain` decides what blocks. Don't conflate "high tier = always trust" with "no verification needed."

- **F.3 — `corrective_source` should reference, not duplicate.** L.2 currently treats `corrective_source` as a free-text field. The right shape is a structured ref: `{ kind: "user_message" | "pr" | "commit" | "incident", id: <stable-id>, scope: <scope> }`. This makes the L.5 quarterly pruning pass machine-checkable: a lesson whose `corrective_source.kind: pr` points at a closed-with-no-merge PR is a candidate for review (the originating signal evaporated).

- **F.4 — Same-scope vs cross-scope contradiction changes the L.4 / L.5 pruning protocol.** Scoped-KG §5.12 says cross-scope disagreement is fresh-vs-fresh metadata; only same-scope contradiction stales the older entry. The L.5 pruning sort ("age × access × contradiction count") must respect this:
  - Same-scope contradictions → driver fired → entry already `stale` → high pruning priority.
  - Cross-scope disagreements → both fresh → metadata-only → **not** a pruning signal; the disagreement is legitimate.
  
  Refined L.5: pruning candidate score uses same-scope contradiction count, never cross-scope.

- **F.5 — SessionEnd correction-detector (L.3) must declare write scope.** A pushback-pattern detector that emits a fold-back observation is performing a `repo`-scope write under scoped-KG §3.3 (default). When the user's correction is platform-wide ("never do X"), the right scope is `user`, not `repo`. Refined L.3: detector classifies correction by language ("in this repo" → `repo`; "never" / "always" → `user`); writes target the classified scope; ambiguous corrections require human confirmation before persisting.

- **F.6 — Self-test the recommendations.** This evaluation document is itself a research artifact whose claims should ride the `research` profile (app-type-profiles §3): citation-presence on each per-article block, source-freshness on the linked URLs, rubric-check on the converging-themes claims. A spot check now: `kevin/two-authors`, `arscontexta/cognitive-grounding`, and `the_smart_ape/source-tiers` are all single-operator anecdotes whose tier under their own taxonomy is Tier 3-4. The recommendations built on them therefore inherit that tier, and the trust gate (Part D) is the right stopper. Apply this check during quarterly pruning of the doc itself.

---

## Part G — 2026-05-03 ashwingop Company Brain addendum (ontology + reread-the-past)

*Added after extracting Part 3 (Interaction Memory). Full per-article evaluation in `articles-evaluation-kg-and-adjacent.md` §A.2. This addendum captures the lessons/memory-specific implications.*

- **G.1 — Ontology extends NoteType, doesn't replace it.** Part 3's load-bearing claim is that the labels a system uses at write time decide what's recoverable at read time. Our existing `NoteType` (decision/rule/lesson/research-claim) is code-shaped; Part 3's ontology (decision/commitment/objection/escalation/dependency/assumption/customer-pain/owner/precedent/open-question) is conversation-shaped. The two are not in conflict — the existing four labels are a *subset* of the broader ontology. Refined recommendation: extend the enum additively, do not migrate. Code-shaped lessons (`type: rule`) keep their label; agent-observed organizational facts (a planning meeting decision, a customer commitment captured during ingestion) get the conversation-shaped labels.

- **G.2 — "Reread the past" is the dream cycle's load-bearing motivation, not a hygiene claim.** Earlier addenda treated the dream cycle (C.4, F-section reweave references) as a hygiene pass: dedup, link inference, description enrichment. Part 3 inverts this: the *reason* the dream cycle exists is that **the ontology evolves, and old data must be re-interpreted under the new ontology**. A casual customer objection from six months ago becomes evidence of churn risk only after the ontology grows to recognize "objection → churn pattern." Without the dream cycle's *re-interpretation* lane, that promotion never happens. Refined L.5 (quarterly pruning) and any future C.4 spec: the consolidation pass is a *re-interpretation* operation as much as a hygiene operation; both must be specified.

- **G.3 — Ownership is a missing schema field, distinct from author.** Part 2 names "who owns it" alongside "who created it" and "who changed it" as the three time-attribution axes. Today our scope chain implies ownership (a fact at `team` scope is "owned" by the team), but ownership is not a field. This matters for the L.2 recommendation (kevin's two-author): `author` answers *who created this* but does not answer *who can authoritatively revise this*. In a many-team environment those are different. Refined L.2: schema gains both `author: { kind, id }` and `owner: { kind, id, scope }`; promotion-gate reviewer-standing (architecture note §6.3) keys off `owner`, not `author`.

- **G.4 — Privacy at promotion is no longer deferable.** L.2 / L.3 / L.5 recommendations write to a shared store (warm sqlite, possibly cross-scope). Part 3 raises the stakes: "if you get the product wrong, it feels like surveillance." Concrete consequence for this doc: any auto-promotion path (lesson → rule, user-scope correction → team-scope rule) must declare a redaction contract. A user-scope lesson body that quotes a private user message cannot auto-promote without redaction. The privacy boundary is part of the scope-promotion gate, not a separate concern.

- **G.5 — Capture and interpretation are two surfaces, not one.** L.3 (SessionEnd correction-detector) bundles capture (notice the user pushback) with interpretation (classify the correction). Part 3 argues these should be separated: capture writes raw signal into the warm store with `derivation: untracked`; interpretation is a separate pass that applies the current ontology and writes new notes with `derivation: ontology-pass:<ruleset>`. The benefit: when the ontology evolves (new label added), reinterpretation re-runs without re-capturing. Refined L.3: detector emits a raw fold-back observation; ontology-mapping is a downstream pass that may emit a typed lesson when the ruleset matches.

- **G.6 — "Too little forgets, too much surveils" is the score formula's missing axis.** Today our promotion-gate score considers consumers, scope-weight, contradictions, provenance-tier. Part 3 names a *fourth* axis worth promoting to a first-class field: **memory boundary / exposure** — how many readers will be exposed to this fact via auto-promotion? Higher exposure → higher review threshold. This is structurally similar to scope-weight (which is about authority) but distinct (it's about reach). Refined L.5 quarterly pruning: pruning candidate score includes an exposure dimension; high-exposure facts age into review faster.

---

*Document status: draft evaluation. No changes made to code, specs, or plans.*
