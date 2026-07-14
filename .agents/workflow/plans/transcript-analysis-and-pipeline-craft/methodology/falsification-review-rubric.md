# Falsification-First Review Rubric

Applies the scientific-method spine (`scientific-method-spine-domain-general.md`, kg-ideate /
iteration-cycle) to review of BOTH the analysis artifacts and the pipeline-craft deliverables.
Rubber-stamp approvals are void: **a review with zero attempted refutations is returned as
not-performed.**

## Review contract

1. **Pre-registration.** Before examining the work product, the reviewer writes down:
   - ≥2 falsifiable hypotheses about how the work could be wrong
     (e.g. "the cheap-tier frontier claim disappears when cache-regime is controlled",
     "the skill's workflow fails on a repo without `.agents/workflow/`").
   - The concrete test for each (command, query, or counter-example construction).
2. **Execution.** Each hypothesis is *run*, not argued: re-query the transcripts, re-run the
   scorer, construct the counter-example. Outcome per hypothesis: `refuted-the-work` |
   `survived` | `inconclusive` (+ why).
3. **Null results are first-class.** "I tried X to break it and it held" is recorded with the
   same anchor rigor as findings. Unrun hypotheses are listed as review debt, not dropped.
4. **Verdict.** `accept` requires: all pre-registered hypotheses executed or explicitly waived
   with reason; zero `refuted-the-work` outcomes unresolved. Anything else → `reject` with the
   refuting evidence attached.
5. **Cross-family gate.** For substantive slices, the blocking adversarial review runs on a
   different model family than the executor (RULE 7; `cross-harness-adversarial` lens). Same
   family both sides = review invalid. The lens family is **pinned opposite the executor
   family**, never fixed to a constant: a claude-family executor takes the gpt-family lens
   (`cross-harness-adversarial`, `gpt-5.4`/`gpt`); a gpt-family executor takes the claude-family
   lens (`cross-harness-adversarial-claude`, `claude-opus-4-8`/`claude`). Opposite-pinning is the
   correct application of RULE 7 (families differ both sides), not an exception to it — it is
   exactly what avoids the same-family invalidity. For the Pareto live waves this is a
   review-VALIDITY stage excluded from the measured frontier cell; see
   `pareto-measurement-rubric.md` "Cross-family adversarial gate — validity stage".

## Reviewer output schema (per review)

```yaml
review:
  target: <artifact path or PR>
  reviewer_model_family: <family>       # must differ from executor family on blocking gate
  hypotheses:
    - statement: <falsifiable claim>
      test: <what was actually run>
      outcome: refuted-the-work | survived | inconclusive
      evidence: [<anchors / commands / diffs>]
  unrun: [<hypothesis + reason>]        # review debt, carried forward
  verdict: accept | reject
```

## Anti-patterns (auto-reject the review itself)

- Qualitative-only commentary ("looks solid", "LGTM") with no executed test.
- Hypotheses chosen to be unfalsifiable or trivially survivable.
- Verdict `accept` with `inconclusive` outcomes on load-bearing claims.
