# Proposal: Lens Template + Per-Mode Skills

**Status:** draft (project-local design pass)
**Author/driver:** Nikash (delegated)
**Created:** 2026-05-28
**Routing:** project-local (`.agents/proposals/`) per `[[proposal-routing]]` — alters scaffold contents (`internal/scaffold/home/starter/agents/global/...` + `.../skills/global/...`), the copy-test lens assertion, the `.agentsrc.json` schema, and the review-gate dispatch behaviour inside this repo. The per-mode skills land as *scaffolded starter assets* the user promotes into `~/.agents/` via `da skills promote` — they are not authored directly into the global home, so this stays project-scope.
**Parents / siblings:**
- `[[thermo-nuclear-lens-evaluation]]` §7 (the seed — maintainer redirection "mode-as-skill, not mode-as-inline-prompt"). PR #153, file-comment at `.agents/proposals/thermo-nuclear-lens-evaluation.md:135`.
- `[[layered-pr-fanout-with-pr-open-status]]` §3.3 (lens-as-merge-gate; lens-set composition) — merged as #149.
- `[[lens-evidence-policy-renderings]]` (tier-gating / app-type routing that references thermo-nuclear as an addressable lens).
**Method:** Comparative read of the three landed lens AGENT.md files (`architecture-standards-reviewer`, `acceptance-invariants-reviewer`, `adversarial-reviewer`), the thermo-nuclear §7 redirection, and the `verifier_profiles` precedent in `.agentsrc.json` / `da skills` surface.

---

## §1. Problem — AGENT.md overload

The three landed lens reviewers (`internal/scaffold/home/starter/agents/global/<lens>-reviewer/AGENT.md`) are ~70-line prompts that are **~85% identical boilerplate**. Diffing them, the only lens-specific content is:

1. The frontmatter `name` / `description`.
2. The `# Role` paragraph (one lens-charter sentence).
3. The `Concretely check:` bullet list (the actual rubric — 5-6 bullets).
4. The lens name interpolated into the `verdict:` line, the `.agents/active/review/<task_id>-<lens>.md` write target, and the cross-lens guardrails.

Everything else — Startup (bundle read, task-status confirm, target-exists check), Findings format, Closeout (`/iteration-close` sequence), Guardrails (no-edit, no-rerun, no-orchestrator-commands) — is **verbatim across all three**. This is copy-paste-with-drift waiting to happen: a fix to the Closeout sequence today requires three identical edits, and the next lens (thermo-nuclear, security-specialist, …) copies the boilerplate a fourth time.

The thermo-nuclear evaluation (`[[thermo-nuclear-lens-evaluation]]` §4) proposed folding the thermo-nuclear rubric into `architecture-standards-reviewer` behind a `mode: standard | thermo-nuclear` flag with the mode content **inline in the AGENT.md prompt**. Maintainer review (§7) rejected that *mechanism*:

> The tone-divergence and mode questioning gives me the idea of setting up the different modes as a part of a skill they can use, where they load the specific mode's instructions, and have one template used, so architectural-standards and others if they gain a variant / mode to use. […] this AGENT.md is overloaded and can be optimized better to use skills and templates + rubrics properly.

So the problem is two-layered:

- **(P1) Boilerplate overload** — the shared orchestrator scaffolding is duplicated three (soon four) times.
- **(P2) Rubric-in-prompt coupling** — the lens rubric (and any mode variant of it) lives inline in the AGENT.md, so a mode toggle means inline mode-aware prompt logic, a known prompt-engineering anti-pattern (per `[[thermo-nuclear-lens-evaluation]]` §3 Path B cons).

This proposal resolves both: a **thin shared template** (P1) and **per-mode skills the lens loads at dispatch time** (P2).

---

## §2. Approach

Three moving parts:

1. **One shared lens template.** A single orchestrator AGENT.md shape that holds *only* the lens-invariant scaffolding — Startup, Findings format, Closeout, Guardrails — parameterized by `name`, the loaded-skill reference, and the write-target lens slug. Each lens's AGENT.md is a thin instantiation of this template; it carries no rubric content.

