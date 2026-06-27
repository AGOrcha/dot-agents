# config-distribution-model §15 — Coherence Amendments (DELTAS against the shipped substrate)

- **status:** proposed / **DRAFT** — **rev 2** (revised per the cross-brain GATE-1 design audit, which ruled rev 1 NOT-READY-but-salvageable; re-gate-ready). Awaiting owner ratification + GATE-1 re-audit.
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

The substrate must also state **how the two orderings compose** — two rules imported from `unified` so the substrate is independently coherent (not "authority constrains precedence" hand-waving):

- **Cross-authority deny rule (import of `unified` Decision 4 + Decision 8, `design.md:258–275` / `:323–336`).** A **LOWER**-authority scope's `deny` **CANNOT erase a HIGHER**-authority scope's `allow`. Within one authority level, final-deny-wins; **across** authority levels a deny only binds **upward** when it is a **deny-lock** owned by the higher scope. There is **no force-allow** — a lower scope can never punch a capability *through* a higher deny (the public-source / supply-chain injection guard). Cross-authority subtraction is legitimate **only** via a higher-scope deny-lock.
- **Locked-field collision behavior.** If a more-local scope wins value-precedence on a field a higher scope holds **value-locked**, the local write is **REJECTED / IGNORED — the lock wins** — and the rejection is **provenance-visible**. Concretely: repo-local sets `model=Y`, org holds `value-lock model=X` ⇒ effective `model=X`, and `da config explain` reports "repo-local `model=Y` rejected by org value-lock `model=X`" (the attempted local value, the winning locked value, and the lock's owning scope). Value-precedence applies **only** to fields the authority pass leaves unlocked and within-cap; a locked field never reaches Phase-2 last-writer. (This is the concrete behavior, not a vague "authority constrains precedence.")

§15 D1 remains the **single owner** of both orderings; downstream specs reference it rather than redefining either. This is the substrate change that lets `unified` Q1 be pinned and Q6 reconciled.

### Forks to surface (recommended default each — owner ratifies)

- **F1.1 — schema representation of the two orderings.** *Recommended default:* represent them as **two explicitly-named fields** on the layering-policy / scope model (`authority_rank` and `value_precedence`), each an ordered scope list, rather than one `scope_chain` field reused for both. Rationale: one field cannot carry two directions; naming them separately is what makes Q1 answerable and prevents the conflation from re-forming downstream. *Alternative to note:* a single ordered chain with a per-scope `binds: higher|lower` tag — terser but re-invites the conflation the amendment exists to remove.
- **F1.2 — source-authority-registry grant shape AND write-authority (`unified` Q6).** *Recommended default:* a registry-level, **per-source grant list** of the form `source <S> may carry authority of scope <O>` (an allowlist keyed by source id), reconciled into the §15 source-registry schema as a new optional `authority_grants` block. **The grant shape alone is unsafe without a write-authority gate, so the amendment defines both:**
  - **(a) WHO may write the registry.** Only a **HIGHER**-authority scope may grant authority to a source. An org-scope registry may bless a team source to carry org authority; a team source may not bless itself or a peer to carry org authority. Write-authority for an `authority_grants` entry is itself governed by the AUTHORITY-RANK ordering above.
  - **(b) NO self-blessing (explicit invariant).** A lower repo/user layer **CANNOT** grant authority to itself or to a source it controls — a scope can never self-elevate. This closes the **public-source injection vector**: a foreign/public source that ships its own `authority_grants` claiming org authority is **inert**, because the grant is only honored when written by a strictly-higher scope's registry. Self-blessing is a schema/resolve-time **rejection**, not a silent no-op.

  *Alternative to note:* a per-scope "blessed sources" list (keyed by scope instead of source) — equivalent expressiveness, but source-keyed matches Decision 1's "authority is a property of *where a unit came from*" framing better. Either shape **must** carry the (a)+(b) write-authority gate.
- **F1.3 — default authority-rank ordering.** *Recommended default:* `org > team > user > repo` for the binding direction, with `product` as the floor (lowest authority, ships defaults), `runtime` as a non-authority value-only override (highest value-precedence, **zero** authority — it can set values but never locks), `public` as **read-only / lowest authority** (can never bind), and the **project-local overlay** as value-only (highest value-precedence, zero authority, like runtime). This is the reconciliation `unified` Q1 demands across the three divergent chains (§15 D1, the prototype, scoped-KG). *Owner must confirm* the exact placement of `product`/`public`/overlay relative to authority.
- **F1.4 — replace vs augment the existing D1 text.** *Recommended default:* a **CLEAN D1 rewrite** (introduced as **D1a**) that names the two orderings explicitly, **not** a silent augmentation of the existing chain. Rationale (audit-corrected): §15 D1's own text says scope "*merges* into effective **policy**" (`design.md:867`), not just into ordinary values — so the existing chain already *implies* the authority axis it never names. Bolting AUTHORITY-RANK onto that chain in-place would **change shipped semantics invisibly** (a reader of the old D1 assumed one ordering governed both policy and values). A clean D1a that states "there are two orderings" makes the change legible instead of hidden. **Compatibility notes the rewrite must carry:** (i) the existing value-precedence chain is **preserved verbatim** as the VALUE-PRECEDENCE ordering — no shipped value-merge behavior changes; (ii) downstream references to "the D1 scope chain" resolve to the VALUE-PRECEDENCE ordering by default (back-compat); (iii) the AUTHORITY-RANK ordering + source-authority-registry are **net-new** substrate concepts, so nothing that exists today silently re-binds. *Alternative to note:* a pure augment (keep D1, append D1a) — lower edit surface, but risks leaving the old "merges into policy" wording reading as if one chain still governs both, which is the invisible-semantics-change the rewrite exists to prevent.

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

**One owner for the identity surface.** Amendment 2 must also **reconcile `home-config` R4a vs manifest D6** — they currently both claim portable identity. The fix is a **single neutral owner both reference**, not picking one spec to own the other's surface (the recommended default below makes the owner a first-class §15 `kind: project-set` unit so neither home-config nor manifest depends on the other — preserving standalone, manifest-free portability). Same "one owner, everyone else references" discipline §15 already applies to the lock. *This proposal recommends a default below but does not resolve it unilaterally.*

**Distinct from Amendment 1's registry.** The **identity registry** (this amendment: which projects exist + their portable keys) is a **different registry** from the **source-authority registry** (Amendment 1: which source may carry which scope's authority). Two registries, two purposes — the substrate must name them distinctly and **not** conflate them.

