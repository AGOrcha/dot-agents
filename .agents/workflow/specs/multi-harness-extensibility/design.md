# Multi-harness extensibility — design spec

- **id:** multi-harness-extensibility
- **status:** DRAFT (F4 probe executed; pending owner ratification of the open forks in §5)
- **author:** Nikash Prakash
- **created:** 2026-06-26
- **revised:** 2026-06-26 — GATE-1 audit fixes: D3 made capability-granular, D2 reframed to
  descriptor + irreducible Go core (no descriptor-only claim), D1 survey-gated, F4 promoted
  to a mandatory probe gating schema ratification, completeness items added (R7-R10, DC0,
  DC6-DC7)
- **revised:** 2026-06-28 — **F4 "hand-add-one-harness" probe run (read-only, empirical).**
  The concrete touchpoint inventory the F4 gate demands is now recorded in **§9**: every
  hard-coded harness edit site (file:line), classified data-vs-logic per D2/F1, plus the
  partial irreducible-Go inventory DC0 requires. One stale citation corrected
  (`commands/install.go` no longer exists — projection now enters via `commands/refresh.go`).
  This satisfies the *enumeration* half of the F4 gate; DC0 still requires a *real* harness
  to be hand-added end-to-end before schema ratification.

---

## 1. Problem statement

dot-agents already projects managed resources to five AI harnesses (Cursor, Claude
Code, Codex, OpenCode, GitHub Copilot) from one canonical source. The fan-out engine
itself is sound and **not regressed**: each platform contributes `ResourceIntent`s via
`SharedTargetIntents` (`internal/platform/platform.go:66-71`), the command layer
aggregates them into one `ResourcePlan`, and the projector was born multi-harness in
commit `2c0733db` ("resource model, shared target plan, projectsync"). This spec is
**not** about fixing a regression in the engine.

The problem is **extensibility cost**. Adding a sixth harness (Hermes, Pi-agent, Aider,
Antigravity-CLI, …) today is expensive and Claude-shaped, because per-harness knowledge
is encoded as *logic* (bespoke Go) rather than *data* (a descriptor the engine reads):

1. **A harness is ~300-700 lines of bespoke Go.** Each must implement the 9-method
   `Platform` interface (`internal/platform/platform.go:49-72`) — including the
   `CreateLinks`/`RemoveLinks` symmetric pair (`platform.go:58-61`), where `RemoveLinks`
   must tear down exactly what `CreateLinks` wrote — plus a de-facto-required set of
   optional interfaces. That optional surface is **larger than session/stats reading**:
   beyond `SessionReader` / `StatsReader` / `SessionTokenScanner` / `BranchSessionFinder`
   (`platform.go:11-46`), the diagnostics and status paths hang off
   `BrokenLinkReporter`, `LinkCounter`, `StatusBadger`, `UserConfigReporter`,
   `OrphanCanonicalReporter`, and `AuditPrinter`
   (`internal/platform/diagnostics.go:92-143`). Registration is a hardcoded slice:
   `All()` returns five literal constructors (`platform.go:75-83`), and `ByID` walks it
   (`platform.go:86-93`). There is no data path to register a harness, and the command
   layer reaches the engine by enumerating `platform.All()` then running shared
   projection + per-platform `CreateLinks` (`commands/install.go:292-348`,
   `commands/add.go:597-616`, `commands/import.go:1632-1651`).

2. **Hooks need a hand-maintained per-harness event table.** Canonical event names are
   mapped to each harness's native event vocabulary by one Go map literal per harness —
   `claudeEventTable` (`internal/platform/hooks.go:736`), `codexEventTable` (`:779`),
   `cursorEventTable` (`:799`), `copilotEventTable` (`:837`). `isKnownCanonicalEvent`
   loops over exactly these four tables (`hooks.go:616-622`) and `mapEventName`
   (`hooks.go:718`) dispatches into them. A new harness's event vocabulary cannot be
   added without editing this Go.

3. **The shared-mirror allowlist is a hardcoded prefix list.**
   `isAllowlistedSharedMirrorTarget` enumerates seven literal path prefixes
   (`.agents/skills/`, `.claude/skills/`, `.claude/agents/`, `.codex/agents/`,
   `.opencode/plugins/`, `.opencode/agent/`, `.github/agents/`) in
   `internal/platform/resource_plan.go:267-276`. A new harness's target roots are not
   safe to overwrite until they are added to this list by hand.

