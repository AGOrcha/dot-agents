## Extract: The Problem is Prompt Debt (@dbreunig / Drew Breunig)

**Source**: https://x.com/dbreunig/status/2069455716478603536 (body: dbreunig.com blog cross-post)
**Author**: Drew Breunig (@dbreunig)
**Date**: 2026-06-22
**Method**: Playwright (tweet) + WebFetch (author blog)
**Raw archive**: research/articles/prompt-debt.md

### Summary

Hand-tuned NL prompts accrue as technical debt and lock you to one model. Specify behavior with evals/typed specs, not prose; search/optimize prompts (DSPy/GEPA) instead of hand-writing. Validates our KG-as-SOT direction; challenges our own large NL skill/agent prompts.

### Key Quotes

> "You can't be model agnostic if you're hand-tuning prompts."

> "Once we have metrics that can score candidates, the prompt is no longer something to craft but something for which to search."

---

## Key claims
- Hand-written natural-language prompts are great for prototypes but accrue as **technical debt** in production; cost arrives slowly until the app can "barely move."
- NL is the wrong specification language: its imprecision + probabilistic models yields brittle output, including **spurious cross-instruction interference** (unrelated statements change refusal/answer behavior).
- Three failure modes: (1) slowing iteration from accreted edge-case patches, (2) team paralysis as prompts become unreadable, (3) **model lock-in** — tuned prompts don't port across models.
- "Fighting the weights" (repeating instructions with increasing severity) is endemic — Claude Code tells Opus 7x to return multiple tool calls; Fable repeats a copyright rule 6x.

## Techniques / prescription
- Specify behavior with **measurements (evals, metrics, typed specs)**, not prose — legible shared artifacts colleagues can contribute to.
- **Stop writing prompts by hand**: once you can score candidates, the prompt is something to *search for*. Use DSPy / GEPA to optimize against your design.
- Result: model-agnosticism — evaluating a new model takes hours, deprecation becomes a chore not a fire drill.

## What's novel
- The "prompt debt" framing and the sharp line: **"You can't be model agnostic if you're hand-tuning prompts."** Reframes prompt quality as a *portability/durability* problem, not a wording-craft problem.
- Historical analogy: assembly->compilers, hand queries->planners; prompting should automate next.

## Mapping to our work
- **KG-as-SOT for SDD artifacts (memory theme) + planner-evidence work:** Our system already pushes "specify behavior as evals/typed specs, not prose" — verifier/reviewer stage profiles, eval-backed write_scope, evidence-backed planning. Breunig is independent validation that the *spec/eval-as-canonical, prose-as-projection* direction is the durable one. Our committed YAML = "regenerable projection," prose indexed into the semantic view — same instinct.
- **stage-profile consolidation / app-type-profiles:** "different words = different outputs; gaps get filled with in-distribution choice" argues for keeping behavior in structured profiles + evals (which we have) rather than free-text agent prompts. Supports tightening any remaining prose-heavy agent briefs into typed contracts.
- **Challenge / open tension:** Our skills and agent briefs ARE large hand-tuned NL prompts (CLAUDE.md, skill instructions, delegation bundles). Breunig's thesis implies these accrue prompt debt and quietly lock us to a model family. A KG that stores skills/rules as typed nodes with attached evals (the operational view) is the antidote — but we don't yet auto-optimize prompts against evals.
- **Concrete proposal idea:** A `prompt-debt audit` lens — query the KG operational view for skills/rules with no attached eval and high edit-churn (the "fighting the weights" signature: repeated/all-caps instructions), and flag them as debt. Pairs naturally with the result->skill/rule correlation edges in work-tracking §3A.

## Caveats
- WebFetch returned a digested render; long quotes are verbatim, connective summary is condensed. Example model names (GPT-4o/5.4/5.5, Fable) are as stated by the author.
