# Proposal: Hook-wiring accuracy (#33 HOOKS.md) + OpenCode-plugin fit + HOOKS.md lifecycle completeness

- type: project-local audit / analysis proposal (read-only investigation)
- status: **accepted (2026-05-25)** — Deliverable-1 doc edits landed via commits `af982a00` (hooks split) and `5a1b3a9d` (Codex projection correction); `docs/HOOKS.md:52` now reflects the proposal's exact replacement text. Deliverable-2 (OpenCode fit) was a "do not wire" recommendation — no plan needed. Confirmed as intentional plugins-vs-hooks split in `loop-discipline-stop-hooks-scoping.md` Thread D.7.
- date: 2026-05-18
- scope: `internal/platform/` (codex.go, copilot.go, cursor.go, opencode.go,
  hooks.go), `commands/` (refresh.go), `docs/HOOKS.md` (PR #33 / branch
  `docs-refresh`), `docs/PLATFORM_DIRS_DOCS.md` (origin/master)
- refs inspected: `origin/master` = `f38912ff`, `origin/docs-refresh`
  = `48b88217` (the SAME branch backs both merged PR #29 and open PR #33)
- builds on (does NOT redo): `.agents/proposals/codex-hooks-agents-linking-gap.md`
  (its Finding 1 + 2026-05-18 correction established Codex hooks ARE
  wired and `RunSharedTargetProjection` IS invoked by refresh/install/add).
  This proposal re-confirms against current origin/master and extends to
  Copilot, the #33 HOOKS.md text, and lifecycle completeness.

## Problem

The maintainer, reading `docs/HOOKS.md` as added by PR #33 (branch
`docs-refresh`), saw its per-platform table claim **Codex hooks "Not
wired"** and **Copilot "Partial"**. The prior proposal's Finding 1 and
the merged `PLATFORM_DIRS_DOCS.md` Hook Wiring Audit (origin/master)
say Codex hooks are wired ("Yes"). The HOOKS.md table therefore
contradicts both the code and the already-merged authoritative doc.
Additionally the maintainer reports HOOKS.md "starts well with add and
refresh but is incomplete" on the management lifecycle. This audit
establishes ground truth and proposes exact corrected text.

A second-order trap: the prior proposal cited a "Hook Wiring Audit" at
`PLATFORM_DIRS_DOCS.md:135`. On current origin/master that table has
moved — the real Hook Wiring Audit is at **lines 226–232** (line 135 is
now the Cross-Platform Session Storage Matrix). All corrections below
cite current origin/master line numbers.

---

## Deliverable 1 — Hook-wiring accuracy (Codex + Copilot)

### Codex — ground truth: WIRED = YES

Traced on `origin/master` (`f38912ff`):

- `codex.CreateLinks` calls `createHooksLinks` —
  `internal/platform/codex.go:141-143`.
- `createHooksLinks` → `writeRepoHooks` + `writeUserHomeHooks` —
  `codex.go:241-246`.
- `writeRepoHooks` renders **repo `.codex/hooks.json`** via
  `renderCodexHookConfig` through `emitPreferredHookFile` —
  `codex.go:248-265` (target `filepath.Join(repoPath, codexDir,
  codexHooksJSON)`, `codexHooksJSON = "hooks.json"`, `codex.go:23`).
- `writeUserHomeHooks` renders **user `~/.codex/hooks.json`** via
  `emitPreferredHookFileToUserHomes` — `codex.go:267-280`.
- Production invocation: `commands/refresh.go:180` calls
  `p.CreateLinks(name, path)` inside the enabled-platform loop
  (`refresh.go:171`), and `refresh.go:157` calls
  `platform.RunSharedTargetProjection(...)` before it. Codex hook
  emission lives entirely inside `CreateLinks`, so it does NOT depend on
  the shared-target projection (the narrow projection gap in the prior
  proposal does not touch hooks).