2. **Per-mode skills.** Each (lens, mode) pair is a skill — `architecture-standards.standard`, `architecture-standards.thermo-nuclear`, `acceptance-invariants.standard`, `adversarial.standard`. The skill holds the rubric: the `Concretely check` bullets, the severity calibration, the tone calibration, and (for thermo-nuclear) the deletion-bias + 1k-line rule. The lens AGENT.md loads its mode skill at the top of Review execution via a `Load → <lens>.<mode>` directive.

3. **Dispatch-time mode selection.** The review-gate dispatcher reads the per-lens mode from `.agentsrc.json` (`lens_modes.<lens>`) and tells the spawned lens worker which mode skill to load. The dispatcher does *not* fork which agent it spawns; it spawns the same lens worker and parameterizes the loaded skill. Default mode is `standard`; absent config = `standard`.

This keeps the staged-dispatch contract (one target, one lens, one worker) intact while collapsing the rubric and tone into loadable, separately-versioned, separately-testable skill files.

---

## §3. Decisions

### D1 — Mode-skill naming convention: dotted `<lens>.<mode>` (RECOMMENDED)

`architecture-standards.thermo-nuclear`, `acceptance-invariants.standard`, `adversarial.standard`.

**Rationale:** mirrors the existing `verifier_profiles` keying in `.agentsrc.json` (an object keyed by profile name — `unit`, etc.) and the `da skills` flat-name surface (`da skills list` / `da skills promote <name>` operate on flat skill names, no nested-directory mode). Dotted names are cheaper to grep (`grep -r 'architecture-standards\.'`), sort adjacently in `da skills list`, and avoid a `modes/` subdirectory convention that `da skills promote` does not currently understand. Rejected alternative — hierarchical `architecture-standards/modes/thermo-nuclear/SKILL.md` — would need `da skills` to learn nested skill discovery (out of scope) and breaks the flat promote contract. This resolves `[[thermo-nuclear-lens-evaluation]]` §7.6 OQ-A.

### D2 — Migration is per-lens, NOT flag-day (RECOMMENDED)

Convert one lens at a time: land the template + `architecture-standards`'s two mode skills first, then `acceptance-invariants`, then `adversarial`. During migration the dispatcher (D3) reads either the inline-AGENT shape or the template+skill shape, so a half-migrated lens-set is coherent.

**Rationale:** per the `[[parallel-worker-branch-drift]]` lesson, concurrent edits to a shared template *and* the per-lens skill files *and* the dispatcher in one flag-day PR is exactly the partial-coherence trap — a merge conflict on the template silently strands a lens on the old shape. Per-lens migration keeps each PR's blast radius to one lens + the (idempotent) backward-compat dispatcher. This resolves `[[thermo-nuclear-lens-evaluation]]` §7.6 OQ-B (recommends path (b)).

### D3 — Dispatcher reads inline OR template+skill (backward-compat, time-boxed)

During migration the review-gate dispatcher detects, per lens, whether the AGENT.md is the legacy inline-rubric shape or the new template shape (presence of a `Load → <lens>.<mode>` directive / a `lens_template_version` frontmatter key is the discriminator). Legacy → spawn as today. New → resolve the mode from `lens_modes`, inject the `Load → <lens>.<mode>` instruction. Once all three lenses are converted (t7), the inline branch is ripped out and the discriminator becomes an assertion.

**Rationale:** lets D2's per-lens migration land without a dispatcher that breaks the not-yet-migrated lenses. The backward-compat branch is explicitly temporary — t7 removes it so the dispatcher does not carry two code paths forever.

### D4 — Default mode is declared, not implicit

Each lens template declares its default mode in frontmatter (`default_mode: standard`). The dispatcher's mode resolution order is: `lens_modes.<lens>` in `.agentsrc.json` → template `default_mode` → hard fallback `standard`. A lens that ships only a `standard` skill is valid; a lens with no `standard` skill is a scaffold error caught by copy-test.

**Rationale:** an explicit default in the template means the lens is self-describing (you can read the AGENT.md and know what runs with no config), and the `.agentsrc.json` field stays optional — the common case (everyone wants `standard`) requires zero config.

