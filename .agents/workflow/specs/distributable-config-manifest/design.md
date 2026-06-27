# Distributable Config Manifest — Design Spec

- **id:** distributable-config-manifest
- **status:** DRAFT (owner-confirmed gap; open forks in §6 require ratification before leaving DRAFT)
- **author:** Nikash Prakash
- **created:** 2026-06-26
- **owns:** the *what & why* of a single publishable, scope-attachable artifact that bundles the
  canonical sources, the layering policy, and (optionally) the project-set a teammate or a new
  machine can `init --from`. File paths, struct shapes, command wiring, and task ordering belong
  to a later plan, not here.

---

## 1. Problem Statement

Three primitives already exist for *resolving* and *carrying* dot-agents configuration, but
**no single referenceable artifact bundles "everything a person/team needs to reproduce this
setup" into one thing you can publish and `init --from`.** A new teammate or a new machine
today must assemble the setup out of three separate pieces, by hand, in the right order:

1. **The sources to pull** are declared per-scope on the config-v2 substrate
   (config-distribution-model §15 D1/D2: `source:path@version` refs, the `local`/`git`/`http`/`oci`
   source types). But *which* sources constitute "our team's setup" is not itself a nameable,
   distributable unit — it is scattered across whatever manifests a person happens to have
   authored.

2. **The layering policy** — which profiles bind, with what precedence, locks, and permission
   states — is owned by unified-config-profiles (the scope-attached `layering_policy` unit and the
   `kind: profile` fragments, resolved by the two-phase selector-merge engine). That spec defines
   *how* fragments resolve in a context; it does **not** define a distributable bundle that says
   "here is the policy set a member should adopt."

3. **The project-set** — *which repos* are managed, by portable identity — is the
   home-config-portability "portable project identity registry" (its GATE-1 BLOCKER-1 fix: a
   synced surface carrying each project's stable id + portable key, **no path**, distinct from the
   machine-local binding table). That registry is designed as a home-config-internal surface for
   the **single-owner** device-to-device case; there is no general, team-distributable form of it.

**The gap (owner-confirmed).** A team that wants reproducible onboarding has to say, in prose:
"add these sources, adopt this layering policy, and manage these repos." There is no **one
publishable artifact** that captures all three so a member can run a single `init --from
<one-ref>` and converge on the same effective config + the same bound project-set. The
home-config `init --from <home-source>` (D-D) is exactly this entrypoint **for one personal home**;
but its "home source" is an un-named, single-owner repo, not a first-class, scope-attachable,
org/team-publishable bundle on the §15 substrate. The three pieces exist; the **bundle** does not.

### Why now

home-config-portability's `init --from` already needs *something* to resolve — it currently
points at a bare home-repo URL and then hand-reconstructs the pieces. Naming that "something" as a
first-class config-v2 unit (a) gives `init --from` a typed, lockable, scope-attachable seed instead
of a bespoke repo convention, and (b) generalizes home-config's single-owner story into the
team-reproducibility primitive the owner actually wants: **an org/team publishes its manifest;
members init from it and get a reproducible setup.** The single-owner home becomes one (personal)
case of the general manifest, not a separate bespoke surface.

---

## 2. Goals

- Define **one** distributable artifact — the **config manifest** — that bundles, into a single
  referenceable thing, the canonical **sources** to pull, the **layering policy** that binds, and
  (optionally) the **project-set** to manage.
- The manifest is a **config-v2 unit** on the §15 substrate (a new `kind: manifest`): its identity
  is the existing absolute ref `<source>:<name>` (§15 D2), it is recorded in `.agentsrc.lock` with
  its `digest`, and it is resolved/locked through `EnsureResolved` + `inputs_digest` (§15
  D3/D4/D10) — **not** a bespoke new file format with parallel sourcing/lock/cache machinery.
- A member runs **`da init --from <manifest-ref>`** and converges on a **reproducible setup**:
  the manifest's sources are pulled, its layering policy is applied, and its project-set is
  materialized into machine-local bindings — re-using home-config's identity-registry →
  machine-local-binding mechanism, not a second one.