**Verdict: Codex hooks are WIRED = YES.** Both repo `.codex/hooks.json`
and user `~/.codex/hooks.json` are rendered and written as managed
regular files via the production `da refresh` / `install` / `add` path.
This matches the prior proposal's Finding 1 and the merged
`PLATFORM_DIRS_DOCS.md:230`.

### Copilot — ground truth: WIRED, "Partial" is ACCURATE (not stale, not wrong)

Traced on `origin/master`:

- `copilot.CreateLinks` calls `createClaudeCompatLinks` (Claude-compat
  hook settings) AND `createProjectHookFiles` (`.github/hooks/*.json`)
  — `copilot.go:204-211`.
- `createClaudeCompatLinks` renders **`.claude/settings.local.json`**
  via `renderClaudeHookSettings` / `emitPreferredHookFile` —
  `copilot.go:270-294`.
- `createProjectHookFiles` renders the canonical or legacy
  **`.github/hooks/*.json`** fanout — `copilot.go:296-338`
  (`emitCanonicalProjectHookFiles` / `emitLegacyProjectHookFiles`).
- Same production path as Codex (`refresh.go:180`).

So Copilot hooks ARE wired — arguably MORE surface than Codex (two
emitters: native `.github/hooks/*.json` + Claude-compat settings). The
relevant question is whether "Partial" (vs Cursor/Codex "Yes") is
justified. **It is, on one concrete axis:** Cursor and Codex each emit a
**user-scope** hook file (`cursor.go:381` → `writeUserHomeHooks` →
`emitPreferredHookFileToUserHomes` at `cursor.go:403-408`; `codex.go:267`
likewise). Copilot's `CreateLinks` has **no** user-home hook emitter —
it writes only repo-scoped artifacts (`.github/hooks/*.json` +
`.claude/settings.local.json`). Copilot's official docs note the CLI
also loads hooks from the current working directory, and a user-scope
`~/.copilot` hook surface is not emitted. "Partial" is therefore an
**accurate** characterization at the granularity the audit uses (repo
covered, user-scope not), **not stale and not wrong** — it should be
kept but its wording should explain *why* it is partial rather than
imply the repo path is missing.

### Diff vs the docs + exact corrections