### D5 — Skill discovery via `da skills`

Mode skills are discoverable and promotable through the existing surface: `da skills list` shows `architecture-standards.standard` etc.; `da skills promote architecture-standards.thermo-nuclear` registers it in `.agentsrc.json` and syncs; `da refresh` links it for the active platform. No new discovery mechanism.

**Rationale:** D1's flat dotted names are exactly what `da skills` already operates on, so discovery is free. This is also what makes the "user-defined mode" future (§7) tractable — a user authors `architecture-standards.<their-mode>` via `da skills new`, sets `lens_modes.architecture-standards` to it, done.

---

## §4. Architecture

### §4.1 Lens template shape

A single template (`internal/scaffold/home/starter/agents/global/_lens-template/AGENT.md`, leading-underscore so copy-test can skip it as a non-instantiated source). Lens-invariant content lives here once; lens-specific slots are marked. Instantiated lens AGENT.md files (`<lens>-reviewer/AGENT.md`) are this template with the slots filled and **no rubric body**.

```markdown
---
name: <lens>-reviewer
description: Bounded reviewer for the <lens> lens. Receives a delegation bundle
  path or target reference; emits structured findings + a pass/fail verdict.
  Never edits production code.
tools: Bash, Read, Grep, Glob
lens: <lens>
default_mode: standard
lens_template_version: 1        # discriminator for D3 backward-compat
---

# Role
You are a bounded single-lens review worker. Your lens is **<lens>**.
You review one target through one lens — never multiple lenses, never the
implementation itself.

# Startup
<verbatim shared: bundle read, task-status confirm, target-exists check>

# Review execution
Load → <lens>.<mode>      # the dispatcher injects the resolved <mode>
Apply the loaded rubric to the target. No production edits.

# Findings format
<verbatim shared: severity/location/scenario/suggested_fix + verdict line>

# Closeout
Write findings to `.agents/active/review/<task_id>-<lens>.md`, then `/iteration-close`.
<verbatim shared: verify record → checkpoint → merge-back>

# Guardrails
<verbatim shared: no-edit, no-rerun, no-other-lens, no-orchestrator-commands>
```

The `<mode>` placeholder in `Load → <lens>.<mode>` is resolved by the dispatcher at spawn time and passed in the worker's prompt; the template ships with the `default_mode` value as the static fallback so a manually-invoked lens still loads something.

### §4.2 Per-mode SKILL.md shape

`internal/scaffold/home/starter/skills/global/<lens>.<mode>/SKILL.md`, orchestrator-pattern per skill-architect principle #1:

```markdown
---
name: <lens>.<mode>
description: Rubric for the <lens> lens in <mode> mode. Loaded by <lens>-reviewer
  at dispatch time. Not model-invocable on its own.
disable-model-invocation: true
---

# <lens> — <mode> rubric

## Concretely check
<the lens+mode-specific bullets — the only place rubric content lives>

## Severity calibration
<BLOCKER/HIGH/MEDIUM/LOW gradient for this lens+mode>

## Tone
<tone calibration — e.g. thermo-nuclear: "direct, demanding; do not soften
major maintainability findings into mild suggestions">

Load → instructions/rules.md      # for richer modes (thermo-nuclear), split out
Load → instructions/gotchas.md    # abstain-conditions (don't flag 1k lines on lockfiles)
```

`standard` modes can be a single SKILL.md (the rubric is short). `thermo-nuclear` mode splits into `instructions/rules.md` (the 7 non-negotiables) + `instructions/gotchas.md` (abstain conditions) + `templates/finding.md` per the §1 audit in `[[thermo-nuclear-lens-evaluation]]`.

### §4.3 Dispatcher wiring (`commands/workflow`)

Review-gate lens dispatch is currently a Task-spawn of the scaffold lens agent (no Go dispatcher enumerates lenses yet — confirmed by grep: lens dispatch is prompt/skill-driven today). When the layered-fanout lens-dispatch lands (`[[layered-pr-fanout-with-pr-open-status]]` §3.3), the dispatcher gains a mode-resolution step:

1. For each lens in the bundle's `lens_set`, resolve mode: `lens_modes.<lens>` → template `default_mode` → `standard` (D4).
2. Spawn the `<lens>-reviewer` worker with the resolved mode injected so its `Load → <lens>.<mode>` directive points at the right skill.
3. (D3) If the lens AGENT.md is still legacy inline-shape (`lens_template_version` absent), spawn as-is and ignore mode.

The dispatcher never branches *which agent* it spawns on mode — only *which skill the agent loads*. This keeps the agent-count == lens-count invariant the copy-test asserts.

### §4.4 `.agentsrc.json` `lens_modes` field

A new optional top-level object field, keyed by lens name → mode name, mirroring `verifier_profiles`:

```json
"lens_modes": {
  "architecture-standards": "thermo-nuclear",
  "acceptance-invariants": "standard",
  "adversarial": "standard"
}
```

Absent field or absent key = `standard`. Per `[[schema-usage]]`, adding this field touches all six sync points: struct + `agentsRCCore` mirror + `UnmarshalJSON` + `MarshalJSON` + `agentsRCKnown` map + `schemas/agentsrc.schema.json` (with `additionalProperties` constrained to string values and an enum is **not** used — modes are open-ended per D5 user-defined-mode future, so the value is `type: string`, validated against discovered skills at dispatch, not at schema time).

---

## §5. Migration tasks

Sequenced under a new sub-plan `lens-template-and-mode-skills` (cleanly separable; the thermo-nuclear decision-record is independently closed). Per D2, per-lens not flag-day.

1. **t1 — Scaffold the lens template.** write_scope: `internal/scaffold/home/starter/agents/global/_lens-template/AGENT.md`, `internal/scaffold/home/copy_test.go` (skip the `_`-prefixed source in the lens-count loop; assert the template exists separately). Verification: `go test ./internal/scaffold/...`.

2. **t2 — Convert `architecture-standards`.** write_scope: rewrite `architecture-standards-reviewer/AGENT.md` to the template shape; create `skills/global/architecture-standards.standard/SKILL.md` (default-loaded, carries today's `Concretely check` bullets) + `skills/global/architecture-standards.thermo-nuclear/SKILL.md` (re-cast from the cursor-team-kit thermo-nuclear rubric per `[[thermo-nuclear-lens-evaluation]]` §7.3 t2'). Verification: copy-test passes; manual prompt readback; `da skills list` shows both modes.

3. **t3 — Convert `acceptance-invariants`.** write_scope: rewrite its AGENT.md to the template; create `skills/global/acceptance-invariants.standard/SKILL.md`. Verification: copy-test; readback.

4. **t4 — Convert `adversarial`.** write_scope: rewrite its AGENT.md to the template; create `skills/global/adversarial.standard/SKILL.md`. Verification: copy-test; readback.

5. **t5 — Add `lens_modes` schema field.** write_scope: `internal/config/agentsrc.go`, `internal/config/agentsrc_test.go`, `schemas/agentsrc.schema.json`. All six `[[schema-usage]]` sync points. Verification: `go test ./internal/config/... ./commands/...`.

6. **t6 — Re-cast `[[lens-evidence-policy-renderings]]` references.** write_scope: `.agents/proposals/lens-evidence-policy-renderings.md`. The `lens_set`/`lens_tier_gate` entries that list `thermo-nuclear` as a separate lens become `architecture-standards` with `lens_modes.architecture-standards = thermo-nuclear` (tier-gating semantics survive — see §6). Verification: re-read renderings doc; confirm tier-gate examples still cohere with the lens collapsed to mode.

7. **t7 — Rip out backward-compat.** write_scope: dispatcher (`commands/workflow`, once lens dispatch lands), remove the D3 inline-shape branch; `lens_template_version` becomes a required-frontmatter assertion in copy-test. Depends on t2-t4 all landed. Verification: dispatcher tests; copy-test asserts every lens AGENT.md carries `lens_template_version`.

**Dependency order:** t1 → {t2, t3, t4} (parallelizable after t1) ; t5 independent (can run with t1) ; t6 after t2 (needs thermo-nuclear-as-mode to exist) ; t7 after t2+t3+t4.

