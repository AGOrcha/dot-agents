# config-distribution-model §15 — Coherence Amendments (DELTAS against the shipped substrate)

- **status:** proposed / **DRAFT** — for owner ratification, then a cross-brain **GATE-1** design audit.
- **type:** project-local proposal (a delta the owner folds into the shipped `config-distribution-model` §15 spec; per `proposal-routing` this targets a repo spec, so it is authored as a proposal, not an in-place spec edit).
- **author:** Nikash Prakash
- **created:** 2026-06-27
- **branch:** `spec/config-15-coherence-amendments` (off `spec/config-architecture-consolidation`, which carries the five family specs + the coherence map).
- **method:** `.agents/proposals/scientific-method-spine-domain-general.md` — ground in the actual §15 text + the downstream refinement, resolve the briefing-decidable parts, **surface** the hard forks with recommended defaults (do NOT unilaterally resolve them).
- **grounding map:** `.agents/proposals/config-harness-architecture-coherence.md` (the cross-brain-validated coherence audit; these three amendments are its Risk 3, Risk 5/2, and Risk 4 made actionable).

---

## Why these three — one shared pattern

All three amendments are the **same defect class**: **§15 is the SHIPPED substrate (`config-distribution-model` §15, canonical, on master 0.4.0–0.4.1) and the downstream family specs refined a shared definition without amending its owner.** §15 D1 (scope), D13 (registry), and the `units` model (R1) each predate the refinement that `unified-config-profiles`, `home-config-portability`, `distributable-config-manifest`, and `multi-harness-extensibility` have since converged on. The substrate still reads as if that refinement never happened, so each downstream spec either contradicts the substrate or carries an unresolved "must reconcile with §15" open question.

The coherence audit's headline finding: the two real CONTRADICTIONs and the OVERLAP all trace to **two shared definitions lacking a single canonical owner** — the **scope chain** (Amendment 1) and the **identity registry** (Amendment 2) — plus a **GAP** where a fourth resolvable thing (the descriptor) never entered the `units` model at all (Amendment 3). Fixing the owner in the substrate collapses the downstream contradictions together.

These are **deltas, not rewrites.** Each amendment cites the exact §15 text it touches, states the downstream conflict, and proposes the minimal substrate change. The **hard forks are surfaced with a recommended default each and explicitly left for owner ratification** — this proposal does not resolve them unilaterally.

---

## Amendment 1 (KEYSTONE) — split AUTHORITY-RANK from VALUE-PRECEDENCE in §15's scope model

### Current §15 text

§15 **D1** (`config-distribution-model/design.md:865–869`) defines `scope` as a **single** axis whose meaning is **value precedence**:

> **Scope** (precedence; *merges* into effective policy): product → user-local → org → team → repo-imported → repo-local (committed) → **project-local overlay (uncommitted)** → runtime. Answers "who gets the last word."

So in the substrate, "scope" = "more-local gets the last word on the value" — repo-local out-precedes org.

### The conflict (the validated diagnosis — NOT "opposite semantics")

`unified-config-profiles` **Decision 1** (`unified-config-profiles/design.md:191–217`) derives a profile's **authority** from `ref.source → source-registry → scope`, and its two-phase resolver runs a **policy-authority pass** (`design.md:151–171`, §2.3 Phase 1) in which a **higher**-authority scope **binds** lower ones: "its precedence wins, its locks are absolute, its `override_permissions` cap what lower scopes may set." That is **org binds repo** — the inverse direction from §15 D1's "repo-local gets the last word."

The coherence audit (Risk 3) verified that this is **not** two specs asserting opposite semantics of one thing. It is **two orthogonal axes conflated under one name** ("scope chain"):