4. **Frontmatter translation exists in exactly one place, and its parser is too weak
   to generalize.** The *only* frontmatter translation anywhere in the projection path
   is agents→Codex-TOML: `renderCodexAgentToml` (`internal/platform/codex.go:418-442`)
   reads frontmatter via `readFrontmatter` (`internal/platform/resources.go:205-239`) —
   a naive single-level line parser that `strings.Cut`s each line on the first `:`
   (`resources.go:232`) and cannot represent nesting, lists, or typed values. Every
   other frontmatter-bearing asset ships verbatim: skills are whole-directory symlinks
   (`buildSharedMirrorIntentsForRoot` emits `Shape: ResourceShapeDirectDir` /
   `Transport: ResourceTransportSymlink`, `resource_plan.go:342-343`, skill variant at
   `:353-361`), so the Claude-authored `SKILL.md` frontmatter reaches every harness
   byte-for-byte. **Whether that is a *defect* is unproven.** Across the five harnesses,
   skills and rules are all YAML-frontmatter markdown today with **no demonstrated
   per-harness frontmatter divergence** — the only asset class with proven divergence is
   agents→Codex-TOML (markdown frontmatter → a different serialization format). So the
   genuine, *proven* Claude-shaped artifact is narrow; the broader "frontmatter leaks
   everywhere" claim must be replaced by an **empirical divergence survey** (D1/F2)
   before any translator is built beyond the agents→Codex seed.

5. **The per-harness × per-asset × scope matrix is documented but not machine-consumed.**
   `docs/PLATFORM_DIRS_DOCS.md` holds a careful, vendor-verified matrix of where each of
   the five harnesses reads each asset class (rules, skills, subagents, hooks, MCP,
   settings) at each scope, including compatibility (COMPAT) read-paths vs native
   (NATIVE) ones — e.g. Cursor documents reading skills from `.agents/skills/`,
   `.claude/skills/`, and `.codex/skills/`, and subagents from `.claude/agents/` /
   `.codex/agents/` in addition to its own `.cursor/` roots. The engine cannot read this
   matrix; the same knowledge is re-encoded by hand in the Go above.

The net effect: harness knowledge lives in code in at least four places that must be
edited in lockstep, and the projection path has exactly one (asset, harness) frontmatter
translator built on a parser that cannot generalize. Two honesty caveats frame everything
below. First, **not all of `CreateLinks` is data** — source-priority selection
(`copilot.go:224-245`), user-home fanout (`copilot.go:216-219`), pruning of stale
rendered files (`copilot.go:321-325`), rendered-hook fallback (legacy vs canonical,
`copilot.go:304-326`), settings-MERGE semantics, MCP folding into `config.toml` /
`opencode.json`, and **semantic** hook rendering beyond event-name maps (Codex matcher
whitelist `hooks.go:929-1007`, Copilot matcher rejection `hooks.go:1057-1085`) are
irreducibly imperative. Second, **the frontmatter "leak" is proven only for
agents→Codex-TOML**; the rest is a hypothesis a survey must confirm. This spec therefore
targets the *declarative* harness knowledge, not a claim that a harness becomes pure data.

## 2. Goals

- **Cheaper harness addition.** Adding a new harness should move the *declarative*
  knowledge (read-paths, transports, event-name maps, allowlist roots, frontmatter
  dialect) out of hand-edited Go tables and into an authored descriptor, while an
  explicit, audited Go renderer/procedural core remains for the irreducible imperative
  steps. The goal is "descriptor + a known, bounded glue surface," **not** "descriptor
  only."
- **No *proven* frontmatter divergence ships unmodified.** Where a harness's frontmatter
  conventions are empirically shown to diverge from the canonical source (agents→Codex
  today), the projected frontmatter should be *translated*, not symlinked verbatim. Asset
  classes with no demonstrated divergence are out of scope until a survey proves a need.
- **Capability-granular minimal projection.** The projector should emit the *smallest*
  set of paths a harness actually reads, deciding compat-vs-native **per capability**
  (asset × scope × feature × form), not per asset — because a single asset can read compat
  for discovery yet still need a native path for some scope/precedence/setting/hook-form
  (and vice versa).
- **The matrix becomes machine-consumed (eventually).** The knowledge in
  `PLATFORM_DIRS_DOCS.md` should drive the engine; until doc-generation or bidirectional
  drift-checks exist, the doc remains the authority and the descriptors are reconciled
  against it (F5/R5).

Non-goals: re-architecting the `ResourcePlan` / intent-aggregation engine (it works);
changing the canonical source-of-truth layout under `~/.agents/`; resolving the cross-
machine portability concern (that is the sibling `home-config-portability` spec).

## 3. Decisions (owner-directed — baked in)

These three decisions are directed by the owner and are **not** open for re-litigation in
this spec. Their *mechanism* (schema shape, migration path) is open — see §5.

### D1 — Survey-gated frontmatter translator (agents→Codex is the seed)

Generalize the existing agent→Codex-TOML translation (`renderCodexAgentToml`,
`codex.go:418-442`) into a **per-harness frontmatter adapter driven by the descriptor's
declared dialect** (D2) — but **gate its scope on an empirical divergence survey**. The
translator is built for an asset class **only after the survey proves that asset class's
frontmatter actually diverges per harness**. agents→Codex-TOML is the *seed* (the one
proven case); every other asset class must earn a translator with evidence, not be
assumed to need one.

