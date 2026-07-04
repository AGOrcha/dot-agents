# corpus-scan — Phase 1 Research Corpus Scan Rules

Rules for Phase 1 research corpus scan:

- Primary target: research/articles-evaluation-kg-and-adjacent.md
  - §C entries: scan for proposal IDs that overlap with topic keywords
  - §B themes: pull theme titles; flag any that the topic touches
  - §A entries: note the 3-5 most relevant article evaluations
- If no §C proposals match, note "no prior research proposals on this topic."
- **Flagged summary escalation**: if a §A evaluation entry will be used to justify a spec
  decision (not just background), and that entry carries a citation-presence, freshness,
  or rubric-check flag (per Part G of the corpus doc), you MUST read the underlying article
  file at research/articles/<filename>.md before including the finding in the briefing.
  Do not promote a flagged secondary summary as authoritative spec rationale.
- For non-decision background context, §A summaries are sufficient — escalation is only
  required when the finding will directly shape a spec decision or done criterion.
- Record the doc line numbers for any §C proposals found (enables later cross-reference).
