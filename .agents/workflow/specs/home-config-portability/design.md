# Home Config Portability — Design Spec

- **id:** home-config-portability
- **status:** DRAFT (revision pass — re-gates after; pending owner ratification of the open forks in §5)
- **author:** Nikash Prakash
- **created:** 2026-06-26
- **revised:** 2026-06-26 (GATE-1 two-leg audit fixes — portable project identity, hybrid default, agents-split + cache-boundary defects; then a git-grounding pass — projection is the existing, working multi-harness fan-out reused as-is and is NOT a portability redesign blocker, D-G references `docs/PLATFORM_DIRS_DOCS.md`, native-path descoped to multi-harness-extensibility, NEW-FORK-B git-auth resolved)
- **owns:** the *what & why* of making the dot-agents user scope (`~/.agents`) portable across machines. File paths, function names, task ordering, and migration code belong to a later plan, not here.

---

## 1. Problem Statement

Moving a dot-agents user environment from one device to another — or using user-scope
skills/sources authored on machine A from machine B — has friction that the model already
claims should not exist. Concrete defects:

1. **Absolute project paths leak into the synced config.** `~/.agents/config.json` stores
   absolute, machine-specific project paths (`Projects.<id>.path`, cleaned via
   `filepath.Clean` in `AddProject`, `internal/config/config.go:84-93`). The struct is
   `internal/config/config.go:11-38`; the only absolute-path leak in the whole struct is
   `Project.Path`. When the home repo is synced to machine B, those paths point at
   directories that do not exist there, and `da refresh` skips every project
   (directory-not-found).

2. **Code/spec divergence on what is machine-local.** The config-v2-coherence proposal
   (`.agents/proposals/config-v2-coherence-scopes-sources-lock.md`,
   `.agents/workflow/specs/config-v2-coherence/`) explicitly classifies `config.json` as
   *"genuinely machine-local — never a scope, never projected."* But `da sync init`
   gitignores only `local/` (`commands/sync/init.go:76-78` `initSyncRepo`, the `.gitignore`
   write of `"local/\n*.dot-agents-backup\n"`). `config.json` is therefore git-tracked and
   travels to machine B. **The model says machine-local; the code syncs it.** That divergence
   is the root cause of defect 1's blast radius.

3. **The portable project *identity* has no home — the project LIST itself does not travel
   (GATE-1 BLOCKER 1).** Defect 2's naive fix — stop syncing `config.json` — makes machine B
   *worse*, not better. Today the whole `Projects` map syncs, so machine B at least learns the
   *names* of the managed projects (even if the paths are dead). If phase-0 simply gitignores
   the registry, machine B's `refresh` resolves against **nothing** and reports "No managed
   projects." Yet R4/DC2 require rebinding **all** projects on machine B. There is currently
   no surface that carries a portable project identity (a stable id + a portable key with no
   path) independent of the machine-local `id → absolute path` binding. Stopping the path sync
   without first shipping that identity registry deletes the only signal machine B has.

4. **No bootstrap verb to stand up the user scope from a remote home source.** `da init`
   (`commands/internal/lifecycle/init.go`, the `runInit` scaffold path) only scaffolds a
   *fresh-local* user home from embedded starters — there is no `--from`, no clone, no
   pull-bootstrap. `da sync init` has no `clone` counterpart (it only `git init`s an existing
   home). `da install` bootstraps a *project* from a repo `.agentsrc.json`, not the user home.
   So adopting an existing home on a new machine is a manual `git clone` into `~/.agents`
   followed by hand-repair of the project table.

5. **Machine-local caches sync (same defect class as `config.json`).** The tier-1 config
   cache `~/.agents/cache/config` (`internal/config/fetcher.go:227-229`) and the tier-2
   artifact cache `~/.agents/cache/packages` (`internal/config/fetcher_oci.go:184-187`) are
   machine-local materialized content, but `da sync init` gitignores only `local/`
   (sync/init.go:76-78) → `cache/` is git-tracked and travels. Both caches must join the
   machine-local sync boundary.

6. **A synced home cannot fetch private git sources on machine B (or leaks credentials).** The
   git layer/artifact fetcher clones via `gitCloneShallow` with **no auth** threaded into
   `go-git`'s `CloneOptions` (`internal/config/fetcher.go` `gitCloneShallow`, ~`:305-312`),
   whereas the HTTP and OCI fetchers DO carry `src.Auth` (`fetcher_http.go:103`,
   `fetcher_oci.go:406`). A freshly-adopted home on machine B that declares a private
   team/org git source therefore either cannot resolve it, or — if credentials were embedded
   to make it work — leaks secrets through the synced tree. Fresh-machine credential bootstrap
   is an unsolved input to `init --from` (see R1, NEW-FORK-B).

The owner wants: `da init --from <home-source>` that resolves the user-scope config from a
chosen source, then materializes machine-local bindings locally — with project re-binding
that resolves a **portable project identity** that *did* survive the trip (defect 3), not a
machine-specific absolute path that did not.

### Why now

Device-to-device portability is a stated user goal and the coherence model already promises
it. The fix is also a forcing function for two things the model wants anyway: a first-class
*loaded* user scope, and a single declared home source that every project inherits.

---

## 2. Goals

- A user can stand up their full user scope on a second machine from a remote home source
  with **zero manual per-project absolute-path entry**.
- After adoption, `da refresh` binds **all** projects on machine B by resolving a **portable
  project identity** (carried in a synced surface) to a machine-local path — not by trusting a
  synced absolute path.
- A **portable project identity registry** (stable id + portable key, **no path**) is a synced
  surface, **distinct from** the machine-local `id → absolute path` binding table. The list of
  managed projects travels; only the path binding is per-machine.
