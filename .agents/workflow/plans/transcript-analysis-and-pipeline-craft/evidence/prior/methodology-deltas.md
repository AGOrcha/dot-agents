# Methodology deltas — prior toolkit vs our rubrics

What the prior analyses' method actually *did* (mechanically, per the `workflow-evidence-analysis`
toolkit they shipped) versus what our `methodology/` rubrics add on top. Every "their method" claim
cites `file:line` in the adminapp-agc toolkit (all under
`adminapp-agc @ feature/analysis-doc : tools/workflow-evidence-analysis/`, ref `bce2a6e7…85a54`).
"Ours" cites our rubric files under
`.agents/workflow/plans/transcript-analysis-and-pipeline-craft/methodology/`.

Toolkit script/schema digests (R4, whole-file sha256, `feature/analysis-doc`):
`correlate-case-study.py`, `score-workflow-evidence.py`, `export-session-events.py`,
`inventory-transcript-sources.ps1`, `render-case-study-report.py` and the three
`schema/*.json` — re-derivable via `git show feature/analysis-doc:<path> | shasum -a 256` in the
adminapp-agc checkout; refs are stable on that branch.

---

## 1. Anchor-based correlation

**Their method — substring anchor match, OR-any, no verbatim-anchor requirement.**
`correlate-case-study.py:matches()` treats a record as relevant if the session-id is in the anchor
set *or* any keyword/repo/story substring appears in a flattened lower-cased haystack
(`session_id + title + repo_root + branch + content + source_file`):

- `scripts/correlate-case-study.py:31-40` — `record_text()` concatenates those six fields, lower-cased.
- `scripts/correlate-case-study.py:42-50` — `matches()`: `session_id ∈ anchors` **or** first substring hit in `("keywords","repos","stories")` returns `True`.
- `scripts/correlate-case-study.py:53-64` — `correlate()` builds `matched_events` by straight filter, then matches inventory artifacts by dumping each source to JSON and substring-testing the same anchor union.

An anchor is therefore a *retrieval selector*, not a *provenance record*: a matched event/artifact
does not carry back a per-claim citation. The rendered report then quotes up to the first 50 matched
events with 240-char snippets (`scripts/render-case-study-report.py:31-38`), and no report line is
required to name a specific record line/offset.

**Ours — verbatim, per-claim, digest-auditable anchors (adds provenance, keeps their retrieval).**
- Rubric E1 (`evidence-rubric.md:30-32`): every claim carries ≥1 anchor; no anchor → the claim goes to gaps/unknowns, never to findings — the inverse of "prose finding stands on its own".
- Anchor format is a locatable offset (`evidence_id#L<line>` or `@<timestamp>`), `evidence-rubric.md:27-28`.
- R4 (`evidence-rubric.md:56-59`): each committed item records `sha256` of the raw anchored line(s) + the redacted excerpt + surrounding context so a reviewer without machine access still audits, and one with access verifies digest↔raw. The toolkit has no digest field anywhere (see §5).

Delta: we **keep** their substring correlation as the discovery step but require every synthesized
claim to resolve to a verifiable, digested offset — provenance the toolkit never enforced.

## 2. No dedup at correlation time

**Their method — repeated observations are separate rows; convergence is never collapsed.**
- `scripts/correlate-case-study.py:54` — `matched_events` is a plain list comprehension (no set/key dedup).
- `scripts/correlate-case-study.py:57` — the only aggregation is `Counter(source_kind)` for a summary count; individual events are neither merged nor deduped.

**Ours — same behavior, made explicit and pushed to a named phase.**
- Rubric E2 (`evidence-rubric.md:33-35`): "no dedup at correlation time (matches `correlate-case-study.py` behavior)… convergence is computed at synthesis, not collection."

Delta: **inherited, not changed.** We deliberately adopt their non-dedup collection and name the
downstream `synthesis` task (`TASKS.yaml`) as the single place convergence is computed. This is the
one axis where we ratify their method verbatim.

## 3. Coarse count scoring

