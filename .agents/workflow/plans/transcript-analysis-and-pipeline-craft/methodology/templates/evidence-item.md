# Evidence Item

```yaml
item_id: <slug>                # stable; referenced by synthesis + reviews
claim: >
  <one falsifiable sentence>
class: mechanism | failure | cost | craft | outcome
anchors:
  - ref: <evidence_id>#L<line>       # or <evidence_id>@<timestamp>
    digest: sha256:<of raw anchored line(s)>   # rubric R4
    excerpt: >                        # redacted, minimal span (R2/R3)
      <quoted span>
sensitivity: public-ok | internal | sensitive   # rubric R5; sensitive => no excerpt
confidence: high | medium | low   # per rubric §3
inference_flags: []            # any reconstructed values, e.g. stage-timing
rubric_version: <score sidecar version if score-derived, else null>
related_prior: []              # IDs from payout/adminapp reports, if convergent
```

Notes:
- No anchor → not an item; goes to gaps/unknowns.
- Repeated observations = separate items (no dedup at collection).
