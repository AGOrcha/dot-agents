# CRG bridge consumer audit & cutover readiness (t6c)

**Verdict for `t6d-final-bridge-deletion`: `NOT-READY` — KEEP THE BRIDGE.**

This audit proves what still depends on the CRG "bridge" and makes the drift
check reproducible. Task: `graph-backend-adapter-contract / t6c-consumer-audit`
(anti-scope: no deletion; if any consumer still needs `reads_from:[crg-bridge]`,
keep the bridge). The bridge is decommissioned by t6d only when **all four**
§11.4 gate conditions hold; today condition 3 is **MET** in-repo, condition 4's
in-repo check is automated and clear (the managed-repo sweep still needs to be
run and confirmed clean on the target installation — see the runbook below),
and conditions 1–2 are external (t6a/t6b CI soak) and still unmet.

> **Updated after the Phase A native cutover
> (`t6-bridge-decommission`, workstream 1).** Condition 3 is now **MET**: the
> kg-native `crg` adapter is registered in both production registries and is the
> **default** backend for `da kg build|update|code-status|impact|flows|
> communities|postprocess|changes` and `da kg serve`. The Python subprocess is
> off the default path entirely. The bridge is **kept** as the explicitly
> configured rollback backend (`kg.graph_backend`), so the verdict stays
> `NOT-READY — KEEP THE BRIDGE` until the §11.4 conditions 1, 2 and 4 close.
> **The soak clock restarts here**: the parity matrix must now soak against a
> native path that is actually serving production traffic, not one that only ran
> in the hermetic gate.

Regenerate this report at any time:

```bash
bash scripts/crg-bridge-consumer-audit.sh              # print the audit
bash scripts/crg-bridge-consumer-audit.sh --check docs/crg-bridge-consumer-audit.md
```

The `--check` mode fails if the embedded block below drifts from a fresh run, so
this doc and the script are kept in lockstep (a CI-runnable readiness gate).

## Two distinct surfaces (t6d removes both, §11.4)

The word "bridge" covers two separate things; conflating them hides the real
blocker. t6d's write_scope (`internal/graphstore/`,
`internal/adapters/builtin/crg-bridge/`, `internal/adapters/builtin/crg/`)
deletes **both**:

- **[A] Legacy Python CRG subprocess bridge** — `internal/graphstore/crg.go`
  (`type CRGBridge` / `NewCRGBridge`, discovery in `crg_venv_{unix,windows}.go`).
  Shells out to the `code-review-graph` Python CLI. It is **no longer the default
  runtime**: after the Phase A cutover it is constructed only when
  `kg.graph_backend` (or `DA_KG_GRAPH_BACKEND`) selects the crg-bridge family.
  It remains load-bearing in the sense that it is the supported rollback path.
- **[B] Migration-only `crg-bridge` mirror adapter** —
  `internal/adapters/builtin/crg-bridge/`. A read-only mirror of the legacy
  bridge state under `kg_crg-bridge.*`, existing solely as a parity read target
  (§11.2). Its consumers are materialized views declaring `reads_from:[crg-bridge]`.

## Reproducible audit output

<!-- BEGIN crg-bridge-consumer-audit.sh output -->
```text
CRG BRIDGE CONSUMER AUDIT
=========================
Scope: dot-agents repo, content-derived (branch-independent). Two surfaces t6d
removes together: [A] Python subprocess bridge, [B] crg-bridge mirror adapter.

[A] Legacy Python CRG subprocess bridge  (internal/graphstore/crg.go: CRGBridge)
    Definition site      : internal/graphstore/crg.go (+ crg_venv_unix.go / crg_venv_windows.go)
    Production consumers  : 2 file(s)
      commands/kg/backend.go  (1 ref-lines)
      internal/graphstore/mcp_server.go  (1 ref-lines)
    Test-only consumers  : 10 file(s)
    STATUS: LIVE / load-bearing — da kg code ops + MCP server route here.

[B] Migration-only crg-bridge mirror adapter  (internal/adapters/builtin/crg-bridge/)
    Production registration (RegisterCRGFamily / adapter import) : 3
    reads_from:[crg-bridge] declarations (schemas + lockfiles)   : 0
    MirrorSnapshot production callers                            : 0
    STATUS: has consumers — see counts.

[C] graph_backend profile references selecting crg-bridge : 0
    (built-in graph backends are crg / crg-bridge / none; selecting the crg-bridge
     ref routes da kg back to the legacy Python runtime — the §11.4 rollback path.
     It does NOT permit a materialized view to declare reads_from:[crg-bridge].)

[D] Named / out-of-tree consumers of the CRG graph (bound to the Python CLI)
    CI KG CODE lane (.github/workflows/test.yml)            : present
    kg update post_tool_use hook (graceful-degrade)         : present
    MCP server (da kg serve -> graphstore.NewMCPServer)     : present
    Plus documented set: da kg {build,update,code-status,impact,flows,communities,
      postprocess,changes}; review skills build-graph / review-delta / review-pr;
      cross-repo sweep target = ~/.agents/config.json managed repos (per-install).

[E] §11.4 decommission gate (all four required before t6d deletes the bridge)
    1. parity matrix 8 rows x 3wk CI      : EXTERNAL — owned by t6a/t6b (t6b pending)
    2. behavior-preservation gate         : EXTERNAL — owned by t6b
    3. out-of-tree consumer migration     : MET — kg-native backend is the default
                                            (builtin registries register the CRG family; the bridge is opt-in via kg.graph_backend;
                                             RegisterCRGFamily prod callers = 3)
    4. zero reads_from:[crg-bridge]        : in-repo grep count 0; `workflow drift`
                                            (da workflow drift --path . --json) reports
                                            bridge_consumer_status=not_a_kg_repo
                                            (0 live consumer(s) here).
                                            Managed-repo sweep: `da workflow drift --json`
                                            (no --path) walks every registered project and
                                            summarizes bridge_sweep.{consumers_found,clean,
                                            not_a_kg_repo}_repos.

VERDICT: NOT-READY — KEEP THE BRIDGE
```
<!-- END crg-bridge-consumer-audit.sh output -->

