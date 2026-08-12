# CRG bridge consumer audit & cutover readiness (t6c)

**Verdict for `t6d-final-bridge-deletion`: `NOT-READY` — KEEP THE BRIDGE.**

This audit proves what still depends on the CRG "bridge" and makes the drift
check reproducible. Task: `graph-backend-adapter-contract / t6c-consumer-audit`
(anti-scope: no deletion; if any consumer still needs `reads_from:[crg-bridge]`,
keep the bridge). The bridge is decommissioned by t6d only when **all four**
§11.4 gate conditions hold; today condition 3 is unmet in-repo, condition 4's
in-repo check is automated and clear (the managed-repo sweep still needs to be
run and confirmed clean on the target installation — see the runbook below),
and conditions 1–2 are external (t6a/t6b CI soak) and also unmet.

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
  Shells out to the `code-review-graph` Python CLI. This is the **load-bearing
  runtime**: every `da kg` code operation and the MCP server go through it.
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
    Production consumers  : 3 file(s)
      commands/kg/bridge.go  (2 ref-lines)
      commands/kg/sync_code_warm_link.go  (11 ref-lines)
      internal/graphstore/mcp_server.go  (1 ref-lines)
    Test-only consumers  : 10 file(s)
    STATUS: LIVE / load-bearing — da kg code ops + MCP server route here.

[B] Migration-only crg-bridge mirror adapter  (internal/adapters/builtin/crg-bridge/)
    Production registration (RegisterCRGFamily / adapter import) : 0
    reads_from:[crg-bridge] declarations (schemas + lockfiles)   : 0
    MirrorSnapshot production callers                            : 0
    STATUS: DEAD WEIGHT — registered nowhere; zero consumers.

[C] graph_backend profile references selecting crg-bridge : 0
    (built-in graph backends are crg / none; the migration-only mirror is not selectable)

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
    3. out-of-tree consumer migration     : NOT MET — no kg-native replacement wired
                                            (both builtin registries register only 'none';
                                             RegisterCRGFamily prod callers = 0)
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

## [A] Python subprocess bridge — live consumers (can they cut over? NO, not yet)

| Consumer | Sites | Role | Cutover |
|---|---|---|---|
| `commands/kg/bridge.go` | `NewCRGBridge` @437, @557 | `da kg code-status` / impact response builders | **Blocked** |
| `commands/kg/sync_code_warm_link.go` | `NewCRGBridge` @147,224,367,436,471,510,546,633; `CRGBridge{}` @268,292,303 | warm-link sync, impact, flows, communities, detect-changes, warm code import, readiness `Status()` | **Blocked** |
| `internal/graphstore/mcp_server.go` | `NewCRGBridge` @84 | MCP server (fronted by `commands/kg/kg.go:714` `da kg serve`) | **Blocked** |

Definition/self-use site (what t6d actually deletes): `internal/graphstore/crg.go`
plus `crg_venv_unix.go` / `crg_venv_windows.go`.

**Why blocked:** there is no kg-native replacement wired into production. Both
builtin registries register **only** the `none` adapter —
`commands/config/graph_backend.go:15` (`registerBuiltinGraphBackends`) and
`commands/kg/lockfile.go:38` (`registerBuiltins`). The kg-native `crg` adapter
and its composite entry point `crgbridge.RegisterCRGFamily`
(`internal/adapters/builtin/crg-bridge/bridge.go:76`) have **zero** production
callers, so nothing serves `da kg` today except the Python subprocess. Deleting
`crg.go` would fail `go build` in `commands/kg/*` and `mcp_server.go`.

**Scope gap flagged for t6d:** the live consumers in `commands/kg/*` are
**outside** t6d's declared write_scope. t6d as written cannot migrate them, so
its write_scope must be widened (or a predecessor task must re-point those
callers) before deletion is even mechanically possible.

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

1. **Register the kg-native `crg` adapter in production.** Have
   `registerBuiltinGraphBackends` (`commands/config/graph_backend.go`) and
   `registerBuiltins` (`commands/kg/lockfile.go`) call
   `crgbridge.RegisterCRGFamily` (or `crg.Register`) instead of `none` only.
2. **Re-point [A]'s consumers onto the kg-native path.** Migrate
   `commands/kg/bridge.go`, `commands/kg/sync_code_warm_link.go`, and
   `internal/graphstore/mcp_server.go` off `NewCRGBridge` onto the adapter/Store
   path. **Widen t6d's write_scope to include `commands/kg/`** (currently absent).
3. **Land + soak the parity gate (t6a/t6b).** Conditions 1–2: 8-row parity matrix
   and behavior-preservation harness green in CI for 3 consecutive weeks.
4. ~~**Automate condition 4.**~~ **Done.** `workflow drift` calls
   `graphstore.BridgeConsumers` for every repo it checks; run the runbook above
   across the managed repo set on the target installation and confirm
   `bridge_sweep.consumers_found_repos` is empty.
5. **Retire the out-of-tree bindings.** Remove the CI `.venv` `code-review-graph`
   install (`ci-venv-crg-interim.md`), repoint the hook + MCP + review skills at
   the kg-native surface.

Until steps 1–4 hold, the CRG bridge is load-bearing and **must be kept**.