- The manifest is **scope-attachable** (personal → team → org → published). A higher-authority
  scope can **constrain** a lower one (narrow-only, deny/value-locks, no force-allow, no
  self-declared authority — the unified-config-profiles policy rulings), so an org can publish a
  manifest that a team extends and a member inits from, without the member escaping the org's locks.
- **Unify, do not duplicate:** the home-config portable project identity registry **is** the
  manifest's project-set component; home-config's `init --from` consumes a manifest; the
  single-owner personal home is the personal-scope case of the general manifest.

### Non-goals (this spec)

- Re-defining the §15 substrate (sources/scope/units/lock/digest) — consumed unchanged.
- Re-defining the unified-config-profiles resolution semantics (the two-phase selector-merge
  engine, the eight ratified decisions) — consumed unchanged; the manifest **carries which policy
  binds**, it does not re-specify how policy resolves.
- Re-litigating home-config-portability's machine-local split, binding-table, or git-auth forks —
  consumed; the manifest is the *seed* those mechanisms reconstruct from.
- A public-source registry / trust / signing / discovery backend for published manifests (deferred,
  §7 — mirrors the public-source deferral in unified-config-profiles §8 and scoped-knowledge-graphs).
- Multi-user conflict-merge semantics for two populated homes (home-config FORK-2 territory).

---

## 3. Decisions (DECIDED — bake into the plan; rationale + the distinction from the three existing pieces)

These are owner-directed. Rationale traces each to intent and, critically, **distinguishes the
manifest from the primitive it sits next to** so the bundle is not a redundant fourth surface.

### D1 — The manifest is a config-v2 UNIT (`kind: manifest`), not a bespoke file format

The manifest is a resolvable unit on the §15 substrate (§15 D1/D2/D3): addressed by the absolute
ref `<source>:<name>`, recorded in `.agentsrc.lock` with its `digest`, resolved/locked through
`EnsureResolved` and covered by `inputs_digest` staleness (§15 D4/D10). It introduces a new
`kind` alongside §15's `layer` and `artifact` (and unified-config-profiles' `profile`).

**Rationale.** §15 D3 collapsed the tier wall precisely so that *every* resolvable thing is
sourced, versioned, and locked one way. Inventing a parallel "manifest file" with its own fetch,
cache, and staleness path would re-introduce exactly the half-old/half-new incoherence §15 killed.
Making the manifest a unit means a team's published manifest is pinned, lockable, and
reproducibility-checkable with the **same** machinery as everything else it references.

**Distinction from §15.** §15 is the *substrate* (how any unit is sourced/scoped/locked). The
manifest is *a unit on that substrate* whose payload happens to be "the set of sources + policy +
project-set to adopt." §15 owns transport; the manifest owns *which things to transport for a
reproducible setup*.

### D2 — The manifest is a distinct KIND from a profile: bundle/entrypoint vs resolution-fragment

A **profile** (unified-config-profiles) is a *resolution fragment*: a selector-scoped config
bundle that the two-phase engine merges to answer "given this context, what config applies and who
may change it." A **manifest** is a *distributable bundle and `init --from` entrypoint*: it names
**which sources to pull, which layering policy/profiles bind, and which project-set to manage** —
the inputs a setup is reproduced from, not a fragment that resolves within a context.

**Rationale.** Collapsing the two would overload a profile into also carrying sources and a
project-set, which is a category error: a profile answers a *per-context resolution* question; a
manifest answers a *per-person/per-team reproducibility* question. Keeping them distinct kinds
keeps each coherent — the manifest **references** the policy/profiles that bind (by ref), it does
not inline a new resolution mechanic. (Whether this distinction is firm is surfaced as a fork in
§6 F2, with the recommended default = distinct kind.)