- **AUTHORITY-RANK** — for **policy / locks / permissions**: **higher** scope (`org > team > user > repo`) **BINDS** lower (it sets denies, value-locks, and override-caps that lower scopes cannot escape). This is the Phase-1 axis.
- **VALUE-PRECEDENCE** — for **ordinary config VALUES**: **more-local** (`repo` / project-local overlay) **WINS** — but **only within the field set the authority pass leaves open**. This is the Phase-2 axis.

They **coexist without contradiction**: org sets the locks (authority-rank); repo-local still wins ordinary values (value-precedence) inside the allowed set. §15 D1 collapsed both into one ordering, which is why `unified` **Q1** (`design.md:448–454`) has to demand "**one** canonical scope ordering… and its authority ranks" — Q1 cannot be answered while the substrate conflates the two axes.

A second strand of the same conflation: authority must be a property of **where a unit came from**, not what it declares. `unified` Decision 1 routes authority through a **source-authority registry** (org *delegates* publishing-authority to a team source via a registry-level grant; `design.md:203–207`), and its **Q6** (`design.md:478–481`) leaves that grant shape open and asks for it to be "reconciled with the §15 source-registry schema." §15 today has no such registry concept attached to its scope/source model.

### Proposed amendment

Amend §15 D1 so the substrate names **two distinct orderings**, not one:

1. **AUTHORITY-RANK** — the policy/lock/permission ordering, **higher-binds-lower** (`org > team > user > repo`, with `product`/`runtime`/`public`/overlay placed explicitly). Governs Phase-1: which scope's locks, value-locks, and override-caps are absolute over which.
2. **VALUE-PRECEDENCE** — the value-merge ordering, **more-local-wins** (the current D1 chain), governing Phase-2 last-writer/union/subtract — **constrained by** the authority pass: a value-precedence win is only honored for a field the authority-rank pass left unlocked and within-cap.

And add the **SOURCE-AUTHORITY REGISTRY** to the substrate: a source's authority-rank derives from its scope **via a registry of delegation grants** (a registry-level grant that source *S* may carry scope-*O* authority), **never self-declared** by the unit. This is the §15-side home for `unified` Decision 1 + Q6.

§15 D1 remains the **single owner** of both orderings; downstream specs reference it rather than redefining either. This is the substrate change that lets `unified` Q1 be pinned and Q6 reconciled.

### Forks to surface (recommended default each — owner ratifies)

- **F1.1 — schema representation of the two orderings.** *Recommended default:* represent them as **two explicitly-named fields** on the layering-policy / scope model (`authority_rank` and `value_precedence`), each an ordered scope list, rather than one `scope_chain` field reused for both. Rationale: one field cannot carry two directions; naming them separately is what makes Q1 answerable and prevents the conflation from re-forming downstream. *Alternative to note:* a single ordered chain with a per-scope `binds: higher|lower` tag — terser but re-invites the conflation the amendment exists to remove.
- **F1.2 — source-authority-registry grant shape (`unified` Q6).** *Recommended default:* a registry-level, **per-source grant list** of the form `source <S> may carry authority of scope <O>` (an allowlist keyed by source id, edited only by the operator who controls the registry), reconciled into the §15 source-registry schema as a new optional `authority_grants` block. *Alternative to note:* a per-scope "blessed sources" list (keyed by scope instead of source) — equivalent expressiveness, but source-keyed matches Decision 1's "authority is a property of *where a unit came from*" framing better.
- **F1.3 — default authority-rank ordering.** *Recommended default:* `org > team > user > repo` for the binding direction, with `product` as the floor (lowest authority, ships defaults), `runtime` as a non-authority value-only override (highest value-precedence, **zero** authority — it can set values but never locks), `public` as **read-only / lowest authority** (can never bind), and the **project-local overlay** as value-only (highest value-precedence, zero authority, like runtime). This is the reconciliation `unified` Q1 demands across the three divergent chains (§15 D1, the prototype, scoped-KG). *Owner must confirm* the exact placement of `product`/`public`/overlay relative to authority.
- **F1.4 — replace vs augment the existing D1 text.** *Recommended default:* **augment** — keep D1's existing chain as the canonical **VALUE-PRECEDENCE** ordering (it is correct *for that axis* and already shipped/referenced), and **add** the AUTHORITY-RANK ordering + source-authority-registry as new sibling definitions under D1 (or a new D1a). Rationale: the shipped chain is not wrong, it is *incomplete* — it only ever described one of the two axes. A full replace risks invalidating downstream references to the value chain. *Alternative to note:* a clean rewrite of D1 into "D1 defines two orderings" if the owner prefers the substrate to read as one coherent statement rather than an original + amendment.

