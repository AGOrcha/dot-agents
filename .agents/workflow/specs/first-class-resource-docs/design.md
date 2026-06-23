# First-class docs for da command trees — design (spec)

**Spec id:** `first-class-resource-docs`
**Status:** design artifact (spec tier). Plan: `workflow/plans/first-class-resource-docs/`.
**Scope:** broadened (2026-06-23) from "resource families" to **every `da` command tree**,
backed by the full audit in §3.

## 1. Problem & why

dot-agents exposes 22 top-level `da` command trees (`commands/root.go:163-184`). An
audit of the whole surface against `docs/*.md` + the Starlight `PUBLIC_PAGES` allowlist
(§3) found coverage is mostly-good-but-uneven: the README command catalog covers most
trees, but a handful are **contract-shaped only** (an implementer `*_CONTRACT.md` + a one-line
README row, no task-oriented guide), **thinly covered**, **dangling** (help cites a
non-existent doc), or **entirely undocumented**.

The substantive user-facing gaps (not the whole surface — see §3): the resource trio
**`rules` / `mcp` / `settings`** (contract-only, no guide), **`score`** (rubric/spec only),
a **dangling `docs/CONFIG_DISTRIBUTION_MODEL.md`** reference in `config --help`, and several
**`workflow` commands absent from the README catalog** (`contract`, `resolve-prompt`) or
flat-missing (`slots`, `blocked-on`). A reader hits these and must read code. That's the gap.

(Several families assumed to be gaps are in fact GOOD per the audit: `hooks` has a full
`HOOKS.md`; `skills`/`agents` are covered in the README catalog; `config` is now covered by
the 0.4.0 `LAYERED_CONFIG_GUIDE.md`. We do NOT add redundant guides for those.)

## 2. Decisions

- **One first-class guide per resource family**, surfaced in the Starlight docs
  site (the `Guides` section) and linked from README.
- **Shared template** (set by the layered-config guide, the exemplar): `Overview`
  → `Model` (the data/resource shape) → `Commands` (the `da <family>` surface) →
  `Worked example` (a real, runnable walkthrough with output) → `Reference`
  (links to the contract/spec doc for implementers). Guides are task-oriented;
  the `*_CONTRACT.md`/`*_SPEC.md` docs remain the implementer-authoritative source
  the guide's Reference section points at (no duplication of contract detail).
- **Public by default**; gate internal-only families per the docs visibility
  model (dm3) if any are maintainer-only.
- **Verify claims against code** (`da <family> --help` + the family's package) —
  never document unshipped behavior.
- **Contracts stay; guides are additive.** This does not rewrite the contract
  docs; it adds the missing user-facing layer and cross-links the two.

## 3. Coverage map — ALL `da` command trees (audited 2026-06-23)

Audited the full surface: 22 top-level commands registered in `commands/root.go:163-184`,
recursing every subcommand via `--help`, against `docs/*.md` + the Starlight
`PUBLIC_PAGES` allowlist (`docs/web/src/content.config.ts:40-64`). **The README *is*
the surfaced command catalog** (`PUBLIC_PAGES` id `index`; `README.md:205-415` carries
per-family command tables), so "documented + surfaced" ≈ "in the README catalog + (where
present) a dedicated `docs/*.md`". Each row carries a code location + doc citation.

