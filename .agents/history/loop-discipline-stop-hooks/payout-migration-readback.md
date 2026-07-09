# Payout Migration Readback — `p6-payout-backfill`

- task: `p6-payout-backfill`
- plan: `loop-discipline-stop-hooks`
- contract: `.agents/workflow/plans/loop-discipline-stop-hooks/tasks/p6-payout-backfill.contract.md`
- downstream target: `/Users/nikashp/proj-docs/payout`
- executed: 2026-07-09 (building on two earlier direct-in-live-tree passes,
  2026-07-07 and 2026-07-08, whose substance this pass re-derives cleanly and
  extends — see "Execution history" below)
- payout PR: [payout-wrk#2](https://github.com/NikashPrakash/payout-wrk/pull/2)
  (`omp-p6-payout-backfill` -> `main`)

## Execution history (why this isn't a from-scratch migration)

Payout already had two earlier, uncommitted-to-a-PR migration passes sitting
directly on its live `docs/luma-backend-contracts` working branch:

1. **2026-07-07** — an initial `p6-payout-backfill` pass created the
   `.agents/workflow/plans/loop-discipline-stop-hooks-backfill/` plan bundle,
   migrated `.agentsrc.json` v1 -> v2 (hooks, `isp`/`loop-worker`), and ran one
   `iteration-close` hook-sentinel dry run. Recorded as a "known gap" that a
   clean `da install --dry-run` still skipped several declared skills.
2. **2026-07-08** — a sibling task (`PayoutTeamConfig`, dot-agents
   config-distribution engineer) added the `po-agents-config` git source +
   `extends: ["po-agents-config:layers/execution-profile.json"]` team
   execution-profile layer on top, committed as payout commit `8e5420d`.

Both passes landed **directly on `docs/luma-backend-contracts`**, a feature
branch carrying ~65 unrelated in-flight paths (go-modular-monolith-migration,
luma real-backend order-entry work, several submodule bumps) that the task
brief explicitly forbids using as a PR base. This task's job was therefore
two-fold: (a) re-derive the same declarative delta cleanly on top of payout's
real mainline so it is committable/reviewable in isolation, and (b) close a
gap the first two passes left open (skill resolution — see below), then write
this readback.

Re-derivation was done in an isolated `git worktree`
(`/tmp/payout-p6-backfill`, off `origin/main`, payout's confirmed default
branch) so the live, dirty `~/proj-docs/payout` working tree — actively used
by several other concurrently-running sessions — was never touched, branch-
switched, or written to. It was read-only inspected to recover the exact
target `.agentsrc.json` shape and cross-checked with `PayoutTeamConfig` over
IRC to confirm its source/extends hunk and avoid drifting from its intent.

## Pre-migration resolved state

Payout's `origin/main` (the true, unmigrated baseline — confirmed by
`git show origin/main:.agentsrc.json`) matches the contract's "Grounded
Starting State" exactly:

- `.agentsrc.json`: schema `version: 1`, `hooks: false`, unnamed `sources`
  (`local`, one `git` source, no `id` fields), no `repo_id`, `agents: [test-
  runner, verifier, security-reviewer]` (no `orchestrator`), `skills` declares
  `iteration-close` and `delegation-lifecycle` but **not** `isp` or
  `loop-worker` (`agent-handoff` was already present).
- No `.agentsrc.lock`.
- Resolved skill/hook paths for a hypothetical clean checkout: `da install
  --dry-run` against this baseline links the already-global skills
  (`agent-handoff`, `agent-start`, `build-graph`, ... `self-review`,
  `skill-architect`, `split-reviewable-commits`) from
  `~/.agents/skills/global/<name>`, and reports `iteration-close`,
  `delegation-lifecycle`, `isp` (n/a pre-migration), `loop-worker` (n/a
  pre-migration) as unresolved because they exist only under
  `~/.agents/skills/dot-agents/<name>` on this operator's machine, not
  `global/` or `payout/`. `hooks: false` means no `.claude/settings.local.json`
  hook wiring was expected.

