# Config / Harness Architecture — Coherence Map (grounding pass)

- **status:** grounding artifact — VERIFIED against actual spec text; surfaces risks for the
  cross-brain audit + owner. Does NOT resolve risks unilaterally and does NOT edit source specs.
- **author:** Nikash Prakash
- **created:** 2026-06-27
- **branch:** `spec/config-architecture-consolidation` (the five family specs gathered into one tree)
- **method:** `.agents/proposals/scientific-method-spine-domain-general.md` (hypothesis → cheapest
  discriminating test → ratify; cross-brain GATE-1 design audit / GATE-2 results audit) — the
  cross-cutting *method*, not a layer.

The five specs read together (all paths relative to `.agents/workflow/specs/`):

| Spec | File | Status (per its own header) |
|---|---|---|
| config-distribution-model | `config-distribution-model/design.md` | §15 **canonical / SHIPPED** 0.4.0–0.4.1 (§15.8) |
| unified-config-profiles | `unified-config-profiles/design.md` | **canonical / converged**; 8 decisions owner-ratified 2026-06-26 |
| home-config-portability | `home-config-portability/design.md` | **DRAFT**; D-A..D-G baked; FORK-1/2 + NEW-FORK-B resolved; FORK-3/4/5 + NEW-FORK-A open |
| distributable-config-manifest | `distributable-config-manifest/design.md` | **DRAFT**; D1–D6 decided; F1–F5 open (F6 inherited-resolved) |
| multi-harness-extensibility | `multi-harness-extensibility/design.md` | **DRAFT**; D1–D3 decided; F1–F3/F5 open; F4 resolved-to-a-gate |

---

## 1. Layered architecture — CONFIRMED (with two corrections)

The proposed L0→L4 stack holds. Each layer cites the spec it owns; corrections are called out.

### L0 — Substrate · config-distribution-model §15
Sources (origin/transport), scopes (precedence/authority), kind, and `units` + lockfile.
- §15 **D1** (lines 865–872): three orthogonal axes — **scope** ("who gets the last word",
  precedence), **source** (`git`/`http`/`local`/`oci`, "where a unit comes from"), **kind**
  (`layer` vs `artifact`).
- §15 **D2** (line 877): unified addressing `source:path@version`.
- §15 **R1** (lines 1011–1014): one `.agentsrc.lock` with a `units` map + top-level
  `inputs_digest`; **D4** content-hash staleness, never clock.
- **CORRECTION (minor):** the proposed wording "sources / scopes / units+lockfile" folds **kind**
  (the third D1 axis) silently into "units". Kind (layer vs artifact) is a first-class axis and is
  what manifest/profile/descriptor each extend — keep it explicit.
- **Note:** L0 is **already shipped and on master** (§15.8) — it is the *landed foundation*, not a
  pending merge.

### L1 — Resolution · unified-config-profiles
Selector-merge fragments + scope-attached layering policy; no-`extends`; source-derived authority;
deny/value-locks (no force-allow).
- §2 / §2.3 two-phase selector-merge; §2.1 **no profile→profile edge**; **Decision 1**
  source-derived authority; **Decision 4** deny-lock + value-lock, **no force-allow**.
