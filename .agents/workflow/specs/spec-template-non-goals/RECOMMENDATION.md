# RECOMMENDATION — `spec-template-non-goals`

**Lead:** `.agents/active/ideation-inbox/obs-spec-template-non-goals-block.md`
**Decision:** `RECLASSIFY-to-skill/convention` (spec-scaffold — NOT a spec+plan)
**Reclassification target:** `spec-scaffold` skill family — template + step-9 structure line + one new instruction file. Secondary doc touch: `docs/concepts/workflow-artifact-model.md` §1.

---

## Phase 1 brief (grounded)

- **KG:** `da kg query --intent repo_context "spec non-goals section convention"` → *No results found* (KG thin on this topic; fell back to corpus/skill inspection per kg-brief).
- **Lessons:** no `.agents/lessons/index.md` entry on spec non-goals / spec templates (grepped `non-goal|spec.*template|workflow-artifact-model`).
- **Rules:** no `non-goal` rule anywhere under `.agents/` or `~/.agents/rules/`. No standalone spec-template rule exists — spec authoring convention is owned entirely by the `spec-scaffold` skill.
- **Existing plans:** none of the four candidate active plans (`agent-ops-hardening`, `full-loop-orchestration-runtime`, `config-transitive-layering`, `config-v2-migration`) own spec-authoring convention. No merge target.
- **Root cause of the observation:** the current spec template (`spec-scaffold/templates/spec-output.md`) ships `## Goals` and `## Deferred` (defined as *"Explicitly out of scope for this spec/plan"*) but **no `## Non-Goals`**. `Deferred` currently conflates *never* with *later* — exactly the failure the lead flags: conflating the two re-surfaces settled scope. `docs/concepts/workflow-artifact-model.md` §1 has the same conflation ("explicitly deferred (out-of-scope) items").

**Why reclassify (not spec+plan):** this is a one-shot convention edit to an existing skill's template and instructions (the actual text is below), with no behavioral system to design, no open questions affecting ordering, and no cross-file write-scope worth a plan. It is purely mechanical documentation-of-convention.

---

## Drafted changes (the actual text to add)

### Change 1 — `spec-scaffold/templates/spec-output.md`

Insert a `## Non-Goals` section immediately after `## Goals`, and reword `## Deferred` so the *never / later* boundary is explicit and non-overlapping. Path: `internal/scaffold/home/starter/skills/global/spec-scaffold/templates/spec-output.md`.

Template body (inside the ```markdown block), replace the `## Goals` block through `## Deferred` with:

```markdown
## Goals
<Bulleted, scoped outcomes — not implementation steps>

## Non-Goals
<Things this spec will NEVER do — a permanent scope boundary, not a "not yet".
Each entry states what is excluded and why, so a future reader does not re-open
settled scope. NEVER conflate with Deferred: a non-goal is never; a deferred item
is later. If an entry could plausibly become a goal in a follow-up, it is Deferred,
not a Non-Goal.>

## Decisions
<Each decision: what was chosen, why, what alternatives were rejected>

## Requirements
<Behavioral requirements — no file paths, no function names>

## Open Questions
<Questions this spec leaves open; the plan must resolve them>

## Done Criteria
<Verifiable: exit code / file exists / test passes / metric threshold>

## Deferred
<Explicitly out of scope for THIS spec/plan but valid future work — "not now,
later". Distinct from Non-Goals (never). A deferred item is a candidate goal for
a follow-on spec/plan.>
```

### Change 2 — `spec-scaffold/SKILL.md` step 9 structure line

Path: `internal/scaffold/home/starter/skills/global/spec-scaffold/SKILL.md`. In step 9, update the enumerated structure line (currently lines 61-62) to name non-goals as its own section:

```
   Structure: problem statement, goals, non-goals, decisions with rationale,
   requirements (behavioral), open questions, done criteria, deferred items.
   Non-goals and deferred items are distinct: a non-goal is never; a deferred
   item is later. Do not merge them.
```

### Change 3 — new instruction `spec-scaffold/instructions/non-goals.md`

Path: `internal/scaffold/home/starter/skills/global/spec-scaffold/instructions/non-goals.md`. New file capturing the distinction + the "penalty-for-being-wrong" section-inclusion test (the lead's Part J.7 / Part C.11 pairing). Add `non-goals` to `spec-scaffold/SKILL.md` frontmatter `calls:` and load it as a new step (8.5, between done-criteria and write).

```markdown
# Non-Goals & Section-Inclusion Test — Phase 2 Step 8.5

Every spec carries an explicit **Non-Goals** block, separate from **Deferred**.

## Non-goal vs deferred (hard distinction)
- **Non-goal = never.** A permanent scope boundary. Stating it stops a future
  reader (or agent) from re-opening scope the spec deliberately excluded.
- **Deferred = later.** Valid future work, out of scope for THIS spec/plan only.
- Conflating the two re-surfaces settled scope — the exact failure this block
  prevents. When unsure which bucket an item belongs in, ask: *"Could this
  plausibly become a goal in a follow-up?"* Yes → Deferred. No → Non-Goal.

## The penalty-for-being-wrong section-inclusion test
Before including OR omitting any spec section (Non-Goals included), apply the
hard test: **"What is the penalty for getting this wrong / leaving it out?"**
- High penalty (silent scope creep, re-litigated decisions, wrong thing built)
  → the section earns its place; include it.
- No penalty → omit it; do not pad the spec with boilerplate sections.
- Non-Goals fails-open toward inclusion: an empty Non-Goals block ("none — all
  reasonable scope is either a goal or deferred") is a valid, deliberate answer,
  but the section is not silently dropped.

Common false positive: an item written as a Non-Goal that is really Deferred
(a "not yet" dressed up as a "never"). Re-classify it — a mislabeled Non-Goal is
worse than none, because it forecloses legitimate future work.
```

### Change 4 (secondary, doc) — `docs/concepts/workflow-artifact-model.md`

§1 · Spec (line 57) reword *"and explicitly deferred (out-of-scope) items"* → *"explicit non-goals (permanent scope boundaries) and deferred (not-now-but-later) items — kept distinct so settled scope is not re-litigated."* Keeps the concept doc consistent with the template.

---

## 3-line summary

1. **Brief found** the spec template already has `Goals`/`Deferred` but no `Non-Goals`, and `Deferred` conflates *never* with *later* — the precise drift the lead (refactoringenglish J.7 / C.11) warns about; KG/lessons/rules carry nothing on this, and no active plan owns spec convention.
2. **Decision:** RECLASSIFY to a `spec-scaffold` skill-convention edit (template `## Non-Goals` section + reworded `## Deferred` + step-9 structure line + new `instructions/non-goals.md` with the penalty-for-being-wrong section-inclusion test), not a spec+plan.
3. **Key open question:** should the Non-Goals block be *mandatory* on every spec (fail-open with an explicit "none" entry, as drafted) or *conditional* on specs with contested scope — i.e. how hard is the section-inclusion test allowed to prune it?
