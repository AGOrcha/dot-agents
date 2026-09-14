# CRG behavior-preservation gate (§11.4 criterion 2)

The decommissioning gate for the Python code-review-graph bridge
(graph-backend-adapter-contract §11.4) has four criteria. This directory holds
**criterion 2**: "the behavior-preservation gate passes on a corpus of recent
code-review tasks that consumed CRG output".

- **Criterion 1** (parity matrix rows, 3-week soak) → `testdata/crg-parity/SOAK.md`
- **Criterion 2** (behavior preservation) → this document
- **Criterion 3** (migration plan for out-of-tree bridge consumers) →
  `scripts/crg-bridge-consumer-audit.sh`
- **Criterion 4** (zero lockfiles declaring `reads_from: [crg-bridge]`) → a
  separate workstream; this gate says nothing about it.

Its sibling `testdata/crg-parity/` is criterion 1's soak signal: a hermetic
10-commit **synthetic** corpus where both sides are driven in-process. That gate
never touches the real bridge, so it cannot say whether the kg-native adapter
reproduces what the shipped release actually does. This one can.

## The release this gate certifies against

One release, pinned, asserted before anything is compared:

| | |
|---|---|
| package | `code-review-graph` |
| version | **2.3.8** |
| upstream tag | `v2.3.8` (`2c6dae32643572ee528eb9b77dbcc17f58f3a8c9`) |
| graph schema | **v9** (`metadata.schema_version`) |

`release-2.3.8.json` is the checked-in capability fixture for that release: its
extension→language map and the schema tables and columns the gate reads. Every
release-dependent decision — which changed files a commit contributes, which
derived views exist, which columns must be present — is read from it, so moving
the baseline is one reviewable data change plus a re-record.

The observed bridge's `--version` and the graph's `schema_version` are printed
in the report header and persisted in the JSON artifact. A CLI that reports a
different version is a **hard failure**, not a baseline: comparing against an
unpinned build certifies nothing while looking like evidence. Only the gate's
own `.venv` is pinned; the repo's other CRG lanes are untouched.

## Each task is replayed at its own commit

For every pinned review task the gate:

1. creates an **isolated linked worktree with a detached HEAD at that task's
   SHA** (natively, via `internal/gitwt` over go-git),
2. runs a **full** pinned-release build there (a full build is the only mode
   that also computes `community_summaries`, `flow_snapshots` and `risk_index`),
3. reads every persisted view, issues the release's own impact query and its own
   FTS5 searches,
4. ingests the same graph through the kg-native adapter and derives its views
   from the **store readback**,
5. probes the release's build/postprocess lifecycle contract,
6. tears the worktree down.

Replaying historical changed-file paths against a single graph built at HEAD —
the previous model — cannot detect a historical-output regression at all: it
resolves today's symbols, and a path that moved silently resolves to a different
symbol, so agreement is an artifact of the shared input. The cost of doing it
honestly is one worktree and one full build per task; that is why the CI job has
a two-hour budget and its own runner.

The gate runs **no git subprocess**. Revision resolution, the commit window,
commit subjects, tree diffs and blob reads all go through go-git, and the
per-commit checkout goes through `internal/gitwt`'s linked-worktree manager.
The only process the gate starts is the pinned `code-review-graph` itself.

## What is compared, and how exactly

The release's output for a given graph is deterministic. So every oracle is
**exact set equality over canonical rows** — there is no rank correlation, no
partition-similarity score and no token-set overlap anywhere in this gate. Only
genuinely nondeterministic upstream values are normalized away:

- `flows.id` and `communities.id` are AUTOINCREMENT rowids → flows are re-keyed
  onto their entry-point symbol, communities onto their canonical cluster key
  (the smallest member id, which is relabel-invariant);
- `created_at` / `updated_at` / `risk_index.last_computed` are wall clocks →
  not read at all.

| Surface | What is compared |
|---|---|
| `upstream_conformance` | the live bridge against the recorded release-pinned fixture for this commit |
| `changed_nodes` | which symbols the changed files resolve to |
| `impact_radius` | the blast radius reported for those files |
| `flows` | flow identity and **ordered path** (`flows.path_json`) |
| `flow_metrics` | per-flow `depth`, `node_count`, `file_count`, weighted `criticality` |
| `flow_snapshots` | `flow_snapshots` rows, incl. the v2.3.8 **qualified-name** critical path |
| `communities` | the community partition over the changed symbols |
| `community_summaries` | `community_summaries` rows for the touched clusters |
| `risk_index` | `risk_index.risk_score` |
| `risk_detail` | `caller_count`, `test_coverage`, `security_relevant` |
| `fts_index` | `nodes_fts` index content for the changed files |
| `fts_search` | the release's own **FTS5 `MATCH` results** for the changed identifiers |
| `edge_confidence` | the schema-v9 `edges.confidence` / `confidence_tier` columns |
| `lifecycle` | the release's build/postprocess staleness contract |