- Portable configuration (preferences, declared sources, layering policy, platform-management
  *intent*, project identities) **syncs**; machine-local state (the id→path binding table,
  detected platform versions/enablement, caches) **never** appears in the synced git tree.
- The portable parts of `config.json` are expressed through the **one** config-v2 model
  (units / sources / lock / scope chain) by moving them **into the already-loaded user-local
  layer** (`~/.agents/.agentsrc.json`, `LayerUserLocal`), not a bespoke second config format —
  so they reuse the resolver, source resolution, and content-hash staleness.
- A bounded **phase-0** fix lands first: ship the portable project identity registry
  **together with** stopping the path sync (and adding `cache/` to the machine-local
  boundary), so device-to-device improves rather than regresses, before the full `init --from`
  UX is built.

### Non-goals (this spec)

- Choosing the concrete file layout, struct split, or migrator code (plan-level).
- Solving multi-user / shared-team home adoption (the home source is single-owner here).
- Re-litigating the config-v2-coherence scope chain itself; this spec consumes it.

---

## 3. Decisions (DECIDED — bake into the plan)

These (D-A..D-G) are owner-directed and are **not** open for the plan to relitigate. Rationale
is given so the plan can trace each requirement back to intent.

### D-A — Portable parts of `config.json` GRADUATE into the user-scope config-v2 surface; `config.json` shrinks to a pure machine-local registry

This is **not** "split config.json into two files." It is: express the portable fields as
**user-scope config** (units / sources / lock — the one model) so they get single-source and
coherence for free; what remains in `config.json` is a pure machine-local registry.

**Rationale:** reuses units / lock / `EnsureResolved` / source-resolution rather than inventing
a parallel sync path; and it makes the global-home-source default (D-C) fall out for free,
because once the home source is a declared user-scope source, every project inherits it via
the scope chain.

**Field disposition table** (current struct: `internal/config/config.go:11-38`). Note the
`Projects` map splits into **two distinct surfaces** — a portable *identity registry* and a
machine-local *binding table* — which today are conflated in one field:

| Current field | Disposition | Destination | Why |
|---|---|---|---|
| `Defaults.Agent` | **portable** | user-local layer (`~/.agents/.agentsrc.json`) | a preference, identical on every machine |
| `Features.{Tasks,History,Sync}` | **portable** | user-local layer | feature opt-ins are user intent, not machine state |
| declared user sources / layering policy | **portable** | user-local layer | the home source + scope-attachment policy must travel |
| `Agents.<id>` — *intent to manage this platform* | **portable** | user-local layer | "I use Claude Code + Codex" is a user choice — **but is not representable today** (see below) |
| `Agents.<id>.{enabled, version}` — *detected* | **machine-local** | machine-local registry | seeded from per-machine `IsInstalled` probing and re-probed by refresh; different per box |
| `Projects.<id>` — *stable id + portable key (no path)* | **portable** | **portable identity registry (synced surface)** | the project LIST must travel so machine B knows what to rebind (defect 3 / BLOCKER 1) |
| `Projects.<id>.path` | **machine-local** | machine-local **binding table** | absolute, machine-specific — the leak in defect 1; distinct surface from the identity registry |
| `Projects.<id>.added` | **machine-local** | machine-local binding table | registry bookkeeping, no portable meaning |

**Portable project identity registry (the BLOCKER-1 fix).** A project has two facets: *which
project this is* (a stable id + a portable key — see FORK-1, now resolved=hybrid) and *where
it lives on this box* (an absolute path). The first is portable and **must travel in a synced
surface**; the second is machine-local. These are **two distinct surfaces**, not one field
classified one way. The identity registry carries the id + portable key with **no path**; the
binding table maps id → absolute path per machine. R4/DC2 resolve a project by joining the
synced identity registry to the local binding table (binding it on first touch where absent).

**`Agents.<id>` split is NOT representable today (GATE-1, codex).** `config.Agent` carries only
`Enabled` and `Version` (`internal/config/config.go:29-31`) — there is no field expressing
*intent to manage* separate from *detected state*. Worse, the precedence the split needs is not
present: `refresh` auto-enables **every** installed platform (`DetectAndEnableNewPlatforms` →
`SetPlatformState(p.ID(), true, p.Version())`, `commands/internal/lifecycle/refresh.go:37`),
and an absent platform **defaults to enabled** (`IsPlatformEnabled`, `config.go:125-126`). So
the portable-intent vs. machine-local-detection split **requires** (a) adding an intent field
to the schema and (b) changing the auto-enable default. This is surfaced as a requirement (R11)
and as an owner fork (NEW-FORK-A); it must honor the AgentsRC additive-field lifecycle
(`.agents/rules/dot-agents/schema-usage.md`). **Precedence rule to preserve:** *intent gates
candidacy, probe gates realization* — a platform is a candidate only if intent says manage it,
and is realized only if `IsInstalled` probes true on this machine.

### D-B — Move the portable fields INTO the already-loaded user-local layer

**Corrected premise (GATE-1, codex).** The earlier framing — "make the user scope a *loaded*
surface" — was **false**. The user-scope manifest is **already a loaded surface today**: the
resolver loads `LayerUserLocal` from `~/.agents/.agentsrc.json` and resolves it through the
scope chain (`internal/config/resolver.go:133-144`), and that file is git-tracked / synced
today. The work is therefore **smaller** than originally stated: it is not "stand up a new
loaded user surface," it is "**move D-A's portable fields into the user-local layer that is
already loaded**."

