# Research evaluation (targeted) — context-engineering batch → fold-back redesigns

Depth: **targeted** (§0 gate) — these inform bounded, reversible fold-back design decisions, not a
methodology freeze. Freeze discipline: the transcript-analysis wave decisions stay settled; every
recommendation below is additive / gate-neutral or a parked lead.

## §1 Question & scope

Which of the five open fold-backs do these sources inform, and how?
`cross-family-gate-not-fail-closed-specialized`, `cross-family-lens-autoswap`,
`review-gate-deterministic-pre-llm-tier`, `craft-doc-stable-location`,
`plan-archive-foldback-sweep`.

## §2 Corpus & selection

| # | source | genre | file |
|---|---|---|---|
| S1 | long-horizon-prompting skill (muratcankoylan/Agent-Skills-for-Context-Engineering) | practitioner skill repo, `claim-*`-backed | `articles/long-horizon-prompting-context-engineering.md` |
| S2 | "Context rot" X-native article (@0xCarnagee) | growth/creator genre, secondhand study summary | `articles/context-rot-million-token-window-study.md` |
| S3 | LangChain-founder context-engineering note + 40-min video (@0xCodez) | growth/creator genre, secondhand; video untranscribed | `articles/langchain-context-engineering-compaction.md` |

**Selection bias (a finding, not housekeeping):** all three are user-dropped links; S2 and S3 are
same-cluster (S3 quote-tweets S2, both `@zscdao`-affiliated creators). Convergence between S2/S3 is
NOT independent. S1 is independent of S2/S3. Genre caps weight: S2/S3 corroborate *design shape*,
never effect sizes; S1's numbers are `claim-*`-backed but pending its `researcher/claims/index.jsonl`.

## §3 Per-source appraisal

**S1 — long-horizon-prompting.** Claims: pseudo-formal brief (definitions/predicate/non-counting/
auditor-failure-modes); "never add a persistence instruction without a matching verification gate";
**fresh-context adversarial verifiers > self-critique** (a verifier that didn't build the artifact
can't rationalize its gaps); **role labels don't create diversity — engineer independence**;
inter-agent agreement is a diversity-failure signal; **move optimization-pressure constraints to the
harness — prompt constraints are advisory**; externally re-injected progress ledger. Grade:
`practitioner-report` + `measured-with-method` for the cited studies (METR persistence/hacking,
Anthropic multi-agent, vendor coding-agent evals) — pending the repo's claim index (chase). Bias:
skill-repo synthesis, low commercial incentive; strong for design shape and citable mechanism.

**S2 — context rot.** Claims: Chroma 18 models / 194K calls, every model degrades with length;
effective context 50-70% of advertised (RULER cross-check); middle "death zone" (−30+ pp at
positions 5-15); length-only floor −7.9%; **shuffled context beats coherent past 32K**; distractors
scale with window; **GPT hallucinate vs Claude give-up (complementary failure modes)**;
autoregressive output→input rot; PART-10 recs (25-30% working cap, needle-in-the-middle eval,
raw-chunks+headers, compact at 60%, read context as data). Grade: underlying Chroma/RULER/MRCR are
`measured-with-method`, but this is a `measured-opaque`/secondhand marketing-genre SUMMARY — numbers
are [UNVERIFIED]. Chase: primary Chroma "context rot" report; RULER; MRCR v2; Anthropic Opus-4.7
system-card "keep 4.6 for multi-needle."

**S3 — LangChain context engineering.** Claim: windows aren't infinite → must compact;
**compaction + file systems + memory = context engineering** for long-horizon agents. Grade:
`asserted`/`practitioner-report` (secondhand tweet; 40-min video untranscribed). Bias: creator
genre. Design shape only. Chase: transcribe the interview if mechanics need to inform impl.

## §4 Current-state comparator (the five fold-backs)

- Cross-family gate: code refuses same-family (`pipeline_projection.go:409`), specialized profiles
  may omit it (CrossFamily==nil); no fallback when only one family is available.
- Lens autoswap: no code auto-selects the opposite-family lens; it's a manual wave-prep pin, keyed
  on a declared `model_family` STRING (no capability/registry awareness).
- Review gate: single LLM-lens tier; no deterministic pre-LLM tier; verifier stage likewise.
- Craft doc: plan-scoped, deferred relocation.
- Archive sweep: overlaps the fold-back-resolution-lifecycle proposal.

## §6 Findings → graded recommendations (route onward to the fold-backs)

| # | recommendation | grade | routes to |
|---|---|---|---|
| R1 | **Cross-family fallback = fresh-context adversarial with the OPPOSITE family's failure-mode as the audit hunt-list.** When the registry offers no opposite family, emulate it (fresh-context, different-blind-spot brief) rather than skipping; keep the gate REQUIRED-when-configurable, harness-enforced not prompt-advisory. GPT-lies / Claude-gives-up (S2 PART 7) IS the enumerated hunt-list. | adopt (S1 design-shape + S2 mechanism) | `cross-family-gate-not-fail-closed-specialized` |
| R2 | **Capability-aware autoswap:** select the opposite lens by eval-measured capability/failure-mode COMPLEMENTARITY against the executor, filtered by the model registry of what's available — not the declared family string. "Role labels don't create diversity" (S1); complementary failure modes (S2) are the capability axis to maximize. | adopt (S1) + sharpen (S2) | `cross-family-lens-autoswap` |
| R3 | **Deterministic pre-LLM tier (review AND pre-verifier):** harness-enforced fast checks before spending lens/verifier tokens (S1 "constraints that must survive optimization pressure live in the harness"). Candidate deterministic checks from S2 PART-10: context-budget cap (25-30% of advertised window), middle-position exposure eval, compaction-at-60% trigger, raw-chunks-vs-coherent structure check. | adopt (S1) + sharpen (S2 recs) | `review-gate-deterministic-pre-llm-tier` |
| R4 | **Context-budget / compaction section in the craft** when relocated: cite context-rot for the "short, structured, <30% effective" rationale; compaction+filesystem+memory triad (S3) as the long-horizon context contract. | sharpen (S2/S3 design-shape) | `craft-doc-stable-location` |
| R5 | Agreement is a diversity-failure signal, not corroboration; treat same-cluster source convergence (S2/S3) and same-family reviewer agreement as weak evidence. | sharpen | methodology (falsification-review advisory) + `cross-family-lens-autoswap` |
| — | `plan-archive-foldback-sweep` — no research bearing (workflow-design merge). | n/a | — |

## §7 Limitations & chase list

- S2/S3 numbers are secondhand marketing-genre summaries — **[UNVERIFIED]**: (1) primary Chroma
  context-rot report (18 models / 194K calls, −7.9% length floor, position −30pp, shuffled>coherent);
  (2) RULER effective-context 50-65%; (3) MRCR v2 %s + Anthropic Opus-4.7 system-card fallback note;
  (4) S1's `claim-*` index. (5) LangChain interview video untranscribed.
- Cannot conclude effect sizes; design-shape adoption only. R1-R4 are additive to the (frozen)
  wave; none reopens a settled decision.