---

## Amendment 2 — split the project registry (amend §15 D13)

### Current §15 text

§15 **D13** (`config-distribution-model/design.md:977–981`) classifies the managed-project registry **wholesale** as machine-local operational state, never a scope:

> Only the fetch `cache/` and the managed-project registry are genuinely non-scoped local operational state — never a scope, never a source, never projected.

So in the substrate the *entire* project registry is machine-local and explicitly **not** a scope or a syncable surface.

### The conflict

`home-config-portability` **splits** the registry into two surfaces (`home-config-portability/design.md:128–149`, field-disposition table + the BLOCKER-1 narrative; **R4a** `design.md:346–348`):

- **portable project IDENTITY** — `id` + a portable key, **no path** — which **must travel in a synced surface** so machine B knows which projects to rebind. This is config that rides the user-local layer — i.e. **a scope**.
- **machine-local BINDING** — `id → absolute-path` (plus `added` bookkeeping) — machine-specific, **never synced**, **never a scope**.

And `distributable-config-manifest` **D6** (`distributable-config-manifest/design.md:184–196`) claims that *same* portable identity registry as **its** project-set component ("the *personal-scope instance* of the manifest's project-set"). So the portable-identity surface now has **two would-be owners** (the coherence audit's Risk 2 / OVERLAP), and §15 D13 — which predates the identity/binding split entirely — still declares the whole thing machine-local, contradicting both (Risk 5 / partial CONTRADICTION).

### Proposed amendment

Amend §15 D13 so the substrate reflects the split:

- **(a) portable project IDENTITY** — `id` + portable key, **no path** — is **synced** config that **MAY be a scope / a manifest component** (it rides the user-local layer like any other portable config; it is *not* non-scoped operational state).
- **(b) machine-local BINDING** — `id → absolute-path`, plus `added` — **stays** exactly as D13 says today: machine-local, **never synced, never a scope, never projected.** Caches stay machine-local too (unchanged). Credentials-always-machine-local is already consistent across specs — leave it.

D13's "never a scope" clause narrows from "the registry" to "the **binding table** (id→path) + `added` + caches."

**One owner for the identity surface.** Amendment 2 must also **reconcile `home-config` R4a vs manifest D6** — they currently both claim portable identity. Pick **one** owner and have the other reference it by ref (the same "one owner, everyone else references" discipline §15 already applies to the lock). *This proposal recommends a default below but does not resolve it unilaterally.*

**Distinct from Amendment 1's registry.** The **identity registry** (this amendment: which projects exist + their portable keys) is a **different registry** from the **source-authority registry** (Amendment 1: which source may carry which scope's authority). Two registries, two purposes — the substrate must name them distinctly and **not** conflate them.

### Forks to surface (recommended default each — owner ratifies)

