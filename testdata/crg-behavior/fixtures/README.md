# Release-pinned upstream behavior fixtures

One file per pinned corpus commit (`<sha12>.json`), recording what
`code-review-graph 2.3.8` itself produced for that commit: flow identity and
ordered paths, per-flow metrics, flow snapshots, the community partition and its
summaries, the full `risk_index` rows, the FTS5 index content and search
results, the schema-v9 edge confidence columns, and the observed
build/postprocess lifecycle contract.

They are the **release-conformance baseline**: the gate's
`upstream_conformance` surface compares the live bridge against them, so
upstream drift (or a graph that was not materialized at the pinned commit) shows
up as a plain text diff instead of silently redefining "correct".

## These files are recorded, never hand-authored — and never committed

```sh
python3 -m venv .venv
.venv/bin/python -m pip install 'code-review-graph==2.3.8'
go run ./tools/crgbehaviorgate -record -repo .
```

A hand-written "expected" file asserts what someone believed. A recorded one
asserts what the release did — which is the only thing that makes it evidence.
Do not edit a recording to make a run green; re-record it, and review the diff.

Nothing but this README is tracked here, and `*.json` in this directory is
gitignored. A checked-in "expected upstream" file that no run of the release
ever produced is a fabrication, and diffing a bridge against a fabrication
certifies nothing. The `crg-behavior-gate` CI job is therefore
**record-then-compare**: it installs the pinned 2.3.8 wheel, runs
`-record -fixtures crg-upstream-recordings` to produce the baseline in the job
workspace, then runs the gate against that same directory and uploads the
recordings with the verdict. Locally, recordings land here and stay on your
machine.

A missing recording is a gate **failure**, not a skip: with no recorded baseline
there is nothing proving the bridge still behaves like the release the gate
claims to certify against.

## When to re-record

- the pinned release moves (`crgbehavior.PinnedVersion`);
- the corpus is regenerated (`-regen`) and pins different commits;
- a recorded surface's canonical row rendering changes.

Re-recording is an explicit command precisely because a gate that refreshed its
own baseline on every run could never detect a behavior change.
