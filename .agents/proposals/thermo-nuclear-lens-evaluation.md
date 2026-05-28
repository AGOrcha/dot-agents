# Proposal: Thermo-Nuclear Lens Evaluation — 4th lens vs meld decision

**Status:** draft (project-local design pass)
**Author/driver:** Nikash (delegated via `pr10-branch-split/thermo-nuclear-lens-evaluation`)
**Created:** 2026-05-28
**Routing:** project-local (`.agents/proposals/`) per `[[proposal-routing]]` — alters scaffold contents (`internal/scaffold/home/starter/agents/global/...`), copy-test lens-count assertion, and review-gate dispatch behaviour inside this repo. No global rule/skill changes proposed.
**Parents / siblings:**
- `[[layered-pr-fanout-with-pr-open-status]]` §3.3 (lens-as-merge-gate)
- `[[lens-evidence-policy-renderings]]` (sequencing strategies that already reference thermo-nuclear as a tier-3 / opt-out candidate)
**Method:** `/skill-architect audit` run over `~/.agents/skills/global/thermo-nuclear-code-quality-review/SKILL.md` and `~/.agents/agents/global/thermo-nuclear-code-quality-review/AGENT.md`, plus comparative read of the three landed lens agents.

---

## §1. Skill design assessment (via /skill-architect audit mode)

Audit applies `instructions/principles.md` + `instructions/audit.md` from `~/.claude/skills/skill-architect/`.

### Skill Audit: thermo-nuclear-code-quality-review

**Category:** 6 (Code Quality & Review) — clean fit, no straddle.
**Current structure:** **monolithic** — the entire rubric (tone, rules, questions, remedies, output ordering, approval bar) lives inline in `SKILL.md`. No `instructions/`, no `templates/`, no `references/`, no `eval/`.

#### Passing

- Description **is** a trigger condition (`Use for a thermo-nuclear code quality review, thermonuclear review, deep code quality audit, or especially harsh maintainability review.`) — clears principle #2.
- Category fit is unambiguous.
- Frontmatter sets `disable-model-invocation: true`, which is correct for a skill that should only fire via explicit user phrasing or a parent Task dispatcher — protects against accidental triggering on every PR.
- Tone / approval-bar content is *high-signal*: the "no file past 1k lines," "no random spaghetti growth," and "code-judo" rules are concrete, non-obvious, and capture failure modes the three existing lenses do **not** explicitly cover.
- The AGENT.md correctly identifies itself as a Task subagent with parent-supplied diff + file contents (matches the staged-dispatch contract used by the three landed lens agents).
- Source provenance is recorded inline (`source at -> https://...cursor-team-kit/...`) — useful for upstream re-sync.

#### Issues