- **F2.1 — which surface owns portable identity.** *Recommended default:* the **manifest's project-set component is the single owner** (manifest D6's framing), and `home-config` R4a's registry is the **personal-scope instance** of that one component — `home-config` updates to **cite** manifest D6 rather than standing up a parallel format. Rationale: the owner's stated end-state is that the single-owner home is one case of the general manifest; one portable-project-identity surface resolved by one mechanism (the FORK-1 `repo_id`-else-logical-id key) is the single-source win. *Alternatives to note:* (i) `home-config` standalone owns it and the manifest references it (inverts the dependency — worse, because the team-distribution case then has to reach into a personal spec); (ii) a **new §15 unit kind** (`kind: project-set` or `kind: identity-registry`) owns it as a first-class substrate unit and both home-config and manifest reference *that* — cleanest single-source, but adds a substrate unit kind and is the heaviest change.
- **F2.2 — how (a) is scope/manifest-attachable.** *Recommended default:* the portable identity surface is a **scope-attached unit that layers under the same selector-merge law** as everything else — a team manifest references a shared project-set unit, a member layers their own personal project-set as a lower-authority scope that **adds** to it (matching manifest F1's recommended "separately referenced unit" default). Rationale: keeps "team distributes a repo list" and "member curates their own" composing under one merge law instead of either/or. *Alternative to note:* the identity surface is **inlined** in the manifest/user-local layer (simpler, but forces a manifest fork per member for the curation case — manifest F1 already flags this as worse).

---

## Amendment 3 — descriptor-as-unit (resolve a cross-cutting drift class)

### Current §15 text

§15 makes **resolvable things uniform**: every resolvable unit is tracked in one `units` model carrying `kind` + `digest` + lock freshness (`config-distribution-model/design.md:850` "every resolvable thing… addressed uniformly and tracked uniformly in one lock"; D1 `:865` the three axes incl. `kind`; D3 `:884` "one `units` model"; R1 `:1011` the lock `units` map keyed `source:path@version → {kind, digest, fetched_at, last_checked_at}`).

### The conflict

`multi-harness-extensibility` (`multi-harness-extensibility/design.md:430–435`, §8 Relationships) says descriptors "are data that **flows through the same source/scope distribution machinery**… scope-attachable **in principle**" — **but does not make them units.** A descriptor today declares **no `kind`**, has **no lock entry**, and is **not** a member of `inputs_digest` (the coherence audit's Risk 4 / GAP). The spec also says (`design.md:433–435`) "a future external source **could ship descriptors**." So a descriptor is a resolvable, scope-attachable, potentially-source-shipped thing that sits **outside** the one model §15 promises is uniform — a standing drift channel: if a descriptor is ever shipped via a source, it rides an **un-locked, un-digested** distribution path. The extensibility re-gate (its F4 hand-add probe, DC0) flagged this as a **blocker the spec must take a position on before its descriptor schema can ratify.**

### Proposed amendment

Decide descriptor provenance **in the substrate**, so the extensibility spec can adopt it rather than carry an open question into schema ratification.

**Recommended default:** the **declarative descriptor data IS a first-class §15 unit** — `kind: descriptor` — sourced, versioned, locked, and digested like any layer/artifact/manifest. This is the part of a harness that is genuinely "data about a harness" (read-paths per asset×scope, transports/forms, hook event-name maps, allowlist roots, proven frontmatter dialect — `multi-harness` D2 part 1, `design.md:161–172`). As a unit it can be distributed and **pinned** without drift, riding the lock/digest path instead of an un-locked side channel.

The **irreducible Go renderer / procedural core is NOT a unit** — it is **code, shipped with the binary** (`multi-harness` D2 part 2, `design.md:174–184`: `CreateLinks`/`RemoveLinks`, source-priority selection, user-home fanout, stale-file pruning, semantic hook rendering, the optional-interface surface). D2 already draws this hard line ("descriptor owns the declarative projection; an explicit, named, audited Go core owns the rest"); Amendment 3 just maps the **declarative half** onto the §15 `units` model and leaves the **procedural half** as binary code.

**Alternative to note (owner may prefer):** descriptors stay **Go-internal / non-distributable** — never a §15 unit. Then external descriptor shipping (`multi-harness` §8 "a future external source could ship descriptors," and the deferred §7 external-packaging) is **permanently out of scope**, and the substrate is honest that descriptors are build-time/in-binary data only. This is coherent and lower-risk, at the cost of foreclosing harness distribution via sources.

Either way, the **extensibility spec then ADOPTS this decision** (it currently must take a position before its schema ratifies — this amendment supplies the substrate position it adopts).

### Forks to surface (recommended default each — owner ratifies)

- **F3.1 — descriptor-as-unit: yes / no / hybrid.** *Recommended default:* **hybrid — declarative-only.** The **declarative descriptor data** is a unit (`kind: descriptor`); the **procedural Go core** is not (it is binary code). Rationale: this is the only option that both closes the drift GAP (declarative data rides the lock) **and** honors `multi-harness` D2's audit-honest "Go core remains by design" line — a "yes, everything" would pretend the procedural core is data, which D2 explicitly refutes. *Alternatives:* **no** (descriptors stay Go-internal, external shipping out of scope — lower risk, forecloses distribution); **yes-fully** (rejected — re-opens the descriptor-grows-an-imperative-escape-hatch failure D2 was written to prevent).
- **F3.2 — the `kind: descriptor` unit definition (if yes/hybrid).** *Recommended default:* a new §15 `kind: descriptor` alongside `layer` / `artifact` / `manifest`, defined as **declarative, non-merging, non-installing** projection data: it neither merges into effective config (unlike `layer`) nor installs an executable (unlike `artifact`) — it is **consumed by the projector** to drive per-harness output. It carries a media type distinct from the config-layer and artifact-bundle types (mirroring §15 D15's media-type guard), so a descriptor blob is never mis-resolved as a layer or artifact. *Owner must confirm* the kind's exact resolution behavior (it is a fourth behavior beyond layer-merge / artifact-install) and whether it participates in `inputs_digest` when authored locally vs only in `units` when sourced.

---

## Downstream effects

Which spec each amendment unblocks, and the merge-order tie-in:

- **Amendment 1 (keystone)** unblocks `unified-config-profiles` **Q1** (scope-chain reconciliation — the cross-cutting blocker every other layer waits on) and **Q6** (source-authority-registry grant shape). It is the substrate change that lets the policy-authority pass (Phase 1) and value-merge (Phase 2) be specified without contradiction. **Merge-order tie-in:** Amendment 1 **must land together with unified-config-profiles L1** — L1 is the policy engine that consumes the two orderings, and L1 cannot pin Q1 while the substrate still conflates them. (Per the coherence map's critical path: `L1 (resolve Q1) → {L3-phase0 registry, agree Risk 2 ownership} → L2 → L3-phase1`.) This is the critical-path fix that unblocks the config-architecture merge order.
- **Amendment 2** unblocks `home-config-portability` (R4a identity/binding split) and `distributable-config-manifest` (D6 project-set ownership) — it resolves Risk 2's two-owner OVERLAP and Risk 5's D13 CONTRADICTION before **either** home-config phase-0 **or** the manifest lands. It is a precondition on the `L3-phase0 ↔ L2` boundary in the merge order (both must agree the single owner before they co-land).
- **Amendment 3** unblocks `multi-harness-extensibility` **schema ratification** — its F4 hand-add probe / DC0 gate requires a position on descriptor provenance before the descriptor schema can ratify. L4 is otherwise orthogonal to portability and rides its own probe gate, but it cannot ratify its schema until this substrate decision exists for it to adopt.

**Net:** Amendments 1 and 2 fix the two ownership questions (scope chain + identity registry) that the coherence audit found collapse Risks 2/3/5/7 together; Amendment 3 closes the one remaining GAP (the descriptor outside the units model). All three are substrate-owner decisions that the downstream specs **reference** rather than re-deciding — the "one owner, everyone else references by ref" discipline §15 already applies to the lock, extended to the scope chain, the identity registry, and the descriptor.

---

*This is a proposed delta for owner ratification + a cross-brain GATE-1 design audit. The hard forks are surfaced with recommended defaults, not resolved. §15 and the other family specs were read, not edited.*
