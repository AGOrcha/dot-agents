# TypeScript port — current Go ↔ TS gap audit

This memo is the row-level inventory of every Go ↔ TS divergence the boundary
contract in `docs/TYPESCRIPT_PORT_BOUNDARY.md` has to encode. It feeds two
downstream artifacts:

- **tp2** (`docs/typescript-port-boundary.json`) — machine-readable schema of
  this same classification.
- **tp3** (CI sync pipeline) — diffs the live Go cobra tree against the JSON
  spec on every PR.

Authority: every classification in this memo MUST trace to the Phase 4
decision in `docs/TYPESCRIPT_PORT_BOUNDARY.md`. Anything in this memo that
contradicts the boundary doc is a bug in this memo, not in the boundary.

Conventions used below:

- **Stage 1 + missing** — must mirror in TS per the Phase 4 boundary
  ("workflow / KG / orchestration" stays Go-only, but Stage 1 commands must
  ship in both ports). A gap here is a sync-pipeline failure.
- **Phase 4 opt-out** — intentionally Go-only. The boundary explicitly puts
  this surface in `kg`, workflow writes, or full orchestration territory.
  TS does not, and will not, mirror it.
- **Phase 5 deferred** — Stage 2 surface that the TS port has not yet
  implemented but is allowed to add later under option 2 (read-only workflow
  / Stage 2 status surfaces). Not a sync-pipeline failure today; tracked so
  it can graduate without ambiguity.

---

## 1. Top-level command parity

Source of truth (Go): `commands/root.go` `NewRootCommand()`
(19 commands today): `add`, `agents`, `doctor`, `explain`, `hooks`, `import`,
`init`, `install`, `kg`, `mcp`, `refresh`, `remove`, `review`, `rules`,
`settings`, `skills`, `status`, `sync`, `workflow`.

Source of truth (TS dispatch): `ports/typescript/src/cli.ts`
(8 commands wired today): `init`, `add`, `refresh`, `status`, `doctor`,
`skills`, `agents`, `hooks`. The TS tree also contains library modules
`src/commands/workflow.ts` and `src/commands/kg.ts` that are **not** wired
to the CLI dispatcher; they exist for tests and embedding only, matching the
"library surface, not CLI surface" wording in `ports/typescript/README.md`.