**Rationale:** D-A's portable fields (`Defaults.Agent`, `Features`, declared user sources,
layering policy, the manage-intent, and the project identity registry) need a coherent home;
that home already exists and is already resolved. Choosing how each field maps onto the
user-local manifest's shape is a **plan detail**; the *decision* here is that the portable
fields land in `LayerUserLocal`, not in a new bespoke surface.

### D-C — Global home-source default: the home source is declared once and inherited by every project

The user/home source is declared **once** in the user-scope config; every project inherits it
through the scope chain — replacing today's manual per-project `.agentsrc.json` source-add.

**Rationale:** removes the repetitive per-project source wiring and makes adoption (D-D)
self-describing. **Grounded by D-B:** the resolver already loads the user-local layer
(`LayerUserLocal`, `resolver.go:133-144`) for *every* resolution regardless of the project, so a
project never needs the user/home source re-declared in its own `.agentsrc.json` — it inherits
it through the user-local layer in the scope chain. **Reconcile with** the default
`Sources: [{type: local}]` injection (`internal/config/agentsrc.go:782`): because the
user-local layer is loaded regardless, that injection is **redundant for the user scope** and
should be reconsidered (no-op / removed for the user scope). Captured as OI-1 under §5; the
plan must define precedence and whether the injection survives at all.

### D-D — `da init --from <home-source>` semantics

The seed is a home-repo URL / source ref. `init --from`:
1. clones / resolves the home source — which **contains** the user-scope manifest that
   itself declares the user's sources;
2. then materializes machine-local bindings locally (project registry rebuild, platform
   re-detect).