### The lifecycle surface

In v2.3.8 a full `build` recomputes the summary tables; standalone
`postprocess` rebuilds flows, communities and FTS but **does not** recompute
them. Because `flows.id` is an AUTOINCREMENT rowid that `store_flows` deletes
and re-inserts, a standalone postprocess leaves every `flow_snapshots.flow_id`
pointing at a row that no longer exists. The gate observes exactly that, per
commit: snapshots resolvable after the build, flows rebuilt by postprocess,
snapshots left dangling, and `metadata.last_postprocessed_at` stamped. An
adapter that eagerly recomputes summaries there — or one that assumes snapshots
are fresh after a postprocess — diverges from the release.

### Surfaces the kg-native adapter does not implement

Several release surfaces have **no** kg-native counterpart today (per-flow
metrics, flow snapshots, community summaries, the non-score risk columns, the
edge confidence columns, FTS search). Those are reported as **divergences**,
with the release's own rows attached so the report states exactly what would
have to be implemented. They are deliberately not "skips": a surface the release
produces and the native side cannot produce at all is the strongest possible
negative answer for that surface, and recording it as unexercised would hide a
product gap behind an environment-shaped excuse.

**Expect this gate to be red against the current adapter.** That is the finding,
not a defect in the gate. Do not resolve it by weakening an oracle.

## Schema capabilities are probed, not guessed

Before any view is read the gate probes `metadata.schema_version`,
`sqlite_master` and `pragma_table_info` and classifies every table the release
fixture describes into one of four states:

| state | meaning | verdict |
|---|---|---|
| `populated` | table, columns and rows present | surfaces are exercised |
| `empty` | table and columns present, zero rows | the release computed no such data — surfaces reported **NOTRUN** with the detected release |
| `table_missing` | absent from `sqlite_master` | required → hard failure; optional → surfaces NOTRUN |
| `column_missing` | table present, a read column absent | **always** a plumbing failure |

A `schema_version` other than 9, a missing required table, a missing column, or
an empty required table all fail immediately as a release/schema
incompatibility. **Any other SQL error fails too** — a corrupt page, a locked
database or an FTS5 module the driver cannot load is a failure, never "that view
is unavailable". Collapsing those cases is how a broken environment previously
produced a green run.

## Required surfaces must actually be exercised

`contract.json` names the surfaces a criterion-2 run **must** exercise. After a
run the gate folds every task's outcome into per-surface coverage: a required
surface that no task exercised **fails the run** unless `ratified_exceptions`
carries an entry for it with a reason and a ratifier.

Without that, a run that skipped half its surfaces reported the same verdict as
one that compared everything. There are currently **no** ratified exceptions.

## The release-pinned upstream recordings

`fixtures/<sha12>.json` records what the pinned release itself produced for each
corpus commit, in the same canonical row form the live comparison uses. They are
**recorded, never hand-authored**:

```sh
go run ./tools/crgbehaviorgate -record -repo .
```

A hand-written "expected" file asserts what someone believed; a recorded one
asserts what the release did. A missing recording fails its task's
`upstream_conformance` surface with the command to produce it — there is no
baseline, so there is nothing to certify. Recording is an explicit command: a
gate that re-recorded its own baseline on every run could never detect upstream
drift.

**No recording is committed.** The directory ships with this README and nothing
else. A checked-in "expected upstream" file that no run of the release ever
produced is a fabrication, and diffing a bridge against a fabrication certifies
nothing — so the baseline is produced, per run, by the release. CI is
**record-then-compare**: the `crg-behavior-gate` job installs the pinned wheel,
runs `-record -fixtures crg-upstream-recordings` to write the baseline into the
job workspace, then runs the gate with the same `-fixtures` directory and
uploads both the recordings and the verdict as the run's evidence. Locally the
recordings land in `fixtures/` (gitignored) and stay on your machine.

## The pinned corpus

`manifest.json` (schema v2) pins the review tasks: commit SHA, subject, the
release-indexed files the commit touched, the release's language labels for
them, and the declarations the commit added or removed.

Both the eligible-file set and the identifier extraction are derived from
`release-2.3.8.json`, not from a hardcoded language list. The previous builder
recognized only the Go and Python declaration forms, so a TypeScript, Rust, Java
or Ruby commit was kept with an **empty** identifier list and silently left the
FTS search surface unexercised. Languages the extractor does not cover are
reported by `-regen` rather than hidden.