**Rationale.** Today exactly one translation exists, it is bespoke to one
(asset, harness) pair, and it is built on a parser that cannot survive non-trivial
frontmatter (`readFrontmatter` flattens everything to `map[string]string`,
`resources.go:205-239`; it `strings.Cut`s on the first `:`, `resources.go:232`). The
naive temptation is to declare a sweeping "Claude frontmatter leaks everywhere" problem
and build a universal translator. But the evidence does not support that: across the five
harnesses, **skills and rules are all YAML-frontmatter markdown with no demonstrated
divergence** — they ship verbatim today (`resource_plan.go:342-343`) without a known
correctness defect. The *only* proven divergence is agents→Codex-TOML (markdown
frontmatter serialized into a different format). Therefore D1 requires, as a hard
prerequisite, a **divergence survey**: enumerate, per asset class × per harness, whether
the frontmatter the harness reads actually differs in *shape or serialization* from the
canonical source. Build a translator for a class **iff** the survey finds real
divergence. This both removes the proven Claude-shaped artifact (agents→Codex) and stops
the bespoke-translation count from growing on speculation. See the §5 fork on the dialect
model and the §6 done criteria (DC2).

### D2 — Descriptor (declarative projection) + an explicit, audited Go core

A harness is represented as **two parts that must both be acknowledged**:

1. **A descriptor (data)** that declares the *declarative projection* — the knowledge that
   is genuinely "data about a harness" and today lives as hand-edited Go tables:
   - its **read-paths per asset class × scope**, tagged COMPAT vs NATIVE (the machine form
     of the `PLATFORM_DIRS_DOCS.md` rows);
   - its **transports / file forms** per asset class (the data form of the per-platform
     `Shape`/`Transport` choices, `resource_plan.go:342-345`);
   - its **hook event-name map** (the data form of the four hardcoded `*EventTable`s,
     `hooks.go:736-862`);
   - its **allowlist roots** for destructive replace, plus the **destructive-replace
     policy** those roots imply (the data form of the prefix list
     `resource_plan.go:267-276`);
   - its **frontmatter dialect** for any asset class the D1 survey proves divergent.

2. **An explicit, audited Go renderer / procedural core** that the descriptor **cannot
   absorb** and that *remains by design*. `CreateLinks` (and its symmetric `RemoveLinks`,
   `platform.go:58-61`) carry irreducible imperative logic: source-priority selection
   (`copilot.go:224-245`), user-home fanout (`copilot.go:216-219`), pruning of stale
   rendered files (`copilot.go:321-325`), rendered-hook fallback (`copilot.go:304-326`),
   platform-specific config semantics (settings-MERGE; MCP folding into `config.toml` /
   `opencode.json`), and **semantic** hook rendering beyond event-name maps (Codex matcher
   whitelist `hooks.go:929-1007`, Copilot matcher rejection `hooks.go:1057-1085`). The
   larger optional-interface surface (`diagnostics.go:92-143`:
   `BrokenLinkReporter`/`LinkCounter`/`StatusBadger`/`UserConfigReporter`/
   `OrphanCanonicalReporter`/`AuditPrinter`) is part of this core too.

The engine consumes descriptors for the declarative projection; the Go core handles the
procedural steps. Adding Hermes/Pi/Aider/Antigravity becomes *authoring a descriptor* plus
*writing/reusing a bounded, audited slice of the Go core* — **not** "descriptor only," and
**not** ~300-700 lines of fresh bespoke Go either. The descriptor **must account for** the
proven frontmatter translation (D1) and the capability-granular read-paths (D3).
`PLATFORM_DIRS_DOCS.md` is the human matrix the descriptors formalize.

**Rationale.** The four lockstep edit sites (the `All()` slice `platform.go:75-83`, the
event tables `hooks.go:736-862`, the allowlist `resource_plan.go:267-276`, and the
per-platform intent shapes) are *data about a harness* expressed as *code* — those collapse
cleanly into a descriptor. But the audit-honest position is that `CreateLinks` also holds
logic that is *not* data; pretending otherwise produces a descriptor schema that silently
grows imperative escape hatches until it is a programming language. The design therefore
draws a hard line: descriptor owns the declarative projection; an **explicit, named,
audited** Go core owns the rest. The deliverable is a *smaller, bounded* glue surface with
a written inventory of what stayed imperative (see F1 and the F4 hand-add probe), not the
elimination of Go.

### D3 — Capability-granular projection minimization

Project to what a harness **reads**, deciding compat-vs-native **per capability**, where a
capability is **(asset class × scope × feature × form)** — *not* per asset. For each
capability the descriptor declares the read-path(s) the harness actually consumes, and the
projector emits the minimal set: a NATIVE path is emitted for a capability **only** when
the harness reads no COMPAT path *for that capability*. "Reads compat for asset
**discovery**" is a distinct capability from "needs a native path for some
scope/precedence/setting/hook-form"; one being satisfied by compat does **not** imply the
others are.

**Why per-asset is wrong (the blocker the audit caught).** "Native only when no compat
path exists, at the asset level" silently drops real capability. The evidence:

- **Hooks and MCP have no meaningful cross-platform compat path at all**
  (`PLATFORM_DIRS_DOCS.md`: "No meaningful cross-platform compat path is documented" for
  both). For these the native path is the *only* path — a per-asset compat-first rule that
  ever suppressed it would break the harness.
- **Copilot wires BOTH a Claude-compat surface AND native capability for the same asset.**
  `CreateLinks` emits the Claude-compat hook settings (`.claude/settings.local.json` via
  `createClaudeCompatLinks`, `copilot.go:206-209` / `:279-301`) **and** native
  `.github/hooks/{name}.json` project files (`createProjectHookFiles`, `copilot.go:212` /
  `:304-326`) **and** `~/.copilot/hooks/{name}.json` user-home fanout (`copilot.go:216-219`).
  A per-asset "compat wins, drop native" rule would silently delete the native Copilot hook
  capability. Compat-for-discovery and native-for-execution coexist *within one asset*.
- **Skills are split-GCD, not a single compat winner.** `.claude/skills/` and
  `.agents/skills/` are **both** kept in sync from one canonical source because each has
  4-platform coverage (`PLATFORM_DIRS_DOCS.md` skills row: "keep both … in sync"); the
  practical policy wires two paths, not "the one compat winner." A per-asset winner-take-all
  rule cannot express split-GCD.

So the minimization is **per-capability**: it honors the owner's intent ("don't redundantly
project a native path where the harness already reads a compat path for *that* capability")
at the correct granularity, while still emitting native paths for the capabilities (hooks,
MCP, native-precedence settings, split-GCD second roots) where compat does not cover the
read. This **dissolves** any standalone "native-path projection gap" feature into the
descriptor's capability-level read-paths and the projector's minimal per-capability
emission — there is no separate native-projection feature.

## 4. Behavioral requirements

(Behavioral, not implementation — the *how* belongs in a plan.)

- **R1 — Declarative knowledge moves to the descriptor; the Go core stays explicit.**
  Adding a harness must move its *declarative* projection (read-paths, transports,
  event-name maps, allowlist roots, dialect) out of the hardcoded edit sites — the `All()`
  slice (`platform.go:75-83`), the four event tables (`hooks.go:736-862`), and the prefix
  allowlist (`resource_plan.go:267-276`) — and into a descriptor, **without modifying the
  projection/aggregation engine** to learn those facts. It must **not** claim
  descriptor-only addition: an explicit, audited Go renderer/procedural core (the
  irreducible `CreateLinks`/`RemoveLinks` logic in D2) remains, and the plan must produce a
  written inventory of exactly what stayed imperative for the probe harness (F4).
- **R2 — Translate only proven-divergent frontmatter.** For an asset class the D1 survey
  proves divergent, projected frontmatter must be **translated per the target descriptor's
  declared dialect**, not symlinked verbatim (agents→Codex is the seed). For asset classes
  with no demonstrated divergence, verbatim projection remains correct and **no translator
  is built** — the requirement is survey-gated, not blanket.
- **R3 — Capability-granular minimal projection.** For each **(harness, asset, scope,
  feature, form) capability**, the projector emits the COMPAT read-path(s) the harness
  reads for that capability and a NATIVE path only where no compat path covers it (D3).
  This must correctly produce: native-only emission for hooks/MCP (no compat path),
  coexisting compat+native for Copilot hooks (`copilot.go:206-219`/`:304-326`), and
  split-GCD dual roots for skills (`.claude/skills/` + `.agents/skills/`).
- **R4 — Hook events from the descriptor (event-name layer only).** Canonical-event →
  native-event mapping and the known/valid event set for a harness must derive from
  descriptor data, so `isKnownCanonicalEvent`-style validation (`hooks.go:616-622`) covers
  a new harness without a new Go `*EventTable`. **Semantic** hook rendering beyond the
  name map (Codex matcher whitelist `hooks.go:929-1007`, Copilot matcher rejection
  `hooks.go:1057-1085`) stays in the Go core (R1) — the descriptor declares the
  event-name map and matcher-support shape, not the rendering procedure.
- **R5 — Matrix fidelity, with the doc as authority (for now).** The descriptors must
  agree with `docs/PLATFORM_DIRS_DOCS.md` for the five existing harnesses. **Until**
  doc-generation or bidirectional drift-checks exist (F5), the matrix is the authority and
  the descriptors are reconciled against it; descriptor↔matrix divergence is a defect.
- **R6 — No projection regression for the five existing harnesses.** Whatever the
  migration path (§5), the projected output for Cursor/Claude/Codex/OpenCode/Copilot must
  remain correct (the engine is not being changed, only its inputs), including the symmetric
  `RemoveLinks` teardown (`platform.go:58-61`).
- **R7 — `CreateLinks`/`RemoveLinks` symmetry.** Whatever a harness's descriptor + core
  writes via `CreateLinks`, its `RemoveLinks` must remove exactly — including
  capability-granular native paths, compat surfaces, and user-home fanout. The descriptor's
  declared target roots are the shared source of truth for both directions.