**Their method — count/threshold ratings driven by substring presence; self-declared coarse.**
`score-workflow-evidence.py` derives five ratings purely from counts of matched events/artifacts and
substring probes over dumped artifact JSON:

- transcript coverage — `scripts/score-workflow-evidence.py:23-27`: `high` if `event_count>=25` or `>=3` source kinds; `medium` if `>=5`; else `low`.
- canonical artifact coverage — `:29-33`: `high` if `artifact_count>=5`; `medium` if `>=1`.
- verification coverage — `:35-37`: `medium` if any artifact matched, upgraded to `high` if the substring `"result"`/`"verification"` appears anywhere in a dumped artifact.
- delegation-bundle richness — `:39-43`: `high` if substring `"merge-back"` present; `medium` if `>=2` artifacts.
- time-anchor confidence — `:45-49`: `high` if a `time_window` exists **and** `artifact_count>=1`; `medium` if either.
- Self-declared limits — `:57-60`: "coarse first-pass score intended to prioritize analyst review, not replace it… overlays can refine thresholds."

Scoring is one-directional (a rating, no dominance), version-blind, and has no notion of a unit of
work below the case study.

**Ours — two separate, stricter scoring regimes replace the single coarse pass.**

(a) Evidence confidence grading — anchor-count + primary-source based, not raw-count based:
- `evidence-rubric.md:64-67`: `high` = ≥2 independent anchors incl. ≥1 primary transcript; `medium` = 1 primary anchor; `low` = secondary-only; a `low` finding cannot be an actionable outcome alone.
- Version-aware scoring E4 (`evidence-rubric.md:37-40`): sidecar comparisons only within the same `rubric_version` (3.0.0 vs 2.1.0); mixed-version aggregates flagged. Their scorer has no version dimension at all.

(b) Pareto measurement — introduces a **stage-run unit** and explicit dominance the toolkit lacks:
- Unit of measurement (`pareto-measurement-rubric.md:7-11`): one row per `(session_id, iteration)`, blocked into `model_family × task_class × cache_regime × retry_regime` cells; never compare across cells.
- Primary unit = **stage-run** (`pareto-measurement-rubric.md:24-25`): one `(stage, model, task, wave)` execution with its own tokens/cost/wall-clock; task rows aggregate stage-runs. The toolkit's smallest unit is the case study/session (`correlate-case-study.py` operates on whole sessions).
- Explicit dominance directions (`pareto-measurement-rubric.md:26-28`): minimize cost/volume/wall-clock, maximize accuracy; a point dominates iff ≤ on all minimized axes, ≥ on accuracy, strict on ≥1. Their score emits independent per-dimension bands with no dominance relation.
- Wall-clock decomposition + critical-path (`pareto-measurement-rubric.md:29-32`): model latency vs tool vs queue, parallel overlap credited not summed. Their score has no timing decomposition (time-anchor is a boolean-ish `high/medium/low`).

Delta: we replace a single substring/count rating with (i) primary-anchor-weighted confidence, (ii)
version-matched scoring, and (iii) a stage-run-unit 4-axis Pareto frontier with explicit dominance —
none of which exist in `score-workflow-evidence.py`.

## 4. Timestamps (mtime dependence)

**Their method — content timestamps where present, but mtime/ctime/git-commit as first-class fallback anchors.**
- Content-derived: `scripts/export-session-events.py:69` reads `obj.get("timestamp")` from the record; `:65-78` is the normalized event.
- But the inventory layer uses filesystem mtime directly: `scripts/inventory-transcript-sources.ps1:32` and `:52` record `LastWriteTime` as `latest_write_time`.
- And both analysis reports use mtime/ctime as an explicit time anchor: `EFF#L87` ("history-only artifacts: use git commit time… otherwise use filesystem create/modify time as a weaker fallback"), `BEH#L62-63` (APPDEV-9175 "git commit + file mtime"; APPDEV-7817 "file ctime/mtime fallback").

