# Corpus Scan

Rules for Phase 1 research corpus scan:

- Primary target: `research/articles-evaluation-kg-and-adjacent.md`.
- In section `§C`, scan for proposal IDs that overlap with the topic keywords.
- In section `§B`, pull theme titles and flag any the topic touches.
- In section `§A`, note the 3-5 most relevant article evaluations.
- If no `§C` proposals match, record `no prior research proposals on this topic`.
- **Flagged summary escalation**: if a `§A` evaluation will be used to justify a spec
  decision rather than only background context, and that evaluation carries a
  citation-presence, freshness, or rubric-check flag, read the underlying
  `research/articles/<filename>.md` file before including the finding in the briefing.
- For non-decision background context, `§A` summaries are sufficient; escalation is only
  required when the finding will directly shape a spec decision or done criterion.
- Record source line numbers for any `§C` proposal hits so downstream phases can cross-link
  them precisely.