Identifiers are the **symmetric difference of each changed file's declaration
sets** at the commit and at its parent — computed from file CONTENT, not by
reading `+`/`-` diff lines. Git's hunk boundaries come from its own xdiff
implementation (Myers plus change compaction and an indent heuristic), which no
other differ reproduces, so a hunk-reading extractor silently depends on which
of several equally valid alignments git happened to choose. The set difference
is exactly the contract the corpus wants and is differ-independent.

Selection is **language-coverage-first**: the builder scans a wide window
(`-window`, default 250 commits), keeps the commits that touched a
release-indexed file, then pins the newest commit for each language before
filling the remainder newest-first. Each pinned commit costs a full release
build, so the corpus is necessarily a sample — and a plain newest-N sample of
this repository is almost entirely Go. The rule maximizes exercised surfaces; it
can never hide a divergence, because a pinned commit is never dropped for
diverging.

```sh
go run ./tools/crgbehaviorgate -regen -repo . -ref origin/master -commits 25 -window 250
```

## Running it locally

```sh
# 1. install the pinned release and put it on PATH (once)
python3 -m venv .venv
.venv/bin/python -m pip install 'code-review-graph==2.3.8'
export PATH="$PWD/.venv/bin:$PATH"

# 2. record the upstream baseline for the pinned corpus (once per corpus/release)
go run ./tools/crgbehaviorgate -record -repo .

# 3. run the gate
go run ./tools/crgbehaviorgate -repo . -json crg-behavior-gate.json

# useful flags: -tasks N (first N tasks), -depth N, -work-dir DIR
```

CI caps both passes at the same prefix (`CRG_GATE_TASKS`, currently **1** of the
25 pinned tasks). That cap is measured, not guessed: on the CI runner class one
full pinned-release build of this repository takes ~15 min (~25 s parsing,
~14.5 min file-based community detection — `igraph not available`), and each
task additionally runs a standalone postprocess for the lifecycle probe, so one
task costs ~29 min per pass and ~58 min across record-then-compare. The full
corpus would need roughly a day; the job's budget is 120 minutes.

The cap softens no oracle. Contract coverage is judged over the tasks that
**actually ran**, so every required surface the capped prefix does not exercise
still `FAIL`s instead of being quietly waived, and the report's `corpus:` line
states how many of the pinned tasks a run executed. Restoring corpus breadth
needs a larger budget or a faster community-detection path — not a weaker
contract.

PATH matters: each pinned commit is built inside a worktree **outside** the
repository, so the bridge cannot be found as a sibling `.venv` from there. An
undiscoverable bridge is reported as `INCONCLUSIVE`, not as a pass.

Exit codes:

| code | verdict | meaning |
|---|---|---|
| 0 | `PASS` | every required surface exercised, every exact oracle agreed |
| 1 | `FAIL` | a behavior diverged, a comparison failed, or a required surface went unexercised without a ratified exception |
| 2 | `ERROR` | usage, plumbing, off-release bridge, or incompatible schema |
| 3 | `INCONCLUSIVE` | the pinned bridge could not be driven — **no evidence produced** |

An absent bridge is `INCONCLUSIVE` and still exits non-zero. It used to exit 0
with a `SKIP:` notice, which meant "we could not test this" and "behavior is
preserved" produced the same green result.

## CI

The `crg-behavior-gate` job keeps a **non-required status**
(`continue-on-error: true`) until the §11.4 sign-off, but its **result is a real
sign-off signal**: the recording pass and the gate pass both preserve their exit
codes through `pipefail`, every run publishes the `GATE:`, `SIGN-OFF:` and
`corpus:` lines plus the observed bridge version to the job summary, and the
JSON artifact, both logs and the fresh recordings are uploaded on every outcome.
The gate pass runs under `if: always()` so a partial recording still produces a
verdict — a missing recording is a reported evidence gap, not a skipped step.
At sign-off, delete `continue-on-error`.

## Reading a failure

A divergence names the commit, the surface, and the structural diff:

```
commit 685a09a76790  test(agentslock): widen acquire budget ...
  files:  internal/agentslock/lockfile_test.go
  langs:  go
  graph:  18513 symbols / 228319 edges / 1153 files; 18513 symbols ingested natively
  AGREE  changed_nodes         native=91 bridge=91 row(s)
  DIFFER flows                 native=61 bridge=59 row(s)
         only in NATIVE: entry=...::TestReclaim@... path=...
         only in BRIDGE: entry=...::mustWriteFile@... path=...
  NOTRUN community_summaries   code-review-graph 2.3.8 materializes community_summaries but computed no rows for this graph
```

`NATIVE` is the kg-native adapter; `BRIDGE` is the pinned Python release. The
report ends with the run's coverage table and a one-line sign-off claim stating
what was compared, at which release, and over how many tasks — the sentence to
quote in the §11.4 decision.