**Ours — mtime is banned for time anchoring.**
- `evidence-rubric.md:16` (§1): `started_at`/`ended_at` = "first/last record timestamp (UTC ISO-8601); **never mtime**."
- Reinforced in the source-inventory template (`templates/source-inventory.md:3`): "Timestamps from records, never mtime."
- Reconstructed values (e.g. stage timing from `wallTimeMs`) must carry `[INFERENCE]` (E5, `evidence-rubric.md:41-43`; pareto `:20`), never silently mixed with recorded values.

Delta: their strongest-available-anchor policy admits mtime/ctime when content lacks a timestamp; we
forbid mtime entirely and route any non-recorded value through an explicit `[INFERENCE]` flag.

## 5. Redaction / data handling

**Their method — no redaction, secret-scan, or sensitivity gate; schemas are open and store full metadata.**
- `scripts/export-session-events.py:97` dumps the entire upstream metadata blob into the record (`"metadata": metadata`); `:74,:96` only truncate free-text `content` to 1200 chars via `shorten()` (`:25-29`) — a length cap, not a secret scan.
- `schema/session-event.schema.json:23` — `"additionalProperties": true` (also timeline `:24`, score `:24`): any field, including secrets, is schema-valid.
- No `sha256`/`digest`, `sensitivity`, `secret_scanned`, or `redacted` field appears in any of the three schemas (`schema/session-event.schema.json:11-22`, `schema/evidence-timeline.schema.json:11-23`, `schema/evidence-score.schema.json:13-23`).
- The reports quote raw session titles/first-user-messages and Windows home paths verbatim (e.g. `EFF#L30`, `BEH#L16-21`) — acceptable for their internal use, but there is no gate enforcing it.

**Ours — a blocking redaction gate R1–R5 before any excerpt leaves a harness dir.**
- R1 (`evidence-rubric.md:49-51`): raw transcripts never committed; `evidence/` MUST NOT contain `.jsonl` session copies.
- R2 (`evidence-rubric.md:51-53`): secret scan (gitleaks/equivalent) over any excerpt first; a hit = redact or drop.
- R3 (`evidence-rubric.md:54-56`): minimize excerpts; strip absolute home paths to `~`-relative; strip non-public identifiers.
- R4 (`evidence-rubric.md:56-59`): digest-auditable anchors (see §1).
- R5 (`evidence-rubric.md:60-62`): session-level sensitivity triage `public-ok|internal|sensitive`; `sensitive` sessions contribute aggregate stats only, no excerpts.
- Item template enforces these fields structurally: `templates/evidence-item.md:10-13` (`digest`, `excerpt`, `sensitivity`).

Delta: the toolkit has zero data-handling controls (open schema, whole-metadata capture, verbatim
paths); we gate the entire pipeline behind a blocking secret-scan + digest + sensitivity model.

## 6. Stage-run unit (live experiment surface)

**Their method — observational, artifact/transcript-only; no experiment, no unit below the session.**
- Every script consumes existing stores (`correlate-case-study.py:53`, `export-session-events.py:101-109`); nothing executes a task or produces a paired run.
- Scoring is per correlated case study (`score-workflow-evidence.py:51-56`); there is no `(stage, model, wave)` concept anywhere.

**Ours — stage-run is the primary measurement unit, and historical data is demoted to hypothesis-only.**
- Stage-run unit + cell blocking (`pareto-measurement-rubric.md:7-11,24-25`), as in §3.
- Historical pass is hypothesis-generation ONLY (`pareto-measurement-rubric.md:42-45`): existing transcripts are observational and confounded; they can propose candidate routings/priors but NEVER establish the frontier or an accuracy conclusion.

Delta: their whole method is what our rubric labels "historical/observational" and explicitly forbids
from carrying a frontier or accuracy conclusion.

## 7. CI-backed live contrasts

**Their method — none; explicitly no defensible quantitative comparison.**
- `EFF#L674-676`: "No defensible single percentage… older cards do not provide consistent task timings…"; gains are qualitative bands from the coarse scorer (§3).
- No repeats, no pairing, no confidence intervals exist in any script or schema.