## [A] Python subprocess bridge — live consumers (cut over in Phase A)

| Consumer | Sites | Role | State |
|---|---|---|---|
| `commands/kg/backend.go` | `NewCRGBridge` @1 | the single backend-selection chokepoint; constructs the bridge **only** when the crg-bridge family is configured | **Cut over** |
| `internal/graphstore/mcp_server.go` | `NewCRGBridge` @1 | legacy `NewMCPServer` constructor, retained for the rollback path; production `da kg serve` uses `NewMCPServerWithProvider` | **Cut over** |

`commands/kg/sync_code_warm_link.go` (previously 11 ref-lines) and
`commands/kg/bridge.go` (previously 2) no longer reference `CRGBridge` at all:
every call site now goes through `codeGraphProvider(root)`, which returns a
`graphstore.CodeGraphProvider`.

Definition/self-use site (what t6d actually deletes): `internal/graphstore/crg.go`
plus `crg_venv_unix.go` / `crg_venv_windows.go`.

### How backend selection works now

`graphstore.CodeGraphProvider` (`internal/graphstore/provider.go`) publishes the
eight §11.1 rows plus the two bulk-export reads as one interface. Three
implementations satisfy it:

| Adapter ref | Implementation | Default? |
|---|---|---|
| `dotagents-builtin:graph/crg@^1.0` | `internal/codegraph.Engine` — in-process Go ingestion + the crg adapter's parity-verified derivations | **yes** |
| `dotagents-builtin:graph/crg-bridge@^0.1` | `graphstore.CRGBridge` — the legacy Python subprocess | no (rollback) |
| `dotagents-builtin:graph/none@^1.0` | `internal/codegraph.NullProvider` — graph disabled | no |

`commands/kg/backend.go` resolves the ref in this order: `DA_KG_GRAPH_BACKEND`
env override → `.agentsrc.json` `kg.graph_backend` → the kg-native default. The
ref is resolved **through the built-in adapter registry**, so an unregistered or
version-incompatible ref is rejected rather than silently defaulting. A bare
adapter name (`crg`, `crg-bridge`, `none`) is accepted and expanded.

### Where the kg-native graph lives

`<repo>/.dot-agents/code-graph.db`, a graphstore SQLite database written through
the published `graphstore.CodeGraphWriter` contract. It is deliberately a
different file from the bridge's `.code-review-graph/graph.db`, so a rollback
can never read a graph the other backend wrote.

### The product contract is the pinned release, not a subset

`da kg serve` advertises the FULL published surface of `code-review-graph`
**v2.3.8** (tag `2c6dae32643572ee528eb9b77dbcc17f58f3a8c9`, graph schema v9): all
30 tools with their released JSON schemas, defaults and required arguments, the
5 prompts, the release's `initialize` identity, its argument-validation and
error semantics, and its `tools/call` envelope. That surface is not
hand-maintained — `internal/crgrelease` is generated from the release itself by
`tools/crgrelease/generate_release_contract.py`, and the fixtures under
`testdata/crg-release/v2.3.8/` are the oracle the contract tests compare
against. Regenerating is reproducible and byte-stable.

### Which backend answers a call

Routing is per call and capability-based. A call is answered by the kg-native
engine only when **both** hold:

1. the tool has a native handler that reproduces the release's response, and
2. every source file in the repository is one the native scanner extracts.