- **R8 — Diagnostics/status surface accounted for.** The plan must state, per probe harness,
  how the optional diagnostics/status interfaces (`diagnostics.go:92-143`:
  `BrokenLinkReporter`, `LinkCounter`, `StatusBadger`, `UserConfigReporter`,
  `OrphanCanonicalReporter`, `AuditPrinter`) are satisfied — descriptor-derived where the
  data allows, Go-core otherwise — so `da doctor`/`da status` do not silently degrade for
  the new harness.
- **R9 — Command-integration compatibility.** The descriptor + core must slot into the
  existing command paths that enumerate `platform.All()` then run shared projection +
  `CreateLinks` (`commands/install.go:292-348`, `commands/add.go:597-616`,
  `commands/import.go:1632-1651`) without per-command changes per harness.
- **R10 — Descriptor validation + versioning + destructive-replace policy.** Descriptors
  must be **validated** (well-formed read-paths, known transports, event-map shape) and
  **versioned** (schema evolution without breaking existing descriptors). The hardcoded
  allowlist removal (`resource_plan.go:267-276`) requires a descriptor-owned
  **destructive-replace policy** — which target roots a harness may overwrite, and under
  what guard — not merely a list of target roots, since those roots gate managed-replace.

## 5. Open questions / forks (surface — do NOT resolve here)

These require owner ratification before a plan is written. Each lists a **recommended
default**, not a decision.

- **F1 — Where exactly the descriptor/Go-core line falls.** Given that the outcome is
  **"descriptor (declarative) + an explicit, audited Go renderer/procedural core"** (not
  descriptor-only), which specific surfaces cross to data and which stay imperative? The
  surface to triage is the 9-method interface (`platform.go:49-72`), the optional readers
  (`platform.go:11-46`), and the diagnostics/status interfaces
  (`diagnostics.go:92-143`) — each method classified declarative vs procedural.
  *Recommended default:* descriptorize the declarative surfaces (read-paths, transports,
  event-name maps, dialect, allowlist roots + destructive-replace policy) and keep an
  **explicit, named, audited** Go core for the procedural steps (source-priority,
  user-home fanout, pruning, rendered-hook fallback, settings-MERGE, MCP folding, semantic
  matcher rendering, the diagnostics readers). Triage each method explicitly in the plan
  and **record the irreducible Go as an inventory** (per F4); do not assume all-or-nothing
  and do not claim descriptor-only.

- **F2 — The frontmatter-dialect schema / model.** What is the model for a "dialect"?
  Options span (a) a declarative field-mapping + target-format enum (md-frontmatter,
  TOML, JSON, none), (b) a richer transform spec (rename/drop/default/nest rules), or
  (c) a pluggable renderer keyed by format. The current parser
  (`readFrontmatter`, `resources.go:205-239`) is too weak to be the foundation and must
  be replaced/augmented regardless.
  *Recommended default:* **survey first, then size the model to what the survey proves.**
  Run the D1 divergence survey (which asset classes actually diverge, per harness) before
  choosing; start with (a) — field-map + target-format enum — for the proven cases
  (agents→Codex is the seed), and escalate to (b) only if the survey finds transforms (a)
  cannot express. Adopt a real YAML frontmatter parser rather than extending the
  line-splitter (`resources.go:205-239`). Do not build a translator for any asset class
  the survey does not flag.

- **F3 — Migration path for the five existing harnesses: big-bang vs incremental.** Do we
  convert all five platforms to descriptors at once, or run descriptors and the existing
  Go side-by-side, migrating one harness/asset-class at a time behind R6's no-regression
  guarantee?
  *Recommended default:* **incremental** — introduce the descriptor as an additive input,
  migrate one asset class (skills, the clearest leak) for one new sample harness first,
  then back-fill the five, keeping the Go path as the oracle until parity is proven.

- **F4 — Hand-add ONE real harness first (MANDATORY prerequisite, not a recommendation).**
  This fork is **resolved to a hard gate**: the work **must** begin by hand-adding a single
  *real* harness (e.g. Aider or Hermes) end-to-end in Go, and **recording the irreducible
  Go** that surfaces (the inventory R1/F1 require), **before** the descriptor schema is
  ratified. Schema ratification is **blocked** until this probe is done. The only thing
  open for the owner here is *which* harness to probe and *what counts as "done"* for the
  probe — not whether to do it.
  *Rationale:* this is the empirical-first discipline (per the §7 scientific-method-spine
  pattern). Designing the descriptor schema speculatively risks baking in the wrong fields
  and discovering the imperative escape hatches only after committing. The probe lets the
  actual friction define the schema's required fields and produces the honest Go inventory
  D2 demands. DC6 is the verifiable form of this gate.