## `.agentsrc.json` delta

```
  "version": 1,                              ->  "version": 2,
  skills: [... no isp/loop-worker ...]       ->  skills: [..., "isp", "loop-worker"]  (agent-handoff, delegation-lifecycle, iteration-close already present)
  agents: [test-runner, verifier,            ->  agents: [..., "orchestrator"]
           security-reviewer]
  "hooks": false                             ->  "hooks": true
  sources: [{"type":"local"},                ->  sources: [{"type":"local","id":"local"},
            {"type":"git","url":"..."}]                {"type":"git","url":"...","ref":"main","id":"agents-config"},
                                                         {"type":"git","url":"git@github.com:PayoutPos/po-agents-config.git","ref":"develop","id":"po-agents-config"}]
  (no repo_id)                               ->  "repo_id": "github.com/NikashPrakash/payout-wrk"
  (no extends)                               ->  "extends": ["po-agents-config:layers/execution-profile.json"]
  "refresh": {"version":"dev",...}           ->  (dropped — stale hand-authored block; v2 schema requires
                                                   commit/describe on it and it is tool-written by da install/
                                                   da refresh, not hand-maintained)
  (no lock)                                  ->  .agentsrc.lock added (committed, tool-written by da config sync)
```

## Migration / refresh operations used (sanctioned `da` flow only)

All performed against the isolated worktree, using the dev binary built per
the task brief (`go -C ~/proj-docs/dot-agents build -o /tmp/da-selfdev
./cmd/da`):

1. `da config migrate` — v1 -> v2 schema bump (no legacy `verifier_profiles`/
   `reviewer_profiles`/`app_type_verifier_map` keys were present to fold).
2. Declarative hand-edit of `.agentsrc.json` on top (the manifest is
   hand-authored by design — `docs/LAYERED_CONFIG_GUIDE.md`: "You edit the
   manifest; dot-agents writes the lockfile"): `hooks: true`, `isp`/
   `loop-worker` skills, `orchestrator` agent, `repo_id`, source `id` fields,
   the `po-agents-config` source + `extends` layer, dropped the stale
   `refresh` block.
3. `da config lint` — iterated until 2/2 passed (caught the stale `refresh`
   block missing `commit`/`describe` before the fix).
4. `da config sync` — fetched `po-agents-config:layers/execution-profile.json`
   for real against `origin/main`'s own resolution context (not copied from
   the earlier live-tree lock) and wrote `.agentsrc.lock`.
5. `da config verify` — 6 checks passed, 1 warning (see "Notable tool
   observations" below).
6. `da install` (real run, not `--dry-run`) — resolved sources, materialized
   Cursor/Claude Code/Codex CLI/GitHub Copilot platform links, recorded the
   install stamp in `.agentsrc.lock`.
7. `da skills promote <name>` for `isp`, `loop-worker`, `iteration-close`,
   `delegation-lifecycle` (see "Skill-resolution fix" below) — the sanctioned
   command for promoting a repo-local skill into payout-scoped shared
   storage, run **after** staging each skill's canonical content
   (`internal/scaffold/home/starter/skills/global/<name>`) into the
   worktree's gitignored `.agents/skills/<name>/`.
8. `da workflow app-types` — confirms all 10 declared `app_type`s resolve.
9. `da workflow hook-sentinel write/clear` + direct `gate.sh` invocation —
   the representative dry runs (below).
10. `da workflow task add` / `da workflow advance` / `da workflow plan
    update` — populated and completed the payout-side plan bundle via the
    canonical workflow CLI rather than hand-editing YAML state.

## L1 — unified-config profile model

Payout's `po-core-api-se` (Go), the Python services (`client-se`,
`manager-se`, `sync-engine`), and the two Next.js UIs are exactly the
canonical multi-profile case cited in the contract. Post-migration,
`da workflow app-types` resolves all 10 declared `app_type`s from the
`po-agents-config:layers/execution-profile.json` team layer:

```
cicd, gitops, go-http-service, poc, python-batch, python-http-api,
python-library, python-streaming, ts-native-ui, ts-web-ui
```

`da config explain --all` shows `execution_profile` resolving from
`po-agents-config:layers/execution-profile.json` (origin), confirming the
selector-merge layer engine is live for payout, not just declared.

## L2 — distributable-config-manifest / `init --from`

Payout's `sources`/`extends` now follow the `<source-id>:<layer-path>[@ref]`
distributable-layer shape documented in `LAYERED_CONFIG_GUIDE.md` (the
"add a source, `extends` a layer, `da config sync` locks it" flow is
literally payout's own worked example in that guide — `docs/
LAYERED_CONFIG_GUIDE.md` line ~357 names `payout` directly as an expected
`da config migrate` consumer during the deprecation soak). `da init --from
<manifest-ref>` itself is a **home-scope** (`~/.agents`) bootstrap verb, not
a per-project one — it was not run against payout because payout is an
already-`da add`-registered project on an already-initialized operator
machine, not a fresh-machine adoption; the relevant reproducibility guarantee
for an *existing* project is `da config sync` + `da install`, both exercised
above. `packages`-based skill/artifact distribution (the mechanism that
would let a project pull a skill bundle from a declared source rather than
requiring it to already sit in `~/.agents/skills/`) is declared in the schema
but **not yet resolved by the shipped resolver**
(`docs/concepts/config-model.md`: "packages are declared in the manifest, but
the shipped resolver does not resolve them into materialized skills yet") —
this is the root cause of the skill-resolution gap below, and is out of this
task's scope to implement.

## L3 — home-config machine-local split / `kind:project-set` identity registry

Verified, no payout-side change was needed. On the operator machine:

- `~/.agents/config.json` (synced, git-tracked): `"projects": {"payout": {},
  "dot-agents": {}, "ResumeAgent": {}}` — portable identity only, **no path**.
- `~/.agents/local/bindings.json` (machine-local, gitignored via the
  top-level `local/` pattern): `"payout": {"path":
  "/Users/nikashp/proj-docs/payout", "added": "2026-03-13T04:24:43Z"}` — the
  machine-local `id -> absolute path` binding, correctly excluded from the
  synced tree.
- `~/.agents/cache/` is likewise untracked (though not yet explicitly
  gitignored by name at the top-level `~/.agents/.gitignore` — noted as a
  pre-existing, dot-agents-repo-level observation, not a payout defect and
  not touched by this task).

This already matches the phase-0 model from `home-config-portability`
(portable identity registry synced, binding table + caches machine-local).
Nothing in this task's write-scope needed to change for L3.

## Skill-resolution fix (the gap this pass closes)

Root cause: dot-agents' embedded starter
(`internal/scaffold/home/starter/skills/global/{isp,loop-worker,
iteration-close,delegation-lifecycle,provider-consumer-pair,
plan-wave-picker}`) ships all six skills as **global** (cross-project), but
this operator machine's persisted `~/.agents/skills/` still has them scoped
only to the `dot-agents` project (`~/.agents/skills/dot-agents/<name>`,
content diverged from the current starter version) — a pre-dates-the-
starter-update artifact. A clean `da install` for *any other* project
therefore reported `! skill '<name>' not found in any source — skipping` for
all six.

Fix, scoped strictly to payout: staged each of `isp`, `loop-worker`,
`iteration-close`, `delegation-lifecycle` from the canonical starter content
into the worktree's repo-local, gitignored `.agents/skills/<name>/`, then ran
`da skills promote <name>` — which promotes repo-local skill content into
`~/.agents/skills/payout/<name>/` and converges the repo-local copy to a
managed symlink. This **does not touch** `~/.agents/skills/global/` or
`~/.agents/skills/dot-agents/`, so no other project or any of the several
concurrently-running sibling agent sessions on this machine were affected.
`agent-handoff` needed no fix — it already resolved via the pre-existing
`~/.agents/skills/global/agent-handoff`.

**Deliberately not fixed (flagged for P7, not silently replaced):**
`plan-wave-picker` and `provider-consumer-pair` — both declared in payout's
manifest *before* this task, both hitting the identical "scoped to
dot-agents, not global" cause, but outside the required-skill set this task
owns (`iteration-close`, `isp`, `loop-worker`, `agent-handoff`,
`delegation-lifecycle`). Fixing them here would be a drive-by outside this
task's declared scope. The durable fix for the whole class — a home-config
sync/upgrade path that reconciles a machine's skill-scope assignments
against the current starter, or shipping the `packages` resolver — belongs
to a dedicated cross-cutting task, not a payout-scoped backfill.

## Post-migration skill resolution

| Skill | Resolves for payout (project-scoped)? | Path |
|---|---|---|
| `iteration-close` | Yes | `.claude/skills/iteration-close -> ~/.agents/skills/payout/iteration-close` |
| `isp` | Yes | `.claude/skills/isp -> ~/.agents/skills/payout/isp` |
| `loop-worker` | Yes | `.claude/skills/loop-worker -> ~/.agents/skills/payout/loop-worker` |
| `agent-handoff` | Yes (pre-existing) | `.claude/skills/agent-handoff -> ~/.agents/skills/payout/agent-handoff` |
| `delegation-lifecycle` | Yes | `.claude/skills/delegation-lifecycle -> ~/.agents/skills/payout/delegation-lifecycle` |
| `plan-wave-picker` | No (pre-existing gap, out of scope) | not found in any source |
| `provider-consumer-pair` | No (pre-existing gap, out of scope) | not found in any source |

`da config explain skills` (config-level resolution, distinct from platform
materialization) has always shown all declared skills as effective for
payout — the gap closed here was specifically the *platform-link
materialization* layer (`da install`'s skill-linking phase), not config
resolution.

## Materialized gate hooks and platform outputs

`da install` materialized, for payout, in the isolated worktree:

- `.claude/settings.local.json` — `PreCompact`, `PreToolUse`, `Stop`,
  `SubagentStart`, `SubagentStop` all wired to the global `isp-gate`,
  `iteration-close-gate`, and `loop-worker-gate` scripts under
  `~/.agents/hooks/global/`.
- `.cursor/hooks.json` — equivalent Cursor wiring.
- `.github/hooks/*.json` — equivalent GitHub Copilot wiring (session-start,
  pre-tool-use, pre-compact, stop, subagent-start, subagent-stop, several
  numbered variants for composed hooks).
- `.codex/hooks.json` — equivalent Codex CLI wiring.
- `.claude/skills/*`, `.codex/agents/*.toml`, `.github/agents/*.agent.md` —
  skill/agent platform projections (see resolution table above).

Per the task brief, these generated platform outputs are **verification**,
not something committed by hand — the payout PR commits only the declarative
`.agentsrc.json`/`.agentsrc.lock`/plan-bundle inputs; the generated
`.claude/`/`.cursor/`/`.codex/`/`.github/hooks` outputs remain local,
regenerable via `da install`/`da refresh` by anyone with the committed
manifest (consistent with the existing, pre-migration `.gitignore`/tracked-
state split already present in payout — the live tree already left these
generated-but-not-yet-gitignored paths uncommitted before this task, and
this pass changes nothing about that).

## Artifact-preservation confirmation (zero live-artifact loss)

The isolated worktree was checked out fresh from `origin/main` and the
migration applied on top — `git status --porcelain` in that worktree, both
before commit and confirmed again in the final diff against `origin/main`,
shows **additive changes only**:

- `.agentsrc.json` modified in place (the intended delta above).
- `.agentsrc.lock` added (new).
- `.agents/workflow/plans/loop-discipline-stop-hooks-backfill/` added (new —
  this task's own plan bundle, contract target #6).
- `.agents/history/loop-discipline-stop-hooks-backfill/hook-sentinels/
  2026-07-09/` added (new — dry-run evidence).

Nothing under `.agents/active/`, any other `.agents/workflow/plans/<id>/`, or
any other `.agents/history/<id>/` was touched, removed, or overwritten.
Separately, the **live** `~/proj-docs/payout` working tree (on
`docs/luma-backend-contracts`, carrying the two earlier in-progress passes
plus ~65 unrelated paths from concurrent go-modular-monolith-migration and
luma real-backend work) was **read-only inspected** throughout this task —
no branch switch, no file write, no git operation that could disturb it or
the several other sibling agent sessions actively working inside it.

## Representative dry run / sandboxed hook invocation

Three `da workflow hook-sentinel write` -> `gate.sh` invoke -> `hook-sentinel
clear` cycles were run in the isolated worktree, archived at
`.agents/history/loop-discipline-stop-hooks-backfill/hook-sentinels/
2026-07-09/` in the payout PR:

1. **`isp-gate`, `pre_compact` mode** (sentinel `isp-p6dryrun2`): non-blocking
   advisory naming the active run/plan/task and the next unresolved stage
   (rule `isp.R2.6`). Exit 0.
2. **`isp-gate`, `stop` mode**, same sentinel: non-blocking advisory (no
   readable transcript supplied, so the trace-dependent rules R2.2/R2.3
   correctly deferred as designed). Exit 0.
3. **`loop-worker-gate`, `stop` mode** (sentinel `loop-worker-p6dryrun3`),
   matching `--write-scope` and the expected `TASKS.yaml` artifact present:
   silent pass. Exit 0.
4. **Negative control** — `isp-gate`, `stop` mode, sentinel `isp-p6negtest`
   with `--expect` pointed at a nonexistent artifact path: returned
   `{"decision":"block","reason":"...Missing: .../NONEXISTENT.yaml..."}` and
   **exited 2** — proves the gate genuinely enforces (blocks) rather than
   unconditionally passing.

All four ran against the real global gate scripts
(`~/.agents/hooks/global/{isp,loop-worker}-gate/gate.sh`) with
`DA_HOOK_PLATFORM=claude`, in the isolated worktree, so they exercise the
exact code path a live Claude Code session in payout would hit.

## Notable tool observations (not payout-specific, recorded for awareness)

- `da skills promote --dry-run` does not actually preview — it writes for
  real even with the flag set. Not blocking here (the payout-scoped
  destination was the intended outcome regardless), but worth a dot-agents
  fix independent of this task.
- `da config verify`'s `config-staleness` check reports a `warn` even
  immediately after a fresh `da config sync` with an unchanged
  `inputs_digest` (the abbreviated digest shown for "lock" and "now" are
  identical). Did not block `lint`/`install`; looks like a display/comparison
  quirk in the check, not a payout defect.

## Intentional project-local overrides / gaps recorded for P7

- `plan-wave-picker` / `provider-consumer-pair` skill-resolution gap
  (pre-existing, out of this task's required-skill scope — see above).
- The general "clean clone -> `da install` -> zero skips" reproducibility
  gap for any dot-agents-scoped-not-global skill remains open until either a
  home-config sync/upgrade path ships, or the `packages` artifact resolver
  is implemented.
- No payout-local policy overrides (deny-locks, value-locks, or
  `override_permissions`) were introduced by this migration — payout adopts
  the team execution-profile layer as-is.

## L4 readiness note

No L4 (multi-harness descriptor schema) work was performed or is required
here — per the contract, this task must not block on it. Payout's `sources`/
`extends`/`repo_id` v2 shape and its `kind:profile`/`kind:manifest`-ready
manifest surface carry forward without rework once L4 ships; nothing in this
migration hard-codes an assumption L4 would need to unwind.
