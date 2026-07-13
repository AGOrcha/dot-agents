# Evidence Collection Rubric — local transcript analysis

Extends the provadm-agc `workflow-evidence-analysis` toolkit (generic core) with this machine's
corpora. Report section order is canonical and MUST match prior analyses:
**sources → anchors → timeline → coverage/confidence → gaps/unknowns.**

## 1. Source inventory (per harness)

One inventory row per session directory/file. Required fields:

| field | rule |
|---|---|
| `evidence_id` | `<harness>:<path-basename>:<session-uuid>` — stable, path-derived |
| `harness` | `omp \| claude-code \| codex \| cursor \| copilot` |
| `project_slug` | as encoded in the harness dir name; normalize to repo path when resolvable |
| `started_at` / `ended_at` | first/last record timestamp (UTC ISO-8601); never mtime |
| `record_count` | raw line count of primary `.jsonl` only |
| `has_tokens` / `has_cost` / `has_wallclock` | booleans — drive which axes the session can feed |
| `model(s)` | all models seen (`model_change` records included) |
| `status` | `complete \| truncated \| cutoff` — cutoff = ends mid-tool-call or mid-assistant turn |

Exclusions (hard): derived `.md`/`.meta.json`/`.log`, `memory/`, `tool-results/`, SQLite WAL/SHM,
stats/index caches. OpenCode: absent on this machine — record as a *known-absent* source, not a gap.

## 2. Evidence item extraction

An evidence item is a claim + anchor(s). Anchor format: `evidence_id#L<line>` (jsonl line number)
or `evidence_id@<timestamp>`. Rules:

- **E1 — verbatim anchor**: every claim carries ≥1 anchor; no anchor → the claim goes to
  gaps/unknowns, never to findings.
- **E2 — no dedup at correlation time** (matches `correlate-case-study.py` behavior): repeated
  observations across sessions are separate items; convergence is computed at synthesis, not
  collection.
- **E3 — classify each item**: `mechanism` (how the loop behaved) | `failure` (what broke) |
  `cost` (tokens/wall-clock/$) | `craft` (methodology worth extracting) | `outcome`
  (completion/verification result).
- **E4 — version-aware scoring**: score sidecar comparisons only within the same
  `rubric_version` (3.0.0 current, 2.1.0 historical) unless explicitly normalized; mixed-version
  aggregates MUST be flagged.
- **E5 — provenance over inference**: `[INFERENCE]` tag on any reconstructed value (e.g.
  stage timing rebuilt from tool `wallTimeMs`); inferred values never silently mix with recorded.
- **E6 — per-harness token-field trust (added 2026-07-12; graduates pareto erratum #2).** Every
  token/cost row states which raw fields it trusts, per this table, and pins its corpus slice:

  | harness | trust rules |
  |---|---|
  | claude-code | **Never sum raw usage rows.** Dedup by `requestId`, LAST entry wins (empirically: last = max in 100% of 33,194 dup groups; requestIds never span files). Exclude `<=1` placeholder values from rate stats. `cache_read`/`cache_creation` are the highest-trust fields. Naive-sum overcount is **slice-dependent**: ~3× per field on PRIMARY session files, diluted (output ~1.8×) when `subagents/` files are pooled — a row MUST state which slice it used ("primary" = `*.jsonl` directly under the project dir; 0 `isSidechain`). |
  | omp | `usage.{input,output,cacheRead,cacheWrite,cost}` recorded per turn — highest-fidelity source; cacheRead ≈ 95-98% of tokens but only ≈ 58-69% of $ (never conflate the two shares). |
  | codex | token_count events cumulative; per-session total = LAST cumulative value, not a sum. No $ field. |
  | cursor | records NO tokens/$/wallclock/model — hard-excluded from those axes (O2). |
  | copilot | premium-request credits, not USD; never convert silently. |

  Ground truth for the CC rules: devforth mitmproxy capture (API charges once per `requestId`:
  one stream, two tool-call rows, `output_tokens: 101` charged once not 202 —
  `research/articles/devforth-cc-usage-overestimates-output-tokens.md`), Gille's
  placeholder/undercount analysis (`gille-claude-code-jsonl-undercount-tokens.md`), and the
  2026-07-12 independent re-derivation (93,493 usage entries;
  `reviews/red-team-premortem-2026-07-12.md` RT-2).

## 2b. Data handling / redaction gate (blocking)

Local transcripts can contain credentials, private prompts, user data, and machine paths.
Before ANY excerpt leaves a harness dir:

- **R1 — raw transcripts never committed.** Only inventory rows, redacted excerpts, and
  digests enter the repo. `evidence/` MUST NOT contain `.jsonl` copies of session files.
- **R2 — secret scan first.** Run a secret scan (gitleaks or equivalent pattern set: keys,
  tokens, `Authorization:`, connection strings) over any excerpt before it is written to a
  committed artifact. A hit = redact or drop, never "probably fine".
- **R3 — minimize excerpts.** Quote the smallest span that supports the claim; strip absolute
  home paths to `~`-relative; strip other users'/orgs' identifiers not already public.
- **R4 — auditable anchors.** Local `#L` anchors alone are not reviewable by others. Each
  committed item records: stable digest (`sha256` of the anchored raw line(s)) + the redacted
  excerpt + enough surrounding context to re-locate it. A reviewer with machine access verifies
  digest ↔ raw; one without still audits the excerpt.
- **R5 — session-level sensitivity triage.** Inventory marks each session `public-ok |
  internal | sensitive`; `sensitive` sessions contribute aggregate stats only, no excerpts.

## 3. Coverage / confidence grading

Per case study: `high` = ≥2 independent anchors incl. ≥1 primary transcript; `medium` = 1 primary
anchor; `low` = secondary-only (sidecars, checkpoints, prose). A finding graded `low` cannot be an
actionable outcome by itself.

## 4. Templates

- `templates/source-inventory.md` — §1 table.
- `templates/evidence-item.md` — one item: claim, class, anchors, confidence, `[INFERENCE]` flags.
- `templates/case-study.md` — canonical section order, cross-referencing prior payout/provadm
  reports by their existing IDs.