---

## §6. Sanity check against parent proposals

- **`[[thermo-nuclear-lens-evaluation]]` §4 / §5 are SUPERSEDED** by this proposal's mechanism. The §4 *decision* (Hybrid B-leaning meld; no separate 4th lens) stands; the §5 task outline (inline AGENT.md bullets + mode-in-prompt) is replaced by §5 here (template + per-mode skills). Thread back as the §4.5 the seed's §7.5 asked for: "mechanism = template + per-mode skills, not inline mode text." The thermo-nuclear content is not deleted — it becomes the `architecture-standards.thermo-nuclear` skill (§5 t2).

- **`[[layered-pr-fanout-with-pr-open-status]]` §3.3 lens-count stays 3.** This proposal adds *modes*, not *lenses* — the copy-test lens-count assertion (`internal/scaffold/home/copy_test.go:149`, the three-name slice) is unchanged. The "(thermo-nuclear if added)" parenthetical in §3.3 / §5.1 of the parent should be dropped: there is no 4th lens, and thermo-nuclear is a mode of architecture-standards. The lens-as-merge-gate dispatch contract gains a mode-resolution step (§4.3) but the gate semantics are untouched.

- **`[[lens-evidence-policy-renderings]]` references re-cast (t6).** The doc's `lens_set` / `lens_tier_gate` entries treating `thermo-nuclear` as an addressable lens become `architecture-standards` + `lens_modes.architecture-standards = thermo-nuclear`. Tier-gating expressive power is preserved: "run thermo-nuclear only when tier1 passes" becomes "run architecture-standards in thermo-nuclear mode at tier2" — same compute-saving behavior, one lens. The app-type routing in Strategy C ("NO thermo-nuclear on prompt PRs") becomes "architecture-standards in standard mode on prompt PRs" — equally expressive.

---

## §7. Open questions

- **OQ-1 — Mode-skill testing via `eval/` subdir.** Each mode skill is a review rubric; per skill-architect principle #8 the high-stakes ones need an `eval/` layer. Proposal: `skills/global/<lens>.<mode>/eval/checklist.md` (pass/fail gate: "did the rubric flag at least one in-band concern when one demonstrably exists; did it abstain on the documented out-of-band cases; did it emit the structured finding format"). Open: do we gate t2-t4 on eval coverage, or land rubrics first and add evals as a t8 hardening pass? Lean: evals are a follow-up (don't block the structural migration).

- **OQ-2 — Multi-mode-per-PR.** Can a single PR run a lens in two modes (e.g., architecture-standards in both `standard` and `thermo-nuclear`)? Today `lens_modes.<lens>` is a single value. A future `lens_modes.<lens>: [standard, thermo-nuclear]` array would spawn two workers for one lens — but that re-introduces the double-verdict reconciliation cost the thermo-nuclear meld was designed to avoid. Defer; single-mode-per-lens-per-PR for v1.

- **OQ-3 — User-defined modes.** D5 makes `da skills new architecture-standards.<custom>` + `lens_modes.architecture-standards = <custom>` work end-to-end. Open: do we validate that a configured mode resolves to an installed skill at `da refresh` time (fail-fast) or at dispatch time (fail-soft to `standard`)? Lean: warn at `da refresh`, fall back to `standard` at dispatch so a typo never hard-blocks a review.

---

## §8. Cross-links

- `[[thermo-nuclear-lens-evaluation]]` §7 — the seed (maintainer redirection). This proposal is the sibling §7.5 asked for. PR #153.
- `[[layered-pr-fanout-with-pr-open-status]]` §3.3 — lens dispatch contract this proposal's §4.3 extends. Merged as #149.
- `[[lens-evidence-policy-renderings]]` — tier-gating / app-type routing, re-cast by t6.
- `[[schema-usage]]` — the six-point AgentsRC field sync discipline for t5.
- `[[parallel-worker-branch-drift]]` — the lesson grounding D2's per-lens (not flag-day) migration.
- `[[verifier_profiles]]` (`.agentsrc.json`) — the object-keyed-by-name precedent for D1 naming and the §4.4 `lens_modes` field shape.
