# CRG behavior-preservation gate (§11.4 criterion 2)

The decommissioning gate for the Python code-review-graph bridge
(graph-backend-adapter-contract §11.4) has four criteria. This directory holds
**criterion 2**: "the behavior-preservation gate passes on a corpus of recent
code-review tasks that consumed CRG output".

Its sibling, `testdata/crg-parity/`, is criterion 1's soak signal: a hermetic
10-commit **synthetic** corpus where both sides are driven in-process. That gate
can never observe what the live Python bridge actually persisted for a real
repository. This gate closes exactly that gap — same comparison discipline, same
oracle functions, real history and a live bridge.

- **Criterion 1** (parity matrix rows, 3-week soak) → `testdata/crg-parity/SOAK.md`
- **Criterion 2** (behavior preservation) → this document
- **Criterion 3** (migration plan for out-of-tree bridge consumers) →
  `scripts/crg-bridge-consumer-audit.sh`
- **Criterion 4** (zero lockfiles declaring `reads_from: [crg-bridge]` across
  the managed-repo set, per `workflow drift`) → **a separate workstream**; this
  gate says nothing about it.

## What the gate compares

For each pinned commit, the gate replays the graph queries a review of that
commit issues, against **both** sides, and diffs the answers:

| Surface | Query | Oracle | Tier |
|---|---|---|---|
| `changed_nodes` | which symbols the changed files resolve to | set equality (`graphstore.CompareImpactRadius`) | **gating** |
| `flows` | membership of every execution flow the changed symbols touch | set equality (`crg.CompareFlowMemberships`) | **gating** |
| `fts` | indexed tokens for the declaration identifiers the commit changed | token-set equality (`crg.CompareFTS`) | **gating** |
| `impact_radius` | blast radius of the changed files | set equality (`graphstore.CompareImpactRadius`) | advisory |
| `flow_order` | step numbering within the touched flows | set equality with positions (`crg.CompareFlowMemberships`) | advisory |
| `communities` | community membership of the changed symbols | partition equivalence (`graphstore.PartitionAgreement`) | advisory |
| `risk_index` | risk ranking of the changed symbols | Spearman ≥ `graphstore.DefaultSpearmanTau` (`graphstore.SpearmanTau`) | advisory |

Every oracle is the parity gate's own function — none is reimplemented here.

**Sides.** The legacy side is the Python bridge's real state: its persisted
`flow_memberships`, `nodes.community_id`, `risk_index` and `nodes_fts` tables,
plus its live `get_impact_radius` query. The kg-native side ingests the bridge's
symbol graph through the adapter's own `crg.Bootstrap` and derives every view
from the **store readback** (`FlowsFromStore`, `CommunitiesFromStore`,
`RiskIndexFromStore`, `FTSFromStore`, `ImpactRadiusFromStore`) — driven through
the adapter/Store API directly, never through `da kg`, so the gate is
independent of which backend the production commands are wired to.

**Normalization.** Legacy absolute paths and qualified names are made
repo-relative so both sides share one id space (`<file>::<symbol>@<file>`), and
the legacy `IMPORTS_FROM` edge kind is mapped onto the kg-native `IMPORTS`
spelling. Call edges the bridge stored with an unresolved bare target (e.g.
`append`) match no symbol and are dropped at ingestion — the same resolvable
subgraph the bridge's own flow derivation runs over.

## Advisory tier — why, with measurements

The advisory surfaces are **reported with their structural diff but do not fail
the gate**, because they are measured differences in derivation, not
regressions. Numbers below are from a 21-task run against this repository
(18,513 symbols / 228,319 references / 1,153 files):

- `impact_radius` — the bridge resolves CALLS targets **by bare symbol name at
  query time**: 106,951 of its 120,400 stored CALLS edges (88.8%) have a target
  that matches no node. Its blast radius therefore includes cross-language name
  collisions (a Go change pulling in `.py` symbols named `blocks`) that an
  id-based traversal cannot and should not reproduce, and it routinely truncates
  at the result cap.
- `communities` — the bridge's communities are **file-scoped clusters** (1,113
  clusters over this repo); the kg-native partition is connected components over
  CALLS+IMPORTS. Not one bridge community is contained in a single native
  component: these are different notions of "community".
- `risk_index` — the bridge's `risk_score` is a coverage/caller heuristic with 9
  distinct values across 17,360 scored nodes; the kg-native score is degree
  centrality. Measured Spearman correlation over the whole graph: **-0.21**.
- `flow_order` — flow **membership** matches exactly (4,762 of 4,762 flows, mean
  Jaccard 1.0 against a full kg-native BFS from each bridge entry point), but
  the legacy `flow_memberships` table is keyed `(flow_id, node_id)` and numbers
  steps along its own path order, while the kg-native positions follow a
  deterministic sorted BFS. So `flows` gates on membership and `flow_order`
  reports the numbering separately.

Running with `-strict` promotes every advisory surface to gating. That flag is
the §11.4 sign-off switch: no code change is needed to tighten the gate once
these differences are resolved or explicitly accepted.

## Running it locally

```sh
# 1. install the legacy bridge (once)
python3 -m venv .venv && .venv/bin/python -m pip install code-review-graph

# 2. build the legacy graph for this repo (needs postprocess — no --skip-flows)
.venv/bin/code-review-graph build --repo "$PWD"

# 3. run the gate over the pinned corpus
go run ./tools/crgbehaviorgate -repo . -graph-repo .

# useful flags: -tasks N (first N tasks), -depth N, -strict
```

Exit codes: `0` pass or skip, `1` gating divergence, `2` plumbing error. Without
the Python CLI or a built graph the gate prints `SKIP:` and exits 0 — an absent
legacy bridge is an environment fact, not a behavior divergence.

The same gate runs inside the Go suite as
`TestBehaviorGate_RealHistoryCorpus` (`internal/crgbehavior`), capped at three
tasks, and skips the same way.

## The pinned corpus

`manifest.json` pins the review tasks: commit SHA, subject, the
graph-indexable files the commit touched, and the declaration identifiers its
diff added or removed. Regeneration is an **explicit command**, never a side
effect of a gate run:

```sh
go run ./tools/crgbehaviorgate -regen -repo . -ref origin/master -commits 25
```

Commits that touched no graph-indexable source file are dropped (a docs-only
commit issues no graph query), so the task count is normally lower than
`-commits`.

## Reading a failure

A divergence names the commit, the query surface, and the structural diff:

```
commit 685a09a7  test(agentslock): widen acquire budget ...
  files: internal/agentslock/lockfile_test.go
  PASS  changed_nodes  native=91 bridge=91
  FAIL  flows          native=61 members bridge=59 members
        flow_membership only in a: ...::TestReclaim...@... 0 ...::mustWriteFile@...
```

`a` / `NATIVE` is the kg-native adapter; `b` / `BRIDGE` is the legacy Python
side. A task whose changed files resolve to no symbol on either side is SKIPped
(the commit is outside the built graph) and does not count toward the verdict; a
run that executes zero tasks never reports PASS.