**Ours — paired, snapshot-identical live runs with bootstrap CIs and a noise floor.**
- Live protocol (`pareto-measurement-rubric.md:46-50`): paired runs from identical disposable-task snapshots (same repo SHA/bundle/profile/verifiers/lenses), swap ONE stage's model per contrast; ≥3 repeats per (route,task); per-cell medians with bootstrap CIs; "a frontier move smaller than its CI is noise, not signal."
- Stopping rule (`pareto-measurement-rubric.md:51-53`): stop when the non-dominated set is stable across k=3 batches AND every surviving point's CI excludes dominance reversal.
- Live-Pareto review gate (`TASKS.yaml` task `pareto-live-review`): "CI-backed contrasts only; frontier claims without CI exclusion of dominance reversal are rejected."

Delta: we add an entire live-experiment tier with statistical identification (pairing + CIs + stopping
rule) that the toolkit not only lacks but whose absence its own report acknowledges as the ceiling on
its conclusions.

## 8. Falsification reviews

**Their method — one-way scoring + prose interpretation; no refutation, no counter-example construction.**
- The pipeline ends at render (`render-case-study-report.py:54-79`): sources → anchors → timeline → coverage → gaps, all affirmative; "Gaps" is populated only by empty-match fallbacks (`:48-52`), not by attempted refutation.
- No script re-queries to try to break a finding; there is no reviewer, verdict, or hypothesis field in any schema.

**Ours — pre-registered, executed refutation with a cross-family blocking gate.**
- Review contract (`falsification-review-rubric.md:9-25`): before examining the work, write ≥2 falsifiable hypotheses + a concrete test each; each hypothesis is *run* not argued (`refuted-the-work|survived|inconclusive`); null results are first-class; unrun hypotheses are carried as review debt.
- `accept` requires all hypotheses executed/waived and zero unresolved `refuted-the-work` (`:21-22`).
- Cross-family gate (`falsification-review-rubric.md:23-25`): blocking adversarial review runs on a different model family than the executor; same-family both sides = review invalid.
- Anti-patterns auto-reject the review itself (`:42-46`): "looks solid" with no executed test, unfalsifiable hypotheses, `accept` with inconclusive load-bearing claims.

Delta: we bolt an adversarial, executed-refutation review onto every substantive slice; the toolkit
has no refutation step and a rubber-stamp-shaped output (all-affirmative render).

---

## Summary table

| axis | their method (toolkit) | our rubric adds |
|---|---|---|
| correlation | substring OR-any, no per-claim citation (`correlate-case-study.py:42-64`) | verbatim digest-auditable anchors, no-anchor→gaps (`evidence-rubric.md:27-32,56-59`) |
| dedup | none; count summary only (`correlate-case-study.py:54,57`) | **inherited** — no-dedup at collection, converge at synthesis (`evidence-rubric.md:33-35`) |
| scoring | coarse count/substring bands, self-declared prioritizer (`score-workflow-evidence.py:23-60`) | primary-anchor confidence + version-match + stage-run Pareto dominance (`evidence-rubric.md:37-40,64-67`; `pareto-measurement-rubric.md:24-28`) |
| timestamps | content ts but mtime/ctime/git fallback (`inventory-transcript-sources.ps1:32,52`; `EFF#L87`) | mtime banned; `[INFERENCE]` on reconstructed (`evidence-rubric.md:16,41-43`) |
| redaction | none; open schema, whole-metadata capture (`export-session-events.py:97`; `session-event.schema.json:23`) | blocking R1–R5 secret-scan/digest/sensitivity (`evidence-rubric.md:44-62`) |
| unit | case study / session (observational) | stage-run, cell-blocked; historical = hypothesis-only (`pareto-measurement-rubric.md:7-11,24,42-45`) |
| live contrasts | none; "no defensible %" (`EFF#L674-676`) | paired snapshot-identical runs, ≥3 repeats, bootstrap CIs, stopping rule (`pareto-measurement-rubric.md:46-53`) |
| review | affirmative render, no refutation (`render-case-study-report.py:54-79`) | pre-registered executed falsification + cross-family gate (`falsification-review-rubric.md:9-46`) |