- **Monolithic SKILL.md (~190 lines of inline rules)** → violates principle #1 (SKILL.md should be an orchestrator with `Load →` directives, not the rules themselves). Recommended fix: split into `instructions/rules.md` (the 7 non-negotiables), `instructions/questions.md` (primary review questions), `instructions/remedies.md`, and keep SKILL.md as a 25-line workflow.
- **No `instructions/gotchas.md`** → violates principle #4. Failure points specific to this skill that would belong here: "Do not flag 1k-line crossings on generated code, lockfiles, scaffolded fixtures, or test data tables — those are not authored maintainability surface"; "Do not propose code-judo restructurings that cross package boundaries owned by another in-flight delegation (will conflict at merge)"; "Do not double-flag findings already raised by architecture-standards" (the redundancy concern in §2 below).
- **No output template** → the rubric specifies a *priority order* for findings but provides no `templates/finding.md` or `templates/review.md`. The three landed lens agents use a uniform `severity / location / scenario / suggested_fix` block; thermo-nuclear's SKILL.md does not commit to this shape, so a reviewer using it would emit free-form prose that the parent's lens-aggregation logic cannot parse.
- **Findings format diverges from the three landed lenses.** Landed lenses emit: `severity / location / scenario / suggested_fix` + `verdict: pass | fail (lens: <name>)`. Thermo-nuclear's SKILL.md emits a free-form "prioritized findings list" with no per-finding structured fields and no verdict line. Without alignment, `workflow review_gate` cannot aggregate it into the consolidated decision — it has to be either: (a) re-shimmed by the parent, or (b) updated at the source. (b) is the correct fix.
- **No `eval/` layer** despite being a "high-stakes" review skill (principle #8). At minimum it needs `eval/checklist.md` ("did the review flag at least one structural concern when one demonstrably exists; did it abstain from 1k-line flags on lockfiles; did it produce the structured finding format").
- **Closeout / merge-back contract missing** in the AGENT.md. The three landed lens agents specify the iteration-close sequence (`workflow verify record --kind review` → `workflow checkpoint` → `workflow merge-back`) and the write target (`.agents/active/review/<task_id>-<lens>.md`). The thermo-nuclear AGENT.md does not — so it would not slot into the existing review-gate dispatch without modification.
- **Tools list missing** on the AGENT.md frontmatter. The three landed lenses pin `tools: Bash, Read, Grep, Glob` (deliberately omits Edit/Write so the lens cannot mutate code). Thermo-nuclear's AGENT.md omits the tools key → inherits the default toolset (includes Edit/Write), which violates the "lens never edits production code" guardrail.
- **Source-control coupling to upstream URL.** The skill claims it can "fall back to a harsh maintainability audit aligned with that skill's intent" if cursor-team-kit isn't available — fine for a standalone skill, but if we adopt it as a lens we own the rubric copy and the fallback clause becomes dead code (and a drift vector if cursor updates upstream and we silently re-sync).

#### Priority improvements (if we keep the skill in any form)

1. Restructure SKILL.md → orchestrator pattern; move the 7 rules into `instructions/rules.md`; add `instructions/gotchas.md`; add `templates/finding.md` matching the landed lens shape.
2. Rewrite AGENT.md frontmatter to pin `tools: Bash, Read, Grep, Glob` and add the iteration-close + merge-back closeout block, mirroring the three landed lens agents' final section verbatim (modulo lens name).
3. Add `eval/checklist.md` (pass/fail gate) + `eval/advisory-board.md` (3 personas: pragmatic-author, security-reviewer, codebase-archaeologist) per principle #8.

#### Integration friction with the existing review-gate dispatch

- **Lens-count assertion** in `internal/scaffold/home/copy_test.go:144-155` (TestCopyStarterAssetsIncludesReviewerLensAgents) hard-codes the three lens names. Adding a 4th requires updating the slice — additive, cheap, but must not be forgotten.
- **`workflow review_gate`** dispatcher does not currently enumerate "all 4 lenses." Adding one shifts the default-parallel cost from 3× to 4× wall-clock-equivalent lens compute on every PR (unless tier-gated per `[[lens-evidence-policy-renderings]]` §2.1 Strategy B). This is the dominant integration cost.
- **Bundle schema** `review_type` enum (referenced in each landed AGENT.md as `Confirm review_type: <lens> is set on the bundle`) needs `thermo-nuclear` added as a valid value. Search target: wherever the review-gate fanout writes the `review_type` field — touch points are the fanout flag and the bundle-builder helper.
- **Iteration-close skill** does not validate lens names, so no change there.

#### Redundancy vs the three existing lenses — quick read (full table in §2)

The thermo-nuclear rubric overlaps **most heavily with architecture-standards** (file-size, layering, helper duplication, canonical-helper reuse, abstraction quality). It overlaps **moderately with adversarial** on "swallowed errors / silent fallback" framing but adversarial owns the security/threat angle and thermo-nuclear does not. Overlap with acceptance-invariants is **near zero** — thermo-nuclear has no concept of business intent or out-of-band requirements. Distinct value of thermo-nuclear sits in **two narrow but real bands**:

1. **Code-judo / dramatic-simplification** push — "rethink the structure so whole branches disappear," not "polish the structure." None of the three landed lenses tell the reviewer to be *ambitious about deletion*.
2. **Hard 1k-line file-size rule with explicit waiver discipline.** Architecture-standards talks about "module placement" but does not enforce a numeric threshold or require justification at the boundary.

Everything else in the rubric is reachable from a sufficiently strict reading of architecture-standards' charter.

---

## §2. Topical overlap analysis

| Lens | Thermo-nuclear overlap | Distinct value of thermo-nuclear |
|---|---|---|
| **architecture-standards** | **HIGH.** Both cover: module/package boundaries, public-interface design, helper duplication / canonical-utility reuse, layering violations, separation of concerns, abstraction quality (thin wrappers, identity helpers), naming and type-boundary cleanliness. The architecture-standards charter ("design coherence, module/subpackage boundaries, interface & data-shape design, separation of concerns") subsumes ~70% of the thermo-nuclear rubric (rules 4, 5, 6, plus most of rule 0 once read strictly). | Adds: (i) **code-judo / deletion-bias** framing — "look for the restructuring that makes whole branches disappear," not "tighten what is there"; (ii) **explicit 1k-line file-size rule** with required justification for crossings; (iii) **tone calibration** — "demanding, do not soften major maintainability issues into mild suggestions." None of these are stated in architecture-standards today. |
| **acceptance-invariants** | **LOW.** Acceptance-invariants is about business intent, out-of-band domain knowledge, and platform invariants surviving design→impl. Thermo-nuclear says nothing about intent or invariants. The only genuine touch-point is rule 7's "partial-update logic" / atomicity language, which gestures at an invariant concept but is framed as a *design smell* (not "does the system still satisfy contract X?"). | Adds: nothing meaningful to this lens's domain. Thermo-nuclear would not catch a missed acceptance criterion or a violated platform invariant unless it happened to also be ugly structurally — and acceptance-invariants would catch those first. |
| **adversarial** | **LOW-to-MEDIUM.** Both flag "swallowed errors" and "silent fallback to paper over an unclear invariant." Both note ad-hoc branching as suspect. But adversarial owns security (injection, secret leakage, PATH), races/TOCTOU, data-loss/clobber, POSIX/Windows divergence — none of which thermo-nuclear addresses. Conversely thermo-nuclear's overlap is narrow: it flags swallowed errors as a *quality* smell, not as a *correctness* hazard. | Adds: stylistic-quality framing on error-handling smells (different verdict severity than adversarial would give the same site). Marginal — not enough to justify a separate lens. |

**Summary read of the table:** thermo-nuclear and architecture-standards are ~70% the same lens with two distinct knobs (code-judo + 1k-line rule). They are *not* near-duplicates of the other two lenses.

---

## §3. Two paths in detail

### Path A — Land thermo-nuclear as a 4th lens

**Files that would change** (additive):
- `internal/scaffold/home/starter/agents/global/thermo-nuclear-reviewer/AGENT.md` (new — rewritten from the cursor skill's AGENT.md into the landed lens shape: pinned tools, iteration-close closeout, `.agents/active/review/<task_id>-thermo-nuclear.md` write target, structured findings format, verdict line).
- `internal/scaffold/home/starter/skills/global/thermo-nuclear-code-quality-review/SKILL.md` (new — restructured per §1 audit recommendations: orchestrator SKILL.md + `instructions/rules.md` + `instructions/gotchas.md` + `templates/finding.md`).
- `internal/scaffold/home/copy_test.go` lens-count slice → add `"thermo-nuclear"` to the list at line 149.
- `workflow review_gate` dispatcher → register the new lens in the default lens-set; update any `review_type` enum / validator.
- `workflow fanout` flag plumbing → accept `review_type=thermo-nuclear`.
- `.agentsrc.json` schema → if `lens_set` lands as a config key (per `[[lens-evidence-policy-renderings]]` §0.1), add `thermo-nuclear` as a valid enum value.

**Pros:**
- **Preserves the deletion-bias framing.** A reviewer specifically prompted with "look for the code-judo move that deletes whole branches" produces different output than one prompted with "check module placement and interface design." The framing is load-bearing.
- **Explicit, queryable verdict.** A fail on `lens: thermo-nuclear` is a different signal from a fail on `lens: architecture-standards` — distinguishable in the audit trail (`.agents/active/review/<task_id>-thermo-nuclear.md`), distinguishable in per-lens telemetry, distinguishable in evidence-policy routing (tier-3 gating, opt-out per app_type per `[[lens-evidence-policy-renderings]]` §2.1 Strategy B).
- **Composable with the rendering proposal.** `[[lens-evidence-policy-renderings]]` already treats thermo-nuclear as a tier-3 lens behind a `tier_promotion` gate and as an excludable lens on `agent-prompt` / `docs` app_types. That design assumes a separately-addressable 4th lens.
- **Upstream re-sync stays cheap.** If the cursor-team-kit updates the upstream rubric, we re-pull into one file (`instructions/rules.md`) rather than scattered diffs across architecture-standards.

**Cons:**
- **70% rubric overlap with architecture-standards.** Two lenses reviewing largely the same surface produce double-flagged findings in `.agents/active/review/`, increasing maintainer reconciliation cost.
- **+33% default-parallel lens compute** (3 → 4 lenses run on every PR) unless tier-gating is enabled. Wall-clock unchanged in pure parallel, but token spend grows.
- **Schema churn cost.** Lens-count assertion + review_type enum + fanout flags + scaffold copy → 4-5 touch points must move together (additive, but with merge-conflict risk against in-flight pr10 work).
- **Lens-set discipline risk.** Once the 4th lens lands, adding a 5th (security-specialist, perf-specialist, ...) becomes the obvious next move. Lens proliferation is the *opposite* of the staged-dispatch contract's tight 3-lens default.

### Path B — Meld into architecture-standards-reviewer

**Files that would change** (substantive edit, not additive):
- `internal/scaffold/home/starter/agents/global/architecture-standards-reviewer/AGENT.md` — fold the 7 thermo-nuclear rules into the "Concretely check" block; add a `## Mode` section (or frontmatter flag) for `mode: standard | thermo-nuclear` that toggles the deletion-bias framing and the 1k-line rule.
- `workflow fanout` → accept `--lens-mode thermo-nuclear` (or equivalent) when dispatching the architecture-standards lens.
- `.agentsrc.json` schema → `architecture_standards.mode: standard | thermo-nuclear` (per-project default).
- Lens-count assertion in `copy_test.go` → unchanged (still 3 lenses).
- Cursor-team-kit source skill → archived as a reference, not a live scaffold asset.

**Pros:**
- **Zero lens-count churn.** No update to copy_test.go, no review_gate enum bump, no per-lens telemetry split.
- **Eliminates the 70% overlap.** One reviewer covers the whole architecture-quality surface; severity calibration becomes a mode flag rather than two parallel verdicts.
- **Lower maintainer cognitive load per PR.** One arch verdict to reconcile, not two.
- **Cheaper default-parallel cost.** Stays at 3 lenses → no 4× compute regression.

**Cons:**
- **Loses the deletion-bias signal in the default mode.** Unless every PR opts into `mode: thermo-nuclear`, the code-judo / 1k-line discipline only fires when explicitly requested — which means it does not fire on the long tail of routine PRs where it would most catch sprawl.
- **Mode toggling muddies the AGENT.md.** The landed architecture-standards prompt is tight (75 lines, single-charter). Adding a mode-switch ("if mode=thermo-nuclear, also apply rules X/Y/Z, escalate severity Q to BLOCKER, use harsher tone") makes the prompt longer and asks the reviewer to do per-finding mode-aware adjustments — a known prompt-engineering anti-pattern.
- **Tone divergence in one prompt is hard.** The thermo-nuclear tone block ("be direct, serious, demanding; do not soften major maintainability issues into mild suggestions") is *prescriptively different* from the neutral architecture-standards tone. Asking one prompt to switch tones based on a mode flag has lower fidelity than two prompts each with their own calibrated tone.
- **Upstream re-sync gets harder.** Cursor-team-kit updates land as scattered edits across the merged file instead of a clean refresh of one rules file.
- **`[[lens-evidence-policy-renderings]]` references break.** The rendering doc treats thermo-nuclear as a separately-addressable lens for tier-gating and app-type routing. Meld breaks that addressability — the rendering doc would have to be rewritten to "architecture-standards mode-flag" semantics, which is uglier.

---

## §4. Recommendation

**Decision: Hybrid — Path B-leaning meld with a narrow Path A-style escape hatch. Specifically: meld the thermo-nuclear rubric into architecture-standards-reviewer behind a `mode: standard | thermo-nuclear` toggle, AND keep the deletion-bias / 1k-line rule active in the *default* mode so the long-tail PR benefits do not require opt-in.**

### Rationale

1. The §2 overlap table makes Path A's case weak: a 70% redundant lens producing parallel verdicts on the same surface is exactly the lens-proliferation pattern the staged-dispatch contract was designed against.
2. The two genuinely distinct knobs (code-judo deletion-bias + 1k-line rule) are *additive prompts*, not a different charter. Adding them to architecture-standards' "Concretely check" list strengthens the existing lens rather than spawning a parallel one.
3. The tone-divergence concern from Path B is real but recoverable: the architecture-standards prompt already permits per-finding severity calibration via the standard BLOCKER/HIGH/MEDIUM/LOW gradient. Tone calibration ("be direct about structural regressions") can live inline in the prompt without a runtime mode-switch.
4. The mode-switch escape hatch is preserved for **project-level opt-in to the harshest framing** (e.g., a project that wants the full thermo-nuclear tone in CI). This is the narrowest viable Path A surface: one config flag, no new lens, no new files.
5. `[[lens-evidence-policy-renderings]]`'s tier-3 / app-type-routing references to thermo-nuclear can be re-cast as `architecture-standards.mode = thermo-nuclear` references with no semantic loss — they were never really about "a separate lens" so much as "this severity tier of architecture review."

### Migration path (concrete)

**Files modified:**
- `internal/scaffold/home/starter/agents/global/architecture-standards-reviewer/AGENT.md` —
  - Extend "Concretely check" with: (a) deletion-bias / code-judo question, (b) hard 1k-line file-size rule with required justification language, (c) "thin wrapper / identity-abstraction" smell, (d) "ad-hoc branching grafted onto unrelated flows" smell, (e) "feature logic leaking into shared paths" reuse-of-canonical-helper smell. Five additive bullets in the existing format.
  - Add a single sentence to the Role block: "default tone is direct about structural regressions — do not soften major maintainability findings into mild suggestions."
  - No mode-switch in the agent prompt itself in v1. (The mode flag lives in `.agentsrc.json` and influences only the *dispatch decision* of which secondary checks to require — kept out of the prompt to avoid mode-aware finding logic.)

**Files added (optional, v1.5):**
- `internal/scaffold/home/starter/skills/global/architecture-standards/SKILL.md` — only if we want a loadable rubric file for the agent to `Load →` (matches the orchestrator pattern from skill-architect principle #1). Today's AGENT.md is inline-rules; promoting to orchestrator+rules is a separate refactor and **not** strictly required for the meld.

**Files NOT changed (good — additive-only impact):**
- `internal/scaffold/home/copy_test.go` lens-count assertion → unchanged (still 3 lenses; no test rewrite).
- `workflow review_gate` dispatcher → unchanged enum / lens-set.
- `acceptance-invariants-reviewer` / `adversarial-reviewer` → untouched.
- `.agentsrc.json` schema → optional addition of `architecture_standards.mode` enum (only if the project-level escape hatch is wired in v1; defer to v1.5 if not).

**Cursor-team-kit upstream:**
- Archive `~/.agents/skills/global/thermo-nuclear-code-quality-review/` and `~/.agents/agents/global/thermo-nuclear-code-quality-review/` to a `~/.agents/archive/` location with a one-line stub pointing at the architecture-standards-reviewer (so any in-flight `/thermo-nuclear-code-quality-review` invocation still resolves to a clear redirect).
- Cite source provenance in the architecture-standards-reviewer prompt (one-line attribution comment, matching the existing cursor-team-kit attribution style).

### Estimated impl size

**Small** — single agent prompt edit (~30-50 lines added to one AGENT.md), one archive move with redirect stub, optional schema addition. 3-4 tasks. No scaffold restructure, no test rewrites, no review-gate dispatcher changes.

---

## §5. Implementation task outline (if accepted)

Sequence under either a new sub-plan `thermo-nuclear-meld` (preferred — cleanly separable) or as a tail-set of tasks on `pr10-branch-split` if velocity matters more than plan hygiene.

1. **t1 — Extend architecture-standards-reviewer prompt** (write_scope: `internal/scaffold/home/starter/agents/global/architecture-standards-reviewer/AGENT.md`)
   - Add the five additive "Concretely check" bullets (deletion-bias, 1k-line rule, thin-wrapper smell, ad-hoc branching smell, canonical-helper reuse).
   - Add the tone sentence to the Role block.
   - Source attribution comment.
   - Verification: `go test ./internal/scaffold/...` (lens-count test still passes), prompt readback by manual reviewer.

2. **t2 — Archive cursor-team-kit thermo-nuclear skill + agent with redirect stub** (write_scope: `~/.agents/archive/thermo-nuclear-code-quality-review/`, plus stub files at the old paths)
   - **Note: write_scope is in `~/.agents/`, not the repo.** Must be tagged as a global-scope task and routed through `da` global-config tooling rather than a repo PR. If global-scope CLI support is unavailable, fold this into a manual one-liner during release notes and skip the task.
   - Verification: invoking `/thermo-nuclear-code-quality-review` resolves to the redirect.

3. **t3 — Optional: add `architecture_standards.mode` to `.agentsrc.json` schema** (write_scope: `schemas/agentsrc.schema.json`, `internal/config/agentsrc.go`, `internal/config/agentsrc_test.go`)
   - Add enum `{standard, thermo-nuclear}`, default `standard`. Follow the schema-usage rules (all 6 sync points per `[[schema-usage]]`).
   - Defer to v1.5 if not needed at first release.
   - Verification: `go test ./internal/config/... ./commands/...`.

4. **t4 — Update `[[lens-evidence-policy-renderings]]` references** (write_scope: `.agents/proposals/lens-evidence-policy-renderings.md`)
   - Replace the three remaining `thermo-nuclear` mentions in §0, §2.1 Strategy A/B, §2.3 Strategy C, Appendix B with `architecture-standards (thermo-nuclear mode)` semantics; keep the tier-gating examples intact (they remain useful as tier-3 escalation, just routed through one lens not two).
   - Verification: re-read renderings doc end-to-end; confirm Strategy B's tier-gate still makes sense with the lens collapsed.

5. **t5 — Lesson capture** (write_scope: `.agents/lessons/lens-redundancy-vs-mode-flag/LESSON.md`)
   - Capture the "70% overlap → meld with mode-flag, do not spawn parallel lens" decision pattern so future lens-add proposals can reference it as precedent.
   - Update `.agents/lessons/index.md`.

**Dependency order:** t1 (independent) → t2 (independent) → t3 (independent; optional) → t4 (depends on t1 landing in plan-as-shipped) → t5 (depends on t4).

**Parallelizable:** {t1, t2, t3} can fan out in one wave. {t4, t5} sequentially after.

**Out of scope for this implementation:**
- Restructuring architecture-standards into the full orchestrator pattern (separate skill-architect transform task; not required for the meld).
- Adding eval/ layers to architecture-standards (separate hardening task per skill-architect principle #8).
- Auto-routing thermo-nuclear-mode based on file-tree heuristics (e.g., "if PR touches code, use thermo-nuclear mode; if docs, use standard"). That belongs in the layered-fanout / lens-routing work, not here.

---

## §6. Sanity check against the parent proposals

- `[[layered-pr-fanout-with-pr-open-status]]` §3.3 lists thermo-nuclear as a possible 4th merge-gate lens — this proposal recommends it does NOT land as such. Parent proposal should drop "(thermo-nuclear if added — see ...)" wording and the §5.1 "open: do we want two distinct sub-statuses" note loses one consideration (lens-count goes from "3 with potential 4th" to "3 stable").
- `[[lens-evidence-policy-renderings]]` §2.1 Strategy A/B and §2.3 Strategy C explicitly reference thermo-nuclear as a tier-3 / opt-out lens. Under this recommendation those become `architecture-standards.mode=thermo-nuclear` references with no loss of expressive power; rendering doc edits captured as task t4.
- The pr10-branch-split plan's `thermo-nuclear-lens-evaluation` task closes out at "decision recorded; implementation deferred to follow-up plan." This proposal *is* the closeout artifact.

---

## §7. Maintainer redirection — mode-as-skill, not mode-as-inline-prompt (added 2026-05-28 review fold-in)

The §4 recommendation lands the meld behind a `mode: standard | thermo-nuclear` toggle wired in `.agentsrc.json`. Maintainer review (PR #153, file-comment at `.agents/proposals/thermo-nuclear-lens-evaluation.md:135`) flags that the *mechanism* — inline-prompt mode-aware content — is the wrong vector. Quoted:

> The tone-divergence and mode questioning gives me the idea of setting up the different modes as a part of a skill they can use, were they load the specific mode's instructions, and have one template used, so architectural-standards and others if they gain a variant / mode to use. As you said with v1.5 this AGENT.md is overloaded and can be optimized better to use skills and templates + rubrics properly. would want to recheck / plan / think the path forward

This reframes §4 / §5 in a load-bearing way and supersedes the v1 "single AGENT.md with inline mode text" path. Capturing the redirection here, ahead of any C.4-style planning round.

### §7.1 The redirected architecture

- **Each lens AGENT.md becomes a thin template** — orchestrator role, output contract, dispatch rules. No mode-specific rubric content inline. (Matches skill-architect principle #1 "orchestrator + loaded rules.")
- **Each mode is a skill under `~/.agents/skills/global/<lens>.<mode>/`** — `architecture-standards.standard`, `architecture-standards.thermo-nuclear`, future `architecture-standards.<mode-N>`. Contains the rubric, severity calibration, "concretely check" bullets, tone calibration.
- **Per-finding rubric files** under each skill — codifies the BLOCKER/HIGH/MEDIUM/LOW gradient + the deletion-bias + 1k-line rule as standalone rubric documents the agent loads on demand, not inline-baked.
- **Shared template** for all three (later: four+) lenses — a single `lens-template.md` (or starter scaffold) holds the orchestrator pattern; lens-specific instantiation parameterizes via `name`, `loaded-skills[]`, `output-contract-extras`.
- **Mode selection** happens at dispatch time — the review-gate dispatcher reads `architecture_standards.mode` from `.agentsrc.json` (per §4 escape hatch) and instructs the lens to load the corresponding skill instead of forking which agent it spawns.

### §7.1a Multi-mode-per-lens is now ALLOWED (per #167 OQ-2, added 2026-05-28)

The sibling `[[lens-template-and-mode-skills]]` proposal resolved its **OQ-2 to ALLOW multi-mode-per-lens** (PR #167) — a lens can run in more than one mode in a single PR (e.g. `architecture-standards` in both `standard` and `thermo-nuclear`). This is a *mechanism* refinement on top of this proposal's §4 decision and does **not** change the meld decision below.

The reconciliation-cost concern this proposal raised (§3 Path A cons, §4 rationale — "two parallel verdicts on the same surface increase reconciliation cost") is **reconciled, not contradicted**: that cost is real, but it is **addressed by a synthesize step** (models 2-4 of #167 §6.5's execution-model benchmark — single-agent-multi-mode-in-series-then-synthesize, parallel-then-reconcile, interleaved), not by forbidding multi-mode. The meld's "avoid double-verdict reconciliation" framing was correct about the *cost* but the right mitigation is a synthesis/reconciliation pass — which #167 makes a design requirement of every multi-mode execution model — rather than a hard single-mode constraint. Evidence: the prior `.agents/active/reviews/{pr3b,pr3c}` runs show the *combined* (synthesized) output caught cross-cutting findings no single lens caught alone, which is exactly what motivates allowing a lens to run multiple modes and synthesize.

**The §4 meld decision is unchanged** — still no separate 4th lens; thermo-nuclear stays a *mode* of architecture-standards. Multi-mode-allowed means that one melded lens can now run both its modes in a single PR (and synthesize), which strengthens the meld rather than reopening the 4th-lens question.

### §7.2 What stays from §4 / §5

- The **decision** (Hybrid B-leaning meld; no separate 4th lens) is unchanged.
- The **`.agentsrc.json` `architecture_standards.mode` flag** is unchanged — it's the right surface; only what it controls shifts (loaded skill, not inline prompt branch).
- The **archive of `~/.agents/skills/global/thermo-nuclear-code-quality-review/`** still happens; the archived content gets re-cast as `architecture-standards.thermo-nuclear` (one of the per-mode skills) rather than deleted outright.
- The **deletion-bias / 1k-line rule** still lives in the *standard* mode (default), not gated behind opt-in — but it lives in the standard-mode skill file, not in the AGENT.md prompt.

### §7.3 What changes from §4 / §5

- **t1 "extend AGENT.md with five additive bullets"** → REPLACED by t1' "create `architecture-standards.standard` skill (default-loaded) + `architecture-standards.thermo-nuclear` skill (mode-loaded) + slim down AGENT.md to template-only." More work; clearer separation of concerns.
- **t2 "archive cursor-team-kit thermo-nuclear with redirect stub"** → REPLACED by t2' "re-cast cursor-team-kit thermo-nuclear content as the body of the `architecture-standards.thermo-nuclear` skill." Same write-scope target, different framing.
- **t3 "schema add `architecture_standards.mode`"** → UNCHANGED; same surface.
- **NEW t6 "design + land lens-template scaffold"** — the shared orchestrator + parameterization template that all three lenses (and future modes) instantiate against. This is the v1.5 hygiene work the maintainer flagged as overdue.
- **NEW t7 "migrate acceptance-invariants + adversarial lenses to the same template + skills shape"** — once the template lands, the other two lenses should follow for consistency. Don't ship a one-lens-uses-template-others-don't surface area; that's a half-meld.

### §7.4 Cost re-estimate

§4 said "Small — 30-50 lines added to one AGENT.md." This redirection is **Medium** — new skill-files for 2-3 modes per lens × 3 lenses = 6-9 skill files, template scaffold, schema field, dispatcher wiring. Still well under "Large" (no review-gate behavior change, no test scaffold change beyond skill-file fixtures), but no longer a single-PR meld.

### §7.5 Recommendation update

**Recheck / plan / think** the path forward per the maintainer's ask. Recommended next step: open a sibling design proposal `lens-template-and-mode-skills.md` (under `.agents/proposals/`) that captures §7.1-§7.4 in spec-quality detail, then thread its outcomes back here as a §4.5 superseding the current §4 recommendation. Do NOT block this current proposal's decision-record on that — the meld-vs-4th-lens question is independently answered (meld); the mechanism question becomes its own design loop.

### §7.6 Open question added

- **OQ-A (NEW) — mode-skill naming convention.** Use dotted `<lens>.<mode>` (e.g., `architecture-standards.thermo-nuclear`) or hierarchical directories (`architecture-standards/modes/thermo-nuclear/SKILL.md`)? Either resolves; pick one and stick. Recommend dotted (cheaper to grep, mirrors how `[[verifier_profiles]]` are referenced).
- **OQ-B (NEW) — backward compatibility during template migration.** Two paths: (a) flag-day swap all three lenses + template + skills in one PR (high-risk, easy review); (b) per-lens migration, each lens self-converts and the dispatcher reads either inline-AGENT or template+skill (lower risk, longer in-flight inconsistency). Recommend (b) per the `[[parallel-worker-branch-drift]]` lesson — concurrent lens edits + template edits is exactly the partial-coherence trap.