- **F5 — Descriptor vs `PLATFORM_DIRS_DOCS.md`: when (if ever) does the descriptor become
  the source of truth?** Is the descriptor *generated from* the doc matrix, the doc
  *generated from* the descriptors, or both with a consistency check (R5)?
  *Recommended default:* **the doc stays the authority until tooling exists to keep them
  in sync.** The descriptors become the machine source of truth **only after** either
  doc-generation (doc rendered from descriptors) or a bidirectional drift-check exists.
  Until then `PLATFORM_DIRS_DOCS.md` remains the matrix authority (R5) and descriptors are
  reconciled against it via the existing refresh path (`platform-docs-refresh` /
  `release-docs-refresh` skills and the `platform-dirs-change-analyst`). Promoting the
  descriptor to SOT without that tooling would silently let the human matrix and the
  machine data diverge.

## 6. Done criteria (verifiable)

- **DC0 — Empirical probe completed before schema ratification (F4 gate).** A single
  *real* harness has been hand-added end-to-end in Go, and the **irreducible-Go inventory**
  (what stayed imperative: source-priority, fanout, pruning, fallback, settings-MERGE, MCP
  folding, semantic matcher rendering, diagnostics readers) is recorded. The descriptor
  schema is not ratified — and no plan past the probe is written — until this exists.
- **DC1 — Sample harness projects correctly via descriptor + bounded Go core.** A *sample*
  harness added via its descriptor **plus the explicit, audited Go core** (the surface F1
  classifies as irreducible) projects its assets to the correct capability-granular paths,
  validated against `PLATFORM_DIRS_DOCS.md`, **without editing the projection engine, the
  `All()` slice, the event tables, or the allowlist** to teach the engine the harness's
  *declarative* facts. (This is explicitly **not** "descriptor-only" — the bounded Go core
  is expected and inventoried.)
- **DC2 — Proven-divergent frontmatter gets translated output; non-divergent stays
  verbatim.** For an asset class the D1 survey flagged as divergent, a harness declaring a
  divergent dialect receives **translated** frontmatter — verifiably *not* the canonical
  verbatim frontmatter (agents→Codex is the demonstrable seed). For a non-flagged asset
  class, projection remains verbatim (`resource_plan.go:342-343`) and **no** translator
  was built.
- **DC3 — Capability-granular minimal projection.** Verifiable on the three hard cases:
  (a) hooks/MCP (no compat path) emit the NATIVE path; (b) a Copilot-style harness emits
  **both** the compat surface and the native hook files for the same asset
  (`copilot.go:206-219`/`:304-326`) — neither is dropped; (c) skills emit the split-GCD
  dual roots (`.claude/skills/` + `.agents/skills/`), not a single winner; and a capability
  whose compat path the harness reads emits **no** redundant native path.
- **DC4 — Hook event-name map from descriptor.** A canonical hook event declared on the
  sample descriptor passes `when_events` validation (`hooks.go:616-622`) and renders to the
  harness's native event name **without** a new Go `*EventTable`; semantic matcher behavior
  is still driven by the Go core, and the descriptor declares only the matcher-support
  shape.
- **DC5 — No regression (both directions).** The five existing harnesses' projected output
  **and** `RemoveLinks` teardown are unchanged (R6/R7), demonstrated by the existing
  `go test ./internal/platform/... ./commands/...` suite and the platform smoke tests.
- **DC6 — Descriptor validated + versioned; destructive-replace policy enforced.** A
  malformed descriptor is rejected by validation; a descriptor schema version is present
  and honored; and the destructive-replace policy (R10) — not just target roots — governs
  which roots the harness may overwrite (replacing `resource_plan.go:267-276`).
- **DC7 — Diagnostics/status do not degrade.** `da doctor`/`da status` report correct
  broken-link/badge/audit output for the sample harness via the `diagnostics.go:92-143`
  interfaces (descriptor-derived or Go-core, per R8).

## 7. Deferred (out of scope)

- Cross-machine / portable home-config layout — owned by `home-config-portability`.
- External/remote source packaging, registries, signing — owned by
  `external-agent-sources` and `config-distribution-model`.
- Any change to the `ResourcePlan` aggregation/dedup/conflict engine itself (it works;
  this spec changes its *inputs*, not its core).
- Re-verifying vendor read-paths — that is the standing job of `platform-docs-refresh` /
  `release-docs-refresh`; this spec consumes the matrix, it does not re-derive it.

## 8. Relationships

- **`docs/PLATFORM_DIRS_DOCS.md`** — the human-authored, vendor-verified per-harness ×
  per-asset × scope matrix this spec makes machine-consumable (D2); the authority for R5
  / DC1. Reconciliation owned by `platform-docs-refresh` and the
  `platform-dirs-change-analyst`.
