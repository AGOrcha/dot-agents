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
- Later evaluation parts are cumulative and dated (Parts D+ …) — scan the parts whose batch
  themes touch the topic, not only §A-§C; the distilled layer in research/extracts/*.md is a
  faster first index into recent batches.

## Corpus gap trigger (outside evidence missing)

If the scan finds the corpus LACKS coverage the topic needs — the idea rests on external
claims, tools, or prior art the corpus has never ingested, or the user pointed at outside
sources — do NOT paste raw URLs or unvetted summaries into the briefing. Dispatch
`research-intake` in ideation-gather mode (`--topic <topic>`, default `--targeted`): it
extracts properly (fetch-tier escalation, save to research/articles/, dedup/backfill) and
appraises each source (evidence grading, [UNVERIFIED] flags), then RETURNS pointers to the
saved files. Re-run this corpus scan over them and cite the corpus files in the briefing —
the brief is the corpus's read path; research-intake is its write path. Bounded: gather what
the topic/fork needs, not a broad crawl; the full-pass review subtree is reserved for batches
informing permanent/live-with-it decisions (research-evaluate method §0). If research-intake
is not installed, note the gap in the briefing instead — do not improvise extraction inline.