| Go top-level | TS CLI today | Classification | Rationale (boundary doc anchor) |
|---|---|---|---|
| `init` | implemented | **Stage 1 + mirrored** | Boundary §"What the TypeScript port implements today (Stage 1)" lists `init` as required Stage 1 surface. |
| `add` | implemented | **Stage 1 + mirrored** | Stage 1 list. |
| `refresh` | implemented (read-only report) | **Stage 1 + mirrored** | Stage 1 list. TS implementation is reduced to a status report rather than full link re-application; see §2 row. |
| `status` | implemented (canonical buckets + projects) | **Stage 1 + mirrored** | Stage 1 list, plus the explicit Phase 5 alignment in boundary §"Phase 5 — Stage 2 buckets and canonical store (`status`)". |
| `doctor` | implemented (structural checks) | **Stage 1 + mirrored** | Stage 1 list. TS doctor covers home + config + project paths only; per-platform link audit is deferred. |
| `skills` | implemented (`list`, `new`) | **Stage 1 + mirrored** (with row-level Stage 2 deferrals — see §2) | Stage 1 list. |
| `agents` | implemented (`list`, `new`) | **Stage 1 + mirrored** (with row-level Stage 2 deferrals — see §2) | Stage 1 list. |
| `hooks` | implemented (`list`) | **Stage 1 + mirrored** (with row-level Stage 2 deferrals — see §2) | Stage 1 list. |
| `explain` | absent | **Phase 4 opt-out** | Help/topic browser tied to Go's embedded docs and `--help` infrastructure. Boundary doc keeps Go authoritative for full `--help` surface ("Parity limits are repeated in `--help` so users are not steered toward expecting Go-only commands in TS"). The TS port instead emits its own `boundaryHelpLines()` — TS does not need a Go-style `explain` topic browser to satisfy Stage 1. |
| `import` | absent | **Phase 4 opt-out** | Cross-platform discovery / canonicalization of existing repo configs into `~/.agents/`. Reuses the same canonical mappers and platform list that drive `add`/`refresh` link creation; the boundary explicitly keeps the full link-and-canonicalization machinery in Go (Stage 1 TS scope is "Load and save `.agentsrc.json`" + structural status, per `ports/typescript/README.md` §"Current scope"). |
| `install` | absent | **Phase 4 opt-out** | `install` materializes declared skills/agents/hooks/MCP configs from `.agentsrc.json` sources (including git remotes) into `~/.agents/`, then drives full per-platform link application. This is the manifest-resolver counterpart to `add`/`refresh` and lives behind the same Go-only platform/links machinery. The TS port deliberately omits source resolution and full link writing. |
| `kg` | absent (TS module is read-only stub) | **Phase 4 opt-out** | Boundary §"Permanently deferred from the TypeScript port": "All `kg` / knowledge-graph commands (query, ingest, bridge, sync, setup, …)". `ports/typescript/src/commands/kg.ts` exists only as a stub for tests; `runKgQuery` always returns `"KG query requires Go CLI — not available in TS port"`. |
| `mcp` | absent | **Phase 4 opt-out** | Project/global MCP server config write surface. Behaves like `rules` / `settings` / `hooks` — managed-resource CRUD over the canonical store. The boundary keeps Stage 1 TS scope to `init`/`add`/`refresh`/`status`/`doctor` plus the resource-listing variants (`skills`, `agents`, `hooks`) actually shipped; other resource buckets (`mcp`, `rules`, `settings`) follow the same management contract and are intentionally not part of Stage 1 today. |
| `refresh` (covered above) | implemented (report) | — | — |
| `remove` | absent | **Phase 4 opt-out** | Deletes a registered project: backups, link teardown, `config.json` mutation. Mirror of the `add` write path; TS only owns project-registry add (no destructive teardown today). Stage 1 boundary does not require it. |
| `review` | absent | **Phase 4 opt-out** | Operates on global proposals under `~/.agents/proposals/` (`show`/`approve`/`reject`). Proposal application writes into `~/.agents/` rules/skills/hooks/MCP. This is part of the orchestration / shared-store mutation surface; boundary §"Permanently deferred" keeps full orchestration parity in Go. |
| `rules` | absent | **Phase 4 opt-out** | Resource-bucket CRUD (`list`/`show`/`remove`) for `~/.agents/rules/`. Same family as `mcp`/`settings`; not in the Stage 1 list. |
| `settings` | absent | **Phase 4 opt-out** | Resource-bucket CRUD (`list`/`show`/`remove`) for `~/.agents/settings/`. Same family as `mcp`/`rules`. |
| `sync` | absent | **Phase 4 opt-out** | Git sync of the canonical store (`init`/`pull`/`push`/`commit`/`status`/`log`). Inherently a Go-side write path bound to the local Git installation and the canonical store ownership model; not Stage 1 surface. |
| `workflow` | absent from CLI; library-only in TS | **Phase 4 opt-out for writes / Phase 5 deferred for read-only CLI exposure** | Boundary §"Permanently deferred" lists every workflow **mutating** subcommand (`checkpoint`, `advance`, `merge-back`, `fanout`, `verify record`, `sweep`, delegation closeout, `fold-back create`, …) as Go-only. The same section §"In scope for optional future TypeScript work (under option 2)" allows **read-only** subsets in TS later. The TS port already ships `runWorkflowOrient` / `runWorkflowTasks` / `runWorkflowHealth` as a library, but no CLI command wires them — that wiring is the Stage 2 read-only `workflow` subset Phase 4 explicitly **allows but does not commit to**. |