**A. `docs/HOOKS.md` on `origin/docs-refresh` (PR #33) — per-platform table is WRONG.**

Current text (the Codex and Copilot rows):

```
| Codex | `.codex/hooks.json` | Not wired (`AGENTS.md`, config, skills, and agents are linked, but `.codex/hooks.json` is not created) |
| GitHub Copilot | `.github/hooks/*.json` | Partial (project `.github/hooks/*.json` plus Claude-compatible settings) |
```

The Codex row is **factually false** on master and **contradicts the
already-merged `PLATFORM_DIRS_DOCS.md:230`** that the same document links
to as its source of truth. The Copilot row is directionally fine but
mis-attributes the partiality (it implies the repo path is the gap; the
real gap is no user-scope emission).

**Proposed exact replacement for the two rows** (HOOKS.md "Per-platform
behavior" table; keep the table's existing 3-column shape `Platform |
Where hooks land | Status`):

> `| Codex | `.codex/hooks.json` (project) and `~/.codex/hooks.json` (user) | Wired (renders and writes both the repo `.codex/hooks.json` and user `~/.codex/hooks.json` as managed files) |`
>
> `| GitHub Copilot | `.github/hooks/*.json` (project) plus `.claude/settings.local.json` compat | Wired — repo scope only (project `.github/hooks/*.json` and Claude-compatible settings are written; no user-scope `~/.copilot` hook file is emitted, unlike Cursor/Codex) |`

Optionally also update the Codex/Copilot wording in the sentence above
the table so it no longer implies Codex is unsupported.

**B. `docs/PLATFORM_DIRS_DOCS.md` on `origin/master` — Hook Wiring Audit
(lines 226–232).**

- Codex row (`:230`): already correct ("Yes" + accurate notes). **No
  change.** (The prior proposal's request to amend `:135` is satisfied;
  that audit moved to `:226–232` and the Codex row is already fixed.)
- Copilot row (`:231`): currently `| GitHub Copilot | ... | Partial |
  Links project `.github/hooks/*.json` and also wires Claude-compatible
  settings. |`. The verdict "Partial" is fine but the Notes column does
  not say *why* it is partial. **Proposed Notes replacement:**

  > `Links project `.github/hooks/*.json` and Claude-compatible `.claude/settings.local.json`; partial because no user-scope (`~/.copilot`) hook file is emitted, unlike Cursor/Codex which also write `~/.cursor/hooks.json` / `~/.codex/hooks.json`.`

  (Verdict column stays `Partial`.)
- All other rows on `:226–232` are accurate. No other PLATFORM_DIRS
  hook-wiring corrections required.

### #33 verdict (hook table)

**HOOKS.md must be AMENDED before merge (do not revert the file).** PR
#33's value (rehoming the hooks design, adding a public guide) is worth
keeping, but its per-platform table ships a falsehood about Codex and a
mis-framed Copilot row, while citing the merged PLATFORM_DIRS audit that
already contradicts it. Apply the Section-A replacement rows. This is a
bounded doc edit — no plan needed.

---

## Deliverable 2 — OpenCode-plugin-as-hook FIT analysis

### OpenCode's plugin/hook contract (from PLATFORM_DIRS_DOCS + opencode.go)

- OpenCode documents **no dedicated hooks file** in the Cursor / Claude /
  Codex / Copilot style (`PLATFORM_DIRS_DOCS.md:59`, `:201`).
- Its hook-equivalent is the **plugin system**: code modules under
  `.opencode/plugins/` (project) and `~/.config/opencode/plugins/`
  (user), with npm deps via `.opencode/package.json` /
  `~/.config/opencode/package.json` (`bun install` at startup) —
  `PLATFORM_DIRS_DOCS.md:58`, `:187`. Plugins are **JS/TS code modules
  exporting event handlers**, not declarative command/event JSON.
- Current `internal/platform/opencode.go` does **zero** hook or plugin
  handling. `opencode.CreateLinks` (`opencode.go:108-126`) only links
  `opencode.json` settings; `.opencode/agent/*.md` + `.agents/skills/`
  are emitted by the shared-target plan. There is no hooks code path,
  no plugin emitter, no event map.

### Mapping the canonical hook model onto OpenCode plugins

The canonical model is `~/.agents/hooks/<scope>/<name>/HOOK.yaml` +
a command/event JSON shape, rendered per-platform by functions like
`renderCodexHookConfig` / `renderCopilotHookFile` /
`renderClaudeHookSettings` (`internal/platform/hooks.go`). Every wired
platform consumes a **declarative file** (`.cursor/hooks.json`,
`.codex/hooks.json`, `.github/hooks/*.json`, Claude `settings*.json`).
The renderer maps canonical events → platform event names and writes
JSON. There is no code generation anywhere in the hook path.

OpenCode breaks every assumption of that path:

1. **Artifact kind impedance (the blocker).** OpenCode has no
   declarative hook/JSON surface. To wire it we would have to emit a
   **JS/TS plugin module** that imports the OpenCode plugin SDK, exports
   the right event handlers, and shells out to the canonical hook
   command. That is a code-generation shim — categorically different
   from the existing `render*HookConfig` JSON renderers. It would be the
   only code-emitting target in the hook subsystem.
2. **Runtime/dependency coupling.** A generated plugin participates in
   OpenCode's `bun install` startup and plugin API/version surface.
   dot-agents would own generated executable code whose correctness
   depends on an external SDK's evolving API — a maintenance and
   security surface the declarative renderers do not have.
3. **Aggregation mismatch.** Other platforms get per-hook fanout
   (`.github/hooks/<name>.json`) or a single merged config
   (`.codex/hooks.json`). OpenCode plugins are loaded as modules; the
   natural shape is a single aggregate plugin dispatching to all hooks
   — a third emission topology to design and test.
4. **Event-name mapping is unverified.** The existing event map
   (`SessionStart`, `PreToolUse`, `PostToolUse`, `UserPromptSubmit`,
   `Stop`) has no audited correspondence to OpenCode plugin events. The
   docs in PLATFORM_DIRS do not enumerate an OpenCode hook event surface
   — coverage is presently unknown, so even a shim's correctness can't
   be asserted.

### Verdict: POOR FIT (do not wire into the canonical hook path)

Wiring OpenCode into the hook emitter requires a code-generating plugin
shim, an external-SDK coupling, a new aggregate-emission topology, and
an unverified event map — none of which the declarative hook subsystem
has or wants. The cost/risk is disproportionate to the benefit, and it
contaminates a clean declarative renderer set with a code generator.

**Recommended alternative:** keep OpenCode OUT of the hook path
(status quo, correctly reflected by `PLATFORM_DIRS_DOCS.md:232` "No" and
the proposed HOOKS.md OpenCode row "no dedicated hook file is documented
upstream, so none is created" — both are accurate; no change needed).
Hook-like OpenCode behavior, if ever wanted, belongs in the **separate
plugin/`PLUGIN.yaml`** track already noted in
`PLATFORM_DIRS_DOCS.md:170` (canonical plugin bundle with an OpenCode
plugin emitter), NOT in the hook subsystem. That is a distinct resource
category with its own contract (`docs/PLUGIN_CONTRACT.md`).

**Does it warrant a spec/plan?** No new hook-path spec or plan. If the
maintainer ever wants OpenCode hook-like behavior, it should be folded
into the existing plugin-emitter track, not opened as a hook-wiring
plan. No artifact to create here.

---

## Deliverable 3 — HOOKS.md management-lifecycle completeness

### The real hook management lifecycle (verified command surface)

Verified via `go run ./cmd/dot-agents hooks --help`, `hooks list
--help`, and the root command list. **The `hooks` subtree has exactly
ONE subcommand: `list`.** There is **no `hooks show` and no `hooks
remove`** on origin/master. The full lifecycle is spread across
top-level commands:

| Stage | Real command surface (verified) |
|-------|---------------------------------|
| Create / add | Author bundle dir `~/.agents/hooks/<scope>/<name>/HOOK.yaml` by hand; `da add .` registers a project so hooks distribute to it |
| Distribute / relink | `da refresh` re-distributes canonical hooks to every managed project + platform; `da install` (manifest-driven setup) also wires hooks |
| Inspect | `da hooks list [project]` — the ONLY `hooks` subcommand (`--json` for machine output). No `hooks show`, no `hooks remove`. |
| Import (canonicalize legacy) | `da import <project>` detects existing project/global hooks (with rules/skills/agents) and copies them into `~/.agents/` |
| Sync (push/pull `~/.agents`) | `da sync` (e.g. `da sync status`) runs git operations on `~/.agents/` so canonical hooks move between machines |
| Repair | `da doctor` checks installations, validates links, detects issues (the broken-link repair path; note the narrow projection gap from the prior proposal applies to doctor, but hook emission via `CreateLinks` is unaffected) |
| Remove (project) | `da remove` removes a project from management (stops hook distribution to it). There is no per-hook delete subcommand. |

`hooks --help` short string is `Manage ~/.agents/settings/*/claude-code.json
hooks` — itself narrow/misleading vs the bundle model HOOKS.md
describes; worth a maintainer note but outside this doc edit.

### Diff vs current HOOKS.md (origin/docs-refresh) — missing stages + a factual error

HOOKS.md currently covers: the canonical model, the per-platform table
(wrong — D1), `da hooks list`, **and documents `da hooks show` and `da
hooks remove` which DO NOT EXIST**, "Adding a hook", `da import`, and
`da refresh`. Missing/incorrect:

1. **Factual error (must fix):** the "Using hooks" block shows
   `da hooks show global session-orient` and `da hooks remove global
   old-hook-bundle`. **Neither subcommand exists** — `hooks` has only
   `list`. This is a documentation bug independent of D1.
2. **Missing: `da install`** as a hook-wiring entry point (manifest
   setup also distributes hooks, not just `refresh`).
3. **Missing: `da sync`** — moving canonical hooks across machines via
   git on `~/.agents/`.
4. **Missing: `da doctor`** — validating/repairing hook links.
5. **Missing: removal/deregistration semantics** — how a hook stops
   being distributed (delete the bundle dir under `~/.agents/` then
   `da refresh`; `da remove <project>` to stop distribution to a
   project). HOOKS.md never explains teardown.
6. **Missing: an explicit end-to-end lifecycle ordering** (create →
   distribute → inspect → import → sync → repair → remove).

### Proposed added prose for HOOKS.md

Replace the "Using hooks" example block's false subcommands so it only
shows what exists:

> ```bash
> # List hooks for the global scope (or a project scope)
> da hooks list
> da hooks list my-app
> ```
>
> `da hooks` currently exposes only `list` (with the global flags
> `--json`, `--dry-run`, `--verbose`, `--yes`). Inspecting or removing an
> individual hook is done directly against its bundle directory under
> `~/.agents/hooks/<scope>/<name>/` followed by `da refresh`.

Add a new section after "Adding a hook":

> ### The hook lifecycle
>
> Hooks move through a fixed lifecycle, mostly via top-level `da`
> commands rather than the `hooks` subtree:
>
> 1. **Create** — author a bundle directory
>    `~/.agents/hooks/<scope>/<name>/HOOK.yaml` (plus sidecars).
> 2. **Distribute** — `da refresh` re-distributes canonical hooks to
>    every managed project and platform that supports them. `da install`
>    does the same as part of manifest-driven project setup.
> 3. **Inspect** — `da hooks list [project]` (add `--json` for machine
>    output) shows what is configured.
> 4. **Import legacy hooks** — `da import <project>` detects hooks (and
>    rules/skills/agents) already living in a project and copies them
>    into the canonical `~/.agents/` store.
> 5. **Sync across machines** — `da sync` runs git operations on
>    `~/.agents/` so your canonical hooks travel with you.
> 6. **Repair** — `da doctor` validates managed links and reports
>    issues; re-run `da refresh` to fix drift.
> 7. **Remove** — delete the bundle directory under `~/.agents/hooks/`
>    and run `da refresh` to withdraw it everywhere; `da remove
>    <project>` stops hook distribution to a specific project. There is
>    no per-hook delete subcommand.

### #33 verdict (folds D1 + D3)

**AMEND HOOKS.md before merging PR #33** with: (a) the D1 Codex/Copilot
table rows, (b) the D3 removal of the non-existent `hooks show`/`hooks
remove` examples, and (c) the D3 lifecycle section. Do NOT revert the
file — the doc's structure and canonical-model section are good. One
bounded doc edit on the `docs-refresh` branch covers all of it.

---

## What the maintainer must decide

1. **PR #33 path:** amend `docs/HOOKS.md` on `docs-refresh` with the D1
   table rows + D3 fixes/lifecycle section before merge (recommended),
   vs merge-then-fast-follow. Reverting is not recommended.
2. **`PLATFORM_DIRS_DOCS.md:231` Copilot note** — apply the proposed
   "why partial" Notes wording (verdict stays `Partial`)? Codex row
   (`:230`) is already correct; no other PLATFORM_DIRS change needed.
3. **`hooks --help` short string** (`Manage
   ~/.agents/settings/*/claude-code.json hooks`) is narrower than the
   bundle model — note for a future CLI/help cleanup (out of scope for
   the doc edit).
4. **OpenCode:** accept POOR FIT — keep OpenCode out of the hook path;
   no hook spec/plan. Any future OpenCode hook-like behavior goes
   through the existing plugin/`PLUGIN.yaml` emitter track, not hooks.
5. None of D1/D2/D3 requires a new spec or plan: D1+D3 are one bounded
   doc edit; D2 is a "do nothing in the hook path" decision.