### Forks to surface (recommended default each — owner ratifies)

- **F2.1 — which surface owns portable identity.** *Recommended default (audit-corrected):* portable identity is its **OWN first-class §15 unit** — `kind: project-set` (a.k.a. identity-registry) — that **BOTH** `home-config` **AND** the manifest **REFERENCE**; **neither owns the other's surface.** Rationale: defaulting to "manifest owns identity" **inverts the dependency** — `home-config` (portability) must work **WITHOUT a manifest** at all: a single-machine user with no team still needs the identity registry; phase-0 ships the identity registry **before** any manifest path exists; and `init --from`'s seed in home-config's own text is a **home-repo / source ref, not a manifest** (manifest is the *manifest spec's* later re-targeting of that seed). Making the manifest the owner would force standalone home-config to depend on a layer above it. A neutral §15 `kind: project-set` unit lets **home-config function standalone** (it references the unit at personal scope) while the **manifest consumes the same unit when present** (a team manifest references it at team scope) — same component, two scopes, one merge law, no inversion. *Alternatives to note:* (i) the manifest's project-set is the single owner (the prior default — **rejected**: inverts the dependency, breaks standalone home-config); (ii) `home-config`'s registry is the single owner and the manifest references it (also inverts — the team-distribution case then reaches *down* into a personal spec). The neutral-unit default is the only option where neither portability nor distribution depends on the other.
- **F2.2 — how (a) is scope/manifest-attachable.** *Recommended default:* the portable identity surface is a **scope-attached unit that layers under the same selector-merge law** as everything else — a team manifest references a shared project-set unit, a member layers their own personal project-set as a lower-authority scope that **adds** to it (matching manifest F1's recommended "separately referenced unit" default). Rationale: keeps "team distributes a repo list" and "member curates their own" composing under one merge law instead of either/or. *Alternative to note:* the identity surface is **inlined** in the manifest/user-local layer (simpler, but forces a manifest fork per member for the curation case — manifest F1 already flags this as worse).

---

## Amendment 3 — descriptor-as-unit (resolve a cross-cutting drift class)

### Current §15 text

§15 makes **resolvable things uniform**: every resolvable unit is tracked in one `units` model carrying `kind` + `digest` + lock freshness (`config-distribution-model/design.md:850` "every resolvable thing… addressed uniformly and tracked uniformly in one lock"; D1 `:865` the three axes incl. `kind`; D3 `:884` "one `units` model"; R1 `:1011` the lock `units` map keyed `source:path@version → {kind, digest, fetched_at, last_checked_at}`).

### The conflict

`multi-harness-extensibility` (`multi-harness-extensibility/design.md:430–435`, §8 Relationships) says descriptors "are data that **flows through the same source/scope distribution machinery**… scope-attachable **in principle**" — **but does not make them units.** A descriptor today declares **no `kind`**, has **no lock entry**, and is **not** a member of `inputs_digest` (the coherence audit's Risk 4 / GAP). The spec also says (`design.md:433–435`) "a future external source **could ship descriptors**." So a descriptor is a resolvable, scope-attachable, potentially-source-shipped thing that sits **outside** the one model §15 promises is uniform — a standing drift channel: if a descriptor is ever shipped via a source, it rides an **un-locked, un-digested** distribution path. The extensibility re-gate (its F4 hand-add probe, DC0) flagged this as a **blocker the spec must take a position on before its descriptor schema can ratify.**

### Proposed amendment

Decide descriptor provenance **in the substrate**, so the extensibility spec can adopt it rather than carry an open question into schema ratification.

A `kind: descriptor` would be a **FOURTH resolver behavior** — §15's `kind` today is exactly two: mergeable `layer` **OR** installable `artifact` (D3, `design.md:884–897`). A descriptor is **neither** (non-merging, non-installing projection data), so adding it introduces a new resolution behavior with **TBD digest / lock / validation semantics**. That is too large a substrate change to default into before the behavior is proven.

**Recommended default (CONDITIONAL):** descriptors stay **INTERNAL / probe artifacts** — Go-internal declarative data, **not** a §15 unit — **until** the `multi-harness` **F4 hand-add-one-harness probe completes** (the mandatory DC0 experiment that ratifies/refutes the descriptor schema). **ONLY IF** a descriptor later becomes **source-shipped** (`multi-harness` §8 "a future external source could ship descriptors") must it become a **full §15 unit** with **all** of: a defined **media type** (distinct from config-layer/artifact-bundle, mirroring §15 D15's media-type guard), a defined **resolver order** (where the fourth behavior sits relative to layer-merge and artifact-install), **validation** rules, a **lock entry**, and defined **local `inputs_digest`** behavior. Until that source-shipping need is real, none of that substrate surface is added.

The **irreducible Go renderer / procedural core is NOT a unit** in **any** case — it is **code, shipped with the binary** (`multi-harness` D2 part 2, `design.md:174–184`: `CreateLinks`/`RemoveLinks`, source-priority selection, user-home fanout, stale-file pruning, semantic hook rendering, the optional-interface surface). D2 draws this hard line ("descriptor owns the declarative projection; an explicit, named, audited Go core owns the rest").

**Alternative to note (owner may prefer):** make the declarative descriptor a first-class `kind: descriptor` unit **now** (sourced/locked/digested), accepting the fourth-behavior substrate cost up front so external shipping is never an un-locked side channel. Coherent, but commits the substrate to a resolver behavior the F4 probe has not yet validated — the conditional default defers exactly that risk.

**Crucially, Amendment 3 alone does NOT unblock `multi-harness` schema ratification.** The **F4 hand-add probe still gates** the descriptor schema regardless of which descriptor-provenance default the owner picks; this amendment only supplies the *substrate position* (unit-or-not, and under what condition) that the extensibility spec adopts — it does not substitute for the probe.

### Forks to surface (recommended default each — owner ratifies)

- **F3.1 — descriptor-as-unit: CONDITIONAL (internal-until-probe, unit-only-if-source-shipped).** *Recommended default:* descriptors stay **internal / probe artifacts** through the F4 probe; a descriptor becomes a **full §15 unit only if/when it is source-shipped**. Rationale: `kind: descriptor` is a **fourth resolver behavior** with TBD lock/digest/validation semantics — committing the substrate to it before the F4 probe validates the schema is premature; the un-locked-side-channel drift only becomes real **at** the moment of source-shipping, so the unit obligation attaches exactly there. The **procedural Go core is never a unit** (binary code) in any case. *Alternatives:* **first-class unit now** (accept the fourth-behavior cost up front — pre-empts a future side channel but commits before the probe); **never a unit** (descriptors stay permanently Go-internal — only viable if external descriptor shipping is ruled permanently out of scope).
- **F3.2 — the `kind: descriptor` unit definition (ONLY IF source-shipped per F3.1).** *Recommended default (applies only when the condition fires):* a new §15 `kind: descriptor` alongside `layer` / `artifact` / `manifest`, defined as **declarative, non-merging, non-installing** projection data (a **fourth behavior** beyond layer-merge / artifact-install) — **consumed by the projector** to drive per-harness output. It must define, before it ships: a **media type** distinct from config-layer/artifact-bundle (mirroring §15 D15's media-type guard, so a descriptor blob is never mis-resolved), its **resolver order**, **validation**, its **lock entry** shape, and its **local `inputs_digest`** participation (in `inputs_digest` when authored locally vs only in `units` when sourced). *Owner must confirm* this whole fourth-behavior surface as part of ratifying the source-shipping condition — it is deliberately **not** specified until that condition is real.

---

## Downstream effects

Which spec each amendment unblocks, and the merge-order tie-in:

- **Amendment 1 (keystone)** unblocks `unified-config-profiles` **Q1** (scope-chain reconciliation — the cross-cutting blocker every other layer waits on) and **Q6** (source-authority-registry grant shape). It is the substrate change that lets the policy-authority pass (Phase 1) and value-merge (Phase 2) be specified without contradiction. **Merge-order tie-in (audit-corrected):** Amendment 1 **lands in §15 FIRST**, as a self-contained substrate change, **before** unified-profiles L1. §15 is the shipped substrate that L1 **CONSUMES** — L1 does not redefine the scope model, it pins its Q1/Q6 **by reference** to §15's two named orderings + source-authority-registry. **Do NOT co-land Amendment 1 with L1:** co-landing would hide whether the substrate rule (the two orderings, the cross-authority deny rule, the no-self-blessing invariant) is **independently coherent** on its own — the whole point of putting it in the substrate is that it stands alone and everything above references it. Sequence: `§15 Amendment 1 lands → L1 pins Q1/Q6 by reference → {L3-phase0, L2, L3-phase1}`.
- **Amendment 2** unblocks `home-config-portability` (R4a identity/binding split) and `distributable-config-manifest` (D6 project-set ownership) — it resolves Risk 2's two-owner OVERLAP and Risk 5's D13 CONTRADICTION. With the neutral-unit default (F2.1), the §15 `kind: project-set` unit lands in the substrate and **both** home-config phase-0 and the manifest reference it — home-config no longer waits on the manifest, so standalone portability is unblocked independently.
- **Amendment 3** does **NOT** unblock `multi-harness-extensibility` schema ratification — the **F4 hand-add probe / DC0 gate still gates** that, independently of this amendment. Amendment 3 only supplies the *substrate position* (descriptors stay internal until the probe; full §15 unit only if source-shipped) that the extensibility spec **adopts** once its probe runs. It removes the open "is a descriptor a §15 unit?" question from blocking the spec's *position*, not the probe that blocks its *schema*.

**Net:** Amendments 1 and 2 fix the two ownership questions (scope chain + identity registry) that the coherence audit found collapse Risks 2/3/5/7 together; Amendment 3 closes the one remaining GAP (the descriptor outside the units model) conditionally. All three are substrate-owner decisions that land in §15 **first** and that the downstream specs **reference** rather than re-deciding — the "one owner, everyone else references by ref" discipline §15 already applies to the lock, extended to the scope chain, the identity registry, and the descriptor.

---

## NOT covered by these three amendments

These remain **separate open items** that these amendments deliberately do **not** resolve — flagging them so the owner does not read the three as closing more than they do:

- **Agents manage-intent / default-enable split** — `home-config` **NEW-FORK-A** (the portable "intent to manage platform X" field vs machine-local detected state, plus changing `refresh`'s auto-enable-every-installed-platform default). A schema + behavior change in its own right; untouched here.
- **Manifest transitive pinning** — `distributable-config-manifest` **F5** (how/whether a manifest transitively pins the versions of the units it references). Open in the manifest spec; not a §15 substrate question these amendments answer.
- **§15 Q5 workspace lockfile** — the multi-project workspace-level lockfile (ruled either-or, unimplemented; touches manifest F5 + home-config FORK-4). A separate §15-adjacent open question, independent of the scope/registry/descriptor amendments here.

---

*This is a proposed delta for owner ratification + a cross-brain GATE-1 design audit. The hard forks are surfaced with recommended defaults, not resolved. §15 and the other family specs were read, not edited.*