**Distinction from unified-config-profiles.** Profiles = the RESOLUTION semantics. The manifest =
the DISTRIBUTABLE BUNDLE + entrypoint. The manifest's "layering policy" component is *a pointer to*
the unified-config-profiles `layering_policy` + `profile` units that should bind; it reuses that
spec's resolution wholesale and adds zero resolution semantics of its own.

### D3 — NO `extends`; composition is selector-merge across the scope chain

A manifest does not `extends`/`inherits`/`composes-by-ref-that-resolves-recursively` another
manifest. When an org manifest, a team manifest, and a member's personal manifest are all present
in the scope chain, they **selector-merge** under the unified-config-profiles two-phase engine
(Phase-1 effective layering policy, then Phase-2 fragment merge), exactly like profiles do — there
is no manifest→manifest inheritance edge.

**Rationale.** This is the same anti-dependency-hell / anti-supply-chain-injection decision
unified-config-profiles §2.1 made and the prototype proved (order-independent, no transitive trust
edge). Once manifests can come from public/foreign sources, an `extends` edge is an injection
vector. A flat scope-chain cascade has no such edge. Consistency with profiles is deliberate: the
manifest reuses the **same** composition law, it does not invent a second one.

### D4 — Scope-attachable with constrain-only authority (the unified-config-profiles policy rulings apply verbatim)

