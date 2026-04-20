# KG Freshness Audit Findings

## Date
2026-04-19

## Scope

Plan task `kg-command-surface-readiness / kg-freshness-audit`.

Commands audited:

- `dot-agents kg build`
- `dot-agents kg update`
- `dot-agents kg code-status`
- supporting contrast: `dot-agents kg health`

Audit target:

- define the graph-ready contract that downstream planner surfaces can rely on before using code-graph-backed intents such as `symbol_lookup` or `callers_of`

## Reproduction environments

### A. Fresh KG home against the live repo

- `KG_HOME=/tmp/kg-freshness-audit-20260419`
- repo: current dirty workspace at `/Users/nikashp/Documents/dot-agents`

### B. Fresh KG home against a clean checkout

- `KG_HOME=/tmp/kg-freshness-clean-home`
- clean detached worktree at `/tmp/kg-freshness-clean`

This clean checkout matters because the live repo had a concurrent `kg update` process, which was enough to produce a `database is locked` failure during one build attempt.

## Observed behavior

### 1. `kg health` is not a code-graph readiness signal

On a fresh KG home, `kg health` reported:

- status: `healthy`
- total notes: `0`
- sources: `0`

This says the note store is initialized, not that the code graph is ready. A planner could incorrectly treat `healthy` as "graph-backed scope derivation is safe" when the code graph is still absent.

### 2. `kg code-status` depends on repo-local CRG state, not KG_HOME readiness

On the live repo with an existing `.code-review-graph/graph.db`, a fresh `KG_HOME` still returned non-zero stats immediately:

- Nodes: `34570`
- Edges: `101735`
- Files: `897`
- Last updated: `2026-04-19T18:03:45`

On the clean checkout before any build, the same command returned:

- Nodes: `0`
- Edges: `0`
- Files: `0`
- Last updated: `never`

So the readiness boundary for code-graph-backed commands is the repo-local CRG graph, not KG home initialization.

### 3. `kg code-status --json` did not emit JSON

Observed with both forms:

- `dot-agents --json kg code-status --repo /tmp/kg-freshness-clean`
- `dot-agents kg code-status --repo /tmp/kg-freshness-clean --json`

Both still rendered the human UI block instead of JSON.

Contrast:

- `dot-agents --json workflow status` emitted valid JSON in the same session

This is a real machine-readability gap for the freshness surface.

### 4. `kg build` behavior differs sharply by environment

#### Sandbox-only failure

Inside the sandbox, `kg build` failed before meaningful graph work because Python CRG attempted a semaphore-limit check and hit:

- `PermissionError: [Errno 1] Operation not permitted`

This is not a product-level CLI defect, but it does mean local sandboxed automation cannot treat `kg build` failures as graph-readiness evidence without checking whether the failure came from the runtime.

#### Live repo with concurrent update

Against the live repo while an earlier `kg update` was still running, `kg build` failed with:

- `sqlite3.OperationalError: database is locked`

This is operationally relevant: the command surface does not distinguish "graph busy/locked" from other build failures in a typed way.

#### Clean checkout

Against the clean worktree, `kg build` succeeded:

- parsed `196` files
- build output reported `2255 nodes` and `20103 edges`

### 5. `kg build` output and `kg code-status` disagree immediately after a successful build

Immediately after the successful clean-checkout build:

- build output: `2255 nodes`, `20103 edges`
- `kg code-status`: `2154 nodes`, `13687 edges`, `196 files`

This may be explainable by CRG post-processing, filtered persisted state, or CLI reporting differences, but from the command-surface perspective it is drift:

- the user cannot tell which number set is authoritative
- downstream automation cannot safely use build output as proof of final graph contents

For the contract, `code-status` should be treated as the persisted source of truth unless this mismatch is eliminated.

### 6. `kg update` succeeds on the clean checkout, but its summary is not yet trustworthy enough for automation

On the clean detached worktree with no local modifications:

- `git status --short` was empty
- `kg update --repo /tmp/kg-freshness-clean` returned:
  - `Incremental: 1 files updated, 0 nodes, 0 edges`

The command exited successfully and `code-status` afterward still showed the same persisted counts as after build, only with a later timestamp.

This leaves the summary ambiguous:

- why was `1 files updated` reported on a clean checkout?
- does `0 nodes, 0 edges` mean "nothing changed" or "update ran but produced no graph mutations"?

For planner-facing automation, that summary is too opaque.

## Graph-ready contract

Downstream planner or scope-derivation work should treat the code graph as **ready** only when all of the following are true:

1. `kg code-status` runs successfully for the target repo.
2. `code-status` reports:
   - `nodes > 0`
   - `files > 0`
   - `last_updated != "never"`
3. the command can emit a machine-readable form for automation consumers.
4. there is no active build/update lock condition on the repo graph store.
5. `kg health` is not used as the readiness gate for code-graph-backed commands.

If any of those fail, downstream surfaces should treat the graph as **not ready** and refuse to treat empty graph answers as evidence.

## Contract implications for the implementation task

### `kg code-status`

It should become the authoritative freshness probe.

Required direction:

- stable machine-readable output
- explicit readiness fields, not only raw counts
- distinguish at least:
  - `unbuilt`
  - `ready`
  - `busy_or_locked`
  - `error`

### `kg build`

Required direction:

- actionable non-zero failures for:
  - missing CRG binary
  - graph DB lock / concurrent writer
  - runtime execution failure
- avoid leaving automation to infer build state from mixed Python traceback text
- either align final persisted counts with `code-status` or clearly state that `code-status` is the post-build truth

### `kg update`

Required direction:

- success summaries must distinguish:
  - no diff
  - diff present but no graph mutations
  - graph mutations applied
- machine-readable output needs to carry those distinctions
- concurrent lock conditions should be distinguishable from generic execution failure

## Audit conclusions

1. The current code-graph freshness surface is partially operational, but not yet planner-safe.
2. `kg health` is too weak to act as the gate for code-graph-backed commands.
3. `kg code-status` is the natural readiness probe, but it still lacks reliable JSON output and explicit readiness semantics.
4. `kg build` and `kg update` need clearer failure and summary contracts before downstream planner evidence can trust them.
5. The next task should implement readiness semantics rather than adding more audit prose.