- **CORRECTION (precision):** authority derives from **SOURCE scope**, not selector scope and not
  self-declared — Decision 1 is emphatic ("precedence and permission-gating must key off SOURCE
  scope, not selector scope", lines 209–217). The map's "authority-from-scope" is right but must
  read **source-scope**.

### L2 — Distribution · distributable-config-manifest
`kind: manifest` unit bundling {sources + which policy binds + project-set}; `init --from`.
- **D1** (line 99) manifest is a §15 unit; **D2** (line 117) distinct kind = bundle/entrypoint;
  **goals** (lines 61–68); **D5** (line 166) `init --from <manifest-ref>`. CONFIRMED.

### L3 — Portability · home-config-portability
`init --from`; machine-local split (binding-table / caches / creds); identity registry =
project-set; git-auth ambient-first.
- **D-D** (line 196) `init --from`; **D-A/R7** machine-local split; **R4a** (line 346) identity
  registry; **NEW-FORK-B** (line 553) ambient-first auth, creds never synced. CONFIRMED.
- **Note:** in home-config's *own* text the `init --from` seed is "a home-repo URL / source ref"
  (D-D line 197), **not yet a manifest**. "init --from a manifest" is the *manifest spec's*
  re-targeting of this seed (manifest D5 / F4) — a cross-spec dependency, not a fact stated inside
  home-config (see Risk 1 and the merge order).

### L4 — Projection · multi-harness-extensibility
Per-harness descriptor (capability-granular) + survey-gated frontmatter translator + **irreducible
Go core**; PLATFORM_DIRS_DOCS matrix.
- **D2** (line 157) descriptor **+ explicit audited Go core** (emphatically *not* descriptor-only);
  **D3** (line 204) capability-granular = (asset × scope × feature × form); **D1** (line 130)
  survey-gated translator, agents→Codex seed; **R5/F5** PLATFORM_DIRS_DOCS is the authority.
  CONFIRMED.

### Cross-cutting
- **Scope chain at every layer** — present in all specs **but NOT uniform** — this is the headline
  Risk 3 (unified Q1 itself flags three divergent orderings). The map's "the same scope chain at
  every layer" is **aspirational, not yet true in the text.**
- **Scientific-method spine** — CONFIRMED as the method (not a layer); cited by all three DRAFT
  specs as how their forks get resolved (manifest §6 line 280, home-config §5 line 417,
  multi-harness §5 F4 / §8). Folded into the ideation-cycle skill (#186 merged).

## Crisp roles — CONFIRMED (all six)
- **source = where** — §15 D1 "where a unit comes from". ✓
- **scope = whose + precedence** — §15 D1 (precedence) + unified Decision 1 (whose = source→registry→scope). ✓
- **profile = how fragments merge** — unified §2. ✓
- **manifest = what you publish** — manifest D2 "distributable bundle". ✓
- **registry = which projects** — home-config R4a / manifest D6. ✓ (ownership contested — Risk 2)
- **descriptor = how it reaches each harness** — multi-harness D2. ✓

---

## 2. RISKS — the seven coherence checks (verdicts with evidence)

| # | Risk | Verdict | Evidence (cited) | One-line proposed resolution |
|---|---|---|---|---|
| 1 | manifest vs profile boundary | **CLEAN** (ratification pending) | manifest **D2** (l.117–136) + §5 table (l.269) draw it crisply: profile = per-context resolution *fragment*; manifest = distributable *bundle/entrypoint* that *references* profiles by ref and "adds zero resolution semantics". unified-config-profiles never mentions manifest (no contradiction). BUT manifest **F2** (l.300–309) self-flags the distinction as an **unratified fork** (recommended = distinct kind). | Ratify F2 (distinct `kind`) and add a one-line reciprocal note in unified-config-profiles §9 acknowledging `kind: manifest` as a sibling unit, so the boundary is asserted from both sides. |
| 2 | registry duplication (one owner or two?) | **OVERLAP** (one-directional claim) | home-config **R4a** (l.346) presents the portable identity registry as *its own* synced surface; home-config §8 relationships (l.651–692) **does not list distributable-config-manifest at all**. manifest **D6** (l.184–197) reaches back and claims that same registry as "the manifest's project-set component… not a separate bespoke surface." Only the manifest says "shared." | Make the manifest's project-set the **single owner**; update home-config to cite manifest D6 and frame its registry as the *personal-scope instance* of the manifest's project-set (resolve before either lands). |
| 3 | scope-chain consistency (same chain + same authority?) | **CONTRADICTION** (self-flagged open) | Three orderings: §15 **D1** `product→user-local→org→team→repo-imported→repo-local→overlay→runtime` (l.867–869); unified prototype `repo→project→user→team→org`; scoped-KG `repo→user→team→org→public`. unified **Q1** (l.448–454) explicitly demands "one canonical scope ordering… reconciling these three." Worse than ordering: §15 D1 is **value-merge precedence (repo-local beats org)** while unified §2.3 + Decision 4 is **policy/lock authority (org's locks bind repo)** — *opposite* org-vs-repo semantics, conflated under one "scope chain" name. | Pin **one** canonical authority chain via unified Q1, and **explicitly separate** "value-merge precedence" from "policy/lock authority" if both axes are genuinely needed; §15 D1 is the single owner others reference. Highest-leverage risk — blocks L1/L2/L3/L4. |
| 4 | descriptor vs units (in §15 model, or separate axis?) | **GAP** | manifest/profile are explicit §15 units (`kind: manifest` D1; `kind: profile` unified R2 — locked, digested). The descriptor is **not**: multi-harness declares no `kind`, no lock entry, no `inputs_digest` membership; §8 (l.430–432) only says descriptors are "scope-attachable **in principle**", and external descriptor packaging is **deferred** (§7). So the descriptor is a separate axis that could drift from §15. | Decide whether the descriptor becomes a §15 `kind` (sourced + locked) or stays a build-time/doc-derived artifact; **if it is ever shipped via a source** (multi-harness §8 "a future external source could ship descriptors"), it must be a unit so it rides the lock/digest path rather than an un-locked distribution channel. |
| 5 | machine-local boundary (cache + registry + creds classified consistently?) | **CONTRADICTION** (partial) | Consistent: caches + the id→path **binding table** + credentials are machine-local / never-synced across all three (§15 **D13** l.980–981 caches+registry "never a scope… never projected"; home-config **R7/DC3**; manifest **F6/R3**). Diverges on the **registry**: §15 D13 classifies "the managed-project registry" as **wholesale machine-local & never-a-scope**, while home-config **D-A/R4a/R6** splits it and makes the *identity* (id+key, no path) **portable + synced into the user-local layer** (a scope!), and manifest **D6** distributes it. §15 D13 predates the identity/binding split. | Amend §15 D13 to reflect the split: the **binding table (id→path) + `added` + caches** are machine-local; the **identity registry (id+key, no path)** is portable user-scope config. Creds-always-machine-local is already consistent — leave it. |
| 6 | bootstrap / circularity of `init --from <manifest-ref>` | **CLEAN** (bounded) | Trace: manifest is itself a §15 unit `<source>:<name>` (D1); `init --from` resolves the manifest unit, *then* pulls its sources, applies policy, materializes the registry (manifest **D5/R3**; home-config **D-D** "chicken-and-egg dissolves — sources live inside the resolved home"). No unbounded recursion: **no manifest→manifest `extends`** (D3/R10), depth-1, order-independent (unified §2.1 / H1); the seed source is directly addressable (the `local` source §15 D6, or a git URL). The only "egg" is **ambient credentials** for a private seed source — a machine-local *precondition* that fails loudly (NEW-FORK-B), not a recursive dependency. | No circularity to fix. **Confirm manifest F4** (personal manifest = the `local` source's manifest) against home-config **D-B** (portable fields live in the already-loaded user-local layer) so the seed and the user-local layer do not become **two competing personal config surfaces**. |
| 7 | single-source (anything duplicated that should have one owner?) | **PARTIAL** — 2 violations, 3 clean | **Violations:** (a) the **project-set / identity registry** has two would-be owners (Risk 2 — manifest D6 vs home-config R4a); (b) the **scope-chain definition** is duplicated & divergent across 3 specs with no canonical owner (Risk 3 — unified Q1). **Clean / well single-sourced:** the **lock** (§15 owns `.agentsrc.lock`/units/`inputs_digest`; profile R2 + manifest R1 explicitly ride it, "no parallel lock machinery"); the **layering policy + the 8 authority rulings** (unified owns; manifest D2/D3/D4 applies them "verbatim" by *reference*, re-defines nothing); the **descriptor↔matrix** authority (PLATFORM_DIRS_DOCS is the single source per multi-harness R5/F5). | Fix the two violations via Risk 2 (manifest = sole registry owner) and Risk 3 (§15 D1 = sole scope-chain owner, pinned by unified Q1). The lock / policy / rulings discipline is the model the other two should copy: **one owner, everyone else references by ref.** |

**Verdict summary:** 1 CLEAN · 2 OVERLAP · 3 CONTRADICTION · 4 GAP · 5 CONTRADICTION(partial) ·
6 CLEAN · 7 PARTIAL. The two real CONTRADICTIONs (3, 5) and the OVERLAP (2) all trace to **two
shared definitions lacking a single canonical owner** — the **scope chain** and the **identity
registry** — both of which the substrate (§15 D1 / D13) predates and the downstream specs have
since refined without amending the owner. Fixing those two ownership questions collapses risks
2, 3, 5, and 7's violations together.

---

## 3. Draft merge-order recommendation (dependency-respecting)

Dependencies: L0 underlies all; L1 is the policy engine L2/L3-phase1/L4 lean on; L2 references both
L1 (profiles/policy) **and** L3 (registry + `init --from`); L4 is orthogonal to portability (both
home-config §8 and multi-harness §8 state this) and shares only the scope chain.

0. **L0 §15 — ALREADY LANDED** (master, shipped 0.4.0/0.4.1, §15.8). Foundation, not a pending
   merge. Carries **two follow-up amendments** when L1/L3 land: D1 scope-chain canonicalization
   (Risk 3) and D13 registry-split (Risk 5).

1. **L1 unified-config-profiles — FIRST among pending.** Most ratified (status *canonical/
   converged*, 8 owner-ratified decisions — **not DRAFT**). It owns the policy engine and the
   scope-chain reconciliation (**Q1**) that every other layer needs; resolving Q1 here unblocks
   Risk 3 for all. Land before L2.

2. **L3 home-config phase-0 — in parallel with L1.** Bounded and largely substrate-level (identity
   registry + machine-local split + portable prefs; FORK-1/2 + NEW-FORK-B resolved). Can ship
   alongside L1, **but** its registry must be reconciled with L1's scope chain (Risk 3) and
   pre-agreed as the manifest's project-set (Risk 2) before it and L2 both land.

3. **L2 distributable-config-manifest — after L1 + L3-phase0.** It is the integration layer
   (references L1 profiles/policy and L3 registry + `init --from`). Resolve **F2** (kind boundary,
   Risk 1) and **F4** (personal manifest = `local` source, Risk 6) as part of landing it.

4. **L3 home-config phase-1 (full `init --from`) — provider/consumer-paired with L2.** Phase-1
   re-targets its seed from a bare home-repo URL to a **manifest ref** (manifest D5); land *with or
   immediately after* L2 (manifest defines the seed, home-config consumes it).

5. **L4 multi-harness-extensibility — independent / parallel anytime.** Orthogonal to portability;
   gated only on its **own F4 probe** (hand-add ONE real harness end-to-end + record the
   irreducible-Go inventory *before* schema ratification — DC0). Shares only the scope chain (L1
   Q1). Resolve Risk 4 (descriptor-as-§15-unit?) as part of its schema ratification.

**Critical path:** L1 (resolve Q1) → {L3-phase0 registry, agree Risk 2 ownership} → L2 → L3-phase1.
L4 rides alongside on its own probe gate.

### Ratification status of each fork-set (for the audit)
- **L0 §15** — canonical, **shipped**. Open §15-adjacent: Q3 (signing ERROR-by-default), **Q5
  workspace lockfile** (ruled either-or, unimplemented — touches manifest F5 + home-config FORK-4).
- **L1 unified** — **canonical/converged**; **8 decisions owner-ratified 2026-06-26**. Open: the six
  plan-level questions Q1–Q6 (Q1 scope chain is the cross-cutting blocker).
- **L3 home-config** — **DRAFT**. Owner-directed **D-A..D-G baked in (7 decisions)**. Forks
  **resolved: FORK-1 (hybrid), FORK-2 (refined), NEW-FORK-B (ambient-first auth)** = **3 resolved**.
  **Still open: FORK-3, FORK-4, FORK-5, NEW-FORK-A** = 4 open. *(The task brief's "home-config's 4
  ratified" does not match the text — the text shows **3** resolved forks (+7 baked Decisions) and
  **4** still-open forks; flagging the discrepancy rather than asserting "4 ratified.")*
- **L2 manifest** — **DRAFT**. D1–D6 decided; **F1–F5 open**, **F6 inherited-resolved** (from
  NEW-FORK-B).
- **L4 multi-harness** — **DRAFT**. D1–D3 decided; F1–F3/F5 open; **F4 resolved to a hard gate**
  (mandatory probe before schema ratification, DC0).

---

*This is a grounding artifact for the cross-brain audit + owner. The seven risks are surfaced, not
resolved. Source specs were read, not edited.*