Otherwise it is answered by the retained Python bridge, which runs the
release's own MCP server in-process (`internal/graphstore/crg_tool_bridge.go`),
so a bridge-routed answer *is* the release's answer rather than a
re-derivation. When a call must reach the bridge and the release is not
installed, the server returns an explicit capability error naming the missing
capability — never an empty success, which a caller cannot distinguish from
"the graph really is empty".

The asymmetry is deliberate: routing to the bridge when native would have done
is merely slower, while answering natively from a graph that is missing files
is a wrong answer that looks right.

`da kg code-capabilities [--json]` reports the decision for a repository: the
pinned release, the selected backend, per-language file counts with their
native/bridge classification, and the resolved backend **per tool** with the
reason.

### Tools the native backend cannot serve, and why

Each of these depends on an upstream capability the native backend does not
have. They are bridge-routed for **every** repository, including a Go-only one.

| Tool(s) | Blocking upstream capability |
|---|---|
| `embed_graph_tool` | vector embeddings via sentence-transformers or a hosted provider |
| `get_bridge_nodes_tool`, `get_suggested_questions_tool` | betweenness centrality, which networkx estimates from an **unseeded random sample** above 5000 nodes — not reproducible even by the release against itself |
| `detect_changes_tool`, `get_minimal_context_tool` | changed-function attribution from git's own `--unified=0` hunk boundaries (xdiff Myers + `xdl_change_compact` + the default indent heuristic), plus git's rename score and diff-output ordering |
| `generate_wiki_tool`, `get_wiki_page_tool` | the markdown wiki materialized under `.code-review-graph/wiki/` |
| `refactor_tool`, `apply_refactor_tool` | the server-side ten-minute refactor-preview store and source rewriting |
| `list_repos_tool`, `cross_repo_search_tool` | the cross-repository registry at `~/.code-review-graph/registry.json` |
| `get_docs_section_tool` | `docs/LLM-OPTIMIZED-REFERENCE.md`, shipped inside the CRG wheel and not present in this repository |

The single largest unblocking item is a faithful xdiff port (Myers +
`xdl_change_compact` with the indent-heuristic scorer, diffcore rename
detection at git's RenameScore 50 including tree-vs-worktree, and git's
diff-queue ordering, oracle-tested against real `git diff --unified=0`). It
alone would return `detect_changes_tool` and `get_minimal_context_tool` — the
release's primary review tool and its documented entry point — to the native
backend.

### Repository coverage: Go only, today

The native scanner extracts Go. The release indexes a much wider set, and it
**collects** a file whenever `detect_language` recognizes its extension — which
includes files that yield no graph nodes, such as a plain CI `.yaml`. Because
`files_parsed` is part of `build_or_update_graph_tool`'s reported result, a
repository containing such a file genuinely produces a different observable
answer natively, so the capability scan counts it as bridge work.

The practical consequence is stated plainly rather than softened: **in Phase A
almost every real repository routes to the bridge**, and the native cutover
serves Go-only repositories. That is why §11.4 bridge removal stays blocked,
and why the bridge is retained as a working dependency rather than as a
formality.

## [B] `crg-bridge` mirror adapter — zero consumers (removable in isolation)

- Package: `internal/adapters/builtin/crg-bridge/` (`bridge.go`, `schema.yaml`,
  `migration_only: true`). `MirrorSnapshot` is called only by tests.
- **No `reads_from:[crg-bridge]` declarations** exist: no in-repo `.agentsrc.lock`,
  no adapter schema declares such a view, and `graph_backend` profiles select
  only `crg` / `none` (the migration-only mirror is not a selectable backend).
- The `reads_from` gate structurally forbids long-term adapters from depending on
  the mirror: `registry.EnforceReadsFrom` (`internal/kg/registry/adapter.go:230`)
  at load and `sdk.MaterializeView` at runtime both reject a non-`migration_only`
  adapter that reaches `crg-bridge` (spec §11.2). Only another `migration_only`
  mirror could legally declare it — and none does.
- `graphstore.BridgeConsumers` (`internal/graphstore/parity_drift.go:29`) is the
  lockfile scanner that implements the §11.4-condition-4 check; it returns empty
  for any in-repo lockfile. `workflow drift` (`commands/workflow/drift.go`) now
  calls it for every repo it checks (`driftBridgeConsumerPhase`) — see the
  runbook below.

The mirror is dead weight and could be dropped on its own, but §11.4 mandates the
two surfaces retire **together**, gated on the parity soak that validates [A]'s
replacement.

## Named / out-of-tree consumers of the CRG graph

All route through the Python `code-review-graph` CLI (surface [A]), which is why
[A] is load-bearing:

- **`da kg` command surface** — `build`, `update`, `code-status`, `impact`,
  `flows`, `communities`, `postprocess`, `changes` (`commands/kg/cmd.go`).
