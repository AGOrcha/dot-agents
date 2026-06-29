# DC0 — Antigravity hand-add probe (execution-derived friction findings)

- **gate:** F4 / DC0 of `multi-harness-extensibility` — the *real* hand-add the §9
  read-only enumeration left owed ("hand-add a *real* harness end-to-end and record
  what surfaces … still required before schema ratification").
- **harness probed:** Google **Antigravity** (the Gemini-CLI successor).
- **author:** Nikash Prakash
- **date:** 2026-06-28
- **branch / PR:** `impl/antigravity-harness-l4-probe` — *draft* (re-enters the cross-brain codex gate; not merged).
- **status:** execution-derived inventory complete. This file is **evidence**, not a
  re-decision of the spec; the ratified `design.md` body is untouched.

This records what *actually* surfaced when adding a sixth harness by hand, against the
read-only prediction in `design.md §9`. The §9 enumeration was largely correct; the
deltas below are the value of running the experiment for real.

---

## 1. Antigravity research — assumptions flagged for owner confirmation

Antigravity is very new and its docs are a JS-rendered SPA; the layout below is
corroborated from Google Codelabs + vendor-adjacent posts but several items are
**INFERRED / ASSUMPTION** and must be confirmed before this harness ships beyond a probe.
Every assumption is mirrored as an `OWNER-TODO` comment in `internal/platform/antigravity.go`.

| Item | Finding | Confidence |
|---|---|---|
| Home config root | **`~/.gemini/`** (shared with the Gemini lineage), NOT `~/.antigravity/`; CLI under `~/.gemini/antigravity-cli/`, shared `~/.gemini/config/` | INFERRED |
| Project-local root | **`.agents/`** umbrella (`agents.md`, `workflows/`, `skills/`, `hooks.json`); rules under `.agent/rules/` (singular) per one source | INFERRED / conflicting |
| Settings | `settings.json` (**JSON**) | INFERRED |
| Rules / context | `GEMINI.md` + `AGENTS.md` + `.agent/rules/*.md`, **plain markdown, no frontmatter** | INFERRED |
| Skills | `SKILL.md` with **YAML frontmatter** (Claude-style, ships ~verbatim); workspace skills are flat `.md` | INFERRED |
| Agents | **`agent.json`** — a bespoke **JSON object** model (NOT markdown frontmatter); also a project `agents.md` | INFERRED |
| Hooks | `hooks.json`; events `PreToolUse` / `PostToolUse` / `PreInvocation` / `PostInvocation` / `Stop`; matcher + `hooks:[{type,command,timeout}]`; stdin/stdout JSON w/ `decision` | INFERRED |
| MCP | `mcp_config.json` with top-level `mcpServers` (JSON) | INFERRED |

### The headline assumption that changed the implementation

**Antigravity reportedly reuses `.agents/` (project) and `~/.gemini/` (home) — and `.agents/`
is *dot-agents' own canonical source root*.** Projecting into `.agents/` verbatim would
collide the projection target with the source of truth. To keep the probe safe and
reversible I projected into a **dedicated `.antigravity/` repo-local root** instead, and
flagged the collision as the #1 owner decision. This is not a cosmetic choice — see §4,
finding D: it is the single most important thing the descriptor schema must be able to model
and that none of the five existing harnesses exercise.

**Owner-TODOs (must resolve before non-probe ship):** (1) `.agents/` vs dedicated
`.antigravity/` projection root + the source-root collision; (2) `~/.gemini/` home reuse
(no managed user-home wiring done beyond `~/.antigravity/hooks.json`); (3) `.agent` vs
`.agents` rules dir; (4) the `agent.json` JSON agent dialect (shipped verbatim here — a
translator is deliberately *not* built; see §4 finding C); (5) the `PreInvocation`/
`PostInvocation` events that have no canonical analog (omitted from the event map); (6) the
hooks.json top-level shape (per-event vs per-hook-name nesting); (7) session/stats env-var
contract (stubbed).

---

## 2. Touchpoints actually edited (vs the §9 prediction)

§9 predicted **~7 Go files in lockstep, 0 config, 0 templates**, and one new ~300-700 LOC
harness file of ~30-41 receivers. Here is what the real add cost:

| # (§9) | Touchpoint | File | Edit | LOC | Class | Mechanical vs judgment |
|---|---|---|---|---|---|---|
| 1 | Registration `All()` | `internal/platform/platform.go` | add `NewAntigravity()` | **+1** | data | mechanical |
| 2 | New harness file | `internal/platform/antigravity.go` | new | **+395 (282 sloc)**, **22 receivers** | mixed | mostly judgment (paths/dialect) |
| 3 | Event table | `antigravity.go` (lives in the harness file) | `antigravityEventTable` literal | ~6 | data | judgment (which events map) |
| 4 | Event-set validation | `internal/platform/hooks.go` | add table to `isKnownCanonicalEvent` slice | **+1 (−1)** | data | mechanical |
| 5 | Event dispatch wrapper | `antigravity.go` | `antigravityEventName` | ~3 | data | mechanical |
| 6 | Hook render fn | `antigravity.go` | render/entry/detector/remove | ~70 | **logic** | judgment (native shape) |
| 7 | Shared-mirror allowlist | `internal/platform/resource_plan.go` | +2 prefixes (`.antigravity/skills/`, `.antigravity/agents/`) | **+2** | data + policy | mechanical |
| 8 | Frontmatter translation | — | **not built** (see finding C) | 0 | — | judgment to *defer* |
| 9 | Diagnostics/status | `antigravity.go` | 6 reader methods | ~70 | mixed | mechanical (mirrors opencode/cursor) |
| 10 | Session/stats readers | `antigravity.go` | `SessionReader` stubs only | ~4 | logic | judgment to stub |
| 11 | Plugin-import switch | `commands/import_plugins.go` | **no edit** | 0 | — | — (see finding B) |
| 12 | Projection entry-points | `commands/refresh.go` etc. | **no edit** | 0 | none (auto) | confirmed |
| — | **Hardcoded test counts** | `commands/internal/lifecycle/status_test.go` | 5 → 6 | **±8** | — | **mechanical, NOT in §9** |

**Net:** 1 new file + **4 existing files** touched in lockstep (platform.go, hooks.go,
resource_plan.go, status_test.go) + 0 config + 0 templates. Lower than §9's "~7 Go files"
because touchpoints #3/#5/#6 all landed *inside the new harness file* and #11 needed no edit.

### Receiver count came in at the floor, not the ceiling

§9 said "~30-41 receivers (claude carries 41; opencode floor ~15)." Antigravity needed
**22** receivers to be a first-class harness (Platform 9 + SessionReader 4 + BrokenLinkReporter
+ LinkCounter + StatusBadger + UserConfigReporter ×2 + AuditPrinter + 6 hook/diag helpers).
The claude "41" is inflated by per-vendor session/stats/SQLite scanners and orphan-canonical
reporting that a new harness can legitimately stub or omit. **The de-facto-required surface
for a credible harness is ~20, not ~40** — the diagnostics sister-interfaces
(`diagnostics.go:92-143`) are the real floor, and they are almost entirely mechanical
(file/dir lists → shared `addManaged*Counts` / `scanUser*` helpers).

---

## 3. Surprises vs the read-only §9 prediction

1. **§9 missed the hardcoded test-count touchpoint.** Two lifecycle tests assert exactly
   *5* platforms (`status_test.go:422,460`). Adding a sixth is a compile-green, test-red
   change — `go test ./internal/platform/...` stays green (its contract test iterates
   `All()`), but `./commands/internal/lifecycle` fails on the literal `5`. **A real
   touchpoint inventory must include "tests that pin the platform cardinality."** A
   descriptor-driven registry would dissolve this (assert `len(All()) == len(descriptors)`).

2. **The generic contract test is a genuine safety net (positive surprise).**
   `contract_test.go` auto-applies identity/uniqueness, `RemoveLinks`-on-empty-repo, and
   `SharedTargetIntents`-shape to every `All()` member. The harness was held to the contract
   the moment it was registered — zero per-harness contract wiring. This is the part of the
   design that already behaves descriptor-like.

3. **Touchpoint #6 (render) was the only real judgment in the hook path; #3/#4/#5 were
   trivially data.** Mapping `pre_tool_use/post_tool_use/stop` and adding the table to one
   slice took minutes. Choosing the JSON *shape* (and discovering antigravity may nest by
   hook-name, and that `PreInvocation/PostInvocation` have **no canonical event to map
   to**) was the actual work. §9 classified this correctly, but the "new vendor event with
   no canonical name" case is sharper in practice than on paper (see §4 finding E).

4. **#11 (plugin switch) needed no edit** — `commands/import_plugins.go` has `default`
   arms that absorb an unknown harness. §9 listed it as a touchpoint; for a *basic* add it
   is not. It only bites if the harness needs bespoke plugin-manifest handling.

5. **The agent dialect divergence is real and was deliberately NOT chased.** Antigravity's
   `agent.json` genuinely diverges (like Codex TOML) — but building a second bespoke
   translator is exactly the speculative growth D1 warns against, and is the *next* step the
   probe is meant to inform, not part of the probe. Shipping agents as a verbatim dir-mirror
   (as cursor does into `.claude/agents`) is consistent with the existing pattern and is the
   honest probe outcome: **the divergence is documented, the translator is owed to a survey.**

---

## 4. What a data-driven harness descriptor must capture (distilled from what I hit)

The schema the L4 design should abstract, grounded in the concrete edits above:

- **A. Registry identity (touchpoint #1).** `id`, `display_name`, install-probe binary
  name(s), version-probe command. Pure data; the `All()` slice + `IsInstalled`/`Version`
  collapse into a descriptor list. *Hit cost today: trivial but lockstep-fragile (also
  drives the test-cardinality touchpoint #3-surprise).*

- **B. Per-asset read-paths + transport + form (touchpoints #2, #7, #9).** For each asset
  class (settings, mcp, hooks, skills, agents, rules): the **repo-local target root**, the
  **transport** (hardlink-replace single file vs symlink dir-mirror vs rendered file), the
  **scoped canonical source name** (`antigravity.json`), and the **allowlist/destructive-
  replace policy** for mirror roots. Every one of my `createScopedJSONLink` /
  `SharedTargetIntents` / `isAllowlistedSharedMirrorTarget` / diagnostics-file-list edits is
  this same `(asset → {root, transport, source-name, replace-policy})` tuple repeated. **This
  is the bulk of the descriptor and is genuinely data** — diagnostics (#9) fell out *for
  free* once the root/file lists existed, because the shared `addManaged*Counts` /
  `scanUser*` / `printSymlinkAudit` helpers consume exactly those lists.

- **C. Frontmatter dialect, per asset class, survey-gated (touchpoint #8).** The descriptor
  needs a `dialect` per asset: `verbatim` (skills/rules — Claude-like YAML), or a named
  translator (`codex-agent-toml`, and a future `antigravity-agent-json`). The probe confirms
  D1: **build the translator only when the asset's divergence is proven** — antigravity skills
  ship verbatim correctly; antigravity agents *would* need `agent.json` but that is deferred
  to the survey, not assumed.

- **D. ⭐ Identity / zero-projection read-paths (NEW — not in §9, the headline finding).**
  Antigravity reads the **canonical `.agents/` and `~/.gemini/` directly**. None of the five
  existing harnesses read the dot-agents canonical store as their *own* native root, so the
  descriptor has no way today to say "this harness consumes the source verbatim — project
  *nothing*, or project an identity link." A per-capability read-path model
  (design.md D3) must include a **`compat-native-identity`** capability: the harness's
  read-path *is* the canonical path, so the minimal projection is empty (or a single root
  symlink), not a per-entry mirror. Without this the engine would redundantly mirror
  `.agents/skills/*` into `.agents/skills/*` (a self-collision). **This is the strongest
  argument the probe produces for descriptors over per-harness Go: the Go path has no concept
  of "don't project, the harness already reads the source."**

- **E. Event vocabulary, incl. unmappable native events (touchpoints #3-#6).** The descriptor
  needs the canonical→native `event_map` (data) **and** a way to declare native events with
  **no canonical analog** (`PreInvocation`/`PostInvocation`). Today these are simply omitted;
  the descriptor model must decide whether such events (a) extend the canonical set, or (b)
  are declared harness-local. The **render shape stays Go** (semantic, per design.md R4/F1) —
  the descriptor declares the event map + matcher-support shape + output-format *kind*, not
  the rendering procedure. My `renderAntigravityHookEntry` is the irreducible-Go floor for
  this harness.

- **F. Diagnostics surface = derived, not authored.** Confirmed: every diagnostics reader I
  wrote is a thin adapter over the read-path lists from (B). The descriptor should make
  diagnostics **fall out of the read-paths** rather than be a separate authored surface — the
  6 methods are boilerplate, not knowledge.

- **G. Stub-ability flags.** Session/stats readers and orphan-canonical reporting are
  legitimately absent for a new harness. The descriptor needs explicit "not implemented /
  stub" markers so `da doctor`/`da status` degrade *intentionally*, not silently.

### Irreducible-Go floor this probe confirms (the DC0 down-payment, now execution-derived)

Matches §9.3. For antigravity the irreducible Go is: **the hook render functions**
(`renderAntigravityHookConfig`/`Entry`, the `isLikelyRendered…` detector) and the
**`CreateLinks`/`RemoveLinks` symmetry glue** (the managed-replace + teardown ordering). The
config-merge/source-priority/user-home-fanout/stale-prune logic the other harnesses carry did
**not** surface here because antigravity's surface is simple (single-file configs + dir
mirrors + one rendered hook file) — a useful data point that the irreducible-Go floor is
**proportional to harness complexity**, and a *simple* harness is close to descriptor-only
modulo the hook renderer.

---

## 5. Gate evidence

- `gofmt -l` clean; `go vet ./internal/platform/... ./internal/config/... ./commands/...` clean.
- `go test ./internal/platform/... ./internal/config/... ./commands/...` — **pass** (after
  updating the two cardinality assertions in `status_test.go`).
- **Per-file coverage `internal/platform/antigravity.go`: 122/123 statements = 99.2%** (gate ≥95%).
  The single uncovered statement is the `CreateLinks` mcp-error return, unreachable without
  the settings link failing first in the same call.
- Cognitive complexity: all new functions are small and flat (max ≈6, gate <15).
- S1192: the repeated literals (`"antigravity"` ×10, `"hooks"`/`"skills"`/`"agents"`/
  `"global"`) match the established per-harness convention — codex.go/claude.go/cursor.go
  duplicate the same bucket/scope literals far more (e.g. codex `"agents"` ×14) and pass the
  gate; no new S1192 category is introduced.
- No projection regression: the five existing harnesses' output and the generic
  `contract_test.go` (now also covering antigravity) are green (DC5).

---

## 6. One-line conclusion for the L4 descriptor-schema design

The descriptor must abstract **(A) registry identity, (B) per-asset {root, transport,
source-name, replace-policy} read-paths — which also generate diagnostics for free, (C)
survey-gated per-asset frontmatter dialect, (D) a NEW `identity/zero-projection` read-path
capability for harnesses that read the canonical store directly (the antigravity `.agents/`/
`~/.gemini/` case the five built-ins never exercise), (E) an event map that can name native
events with no canonical analog, and (G) explicit stub markers** — while the **hook renderer
and the `CreateLinks`/`RemoveLinks` symmetry stay as an explicit, audited Go core** whose size
is proportional to harness complexity (small here). Finding **D** is the probe's primary new
contribution beyond §9.