A manifest attaches to a scope (personal → team → org → published). A higher-authority scope may
only **narrow** what a lower scope sets (unified-config-profiles Decision 3): it may add deny-locks
and value-locks (Decision 4), tighten precedence, and cap permissions (Decision 2) — it may
**not** force-allow, and it may **not** self-declare its authority (Decision 1: authority is
derived from `ref.source → source-registry → scope`, never from the manifest's own contents).

**Rationale.** This is what makes "org publishes, team extends, member inits" *safe*: an org
manifest's locks bind every lower scope; a member's local additions are a lower-authority scope
that can add but never punch through an org deny. Re-using unified-config-profiles' ratified policy
rulings (rather than inventing manifest-specific authority) means there is **one** authority model
across profiles and manifests. A member cannot escape an org lock by authoring a personal manifest,
because authority keys off the *source the manifest came from*, not what the manifest claims.

### D5 — `da init --from <manifest-ref>` is the entrypoint; it reconstructs via the EXISTING mechanisms

`init --from <manifest-ref>` resolves the manifest unit (D1), then:
1. **pulls its sources** (§15 source resolution; private git sources thread ambient-first auth per
   home-config NEW-FORK-B — credentials machine-local, never in the manifest);
2. **applies its layering policy** (the referenced unified-config-profiles `layering_policy` +
   `profile` units resolve through the two-phase engine);
3. **materializes machine-local bindings for its project-set** by re-using home-config's portable
   identity-registry → machine-local-binding mechanism (D-D/R4/R4a) — the project-set component of
   the manifest **is** that identity registry.

**Rationale.** home-config already specified `init --from <home-source>` and the reconstruct steps
(re-hydrate the machine-local split, source-resolve, re-run the **existing** projector
`RunSharedTargetProjectionExact` + `CreateLinks`, re-bind project identities). This spec changes
**what the seed is** — a typed, lockable manifest ref instead of a bare home-repo URL — and reuses
home-config's reconstruct machinery unchanged. The seed for `init --from` is a single manifest
ref/URL.

### D6 — The home-config identity registry IS the manifest's project-set component (unify, don't duplicate)

The home-config portable project identity registry (stable id + portable key, no path) is **not**
a separate bespoke surface from the manifest's project-set: it is the *personal-scope instance* of
the manifest's project-set component. A team manifest distributing a shared repo list and a
personal home distributing its own project list are the **same** component at different scopes.

**Rationale.** home-config's registry was designed single-owner; the owner's stated end-state is
that the single-owner home is one case of the general manifest. Folding the registry in as the
manifest's project-set component (rather than standing up a parallel team-repo-list format) keeps
**one** portable-project-identity surface, resolved and bound by **one** mechanism (the hybrid
FORK-1 `repo_id`-else-logical-id key, R12). Whether the project-set is *inlined* in the manifest or
a *separately referenced* scope-attached unit is the live fork F1 (§6).

---

## 4. Behavioral Requirements

Behavioral, not implementation. Each must be verifiable.

### Manifest as a unit

- **R1.** A manifest is a §15 unit of `kind: manifest`, identified by `<source>:<name>`, recorded
  in `.agentsrc.lock` with its `digest`, and resolved/locked through `EnsureResolved` +
  `inputs_digest` — no parallel lock/source/cache machinery (D1).
- **R2.** A manifest's payload references (a) the canonical **sources** to pull, (b) the
  **layering policy** that binds (unified-config-profiles `layering_policy` + `profile` refs), and
  (c) optionally the **project-set** (the portable identity registry). It carries **references**,
  not re-defined copies, of the profiles/policy it binds (D2/D6).

### Reproducible setup via init --from

- **R3.** `da init --from <manifest-ref>` resolves the manifest, pulls its sources (threading
  ambient-first auth for private git sources, home-config NEW-FORK-B; credentials machine-local
  only, never in the manifest or any synced surface), applies its layering policy, and materializes
  machine-local bindings for its project-set — re-using home-config's reconstruct machinery, not a
  new path (D5).
- **R4.** Two machines that `init --from` the **same** manifest ref at the **same** lock digest
  converge on an **identical effective config** (the unified-config-profiles reproducibility digest
  over the resolved bundle + contributing refs + policy version, Decision 7) **and** an identical
  bound project-set (every managed project from the registry bound by its portable key, home-config
  R4/DC2).

### Scope-attach + policy

- **R5.** A manifest is scope-attachable (personal → team → org → published). When manifests at
  multiple scopes are present, they selector-merge across the scope chain (D3), governed by the
  Phase-1 effective layering policy; a higher-authority scope's locks/caps bind lower scopes
  (D4). There is no manifest→manifest `extends` edge.
- **R6.** A member's local layer composes with a pinned org/team manifest **without the member
  escaping the org/team policy locks**: the member's additions are a lower-authority scope; an
  org deny-lock or value-lock holds; force-allow and self-declared authority are validation errors
  (D4, unified-config-profiles Decisions 1/3/4).

### Project-set unification

- **R7.** The project-set component of a manifest is the home-config portable project identity
  registry (stable id + portable key, no path); binding it on a machine re-uses home-config's
  identity → machine-local-binding mechanism (hybrid FORK-1, R12) — no second project-set surface
  or second binding mechanism is introduced (D6).

### Versioning / readback

- **R8.** A team can **pin** a manifest for reproducibility by absolute ref + lock digest (§15
  D4); the pin is honored by `--locked`/`--frozen` exactly as for any §15 unit. (Whether the pin
  is **transitive** over the manifest's referenced units is fork F5, §6.)
- **R9.** `da config explain` is the readback truth surface for a resolved manifest: it shows the
  effective sources, the bound layering policy (with the unified-config-profiles contributing refs,
  locks-with-owning-scope, and permission map per that spec's R6), the resolved project-set, and
  the manifest digest; `--json` for machine consumption. (The manifest does not get a bespoke
  inspect surface — it rides §15 D12 / unified-config-profiles R6.)

### Validation is loud

- **R10.** Self-declared manifest authority, a force-allow attempt, and a manifest→manifest
  `extends`/inheritance field are **validation errors**, not silently-ignored inputs (D3/D4,
  mirroring unified-config-profiles R9).

---

## 5. The unification, stated plainly (how the manifest relates the three pieces)

| Existing piece | What it owns | What the manifest reuses from it | What the manifest adds |
|---|---|---|---|
| config-distribution-model §15 | sources / scope / units / lock / `EnsureResolved` / `inputs_digest` | the manifest **is** a unit on this substrate (`kind: manifest`); same sourcing, lock, staleness | a unit whose payload is "the reproducible-setup input set" |
| unified-config-profiles | the two-phase selector-merge **resolution**; the 8 policy rulings (no-extends, source-derived authority, narrow-only, deny/value-locks, no force-allow) | the manifest **references** the `layering_policy` + `profile` units that bind; composition + authority law applied verbatim | a *bundle/entrypoint* that says *which* policy binds — distinct kind from a resolution fragment |
| home-config-portability | `init --from`, the machine-local split, the portable project identity registry (BLOCKER-1) | `init --from` **consumes** a manifest; the identity registry **is** the manifest's project-set component; the reconstruct/projection machinery is reused | the general, scope-attachable, org/team-publishable form; single-owner home = the personal case |

The one-line distinction to hold onto: **§15 = how anything is transported; profiles = how config
resolves in a context; the manifest = the one publishable bundle of {sources + which policy binds +
project-set} that `init --from` reproduces a whole setup from.**

---

## 6. Open Questions — FORKS (surface + recommend; owner ratification required, NOT resolved here)

To be resolved by the method in `.agents/proposals/scientific-method-spine-domain-general.md`
(hypothesis → cheapest discriminating test → ratify), not unilaterally in the plan. A recommended
default is given for each; status is UNRESOLVED until ratified.

### F1 — Is the project-set IN the manifest, or a separately referenced scope-attached unit?

Does the manifest **inline** its project-set, or **reference** a separate scope-attached
project-set unit? The tension: a **team** wants to distribute the repo list (project-set inlined /
referenced from the team manifest); a **member** wants to curate their own machine's project-set
(personal, not dictated by the team).

**Recommended default:** the project-set is a **separately referenced** scope-attached unit, not
inlined. A team manifest *references* a shared project-set unit (so the team can distribute "manage
these repos"); a member layers their own personal project-set as a lower-authority scope that
**adds** to it (selector-merge, D3). The personal home's project-set (home-config's registry) is
the personal-scope instance of that same referenced unit. This keeps "team distributes a repo list"
and "member curates their own" composing under the **same** scope-merge law instead of being an
either/or. (If ratified inline, the team case still works but the member-curation case forces a
manifest fork per member — worse.)

### F2 — Manifest vs profile boundary: new kind, or a profile that also carries sources + project-set?

Is `manifest` a genuinely new `kind`, or is it a profile that additionally carries sources and a
project-set?

**Recommended default: a distinct kind.** A manifest = distributable bundle / `init --from`
entrypoint; a profile = per-context resolution fragment. Overloading a profile to also carry
sources + a project-set conflates a reproducibility-input artifact with a resolution fragment and
muddies unified-config-profiles' clean two-phase model. Distinct kinds keep each coherent; the
manifest *references* profiles rather than being one. (See D2.)

### F3 — Org-publishes / team-extends / member-inits: how local additions coexist with a pinned org manifest

When an org pins a manifest and a member adds local config, **how do the two coexist** without the
member escaping policy?

**Recommended default:** selector-merge across the scope chain + the policy locks (D4). The
member's local manifest/layer is a **lower-authority scope**: its additive sets union into the
effective bundle, but the org manifest's deny-locks and value-locks are absolute and bind it
(unified-config-profiles Decision 4/8). The member can add capability the org left open; the member
cannot re-grant what an org deny-lock forbids. This is the already-ratified profiles policy applied
to manifests — no new mechanism.

### F4 — Relationship to the existing `local` source and `da sync`: is the personal manifest just the `local` source's manifest?

The §15 `local` source **is** the git-backed `~/.agents` repo itself (§15 D6, ref
`local:<rel>@<commit>`). Is the **personal manifest** simply the manifest unit published by that
`local` source — i.e. the home-config "home source" named as a first-class manifest?

**Recommended default: yes.** The personal manifest is the `local` source's manifest; home-config's
`init --from <home-source>` becomes `init --from <the local source's manifest ref>`, and `da sync`
git-manages it like any other `local`-source content. This is the cleanest expression of "the
single-owner home is the personal case of the general manifest" (D6) and avoids a second personal
config surface. Confirm against the §15 `local`-source bootstrap (D6) and the home-config D-B
"portable fields live in the already-loaded user-local layer" so the manifest and the user-local
layer do not become two competing personal surfaces.

### F5 — Versioning / pinning: does a pinned manifest pin its referenced units transitively?

The §15 lock already pins units individually. When a team pins a manifest for reproducibility, does
the manifest pin **transitively** (the manifest ref + every referenced source/profile/project-set
ref at a recorded digest), or only itself (the referenced units re-resolve per their own scope)?

**Recommended default: transitive pin via `inputs_digest` over the referenced ref-set.** A manifest
records the set of refs it binds; the manifest's `inputs_digest` (§15 D4) covers that ref-set, so a
member who inits from a pinned manifest digest gets the **same** referenced unit versions — the
reproducibility guarantee (R4) requires this. Non-transitive pinning would let a referenced profile
drift under a "pinned" manifest, breaking "same manifest digest ⇒ identical setup." Reconcile the
exact transitivity boundary with §15 Q5 (the workspace-aggregate lockfile, owner-ruled either-or)
and home-config FORK-4 (whether the machine-local registry becomes that aggregate).

### F6 — Secrets / auth for authed sources referenced by a manifest

A manifest that references a **private** source (private git, authed http/oci) ties directly to
home-config NEW-FORK-B (RESOLVED = explicit ambient-first auth threading; credentials machine-local,
never synced).

**Recommended default (inherit home-config NEW-FORK-B verbatim):** the manifest **names** the
authed source but carries **no credential**; on `init --from`, auth is threaded ambient-first
(ssh-agent → key-file for SSH; credential-helper/token for HTTPS), credentials are machine-local
only and **never** enter the manifest or any synced surface, and a missing credential **fails**
the source with a clear message rather than silently skipping or embedding a secret. The only open
sub-question is the same host-key-verification posture home-config left as a plan detail. This is a
hard invariant, not a fork to relitigate: **a manifest is publishable precisely because it contains
no secrets.**

---

## 7. Done Criteria (verifiable)

- **DC1.** A manifest resolves as a §15 `kind: manifest` unit: `<source>:<name>` identity, a
  `.agentsrc.lock` `units` entry with a `digest`, resolved through `EnsureResolved` /
  `inputs_digest`. *Verify:* a flat project that declares a manifest ref shows it locked as a unit;
  no parallel lock section exists for manifests. (R1)
- **DC2.** **Two-machine reproducibility (central).** Two machines run `da init --from
  <same-manifest-ref>` at the same lock digest and converge on a **byte-identical effective config**
  (same unified-config-profiles reproducibility digest) **and** an identical bound project-set
  (every registry project bound by its portable key). *Verify:* resolve + bind on two independent
  clones; assert effective-config digest equality and identical project→path bindings (the
  home-config DC2 join + the profiles H7 equality). (R3, R4, R7)
- **DC3.** **Member cannot escape policy locks.** A team/org manifest with a deny-lock + a member's
  local layer that attempts to re-grant the denied capability resolves with the lock **holding**;
  a force-allow attempt and a self-declared-authority manifest are validation errors. *Verify:* the
  unified-config-profiles H8(a)/H8(b) mutation-sensitive proofs, expressed at the manifest scope
  chain. (R5, R6, R10)
- **DC4.** **No `extends` edge / order-independent.** No manifest→manifest inheritance field exists;
  multi-scope manifest resolution is deterministic under input reordering. *Verify:* the type
  carries no inheritance field; a shuffle test holds the effective digest constant (mirrors
  unified-config-profiles done-criterion 3). (D3, R10)
- **DC5.** **Single-owner home = personal case.** home-config's `init --from <home-source>` is
  expressible as `init --from <a personal-scope manifest ref>`, and the home-config portable
  identity registry is consumed as the manifest's project-set component — **one** registry surface,
  **one** binding mechanism. *Verify:* the personal manifest path and the home path produce the same
  bound project-set with no second registry format. (D5, D6, R7)
- **DC6.** **`config explain` readback.** `da config explain` shows, for a resolved manifest: the
  effective sources, the bound layering policy (contributing refs, locks-with-owning-scope,
  permission map per unified-config-profiles R6), the resolved project-set, and the manifest digest;
  `--json` works. No bespoke manifest inspect command is added. (R9)
- **DC7.** **No secrets in the manifest.** A manifest referencing a private git source resolves on a
  second machine via ambient-first auth with **no credential present** in the manifest or any synced
  surface; absent credentials, `init --from` fails that source with a clear message. *Verify:*
  `git ls-files` / manifest content shows no credential; private-source resolution succeeds with an
  agent/helper present and fails cleanly without. (R3, F6/NEW-FORK-B) 

---

## 8. Deferred (explicitly out of scope)

- **Public-source / supply-chain hardening for *published* manifests** — trust, signing, discovery,
  and caching for a `published`/`public` manifest source. This spec is *designed to be safe to
  extend there* (no-`extends`, no force-allow, source-derived authority — all inherited from
  unified-config-profiles), but the public backend and trust model are a separate spec (mirrors
  unified-config-profiles §8 and scoped-knowledge-graphs deferring their `public` backend).
- **Multi-user conflict-merge semantics** — reconciling two populated homes / a shared team home is
  home-config FORK-2 / its deferred `--adopt --merge`; not re-opened here.
- **Workspace-aggregate lockfile coupling** — whether a pinned manifest *is* the §15 Q5 workspace
  aggregate (home-config FORK-4) is a forward-compatibility constraint, not a deliverable here (F5).
- **A manifest authoring/publishing UX** (`da manifest publish`, templating, validation tooling)
  beyond the resolve + `init --from` + `config explain` surfaces this spec requires — a plan/CLI
  matter once the model is ratified.
- **Re-litigating the §15 substrate, the unified-config-profiles resolution engine, or the
  home-config machine-local split** — all consumed unchanged.

---

## 9. Relationships

- **config-distribution-model §15** (`.agents/workflow/specs/config-distribution-model/design.md`)
  — *substrate.* The manifest is a §15 unit of a new `kind: manifest`, addressed by
  `<source>:<name>` (§15 D2), locked in `.agentsrc.lock` (§15 D3 `units`, R1), resolved/locked via
  `EnsureResolved` + `inputs_digest` (§15 D4/D10), read back via `config explain` (§15 D12). The
  manifest **does not** re-define source/scope/kind/lock/digest — §15 owns them. Pin/transitivity
  reconciles with §15 Q5 (workspace lockfile).
- **unified-config-profiles** (`.agents/workflow/specs/unified-config-profiles/design.md`) —
  *resolution + policy law reused; kind distinguished.* The manifest **references** the
  `layering_policy` + `kind: profile` units that bind and applies that spec's composition (no
  `extends`, selector-merge, §2.1) and authority rulings (Decisions 1–4, 8) **verbatim**. A
  manifest is a **distinct kind** from a profile: a profile is a *per-context resolution fragment*;
  a manifest is a *distributable bundle / `init --from` entrypoint* that names which profiles/policy
  bind (D2, F2). The manifest adds **zero** resolution semantics; it reuses the engine.
- **home-config-portability** (`.agents/workflow/specs/home-config-portability/design.md`) —
  *consumer + the project-set component folds in.* home-config's `init --from <home-source>` (D-D)
  consumes a manifest; the manifest is the typed seed it reconstructs from (D5). home-config's
  portable project identity registry (BLOCKER-1 / R4a) **is** the manifest's project-set component
  (D6, R7) — unified, not duplicated. The single-owner personal home is the personal-scope case of
  the general manifest. The git-auth invariant (NEW-FORK-B) is inherited verbatim (F6).
- **Method:** `.agents/proposals/scientific-method-spine-domain-general.md` — the hypothesis →
  cheapest-discriminating-test → ratify discipline by which the §6 forks (F1–F5; F6 inherited-resolved)
  are to be resolved before this spec leaves DRAFT.