**GOOD (current, surfaced; no action):** `init`, `add`, `remove`, `refresh`, `import`,
`status`, `doctor`, `install` (lifecycle, `commands/internal/lifecycle/` + `commands/*.go`;
README:213-220,561); `skills` (`commands/skills/`; README:250-252); `agents`
(`commands/agents/`; README:253-257); `hooks` (`commands/hooks/`; **full HOOKS.md** +
README:267); `config` (`commands/config/`; README:222-242 + lockfile section + the new
LAYERED_CONFIG_GUIDE.md / PR #110); `review` (`commands/review.go`; README:276-278);
`sync` (`commands/sync/`; README:390-405); `explain` (`commands/explain.go`; README:411);
`workflow` core + plan/task/verify/fanout/merge-back/drift/start-close-task
(`commands/workflow/`; README:282-339 + WORKFLOW_CLIENT_COMMANDS.md); `kg` core +
maintain/sync/warm/bridge + build/impact/flows (`commands/kg/`; README:343-388).

**NEEDS-UPDATE (doc exists but contract-shaped / thin / dangling — the real user gaps):**

| Tree | code | today | gap |
|------|------|-------|-----|
| `rules` | `commands/internal/rules/` | RESOURCE_COMMAND_CONTRACT.md + 1 README row (266) | contract-only; no task-oriented guide |
| `mcp` | `commands/internal/mcp/` | RESOURCE_COMMAND_CONTRACT.md + 1 README row (268) | contract-only; no MCP setup guide |
| `settings` | `commands/internal/settings/` | RESOURCE_COMMAND_CONTRACT.md + 1 README row (269) | contract-only; no guide |
| `config` ref | `commands/config/` help text | cites `docs/CONFIG_DISTRIBUTION_MODEL.md` | that file does NOT exist — dangling ref (point to LAYERED_CONFIG_GUIDE) |
| `score` | `commands/score.go` | OUTCOME_SCORING_RUBRIC.md (unsurfaced) + 1 README row (406) | rubric/spec only; no "run/read scores" guide |
| `workflow contract` | `commands/workflow/contract.go` | DEMO mention only | absent from README catalog |
| `workflow resolve-prompt` | `commands/workflow/profile_prompt.go` | VERIFIER_REVIEWER_TEMPLATES.md (unsurfaced) | absent from README catalog |
| `session stats` | `commands/session_stats.go` | 1 README row (407) | thin; low value |

**MISSING (no user-facing doc anywhere):** `kg lockfile` (`commands/kg/lockfile.go`),
`workflow slots` (`eligible_accounting.go`), `workflow blocked-on` (`blocked_on.go`),
`workflow archive-orphans` (`archive_orphans.go`), `workflow hook-sentinel`
(`hook_sentinel.go`).

**INTERNAL / maintainer-only (document as automation/reference, NOT user guides):**
`workflow hook-sentinel`, `hook-outcome`, `commit`, `archive-orphans` (hook/iteration-close
plumbing invoked by skills, not run by hand); `kg serve` (MCP stdio server), `kg lockfile`
(adapter infra). These get a short "automation/internal commands" reference entry, not a guide.

**Doc-surfacing note:** 12 `docs/*.md` are present but NOT in `PUBLIC_PAGES`; two are the
ONLY coverage of a command — `OUTCOME_SCORING_RUBRIC` (`score`) and
`VERIFIER_REVIEWER_TEMPLATES` (`resolve-prompt`) — so those commands are effectively
undocumented for site visitors despite a repo file existing.

So the substantive user-facing work is: the resource trio (`rules`/`mcp`/`settings`),
`score`, the dangling `config` ref, and folding the absent-from-catalog workflow commands
in — NOT new guides for hooks/skills/agents (already GOOD). config is DONE pending #110 merge (the exemplar).

## 4. Done criteria

1. The resource trio (`rules`, `mcp`, `settings`) each has a task-oriented guide
   (promoted beyond the RESOURCE_COMMAND_CONTRACT row), surfaced in the site + README.
2. `score` has a "run and read scores" guide (surfaced), beyond the unsurfaced rubric.
3. The dangling `config` help reference (`docs/CONFIG_DISTRIBUTION_MODEL.md`) is fixed
   (re-pointed to LAYERED_CONFIG_GUIDE, or the doc created) — no dead pointer.
4. The absent-from-catalog `workflow` commands (`contract`, `resolve-prompt`, `slots`,
   `blocked-on`) are added to the README workflow catalog; the maintainer-internal
   commands (`hook-sentinel`/`hook-outcome`/`commit`/`archive-orphans`, `kg serve`,
   `kg lockfile`) get a short "automation / internal commands" reference entry, not a guide.
5. Every new/updated guide's commands+claims are verified against `da <tree> --help` + code
   (citation-checked); guides cross-link the contract/spec doc rather than duplicate it.
6. No regression for the GOOD trees (§3) — they are NOT given redundant guides.

## 5. Deferred / out of scope

- Rewriting the `*_CONTRACT.md`/`*_SPEC.md` docs (they remain the implementer source).
- New guides for the GOOD trees (hooks/skills/agents/config/sync/review/lifecycle/workflow-core/kg-core).
- `kg serve` / `kg lockfile` deep tutorials (reference-level only; infra surfaces).
- Auto-generating guides from the command tree (a future nicety, not required).

## 6. Relationship to other work

- **config guide** (the 0.4.0 layered-config guide) is the exemplar that sets the
  template; the other families follow it.
- Consumes the `RESOURCE_COMMAND_CONTRACT.md` shared-surface contract and each
  family's contract/spec doc as the Reference target.