**Rationale:** the bootstrap chicken-and-egg ("where do sources come from before I have a
home?") dissolves, because the sources live *inside* the resolved home. This mirrors the
existing `local` source pattern, where the `local` source **is** the git-backed `~/.agents`
repo itself (`internal/config/local_source.go`, ref form `local:<rel>@<commit>`).

**Decomposed (per D-G):** standing up a new machine re-runs the **existing** machinery on B —
it does not build new projection. The steps: (1) re-hydrate the machine-local split (binding
table + caches) and the synced identity registry; (2) source-resolve any team/org sources,
which for **private git** needs the resolved auth threading (NEW-FORK-B); (3) re-run the
**existing** multi-harness projection — `RunSharedTargetProjectionExact` + the global-link
`CreateLinks` — so B materializes the same harness surfaces as A (layout governed by
`docs/PLATFORM_DIRS_DOCS.md`); (4) re-bind machine-local project paths by joining the synced
identity registry to the local binding table (FORK-1). Projection is reused, not redesigned.

### D-E — Platform/agents table on adoption = re-detect fresh

On adoption (and on every `refresh`), the detected platform values (`enabled`, `version`) are
**re-probed on this machine**; any synced detection values are discarded.

**Rationale:** these are machine facts, not preferences. `refresh` already re-probes installed
platforms today (the `IsInstalled` re-probe path in `commands/internal/lifecycle/refresh.go`),
so this is consistent with existing behavior — it just becomes the *only* source of those
values rather than an override of synced ones.

### D-F — Phased rollout; phase-0 seam exists and ships first

There is a bounded immediate fix that unblocks device-to-device **before** the full
`init --from` UX:

- **Phase 0 (MVP seam) — three changes that MUST ship together:** (i) ship the **portable
  project identity registry** (synced surface: id + portable key, no path); (ii) stop syncing
  the machine-local **binding table** *and* the `cache/` dirs (fix the `gitignore` /
  classification divergence in defects 2 and 5); (iii) move the portable preferences into the
  user-local layer (D-B) — **specifically `Defaults`, `Features`, and declared user sources /
  layering policy ONLY**. **(i) is a hard precondition for (ii):** stopping the path sync
  *without* the identity registry regresses machine B from "knows the project names" to "No
  managed projects" (defect 3 / BLOCKER 1). After phase 0 done right, a synced home carries no
  absolute paths and no caches, machine B still sees the project list, and `refresh` binds each
  project's identity to a local path (or reports it unbound) instead of all-skipping on a
  foreign machine's paths.
- **Phase 0 explicitly EXCLUDES the `Agents.<id>` manage-intent split.** That split needs a
  schema addition and a default-enable behavior change (R11 / NEW-FORK-A), which is **not**
  required for the device-to-device fix: in phase 0 the agents table stays machine-local and
  re-detected fresh (D-E), exactly as today. So **NEW-FORK-A does not block phase 0** — it is a
  phase-1+ concern, and phase 0 ships without it.
- **Phase 1+:** the full `init --from` bootstrap (D-D) including credential bootstrap
  (NEW-FORK-B), the global home-source default (D-C), and the agents manage-intent split
  (R11 / NEW-FORK-A).

**Rationale:** the divergence fix is high-value, but it is only *safe* with the identity
registry shipped alongside it — hence the three changes are one atomic phase-0, not a
"gitignore it now, registry later" sequence.

### D-G — Portability builds on the EXISTING multi-harness projection; the projection itself is not a portability blocker

**Corrected framing (grounded against git).** An earlier draft of this decision tried to treat
realization as a binary ("user/project free, team/org projected") and then re-derived a
per-asset × per-harness projection matrix inline. Both were wrong turns. Two grounded facts
settle it:

1. **The multi-harness projection is the existing, working machinery.** The resource model was
   **born multi-harness** — introduced across all five platforms at once (commit `2c0733db`) —
   and the later refactor **relocated** per-platform skill wiring into the shared
   `SharedTargetIntents` fan-out (`createSkillsLinks` became per-platform delegations feeding the
   shared path). Each platform implements `SharedTargetIntents` today
   (`internal/platform/{claude,codex,cursor,copilot,opencode}.go`) and the fan-out projects
   across all five. So this spec **reuses** that projection — there is no multi-harness
   resource-planning work to build or re-do here.

2. **The authoritative per-harness × per-asset × scope matrix already exists as a maintained
   doc.** `docs/PLATFORM_DIRS_DOCS.md` is the source of truth for where each of the five
   harnesses (Claude Code, Cursor, Codex, OpenCode, GitHub Copilot) reads rules / skills /
   subagents / MCP / hooks at project and user scope, re-verified against vendor docs. **This
   spec REFERENCES that matrix; it does not duplicate or re-derive it.**

**What this means for portability.** Cross-machine portability **builds on** the existing,
working projection — it does not redesign it. Standing up machine B is just: reconstruct the
machine-local split (binding table + caches), re-hydrate the identity registry, then **re-run
the existing projection** (`refresh`) so the same multi-harness materialization happens on B as
on A. Re-running the projection **is** a required reconstruction step (R10/DC7 make it a done
criterion) — but projection is **not a portability *redesign* blocker**: it is reused as-is. The
portability-specific *new* work is narrow: the **machine-local split**, the **identity
registry**, **`init --from`**, and **git-auth** (NEW-FORK-B).

**Native-path projection is explicitly NOT a portability concern.** Where a harness already
reads the compat path (e.g. `.agents/skills/`), additional native-path projection is redundant —
but that redundancy is a *projection-engine / extensibility* question, not a portability one. It
is **descoped from this spec** and tracked in the separate `multi-harness-extensibility` spec
(see §8 Relationships).

**One retargeting that does belong here (R10).** The redesign's source/lock machinery and the
projector are distinct seams, and getting that wrong would misroute `init --from`'s reconstruct
step. `EnsureResolved` is **LOCK-ONLY** — it owns the lock half of §7A.5 and **projects
nothing** (`internal/config/ensure_resolved.go:1-19`: "This seam owns the LOCK half … the
outputs/projection (sync) half is the caller's concern"). The **projector** is the refresh/output
path — `RunSharedTargetProjectionExact` (`internal/platform/resource_plan.go:589`) and the
global-link `CreateLinks`. R10 targets the projector (the thing `init --from` must re-run on
machine B), not `EnsureResolved`.

**Rationale:** naming the projection a redesign target over-scoped the spec and contradicted git
history. Anchoring to the existing fan-out + `PLATFORM_DIRS_DOCS.md` keeps this spec to its real
job (portability), and the lone retarget (R10 → projector, not `EnsureResolved`) is what
`init --from` actually depends on.

---

## 4. Behavioral Requirements

Behavioral, not implementation. Each must be verifiable.

### Cross-machine adoption

- **R1.** `da init --from <home-source>` resolves the user scope from the given source: it
  obtains the home (clone/pull/resolve), loads the user-local layer (D-B), and from it the
  declared sources (D-C), without the operator hand-adding any source. Where a declared source
  is a **private git source**, `init --from` must thread **explicit auth** into the git fetcher,
  **ambient-first** — ssh-agent → on-disk key-file for SSH, credential-helper/token for HTTPS,
  with a defined host-key policy (NEW-FORK-B, RESOLVED). Today the git fetcher passes **no auth**
  (`gitCloneShallow`, `internal/config/fetcher.go:306`) while HTTP/OCI do (`fetcher_http.go:103`,
  `fetcher_oci.go:406`), so go-git's implicit ssh-agent default silently covers `ssh://git@`
  URLs but fails HTTPS and agent-less setups. Credentials are **machine-local only and never
  enter the synced tree** (R7); R1 must not silently fail a private git source and must not leak
  secrets to satisfy it.
- **R2.** After `init --from`, machine-local bindings are materialized locally: the project
  registry is (re)built for *this* machine and the platform table is re-detected fresh (D-E).
  No machine-local value from machine A survives into machine B's effective state.
- **R3.** `init --from` has a defined behavior when `~/.agents` already exists on the target
  machine (see FORK-2 — refined). Default: **refuse a non-empty existing `~/.agents` with a
  clear message; allow an empty dir** (treat as fresh). Adopting a remote home *into* a
  populated existing home requires an explicit `--adopt`/`--merge` — which neither today's
  refuse-unless-`--force` nor `--force`-clobber (`init.go:262`) covers — and is on the near
  roadmap, not indefinitely deferred.

### Identity & refresh

- **R4.** `da refresh` binds a project by resolving its entry in the **synced portable identity
  registry** (stable id + portable key, no path) to a machine-local path **via the machine-local
  binding table**, not by reading a synced absolute path. On a machine where the binding is
  unknown but the identity is present in the registry, refresh must either resolve it (per the
  hybrid FORK-1 mechanism — repo_id, else logical id) or report the project as *known but
  unbound on this machine* — it must **not** silently skip-as-missing the way it does today
  (the directory-not-found skip in `commands/internal/lifecycle/refresh.go`), and it must
  **not** report "No managed projects" merely because the binding table is empty (defect 3).
- **R4a.** The portable project identity registry is a **synced surface** carrying every managed
  project's stable id + portable key with **no path**. It is distinct from the machine-local
  binding table. The registry is what makes machine B aware of *which* projects to rebind.
- **R5.** The portable-identity → local-path binding (the binding table) is a machine-local
  fact. Establishing or repairing a binding on machine B must not mutate any synced surface.
- **R12.** `repo_id` may be used as the portable key (FORK-1 hybrid) **only** once it is
  trustworthy. Since `DeriveRepoIDFromGit` reads only the first `origin` URL with no
  ambiguity detection (`internal/config/agentsrc.go:47`; `internal/gitremote/gitremote.go`), the
  plan must add **either** ambiguous-remote detection **or** an explicit operator `repo_id`
  override before trusting it; the logical-id table is the fallback for ambiguous/non-git cases.

### Sync boundary

- **R6.** Portable configuration (D-A portable rows in the user-local layer, the portable
  project identity registry, declared sources, layering policy, the manage-intent) **is**
  carried by sync.
- **R7.** Machine-local state — the binding table (`id → absolute path`, `added`), detected
  platform `enabled`/`version`, **and** the materialized caches `~/.agents/cache/config`
  (`internal/config/fetcher.go:227-229`) and `~/.agents/cache/packages`
  (`internal/config/fetcher_oci.go:184-187`) — **never** appears in the synced git tree. This
  is the direct fix for defects 2 and 5: today `da sync init` gitignores only `local/`
  (`commands/sync/init.go:76-78`), so `config.json` *and* `cache/` both travel. Classification
  ("machine-local") and mechanism (the sync gitignore boundary) must agree, and `cache/` must
  join `local/` on the machine-local side of that boundary.

### Scope realization (D-G)

- **R10.** On machine B, `init --from`/`refresh` must re-run the **existing** multi-harness
  projection to materialize the same harness-visible surfaces B that A has — it does not
  redesign projection. The reconstruct step invokes the **projector**
  (`RunSharedTargetProjectionExact`, `internal/platform/resource_plan.go:589`, plus the
  global-link `CreateLinks`), **not** `EnsureResolved` (which is LOCK-ONLY and projects nothing,
  `internal/config/ensure_resolved.go:1-19`). The per-harness × per-asset × scope layout it
  realizes is governed by `docs/PLATFORM_DIRS_DOCS.md` (the authoritative matrix), not by this
  spec. Native-path redundancy is out of scope (D-G; tracked in `multi-harness-extensibility`).
- **R11.** Representing the `Agents.<id>` manage-intent split (D-A) requires a **schema change**:
  `config.Agent` carries only `Enabled`+`Version` today (`internal/config/config.go:29-31`),
  with no intent field; `refresh` auto-enables every installed platform (`refresh.go:37`) and an
  absent platform defaults to enabled (`config.go:125-126`). The plan must (a) add a portable
  *intent* field (honoring the AgentsRC additive-field lifecycle,
  `.agents/rules/dot-agents/schema-usage.md`), and (b) change the auto-enable default so that
  **intent gates candidacy and the probe gates realization** — a platform is managed only if
  intent declares it, and enabled only if `IsInstalled` probes true on this machine. The exact
  field shape and default-change mechanism is NEW-FORK-A.

### Phase-0 MVP

- **R8.** With only phase 0 applied (identity registry + stop-path/cache-sync + portable prefs,
  shipped together): syncing a home from machine A to machine B and running `da refresh` on B
  (a) shows **every managed project** from the synced identity registry — no regression to "No
  managed projects" (defect 3); (b) does **not** fail-all-projects on machine A's absolute
  paths; and (c) produces a synced tree with **no absolute project paths and no caches**.
  Project *binding* on B may still require an `add`/touch in phase 0 (hybrid FORK-1 resolution
  may be a phase-1 refinement) — phase-0's guarantee is "no regression, no corruption, no
  all-skip, project list intact," not yet fully transparent auto-rebind.

### Migration

- **R9.** Splitting the `config.json` struct (D-A) is a schema change. Because `config.Load`
  (`internal/config/config.go:40-66`) has **no migrator** today (unlike `.agentsrc.json`'s
  `da config migrate`), the plan must define a migration + backfill: existing absolute-path
  rows must be re-expressed as (portable identity, machine-local binding), and a version bump
  must gate the new shape. The migration must **not** make `config.Load` a writer — `Load` stays
  a pure read/decode (FORK-5); persistence happens in a mutating command. Exact migrator code is
  a plan detail; the *requirement* is that an existing single-machine user upgrades in place
  without losing their project table.

---

## 5. Open Questions — HARD FORKS (UNRESOLVED, owner ratification required)

These are the load-bearing decisions. They are to be resolved by the method in
`.agents/proposals/scientific-method-spine-domain-general.md` (hypothesis → cheapest
discriminating test → ratify), **not** unilaterally in the plan. A recommended default is
given for each where the owner has leaned; status is UNRESOLVED until ratified.

**Status after the GATE-1 + grounding revisions:** FORK-1 is **RESOLVED = hybrid**. FORK-2 is
**refined** (refuse non-empty / allow empty; `--adopt` near-roadmap). **NEW-FORK-B** (git
credential bootstrap) is **RESOLVED = explicit auth threading, ambient-first, creds never
synced** (one small host-key-policy sub-question remains a plan detail). Still open for owner
ruling: **FORK-3, FORK-4, FORK-5**, and **NEW-FORK-A** (agents manage-intent split mechanism +
default-enable change).

### FORK-1 (THE CRUX) — Portable project identity — **RESOLVED = hybrid**

What replaces absolute `Project.Path` as the **portable key** so a project re-binds on machine B?

- **(a) `repo_id` from git remote** — reuse `DeriveRepoIDFromGit`
  (`internal/config/agentsrc.go:47`). *Risk — and a correction to an earlier draft:*
  `DeriveRepoIDFromGit` reads **only the first `origin` URL** (`gitRemoteOriginURL` →
  `gitremote.CanonicalRepoID`, `internal/gitremote/gitremote.go`) and canonicalizes it; it
  returns `""` **only** on read error / empty remote, **not** on multi-remote ambiguity. There is
  **no ambiguity detection** today (and no test coverage for it). So under multi-remote / fork
  topologies — e.g. the real AGOrcha case where a project has both an `origin` (stale
  `NikashPrakash` fork) and an `org` (`AGOrcha`) remote — `repo_id` silently resolves to whatever
  `origin` happens to be, which may be the *wrong* identity rather than a safe blank. Non-git /
  monorepo projects have no usable repo_id.
- **(b) Per-machine logical-id table** — identity is a logical name; each machine keeps a
  machine-local `id → path` binding, populated at `init`/`add` time or **lazily on first `cd`
  into the project**.
- **(c) Hybrid** — use `repo_id` when explicit/unambiguous, fall back to a logical id otherwise.

**RESOLVED = (c) hybrid** (both GATE-1 legs confirmed). Rationale: option (b)'s "less friction"
claim rested on **lazy-bind-on-`cd`**, but **no shell-hook infrastructure exists in the repo** —
no `chpwd`, no `PROMPT_COMMAND`, no `direnv` integration — so first-touch resolution is not
actually available today and (b) cannot deliver its headline ergonomics. Meanwhile `repo_id`
**already exists**, auto-derived from the git origin (`DeriveRepoIDFromGit`,
`internal/config/agentsrc.go:47`; field `RepoID`, `agentsrc.go:186`). So the default is hybrid:
use `repo_id` as the portable key **where it is trustworthy**, and fall back to a logical-id
table for **non-git / monorepo / ambiguous-remote** projects (e.g. the AGOrcha origin-vs-org
case).

**Plan requirement (R12) — `repo_id` is not yet safe to trust blindly.** Because there is no
ambiguity detection today (the corrected point above), the plan must add **either** explicit
ambiguous-remote detection (multiple remotes / origin-vs-canonical mismatch → treat as
ambiguous) **or** require an explicit operator-set `repo_id` override, **before** `repo_id` is
trusted as the portable key. Until that detection (or override) exists, the **logical-id table
is the safe fallback** for any project whose single-`origin` derivation might be wrong. The
portable key lives in the identity registry (R4a); the local binding stays machine-local
(R5/R7).

**Deliverable note:** if the lazy-`cd` ergonomics are still wanted, **shell integration must be
an EXPLICIT deliverable** of this work — it does not exist and cannot be assumed. Absent that,
binding is populated at `init`/`add`/`refresh` time, and an unbound-but-known project is
reported per R4, not silently skipped.

### FORK-2 — `init --from` reconcile-with-existing-home

When `~/.agents` already exists on machine B, does `init --from` **merge**, **adopt-remote**
(remote wins, local archived), or **refuse**?

**Refined recommended default:** refuse a **non-empty** existing `~/.agents`; **allow an empty
dir** (treat as fresh); require an explicit `--adopt` / `--merge` for the populated case
(deferred mechanism). This is narrower than a blanket "refuse if exists" — an empty `~/.agents`
(e.g. created by a `git clone` placeholder) should not block the common path.

**Gap to flag — `--adopt` cannot stay deferred long.** Today `runInit` already refuses an
existing `~/.agents` unless `--force` (`commands/internal/lifecycle/init.go:262`), and
`--force` **reinitializes** (clobbers, backup-then-replace). So **neither** existing mode covers
"**adopt** this remote home *into* my existing populated home" — refuse blocks it, force
destroys it. That is precisely the headline use case of `init --from` (bring my home to a
machine that already has a home). Deferring `--adopt`/`--merge` indefinitely means the tool
misses its headline use case; it is acceptable to defer the *conflict-merge semantics*, but the
adopt path should be on the near roadmap, not the far one.

### FORK-3 — Where the machine-local registry physically lives

The already-gitignored `~/.agents/local/` (only `local/` is excluded today) vs. an XDG state
dir (`~/.local/state/dot-agents`, see the XDG path helper in `internal/platform/paths.go`).

**Recommended default: `~/.agents/local/`.** It is already gitignored and already the home for
machine-local provenance (`EnsureProvenanceGitignore`, `internal/config/local_source.go`),
so it satisfies R7 with the least new machinery. XDG is cleaner in principle but introduces a
second root to discover, back up, and reason about. Revisit if cross-tool XDG compliance
becomes a goal.

### FORK-4 — Relationship to §15 Q5 workspace lockfile

config-distribution-model §15 Q5 raises a "workspace" aggregate lockfile (owner-ruled
either-or, currently unimplemented). Should the machine-local registry **be** that workspace
aggregate — unifying the project-registry thread and the workspace-lock thread — or stay a
separate artifact?

**Recommended default: keep separate for now, design the registry so it *could* become the
workspace aggregate later.** Unifying prematurely couples this portability fix to an
unimplemented lockfile design; but the registry's shape should not preclude the merge. Mark as
a forward-compatibility constraint, not a phase-0 deliverable.

### FORK-5 — `config.json` schema migration mechanics

`config.Load` has no migrator (R9). The fork is *how* to introduce one: a `config migrate`
sibling to `da config migrate`, an in-`Load` lazy upgrade, or an explicit upgrade step in
`refresh`/`init`.

**Recommended default: keep `Load` PURE — decode + an `UpgradeNeeded` signal, persist on the
next mutating command.** `config.Load` is today a pure read/decode (`internal/config/config.go:40`
— `ReadFile` + `json.Unmarshal`, no writes); it must stay that way. So on load, detect the old
shape from the version field and surface an `UpgradeNeeded` flag (decode old rows into the new
in-memory shape, re-deriving identity per the ratified FORK-1 mechanism), but **write nothing**.
The backfill is persisted by the next command that already mutates `config.json` (`add`,
`refresh`, `init --from`), or by an explicit one-shot `config migrate`. Existing single-machine
users still upgrade transparently, without turning a read into a write.

**Explicitly rejected:** a **side-effecting `Load`** (lazy in-`Load` upgrade that writes back) —
it would convert a pure read into a writer, breaking every read-only caller's expectations and
risking writes from contexts that should never mutate the home (CI checks, dry-runs, concurrent
reads). The fork remains the *mechanism choice* among the non-side-effecting options.

### NEW-FORK-A — Agents manage-intent split mechanism (owner ruling)

D-A/R11 require splitting `Agents.<id>` into a **portable manage-intent** and a **machine-local
detected state**, which is not representable today (`config.Agent` = `Enabled`+`Version` only,
`config.go:29-31`; `refresh` auto-enables every installed platform, `refresh.go:37`; absent =
enabled, `config.go:125-126`). The fork is **how**:

- **(a) Add an explicit `manage`/`intent` field** to the portable user-local layer and treat
  the machine-local `enabled`/`version` purely as detection output; change the default so an
  absent intent means *not managed* (or *managed iff installed*).
- **(b) Keep one field but relocate it** — intent lives in the synced user-local layer,
  detection in the machine-local binding/registry, with a derived effective value.

**Owner ruling needed** on the default-enable behavior change (today's "absent ⇒ enabled" is
load-bearing for existing single-machine users; flipping it risks disabling platforms on
upgrade). Constraint either way: **intent gates candidacy, probe gates realization** (R11), and
any schema addition follows the AgentsRC additive-field lifecycle
(`.agents/rules/dot-agents/schema-usage.md`).

### NEW-FORK-B — Fresh-machine git credential bootstrap — **RESOLVED = explicit auth threading, ambient-first**

A synced home that declares a **private git source** cannot reliably be resolved on machine B
because `gitCloneShallow` threads **no** `Auth` into `go-git`'s `CloneOptions`
(`internal/config/fetcher.go:306`), unlike HTTP/OCI which carry `src.Auth`
(`fetcher_http.go:103`, `fetcher_oci.go:406`). Grounding clarified the partial-coverage trap:
go-git's *implicit* default already uses **ssh-agent** for `ssh://git@…` URLs **iff** an agent
is running and `known_hosts` is populated — but it **fails** for HTTPS private repos and for
agent-less key-file setups. So "do nothing and rely on go-git defaults" silently works for one
common case and silently fails for others.

**RESOLUTION (owner wants ambient SSH creds to "just work"):** thread **explicit `Auth`** into
the git fetcher, **ambient-first**:

- **SSH URLs:** ssh-agent first, then an on-disk key-file fallback (so agent-less setups work).
- **HTTPS URLs:** a token via the system git **credential-helper** (or an explicit machine-local
  token).
- **Host-key policy:** a defined `known_hosts` / host-key verification policy (default to the
  host's `known_hosts`; the precise strict-vs-TOFU posture is the lone open sub-question below).
- **Invariant:** credentials are **machine-local only and MUST NEVER enter the synced tree**
  (R7). No token, key, or credential-helper output is written to any synced surface.

**Open sub-question (small):** the exact host-key verification posture (strict `known_hosts` vs.
trust-on-first-use vs. configurable) — a detail to settle in the plan, not a blocking fork.

Until the threading lands, R1 must **fail** a private git source with a clear "credentials
required / not configured" message rather than silently skip or embed a secret.

### Other open items (not forks — confirm against the loader)

- **OI-1 — default `Sources: [{type: local}]` injection redundancy (D-C/D-B).** Per D-B the
  resolver already loads the user-local layer (`resolver.go:133-144`) for every resolution, so
  the per-project default-local source injection at `internal/config/agentsrc.go:782` is
  redundant for the user scope. Decide whether to make it a no-op or remove it for the user
  scope. Confirm no other consumer depends on that injected source before changing it.
- **OI-2 — per-harness runtime-merge / native-path redundancy is an extensibility concern, not
  a portability one.** Whether a harness runtime-merges its global + project surfaces (so some
  dot-agents projection is redundant where the harness reads the compat path natively) does not
  affect portability — `init --from`/`refresh` re-runs the *existing* projection regardless. The
  layout is governed by `docs/PLATFORM_DIRS_DOCS.md`; the redundancy question belongs to the
  `multi-harness-extensibility` spec (§8). Noted here only to mark it explicitly out of scope.

---

## 6. Done Criteria (verifiable)

- **DC1.** A user scope is stood up on a second machine via `da init --from <home-source>` with
  **zero manual per-project absolute-path entry**. (R1, R2)
- **DC2.** After adoption, machine B's `refresh` **sees every managed project** by reading the
  synced portable identity registry, and binds each by resolving its portable key (hybrid
  FORK-1: `repo_id`, else logical id) to a local path via the machine-local binding table — with
  no project skipped due to a foreign machine's absolute path, and **no "No managed projects"**
  caused by an empty binding table (defect 3 / BLOCKER 1). Known-but-unbound projects are
  reported as such, not silently skipped. (R4, R4a)
- **DC3.** Machine-local state — the binding table (`id → path`, `added`), detected platform
  values, **and** `~/.agents/cache/config` + `~/.agents/cache/packages` — **never** appears in
  the synced git tree, verifiable by `git ls-files` / the `.gitignore` of a synced home showing
  both `local/` and `cache/` excluded. (R7, defects 2 & 5)
- **DC4.** Portable config (preferences, declared sources, layering policy, platform-management
  intent, **project identity registry**) **does** sync and resolves through the user-local layer
  on machine B. (R6, R4a, D-A, D-B)
- **DC5.** Phase-0-only check: syncing A→B and running `refresh` on B produces no all-projects-skip
  failure and no absolute paths in the synced tree. (R8)
- **DC6.** An existing single-machine user upgrades in place (version bump + backfill) without
  losing their project registry. (R9)
- **DC7.** After `init --from`/`refresh` on machine B, the **same** multi-harness surfaces are
  materialized on B as on A by re-running the **existing** projector
  (`RunSharedTargetProjectionExact`, resource_plan.go:589, + `CreateLinks`) — verifiable by
  comparing B's realized harness paths against A's per `docs/PLATFORM_DIRS_DOCS.md`. No
  projection redesign is required, and the reconstruct step is driven by the projector, not the
  LOCK-ONLY `EnsureResolved` (`ensure_resolved.go:1-19`). (R10, D-G) *(Supersedes the prior
  DC7, which over-scoped projection as a redesign target and mis-asserted `EnsureResolved`
  projects.)*
- **DC8.** The `Agents.<id>` manage-intent is representable and portable: an intent field exists
  in the synced user-local layer, and machine B's `refresh` realizes a platform iff intent
  declares it **and** the probe finds it installed (intent gates candidacy, probe gates
  realization). (R11, NEW-FORK-A)
- **DC9.** A declared **private git source** in a synced home resolves on machine B via explicit
  ambient-first auth threading (ssh-agent → key-file / credential-helper; NEW-FORK-B, RESOLVED)
  with **no credential present in the synced tree**; absent credentials, `init --from` fails it
  with a clear message rather than silently skipping. (R1, NEW-FORK-B)

---

## 7. Deferred (explicitly out of scope)

- Multi-user / shared-team home adoption and the conflict semantics of a *shared* home source.
- The **conflict-merge semantics** of `init --from --merge` (field-level reconciliation of two
  populated homes). Note the `--adopt` path itself is **near-roadmap, not deferred** (FORK-2):
  refuse-non-empty + force-clobber leave a gap for "adopt remote into existing home," the tool's
  headline use case.
- XDG-compliant relocation of the machine-local registry if FORK-3 lands on `~/.agents/local/`.
- Folding the machine-local registry into the §15 Q5 workspace lockfile (FORK-4) — kept as a
  forward-compat constraint, not built.
- Any change to the config-v2 scope chain itself; this spec consumes it unchanged.

---

## 8. Relationships

- **config-distribution-model §15** (`.agents/workflow/specs/config-distribution-model/`) —
  supplies the one model this spec graduates into: sources/scopes/units/lock, the
  `source:path@version` ref form, the scope chain (product → user-local → org → team →
  repo-imported → repo-local → project-local-overlay → runtime), the `EnsureResolved` seam
  (`internal/config/ensure_resolved.go`), content-hash staleness (never clock), D9's
  `.agentsrc.local.json` gitignored machine-local overlay (the proven portable/machine-local
  file-pair pattern), and D7's `EnsureProvenanceGitignore`
  (`internal/config/local_source.go`) keeping materialized content out of the synced tree.
  The `local` source — the git-backed `~/.agents` repo itself, ref `local:<rel>@<commit>` —
  is the bootstrap substrate for D-D. **Framing (D-G):** the source/lock half is owned by the
  LOCK-ONLY `EnsureResolved` (`ensure_resolved.go:1-19`); the *projection* half is the existing
  multi-harness projector (`RunSharedTargetProjectionExact` / `CreateLinks`). Portability
  **reuses** that projection — it does not redesign it — so this spec's job on the projection
  side is only "re-run it on machine B."
- **config-v2-coherence proposal** (`.agents/proposals/config-v2-coherence-scopes-sources-lock.md`
  + spec `.agents/workflow/specs/config-v2-coherence/`) — the source of the "`config.json` is
  machine-local, never a scope" classification that defect 2 shows the code violating. This
  spec operationalizes that classification.
- **`docs/PLATFORM_DIRS_DOCS.md`** — the authoritative, vendor-verified per-harness × per-asset
  × scope matrix (Claude Code, Cursor, Codex, OpenCode, GitHub Copilot). This spec **references**
  it as the source of truth for where each harness reads rules/skills/agents/MCP/hooks; D-G/R10
  do not re-derive it.
- **multi-harness-extensibility** (`.agents/workflow/specs/multi-harness-extensibility/design.md`)
  — the sibling spec that owns the *projection-engine* questions **out of scope here**:
  native-path projection redundancy, skill-frontmatter translation, and data-driven harness
  descriptors / pluggability. This spec deliberately descopes those and cross-refs them;
  portability builds on the existing
  projection rather than extending it.
- **da-project-specifics-source** (`.agents/workflow/specs/da-project-specifics-source/`) —
  the per-project source mechanism whose manual per-project source-add D-C replaces with a
  single inherited home-source default.
- **§15 Q5 workspace lockfile** — see FORK-4; possible future host for the machine-local
  registry.
- **unified-config-profiles** — selector-merge config-profiles design (NOT yet a repo
  artifact; design notes only). Relevant because the user-scope layering policy (D-A) is where
  profile attachment would eventually hang; tracked as a relationship, not a dependency.
- **Method:** `.agents/proposals/scientific-method-spine-domain-general.md` — the
  hypothesis → cheapest-discriminating-test → ratify discipline by which the open forks
  (FORK-3/4/5 + NEW-FORK-A/B; FORK-1 resolved=hybrid, FORK-2 refined) are to be resolved before
  this spec leaves DRAFT.