- **CI KG CODE lane** — `.github/workflows/test.yml` installs
  `code-review-graph` into a repo-root `.venv` ("interim coverage until the
  in-process CRG graph adaptor lands") and asserts `da kg build → code-status →
  impact`. Tracked debt: `.agents/proposals/ci-venv-crg-interim.md`.
- **`kg update` post_tool_use hook** — runs on every edit, degrades gracefully
  when the CLI is absent (`commands/kg/sync_code_warm_link.go:203`).
- **MCP server** — `da kg serve` (`graphstore.NewMCPServer`), consumed by Claude
  Code / Cursor via the `code-review-graph` MCP tool family.
- **Review skills** — `build-graph`, `review-delta`, `review-pr` and the
  `mcp__code-review-graph__*` tools.
- **Cross-repo sweep target** — the §11.4 lockfile sweep runs across the
  dot-agents **managed repo set**, which is `~/.agents/config.json`
  (`commands/workflow/drift.go:loadManagedProjects`), i.e. per-install and **not
  enumerable from this repo alone**. `da workflow drift --json` (no `--path`)
  now performs that sweep automatically — see the runbook below.

## §11.4 decommission gate — condition-by-condition

| # | Condition | Status | Evidence |
|---|---|---|---|
| 1 | Parity matrix (8 rows) passing in CI 3 consecutive weeks | **Not met (external)** | Owned by t6a/t6b; `t6b-gate-automation` is `pending` |
| 2 | Behavior-preservation gate on recent-review corpus | **Not met (external)** | Owned by t6b |
| 3 | Migration plan for consumers of the `kg …` surface | **Not met** | No kg-native replacement wired; production registries register only `none`; `RegisterCRGFamily` prod callers = 0 |
| 4 | Zero `reads_from:[crg-bridge]` across managed lockfiles | **Automated; in-repo clear** | `workflow drift` now calls `graphstore.BridgeConsumers` for every repo it checks (in-repo: `bridge_consumer_status=not_a_kg_repo`); run the runbook below on the target installation to sweep the full managed set |

## Runbook: §11.4 criterion 4 (zero `reads_from:[crg-bridge]`)

```bash
# This repo only, independent of the ~/.agents/config.json registry:
da workflow drift --path . --json | jq '.reports[0].bridge_consumer_status, .reports[0].bridge_consumers'

# Every registered project (the managed-repo sweep §11.4 criterion 4 requires):
da workflow drift --json | jq '.bridge_sweep'
# → {consumers_found_repos: [...], clean_repos: [...], not_a_kg_repo_repos: [...], error_repos: [...]}
# Criterion 4 is satisfied iff consumers_found_repos is empty.

# Or the human-readable form (either scope), which also prints per-repo
# bridge_consumer_status inline as a drift warning when consumers are found:
da workflow drift            # full managed-repo sweep
da workflow drift --path .   # this repo only

# scripts/crg-bridge-consumer-audit.sh's [E] section shells out to
# `da workflow drift --path . --json` for this same finding, so the two
# can't diverge — see that script's header comment.
```

## To make t6d READY, in order

1. ~~**Register the kg-native `crg` adapter in production.**~~ **Done (Phase A):**
   `registerBuiltinGraphBackends` (`commands/config/graph_backend.go`) and
   `registerBuiltins` (`commands/kg/lockfile.go`) both call
   `crgbridge.RegisterCRGFamily`, which also runs `registry.EnforceReadsFrom` so
   the §11.2 migration_only gate actually fires.
2. ~~**Re-point [A]'s consumers onto the kg-native path.**~~ **Done (Phase A):**
   `commands/kg/bridge.go`, `commands/kg/sync_code_warm_link.go` and
   `da kg serve` route through `codeGraphProvider` /
   `graphstore.NewMCPServerWithProvider`.
3. **Land + soak the parity gate (t6a/t6b).** Conditions 1–2: 8-row parity matrix
   and behavior-preservation harness green in CI for 3 consecutive weeks. **The
   clock restarts from this cutover**, because the native path is only now the
   one serving production.
4. ~~**Automate condition 4.**~~ **Done.** `workflow drift` calls
   `graphstore.BridgeConsumers` for every repo it checks; run the runbook above
   across the managed repo set on the target installation and confirm
   `bridge_sweep.consumers_found_repos` is empty.
5. **Retire the out-of-tree bindings.** Remove the CI `.venv` `code-review-graph`
   install (`ci-venv-crg-interim.md`) — deliberately **left in place** by Phase A
   so the rollback path stays exercised — and close the language-coverage delta
   above before the bridge is deleted.

Until steps 3–5 hold, the CRG bridge is the supported rollback and **must be kept**.