**Counters and false-positive guards.**

- `workflow` is **not** "Stage 1 + missing" — boundary §"What the TypeScript
  port implements today (Stage 1)" excludes it explicitly: "no `workflow`
  or `kg` commands yet".
- `kg` is **not** "Phase 5 deferred" — boundary marks it permanently
  deferred. Future plugin or read-only KG library work would still ship
  through Go MCP, not by adding a TS `kg` command.
- `install` is **not** Stage 1 even though it shares scaffolding with
  `add`/`refresh`. The Stage 1 list in the boundary doc is enumerated
  ("`init`, `add`, `refresh`, `status`, `doctor`, `skills`, `agents`,
  `hooks`") and `install` is intentionally absent.

---

## 2. Subcommand depth per Stage 1 command

For each Stage 1 command this section enumerates every Go subcommand and
flag (from the Cobra `Use:` / `Flags()` blocks) and marks TS coverage as one
of:

- **implemented** — TS dispatcher handles the same shape (subcommand and
  flag).
- **partial** — TS dispatcher accepts the subcommand but with reduced
  semantics (for example refresh as a status report rather than a writer).
- **missing** — TS does not handle this row; this is the only row class
  that should fail tp3's CI sync diff, and only when the Go row is also
  inside the Stage 1 flag-lock surface in `tp2`'s JSON spec.
- **deferred-by-design** — explicitly outside the Stage 1 contract per the
  boundary doc (for example destructive promote/remove subcommands that
  rewrite repo links). Captured here so tp2's JSON spec can mark them
  inside `phase5_deferred` or `stage2_deferred_subitems`.

### `init`

Source: `commands/init.go`. TS: `ports/typescript/src/commands/init.ts`.

| Go subcommand / flag | TS coverage | Notes |
|---|---|---|
| `dot-agents init` (no subcommands) | implemented | TS creates the standard dirs plus `CANONICAL_BUCKET_SPECS` global-scope dirs, matching `commands/init.go`. |
| `--dry-run` (persistent global) | implemented | Honored as `InitOptions.dryRun`. |
| `--force` (persistent global) | implemented | Honored as `InitOptions.force`; reuses skip-vs-recreate semantics. |
| Confirmation prompt (`--yes`) | deferred-by-design | TS dispatcher is non-interactive; no prompt to suppress. |
| Starter template writes (`config.json`, `rules.mdc`, `claude-code.json`, `.gitignore`, `README.md`) | deferred-by-design | Boundary keeps starter-template seeding Go-side; TS only creates directories so a fresh Go-managed home is later writable. |
| Workflow hook bundle scaffolding (`scaffoldhooks.CopyMissingGlobalBundles`) | deferred-by-design | Hook bundle templates live in Go's `internal/scaffold/hooks` static asset tree. |
| KG MCP config writeout (`ensureGlobalKGMCPConfigs`) | deferred-by-design | Boundary §"Permanently deferred" — no `kg` material is written by the TS port. |
| Claude Code global settings symlink + Cursor hooks hardlink | deferred-by-design | Per-platform link writes are Go-only Stage 1 scope; TS `init` is config-shape only. |

### `add <path>`

Source: `commands/add.go`. TS: `ports/typescript/src/commands/add.ts`.

| Go subcommand / flag | TS coverage | Notes |
|---|---|---|
| `dot-agents add <path>` | implemented | TS resolves `path`, derives a name (`basename`), and registers in `config.json` via `addProject`. |
| `--name <name>` | implemented | Honored as `AddOptions.name`. |
| `--force` | implemented | Honored as `AddOptions.force`; flips `already_registered` → `updated`. |
| `--dry-run` | deferred-by-design | TS is a single config write; no preview pass to short-circuit. |
| Project-name validation regex (`^[a-zA-Z0-9_-]+$`) | missing | Go enforces; TS accepts any basename string. Lock as a stage1 validation row in tp2 if we want CI to enforce parity. |
| Repo scan + AI config discovery (`scanExistingAIConfigs`, `aiScanPatterns`) | deferred-by-design | Discovery / informational scan; not part of registry add. |
| Backup of conflicting root-level configs (`backupExistingConfigsList`) | deferred-by-design | Backup writeout into `~/.agents/resources/` is Go-only Stage 1 scope. |
| Per-platform link creation (`p.CreateLinks`, `RunSharedTargetProjection`) | deferred-by-design | All link writes are Go-only. |
| `restoreFromResources(...)` | deferred-by-design | Restoration of canonical resources from `~/.agents/resources/<project>/` requires the Go canonicalization mappers. |
| Project KG MCP config writeout (`ensureProjectKGMCPConfigs`) | deferred-by-design | KG-related; permanently Go-only. |

### `refresh [project]`

Source: `commands/refresh.go`. TS: `ports/typescript/src/commands/refresh.ts`.

| Go subcommand / flag | TS coverage | Notes |
|---|---|---|
| `dot-agents refresh [project]` | partial | TS resolves managed projects, checks each path, and emits `{ status: "ok" \| "missing_path" \| "not_found" }` per project. No links re-created. |
| `--import` (run `import` first) | missing | TS does not currently run an import pre-pass; a Stage 1 sync would have to either make the flag a no-op stub or implement read-only equivalent. |
| `--dry-run` | deferred-by-design | TS is read-only by construction — every TS refresh is effectively a dry run. |
| Per-platform link refresh (`p.CreateLinks`) | deferred-by-design | Go-only by boundary. |
| Manifest refresh metadata writeback (`projectsync.WriteRefreshToAgentsRC`) | deferred-by-design | Go-only by boundary. |

### `status`

Source: `commands/status.go`. TS: `ports/typescript/src/commands/status.ts`.

| Go subcommand / flag | TS coverage | Notes |
|---|---|---|
| `dot-agents status` | implemented | TS reports `agents_home`, `config.json`, projects, and the same canonical bucket counts (scope/item) that `commands/status.go` emits. |
| `--audit` | missing | Per-platform link-level audit. Stage 1 surface — could be fed into tp2 if we want CI to lock its presence; currently the TS dispatcher silently ignores the flag. |
| `--agent <id>` | missing | Filter to a single platform; same family as `--audit`. |
| `--json` (persistent global) | partial | Go emits `statusJSONReport`; TS dispatcher prints text only, but `runStatus()` returns a structured `StatusResult` library callers can JSON-encode. |
| Plugin spec listing section (`printPluginsSection` / `ListPluginSpecs`) | deferred-by-design | See §3 — Stage 2 deferred per boundary §"Phase 5". |

### `doctor`

Source: `commands/doctor.go`. TS: `ports/typescript/src/commands/doctor.ts`.

| Go subcommand / flag | TS coverage | Notes |
|---|---|---|
| `dot-agents doctor` | partial | TS reports `agents_home`, `config.json`, and per-project path / `.agentsrc.json` presence. |
| `--verbose` (persistent global) | implemented | TS adds `ok` rows per healthy project under `--verbose`. |
| `--dry-run` (persistent global) | deferred-by-design | Doctor is read-only in both ports. |
| Platform installation detection (`platform.All().IsInstalled()`) | missing | Go enumerates Cursor / Claude / Codex / OpenCode / Copilot install state; TS does not. Stage 1 candidate to lock in tp2. |
| User-level config link health (`collectBrokenUserLinks`) | deferred-by-design | Link audit is Go-only Stage 1 scope. |
| Manifest health checks | missing | Go inspects `.agentsrc.json` shape; TS only checks file presence. |

### `skills`

Source: `commands/skills.go`. TS: `ports/typescript/src/commands/skills.ts`,
dispatcher in `cli.ts`.

| Go subcommand / flag | TS coverage | Notes |
|---|---|---|
| `skills list [project]` | implemented | TS reads `~/.agents/skills/<scope>/` and emits name + description from `SKILL.md` frontmatter. |
| `skills new <name> [project]` | implemented | TS creates `<scope>/<name>/SKILL.md` scaffold. Differs from Go in two side effects: |
| ↳ Frontmatter-only `.agentsrc.json` append (`appendSkillToAgentsRC`) | missing | Go appends `name` to project `.agentsrc.json` `skills[]` for non-global scope. |
| ↳ User-level skill symlinks (`ensureUserSkillLinks`) | deferred-by-design | Symlinks into `~/.agents/skills` and `~/.claude/skills` are link-write surface; Go-only. |
| `skills promote <name>` | deferred-by-design | Promotes a repo-local skill, registers it in `.agentsrc.json`, and refreshes shared mirrors — link-write + canonical mirror; Go-only. Stage 2 deferred sub-item if/when read-only "promote dry-run" is desired. |

### `agents`

Source: `commands/agents/cmd.go`. TS: `ports/typescript/src/commands/agents.ts`,
dispatcher in `cli.ts`.

| Go subcommand / flag | TS coverage | Notes |
|---|---|---|
| `agents list [project]` | implemented | TS reads `~/.agents/agents/<scope>/` and emits name + description from `AGENT.md` frontmatter. |
| `agents new <name> [project]` | implemented | TS scaffold writes `<scope>/<name>/AGENT.md`. |
| `agents promote <name>` | deferred-by-design | Same family as `skills promote`: link-write + manifest mutation, Go-only. |
| `agents promote --force` | deferred-by-design | Subset of `promote`. |
| `agents import <name>` | deferred-by-design | Materializes canonical agent into repo via symlinks; link-write surface, Go-only. |
| `agents remove <name>` | deferred-by-design | Destructive: unlinks `.agents/agents/<name>/` and `.claude/agents/<name>/` and updates `.agentsrc.json`. Go-only. |
| `agents remove --purge` | deferred-by-design | Subset of `remove`; also purges `~/.agents/agents/<project>/<name>/`. |

### `hooks`

Source: `commands/hooks/cmd.go`. TS: `ports/typescript/src/commands/hooks.ts`,
dispatcher in `cli.ts`.

| Go subcommand / flag | TS coverage | Notes |
|---|---|---|
| `hooks list [scope]` | implemented | TS reads canonical `HOOK.yaml` bundles, falls back to legacy `claude-code.json` event count — same precedence as `commands/hooks/list.go`. |
| `hooks show <scope> <name>` | missing | Stage 1 candidate. Today the TS dispatcher rejects everything other than `list`. |
| `hooks remove <scope> <name>` | deferred-by-design | Destructive bundle delete, Go-only. |

---

## 3. Stage 2 deferred items (already named)

Items the boundary doc has already named or that have been added since the
boundary doc was written, classified by the contract they need before tp2
can encode them.

| Item | Source (Go) | Status (TS) | Boundary anchor |
|---|---|---|---|
| `status` plugin spec section (`Plugins:` listing after canonical table) | `commands/status.go` `printPluginsSection`; plugin enumeration `internal/platform.ListPluginSpecs` | not implemented; permanently deferred until plugin readback parity is needed | Boundary §"Phase 5 — Stage 2 buckets and canonical store (`status`)" — "**Not implemented in TS:** plugin **spec listing** as a separate `Plugins` section after the canonical table … deferred until plugin readback parity is required." |
| Read-only `workflow` CLI surface (e.g. `workflow tasks <plan-id>`, `workflow health`) | `commands/workflow/cmd.go` (full Go tree); read-only TS implementations exist as library: `runWorkflowOrient`, `runWorkflowTasks`, `runWorkflowHealth` | library-only today; CLI not wired | Boundary §"In scope for optional future TypeScript work (under option 2)" — "Read-only `workflow` subsets … implemented in TS only if they can be done safely **without** duplicating Go graph/Postgres dependencies". Decision is **allow** + **do not commit**. |
| `workflow status` read-only summary | `commands/workflow/cmd.go` `Use: "status"` | not implemented | Same option 2 envelope as the previous row. |
| `workflow log` (iteration log readback) | `commands/workflow/cmd.go` `Use: "log"` (top level + plan-level) | not implemented | Same option 2 envelope as the previous row. |
| Stage 2 canonical buckets in `init` (per `CANONICAL_BUCKET_SPECS`) | `commands/init.go` already creates `<bucket>/global` for every Stage 2 spec | TS `standardDirs` mirrors this — already implemented | Boundary §"Phase 5 — Stage 2 buckets and canonical store (`status`)". Listed for completeness; **not** a deferred row. |
| `agentsrc.json` `ExtraFields` preservation | `internal/config/agentsrc.go` (`ExtraFields` + `agentsRCKnown`) | implemented in TS port | `ports/typescript/README.md` §"Current scope" — "Preserve **unknown top-level JSON keys** on parse → mutate → serialize". Listed so tp2 can lock it as a Stage 1 contract row even though it's not a CLI subcommand. |
| Skills/Agents/Hooks **promote / import / remove** mutating subcommands | `commands/agents/{promote,import,remove}.go`, `commands/skills/promote.go`, `commands/hooks/remove.go` | deferred-by-design (see §2 rows) | Implicit under boundary §"Permanently deferred" — these are link-write + canonical-store mutation surfaces. Captured here so tp3 doesn't classify them as "Stage 1 + missing" by accident. |
| KG `bridge` / `query` / `health` / `sync` / `serve` read-only **library** stubs | `commands/kg/*.go` (full tree); TS stub `runKgHealth` / `runKgQuery` (always returns "Go CLI required") | stub library only; never CLI | Boundary §"Permanently deferred" — KG stays Go-only. The TS stub exists so test code can assert the boundary, not as a real port. **Not** a Phase 5 candidate. |
| Workflow **mutating** subcommands (`checkpoint`, `advance`, `merge-back`, `fanout`, `verify record`, `sweep`, `delegation closeout`, `fold-back create`, …) | `commands/workflow/cmd.go` | absent | Boundary §"Permanently deferred". Treated identically to `kg` — never a Phase 5 candidate. |

---

## How tp2 / tp3 should consume this memo

tp2's JSON should encode three lists derived directly from this memo:

1. `stage1_commands` — the eight rows in the §1 table flagged
   "Stage 1 + mirrored". These are the **only** commands whose absence in
   the TS CLI would trip the sync pipeline.
2. `stage1_flag_lock["<cmd>"]` — for each Stage 1 command in §2, the set
   of subcommands and flags marked **implemented** today. Net additions or
   removals on the Go side need a deliberate update to this list, not a
   silent acceptance.
3. `phase4_optouts` — the §1 rows marked "Phase 4 opt-out" (`explain`,
   `import`, `install`, `kg`, `mcp`, `remove`, `review`, `rules`,
   `settings`, `sync`, plus `workflow` for its mutating surface).
4. `phase5_deferred` — the §3 rows that the boundary explicitly **allows**
   but does not yet require: read-only `workflow` CLI surfaces under
   option 2, plus the `status` plugin section.
5. `stage2_deferred_subitems` — finer-grained rows from §2 marked
   "deferred-by-design" so tp3 can produce actionable diffs ("Go added
   `--audit` to `status` — was that meant to be a Stage 1 lock?") without
   waiting for an entire new top-level command to graduate.

Anything the Go tree gains that this memo does not classify must be
classified in tp2 before merge — that's the false-positive guard tp3 will
enforce.