- **`home-config-portability`** (sibling) — the cross-machine concern. Extensibility
  (this spec, "cheap to add a harness") and portability ("config travels across
  machines") are orthogonal; descriptors should not assume a fixed home layout.
- **`config-distribution-model`** (`source` / `scope` model) — descriptors are data that
  flows through the same source/scope distribution machinery; the dialect/read-path data
  is scope-attachable in principle.
- **`external-agent-sources`** — a harness descriptor and an external agent *source* are
  distinct axes; a future external source could ship descriptors, but that is not in
  scope here.
- **`.agents/proposals/scientific-method-spine-domain-general.md`** — the method behind the
  F4 gate: the hand-add-one-harness probe is the **mandatory experiment** that ratifies or
  refutes the descriptor schema before any plan commits to it. Schema ratification is
  blocked on it (dogfood pattern; DC0).

## 9. F4 probe findings — "what it takes to hand-add ONE harness today"

This section is the **empirical half of the F4 gate** (DC0): a read-only enumeration of
every place a harness is hard-coded today, run against the current tree on 2026-06-28 by
reading `internal/platform/*`, `internal/config/*`, `internal/links/*`, `commands/*`, and
the templates. It answers the probe question literally — *to hand-add a hypothetical
"windsurf" (or "zed") harness, which sites must a contributor edit?* — and classifies each
**data (descriptorizable, D2.1)** vs **logic (irreducible Go core, D2.2)** so the inventory
feeds F1 and the DC0 record directly. The remaining half of the gate (hand-add a *real*
harness end-to-end and record what surfaces) is still required before schema ratification.

### 9.1 Headline numbers

- **~7 Go files must be edited in lockstep** to add one harness; **0 config-layer files** and
  **0 template files** need to change.
- **One new `*.go` harness file of ~300–700 lines** implementing **~30–41 method receivers**
  (the concrete `claude.go` carries **41** `func (c *claude)` receivers; `opencode.go` is the
  floor at ~15). The mandatory `Platform` interface is **9 methods**
  (`platform.go:49-72`); the rest are the optional-but-de-facto-required diagnostics/session
  surfaces.
- **The friction is entirely in the projection/platform layer.** The config layer is already
  data-driven and needs no edit (see §9.4) — which is exactly the asymmetry D2 exploits: the
  *enable/select* side is data; the *project* side is code.

### 9.2 The hard-coded edit sites (data-vs-logic classified)

| # | Touchpoint | file:line | What a "windsurf" addition requires | D2 class |
|---|---|---|---|---|
| 1 | **Registration slice** `All()` | `internal/platform/platform.go:75-83` | Add `NewWindsurf()` literal; `ByID` (`:86-93`) and every `platform.All()` caller pick it up automatically | **data** (registry) |
| 2 | **New harness file** `windsurf.go` | (new file) | ~300–700 LOC: const block (`.windsurf` dir, config/hook/mcp filenames), `ID`/`DisplayName`/`IsInstalled`/`Version`, `CreateLinks`/`RemoveLinks`, `HasDeprecatedFormat`/`DeprecatedDetails`, `SharedTargetIntents` | **mixed** — paths/transports are data; `CreateLinks` body is logic |
| 3 | **Hook event table** | `internal/platform/hooks.go:736 / :779 / :799 / :837` | Add a fifth `windsurfEventTable` map literal | **data** (canonical→native name map) |
| 4 | **Event-set validation loop** | `hooks.go:616-622` (`isKnownCanonicalEvent`) | Add the new table to the literal slice it ranges over | **data** (derivable from #3) |
| 5 | **Event dispatch helper** | `hooks.go:864-876` (`mapEventName` wrappers) | Add a `windsurf` wrapper binding the table | **data** |
| 6 | **Hook render fn** | `hooks.go:879 (claude) / :959 (codex) / :1009 (cursor) / :1057 (copilot)` | New `renderWindsurfHookConfig/Entry/File` in the harness's native shape; **plus** any matcher-narrowing semantics (Codex whitelist `hooks.go:929-1007`, Copilot matcher rejection `:1057-1085`) | **logic** (semantic rendering ≠ name map) |
| 7 | **Shared-mirror allowlist** | `internal/platform/resource_plan.go:267-276` (`isAllowlistedSharedMirrorTarget`) | Add windsurf's destructive-replace target roots (e.g. `.windsurf/skills/`, `.windsurf/agents/`) to the 7-prefix literal — until added, the projector refuses to overwrite them (`:253`) | **data** (roots) + **policy** (the replace guard) |
| 8 | **Frontmatter translation** *(only if dialect diverges)* | `internal/platform/codex.go:418` (`renderCodexAgentToml`) + `internal/platform/resources.go:205-239` (`readFrontmatter`) | Only if windsurf reads a non-md serialization (like Codex TOML). The existing parser `strings.Cut`s each line on the first `:` (`resources.go:232`) → cannot represent nesting/lists/typed values; a real YAML parser is a prerequisite (F2) | **data** (field map) on a **logic** foundation that must be replaced |
| 9 | **Diagnostics/status interfaces** | `internal/platform/diagnostics.go:94 / :101 / :109 / :119 / :132 / :142` | Implement `BrokenLinkReporter`, `LinkCounter`, `StatusBadger`, `UserConfigReporter`, `OrphanCanonicalReporter`, `AuditPrinter` in `windsurf.go` — else `da doctor`/`da status` silently degrade for the harness (R8/DC7) | **mixed** — counts derive from declared roots; some scans are logic |
| 10 | **Session/stats readers** *(optional, stubs allowed)* | `internal/platform/platform.go:11-46` | `SessionReader`/`StatsReader`/`SessionTokenScanner`/`BranchSessionFinder` — empty stubs are valid until the harness's env-var + session-store contract is known | **logic** (per-vendor store formats) |
| 11 | **Plugin-import switch** | `commands/import_plugins.go:419-465` | Per-harness `case` arms for `da import`'s plugin handling | **logic** |
| 12 | **Projection entry-points** | `commands/refresh.go:146` + `:309-322`; `commands/add.go:348/450/605`; `commands/import.go:1639` | **No per-harness edit** — all iterate `platform.All()`, so #1 wires them. (Confirms R9: command layer is already harness-agnostic.) | **none** (auto) |

**Stale-citation correction:** §1, R9, R1 and the DC1/DC3 cites reference
`commands/install.go:292-348` as a projection entry-point. **That file no longer exists.**
The install/refresh path now lives in `commands/refresh.go` — `reportEnabledPlatforms`
ranges `platform.All()` at `:146`, and `recreatePlatformLinks` re-runs `CreateLinks` for
every enabled+installed platform at `:309-322` (`platform.InstalledEnabledPlatforms` at
`:89`). The behavioral claim (commands enumerate `All()` then project) holds; only the
filename is wrong and should be fixed when the plan is written.

### 9.3 Partial irreducible-Go inventory (DC0 down-payment)

The probe confirms the §1/§2 honesty caveats with concrete sites. These resist
descriptorization and define the *floor* of the Go core the plan must keep (F1). This is the
**enumeration-derived** inventory; the **execution-derived** inventory (what actually
surfaces when a real harness is hand-added) is still owed by DC0:

- **Source-priority selection** — candidate-source ordering when several canonical files
  could feed one target (e.g. Copilot legacy-vs-canonical hook spec, `copilot.go:304-326`;
  Claude/Codex rule-source enumeration across `.md`/`.mdc`/`.txt`).
- **User-home fanout** — per-user mirror writes distinct from repo-local projection
  (`copilot.go:216-219`; `claude.go` `ensureUser*` family `:404-509`).
- **Stale-render pruning** — deleting previously-rendered fan-out files no longer wanted
  (`copilot.go:321-325`).
- **Config-merge semantics** — settings-MERGE, and MCP folding into `config.toml` /
  `opencode.json` rather than a standalone file.
- **Semantic hook rendering beyond the name map** — Codex matcher whitelist
  (`hooks.go:929-1007`), Copilot matcher rejection (`hooks.go:1057-1085`).
- **`CreateLinks`/`RemoveLinks` symmetry** (`platform.go:58-61`) — teardown must mirror
  creation exactly, including capability-granular native paths and user-home fanout (R7).
- **Per-vendor session/stats store formats** — JSONL vs SQLite, distinct schemas, no overlap
  (`claude.go`, `cursor.go`, `codex.go`, `opencode.go` reader bodies).

### 9.4 The negative finding (sharpens D2)

The **config layer requires no changes to add a harness**, which is the empirical evidence
for D2's data/code split being drawn in the *right* place:

- `.agentsrc.json` `agents` is an **open `[]string`** (`internal/config/agentsrc.go:231`);
  `agentsRCKnown` validates the *key* `"agents"`, never the member harness names
  (`agentsrc.go:658-688`).
- Home config `agents` is an **open `map[string]Agent`** with only `{enabled, version}`
  (`internal/config/config.go:28`, `:65-68`); the only harness-name `switch` is a 2-arm
  legacy alias (`claude-code`/`github-copilot`, `config.go:365-368`) — read-side fallback,
  not a closed set.
- A profile can already scope to a harness via the `harness` selector key with **no
  registration** (`internal/config/profile.go:50-52`, `:61`, `:89-103`); matching is plain
  string equality. Unit kinds (`unit_kinds.go`, `lock_units.go:17-33`) are harness-agnostic.

**Implication for the descriptor's home (F1/§5):** because the config substrate already
treats harness identity as open data and a profile selector already keys on `harness`, a
**harness descriptor is a natural §15 `unit` of a new `kind` (e.g. `kind: harness`)** — the
distribution/scope/lock machinery would carry it for free, and built-in descriptors for the
five existing harnesses would be the seed set, with user-contributed descriptors arriving
through the same source/scope path as any other unit. The probe does **not** decide this
(it remains an F1/§5 fork), but it shows the substrate is ready to host descriptors without
new plumbing.

### 9.5 Templates: no per-harness surface

`src/share/templates/` (cited in older planning notes) **no longer exists**. The only
templates are `internal/scaffold/templates/files/{skill,agent}.md.tmpl`, which emit a
**generic** YAML frontmatter (`name`/`description`) consumed identically by all harnesses;
per-harness divergence is applied later at link/render time, not at scaffold time. This
corroborates D1's survey-gated stance: there is **no per-harness template fork to
descriptorize** — the only proven dialect divergence remains agents→Codex-TOML (touchpoint
#8), and every other asset ships verbatim (`resource_plan.go:342-343`).
