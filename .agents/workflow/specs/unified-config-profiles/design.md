# Unified Config Profiles — Design Spec

**Status:** canonical / converged. The model below was empirically prototyped
(branch `proto/config-profile-resolver`, PR #181 — H1/H7/H8 PASS,
mutation-verified) and cross-harness reviewed; the eight decisions in §3 were
**ratified by the owner on 2026-06-26**. This spec is the contract the
implementation plan is accountable to.

**Written:** 2026-06-26

**Originating input:** `.agents/proposals/config-derived-agent-capability-profiles.md`
(the Codex proposal that framed "config-derived agent capability profiles" and
enumerated design options A–D). This spec generalizes that proposal's frame from
*one new profile kind* to *one unified profile model* that subsumes the existing
`app_type` and `stage` profiles as well.

**Empirical basis:** `proto/config-profile-resolver` (`resolver.go`,
`resolver_test.go`, `README.md`, `fixtures/`) — a self-contained Go module
proving the two-phase selector-merge resolver behaves as designed. It is cited
throughout as the proof, not duplicated.

**Related specs:**
- [config-distribution-model §15](../config-distribution-model/design.md) — the
  `units` / `scope` / `source` / `kind` substrate and the `.agentsrc.lock` model
  this spec builds on. A profile is a unit; this spec adds `kind: profile`.
- [app-type-profiles](../app-type-profiles/design.md) — defines a profile as a
  named, versioned, composable bundle for *workflow pipeline* behavior. This
  spec re-expresses it as a profile-kind under the shared resolver (zero diff).
- [stage-profile-and-routing-consolidation](../stage-profile-and-routing-consolidation/design.md)
  — the shipped `stage_profiles` primitive (verifier/reviewer prompt
  composition). Also re-expressed as a profile-kind (zero diff).
- [scoped-knowledge-graphs](../scoped-knowledge-graphs/design.md) — the
  `repo → user → team → org → public` scope chain and the "public is read-only /
  driver-event staleness" model that motivates the no-`extends` and lock-hardening
  decisions here.
- [org-config-resolution](../org-config-resolution/design.md) — layer precedence
  and merge-category rules that the Phase-1 policy resolution generalizes.

---

## Table of contents

1. [Problem & goal](#1-problem--goal)
2. [The model](#2-the-model)
3. [The eight ratified decisions](#3-the-eight-ratified-decisions)
4. [Requirements](#4-requirements)
5. [Migration plan: dogfood with zero behavioral diff](#5-migration-plan-dogfood-with-zero-behavioral-diff)
6. [Open questions](#6-open-questions)
7. [Done criteria](#7-done-criteria)
8. [Deferred](#8-deferred)
9. [Relationship to other specs](#9-relationship-to-other-specs)

---

## 1. Problem & goal

The repo has accreted three *separate* notions of "profile," each with its own
schema, its own resolution path, and its own command surface:

1. **`app_type` profiles** — name the *work/pipeline shape* (verifier chain,
   review kind, graph backend, topology, lenses). Owned by app-type-profiles.
2. **`stage_profiles`** — name *verifier/reviewer prompt composition and return
   gates* per stage. Owned by stage-profile-and-routing-consolidation (shipped).
3. **(proposed) agent-capability profiles** — name *what a concrete runtime role
   may access* (tools, skills, MCP, hooks, rules overlays). The originating
   proposal's target; it does not yet exist.

All three answer the same shaped question — *"given a context, what config
fragment applies, and who is allowed to change it?"* — but they answer it three
different ways. The immediate forcing function is narrow: the `orchestrator`
agent needs the `Skill` tool to invoke `orchestrator-session-start`, but Claude
Code has no native per-skill allowlist, so the permitted Skill set has to be
projected from *somewhere*. The originating proposal correctly identified that
hand-maintaining that list inline in each `AGENT.md` is a fourth config surface
and pure drift bait (proposal options A/B), and that the durable fix is a
config-resolved capability bundle (option C).

But adding a *third resolution engine* for a *third profile schema* is exactly
the incoherence config-distribution-model §15 set out to kill (the half-old /
half-new, three-axes-fused problem). The deeper opportunity is to recognize that
all three "profiles" are the **same primitive**: a selector-scoped config
fragment plus a policy that governs how fragments merge across scopes.

**Goal.** Define **one** profile model — a selector-scoped config fragment,
resolved by one shared selector-merge engine governed by scope-attached layering
policy — that:

- subsumes today's `app_type` and `stage` profiles **with zero behavioral diff**
  (the migration is a dogfood, and proving zero-diff is this spec's central
  done-criterion);
- supports the new agent-capability use case (the orchestrator Skill-scoping fix
  becomes the first agent-capability dogfood);
- inherits §15's `units` / `scope` / `source` / `kind` / lock substrate rather
  than inventing a parallel one;
- is **safe to extend to public/foreign sources later** (npm-like), which is the
  constraint that rules out `extends`-style inheritance and force-allow locks.

**Non-goals (from the originating proposal, retained):** do not create a new
standalone skill for this; do not make `app_type` carry every role's runtime
authority; do not make hook-local JSON the long-term source of truth; do not let
trace-derived relevance auto-upgrade permissions without a reviewed config change.

---

## 2. The model

A config **profile** is a **selector-scoped config fragment**, resolved by a
**flat selector-merge cascade**. It is a unit of `kind: profile` in the §15
substrate; its identity is the existing absolute ref `<source>:<name>` (§15 D2);
it is recorded in `.agentsrc.lock` like any other unit; and it is
resolved/read-back through `da config explain`. The prototype's `types.go`
(`Profile`, `LayeringPolicy`, `Bundle`, `Context`, `Resolved`) is the empirical
shape of this model.

### 2.1 No `extends` / no inheritance (the anti-dependency-hell decision)

There are deliberately **zero profile→profile edges**. A profile references only
its own leaf bundle/selector; the only dependency graph is depth-1
(profile → leaf-unit). There is no `extends`, `inherits`, `parent`, `base`, or
`composes-by-ref-that-resolves-recursively` field. Behavior is composed by the
*resolver* selector-merging matching fragments, not by a profile pulling in
another profile.

This is the explicit anti-dependency-hell decision. The prototype proves it
structurally (H1: the `Profile` type carries no inheritance field, and no
bundle value collides with another profile's ref) and behaviorally (H1:
resolution is order-independent — shuffling the input profile order 50× yields an
identical digest). The future public-source world makes inheritance an
*attack surface* (a foreign profile that your local profile `extends` can inject
capability silently); a flat cascade has no such transitive trust edge.

> **Reconciliation with app-type-profiles' `composes:`.** app-type-profiles §2.2
> has a `composes: [ref,…]` field whose semantics are *union, never override*.
> That is compatible: `composes` is sugar for "this context matches several
> selectors," not a trust/inheritance edge — the composite carries no authority
> its children lack, and the union is exactly what the selector-merge cascade
> produces when several fragments match one context. The migration (§5) expresses
> `composes` as multiple selector-matched fragments, not as a profile→profile edge.

### 2.2 Hybrid expression: distinct KINDS, one engine

Keep the three profile *kinds* distinct for clarity and migration tractability —
`app_type`, `stage`, `agent-capability` remain nameable, inspectable categories —
but run them through **one shared resolver and selector-merge engine** underneath.
The kinds differ only in (a) which selector keys are meaningful and (b) which
bundle fields they populate; they do **not** differ in resolution mechanics. This
hybrid is what makes the zero-diff migration tractable: each existing profile
type keeps its name and external shape while its *resolution* moves onto the
shared engine.

### 2.3 Two-phase resolution

Resolution against a dispatch context (role, app_type, stage, harness, scope
chain) is two phases — proven by the prototype's `Resolve()`:

- **Phase 1 — effective layering policy.** Merge every `layering_policy` unit
  whose scope is in the context's scope chain, processed *low-authority first*. A
  higher-authority scope binds lower ones: its precedence wins, its locks are
  absolute, its `override_permissions` cap what lower scopes may set. Output is
  the effective precedence + the set of locks (each tagged with its owning scope)
  + the effective permission map.
- **Phase 2 — effective config.** Selector-merge every `profile` fragment whose
  selector matches the context, ordered by the Phase-1 precedence (local-wins
  tail). Additive sets (`tools.allow`, `skills.*`, `hooks`, `mcp`) union; `deny`
  subtracts; scalars (`model`) are last-writer. Phase-1 locks and permissions
  *gate* every field write. Output = the effective bundle + the contributing
  absolute refs + a stable digest.

The policy is itself a scope-attachable unit (a `layering_policy`), not a global
constant. The **most-authoritative scope present** owns the policy: org normally,
but if no org scope is in the chain, the highest present scope binds.

### 2.4 Reproducibility (the team guarantee)

Same source-set + same lockfile digest ⇒ identical effective bundle. This is the
team-reproducibility guarantee: **team distributes reproducible bundles**; **org
(or the most-authoritative scope present) owns the layering policy**. The
prototype proves it (H7: independent re-parses of the same fixtures — "two
clones" — produce identical bundles and digests). The digest (Decision 7) is the
contract that makes "did my resolved config drift?" answerable locally and
clock-free, consistent with §15's content-hash staleness model.

---

## 3. The eight ratified decisions

Each was settled in design review and **owner-approved 2026-06-26**. Where a
decision *corrects* the prototype, that is called out — the prototype is the
proof-of-mechanism, not the final authority on policy.

### Decision 1 — Source authority is derived, never self-declared

A profile's authority for precedence and permission-gating is **derived from
`ref.source → source-registry → scope`**, not from anything the profile itself
declares. A profile **cannot self-declare its authority**.

*Rationale.* Self-declared authority is a confused-deputy / injection vector:
once sources can be public, a foreign profile that stamps itself `scope: org`
would inherit org authority it has no right to. Binding authority to the source
registry means authority is a property of *where a unit came from* (which the
operator controls via the registry), never of the unit's own contents.

The "a team source is blessed to publish org-authority profiles" case is handled
by **explicit delegation in the §15 source registry** — a registry-level grant
that source S may carry scope-O authority — **not** a per-profile field. The
operator who edits the registry is the one extending trust; the profile author
cannot.

*Correction vs the prototype.* The prototype's `Profile.scope()` falls back to
the *selector's* scope (then to `repo`) when `source_scope` is unset, and uses
that for precedence and permission-gating. That is wrong for the authority axis:
**precedence and permission-gating must key off SOURCE scope, not selector
scope.** A profile that lives in an org source but is *selected for*
`scope:project` must keep **org** authority — its selector narrows *where it
applies*, not *how much it is trusted*. The selector scope governs applicability;
the source scope governs authority. The implementation must split these two,
keying authority strictly off `ref.source → source-registry → scope`.

### Decision 2 — `override_permissions`: omitted vs explicit-empty are different

`override_permissions` has three distinct states, and the spec requires they stay
distinguishable (the prototype conflated the first and third — corrected here):

- **Omitted** (the field is absent) ⇒ *no opinion*; this policy expresses no
  permission constraint and a higher scope's permission map (if any) governs.
  Inherit-higher.
- **Explicit non-empty map** ⇒ an **allowlist**. A scope present in the map may
  change exactly its listed field-paths; a scope **absent** from the map may
  change **nothing**; a field absent from a present scope's list is **not
  allowed**.
- **Explicit empty `{}`** ⇒ **lockdown**. No scope may override anything.

*Rationale.* Omitted-means-unrestricted and empty-means-lockdown are opposite
intents that an operator must be able to express unambiguously. The prototype
treated *empty/absent* identically as "unrestricted," which silently makes
lockdown unexpressible. Distinguishing them is a presence check
(absent-key vs present-empty-map) the schema and resolver must both honor.

### Decision 3 — Policy merge narrows; whole-replace only via a named mode

When policies merge across scopes, a higher-authority scope may only **narrow**
lower grants (remove permissions, tighten precedence, add locks). It may **not**
silently *broaden* what a lower scope granted. The **only** way a higher scope
replaces the whole inherited policy is by explicitly invoking a **named replace
mode** (a declared "this policy supersedes, not merges" marker).

*Rationale.* Monotone-narrowing is the safe default: composing policies can only
ever tighten, so adding a scope to the chain can never accidentally grant more
than was already granted. Whole-replacement is sometimes legitimately needed
(an org re-baselining its policy), but it must be a *visible, named act*, never
an implicit consequence of merge order.

### Decision 4 — Locks are absolute; deny-lock + value-lock only, NO force-allow

Locks are absolute invariants. **Permission never beats a lock.** Two lock
kinds are supported, and only these two:

- **deny-lock** — force a value into the effective deny set for matching
  contexts (e.g. `tools.deny:{Edit,Write}@role:reviewer`), and forbid any
  lower-scope profile from re-granting it.
- **value-lock** — pin a scalar to a fixed value (e.g. `model=X`), which no
  lower scope may change.

There is **no force-allow lock.**

*Rationale (owner).* Force-allow is a capability-injection / supply-chain attack
vector once sources can be public (npm-like): a foreign or lower-trust source
must **never** be able to punch a capability *through* a local or org deny. Deny
and value-pin are safe because they only ever remove or fix capability; allow
must always be earned by passing every higher-scope deny/permission gate, never
asserted. The prototype enforces exactly this asymmetry: deny-locks are forced
into the effective deny set after merge and stripped from allow
(`applyLockDenies`), and `unionMinusLocked` drops any lower-scope allow that a
lock forbids — H8(a) (org lock wins over a malicious team grant) and H8(b) (a
team lock holds where org is silent) both PASS and are mutation-sensitive.

### Decision 5 — Selectors: exact-match v1

A selector constrains the context a fragment applies to. v1 semantics:

- **exact-match** on each present key;
- an **absent key is a wildcard** (matches any context value);
- an **unknown selector key is a validation error** (not a silently-ignored
  no-op).

Glob, list, and negative selectors are **deferred** (§8).

*Rationale.* Exact-match + wildcard-on-absent is the smallest selector algebra
that expresses today's role/app_type/stage/harness routing, and it keeps
matching trivially deterministic. Failing loudly on unknown keys prevents a
typo'd selector from silently matching everything (or nothing). The prototype's
`selectorMatches` implements exactly this.

### Decision 6 — Same-scope conflicts: deterministic tie-break, both shown

When two fragments at the **same** authority scope conflict on a value, resolution
is broken deterministically by `source-id` / absolute-ref ordering (the
prototype's `Ref`-string tiebreak). `da config explain` **must show both
contributors**, not just the winner.

*Rationale.* Same-scope conflict is a genuine ambiguity the operator should see,
not have silently resolved invisibly. A deterministic tiebreak keeps the result
reproducible (no map-iteration nondeterminism), while surfacing both contributors
makes the conflict diagnosable. The prototype sorts contributing refs and the
merge order is ref-stable, satisfying the determinism half; explain surfacing both
is a requirement on the readback layer (§4).

### Decision 7 — Digest hashes bundle + refs + policy version

The reproducibility digest hashes the **canonical effective bundle** *plus* the
**contributing absolute refs** *plus* the **effective policy version** — not just
the normalized bundle alone.

*Rationale.* Two different source-sets / policies can coincidentally produce the
same bundle *values*; a digest over values alone would call them identical and
miss a policy or provenance change that *should* register as drift. Including the
contributing refs and the effective policy version means the digest changes
whenever *how* the bundle was produced changes, even if the resulting values
happen to match — which is what "same source-set + lockfile digest ⇒ identical
bundle" (§2.4) actually requires. The prototype digests the normalized bundle
only; this decision extends that to refs + policy version.

### Decision 8 — Deny/additive order: final-deny-wins within authority

`deny` wins over `allow` — but only **within the same authority**, or via an
explicit lock. A **low-scope deny must not silently erase a higher-scope allow.**

*Rationale.* "Final-deny-wins" is the right intuition for a single authority
level (a profile that both allows and denies the same value means deny). But a
low-trust scope must not be able to *subtract* a capability that a higher-trust
scope deliberately granted — that would be a downward denial-of-service /
authority inversion. Cross-authority subtraction is legitimate **only** when the
higher scope itself owns a deny-lock (Decision 4). So: within one authority,
final-deny-wins; across authorities, a deny only binds upward when it is a lock.
This composes cleanly with Decision 3 (higher narrows lower) and Decision 4
(locks are the sanctioned cross-authority subtraction channel).

---

## 4. Requirements

Behavioral requirements the implementation plan is accountable to. (Field names,
file paths, and function signatures belong in the plan, not here.)

**R1 — One resolver, one engine.** All three profile kinds resolve through a
single two-phase selector-merge engine (§2.3). There is exactly one resolution
path; the kinds differ only in meaningful selector keys and populated bundle
fields, never in mechanics.

**R2 — `kind: profile` is a first-class §15 unit.** A profile is identified by
the absolute ref `<source>:<name>`, recorded in `.agentsrc.lock` with its digest
like any other unit, and resolved through the §15 substrate. No parallel
lock/source/cache machinery.

**R3 — Authority is source-derived.** Precedence and permission-gating key off
`ref.source → source-registry → scope` (Decision 1), independent of the
selector's scope. Cross-source authority delegation is a registry grant, not a
profile field.

**R4 — Two-phase, policy-governed merge.** Resolution resolves the effective
layering policy across the scope chain first (Decision 3 narrowing, Decision 2
permission states), then selector-merges fragments governed by it (Decisions 4,
5, 8). The whole merge is order-independent and deterministic (H1).

**R5 — Reproducible bundles.** Same source-set + lockfile digest ⇒ identical
effective bundle (H7). The digest covers bundle + contributing refs + effective
policy version (Decision 7).

**R6 — `da config explain` readback with context slicing.** `da config explain`
is the effective-policy truth surface for profiles (consistent with §15 D12). It
must accept context selectors —
`da config explain --role <r> --app-type <t> --stage <s> --harness <h>` — and
show, for the resolved profile bundle: the effective values, every **contributing
absolute ref** (including **both** contributors on a same-scope conflict,
Decision 6), the binding **locks** (with owning scope), the effective
**permission map**, the source layers, and the **digest**. `--json` for machine
consumption.

**R7 — Drift detection: AGENT.md (and other projections) vs resolved digest.**
Because platform files (`AGENT.md` frontmatter, hook allowlist data, MCP
inclusion, settings) are *projections* of a resolved profile, the system must
detect projection drift: a check (`da config verify` / `da doctor`) that compares
the materialized projection against the resolved-bundle digest and reports
mismatch — e.g. "AGENT.md grants `Skill` but the skill-allowlist gate is missing,"
or "projected allowlist does not match the effective profile digest." This is the
concrete realization of the originating proposal's drift-verification requirement.

**R8 — Projection, not a second source of truth.** Platform-specific files are
*generated from* the resolved profile (via `da refresh` / install projection),
never hand-authored as the authority. Hook-local JSON is a projection output, not
a source (originating-proposal non-goal, retained).

**R9 — Validation is loud.** Unknown selector keys (Decision 5), self-declared
authority (Decision 1), and force-allow locks (Decision 4) are validation errors,
not silently-ignored inputs.

---

## 5. Migration plan: dogfood with zero behavioral diff

The migration strategy is **dogfood**: re-express today's profiles as profile
units under the shared engine, and **prove zero behavioral diff**. Zero-diff is
this spec's central done-criterion — the migration is not "done" until the
re-expressed profiles resolve to byte-identical effective config as the legacy
paths do today.

**5.1 Re-express `app_type` and `stage` profiles as profile-units.**
Today's `app_type` profiles (app-type-profiles) and `stage_profiles`
(stage-profile-and-routing-consolidation, shipped) are re-authored as
`kind: profile` units with the appropriate selector keys (`app_type` for the
former; `stage` — and `role` where relevant — for the latter) and bundle fields.
Their *names and external shapes are preserved* (the hybrid-kinds decision, §2.2).
The `composes:` relation (app-type-profiles §2.2) is expressed as multiple
selector-matched fragments, not a profile→profile edge (§2.1 reconciliation).

**5.2 Zero-diff proof (the done-criterion).** For the full matrix of contexts
the legacy paths handle (every app_type, every stage, every role currently
routed), the new resolver's effective bundle must equal the legacy resolver's
output. This is proven the way the prototype proves H7: resolve the same context
through both paths from independent parses and assert structural equality +
digest equality. Any diff is a migration bug, not an accepted behavior change.
(The legacy paths are `da config relevance`'s `execution_profile` read and the
delegation/fanout dispatch — both must agree with the unified resolver.)

**5.3 First agent-capability dogfood: orchestrator/delegation Skill-scoping.**
The new profile *kind* is dogfooded by the originating bug: the orchestrator's
permitted `Skill` set (and the loop-worker / reviewer capability bundles) become
an `agent-capability` profile, projected into Claude subagent frontmatter + the
skill-allowlist hook data. The stopgap currently on branch
`fix/orchestrator-skill-scoping` is explicitly a **projection placeholder** of
this model (per the originating proposal's "extract the minimal orchestrator-only
version, but mark it as a projection placeholder" disposition) — when the unified
model lands, that stopgap's hand-authored allowlist is replaced by the projected
output of the resolved capability profile, with R7 drift detection guarding the
two against divergence.

**5.4 Sequencing.** The §15 `units` / lock / source substrate is the
precondition; the unified resolver is the bridge; the two legacy re-expressions
(zero-diff) come before any new agent-capability rollout beyond the orchestrator
dogfood. Concrete task ordering and write scopes belong in the plan.

---

## 6. Open questions

These must be resolved by the plan (or an explicit follow-on), not left dangling:

**Q1 — Scope chain reconciliation.** §15 D1 lists the precedence chain as
`product → user-local → org → team → repo-imported → repo-local → project-local
overlay → runtime`, the prototype uses `repo → project → user → team → org`, and
scoped-knowledge-graphs uses `repo → user → team → org → public`. The unified
model needs **one** canonical scope ordering for authority. The plan must pin the
exact chain and its authority ranks, reconciling these three, and state where
`public` (read-only) and the `project-local overlay` sit relative to authority.

**Q2 — Selector-scope vs source-scope in the unit schema.** The prototype's
`source_scope` field exists precisely because the model under-specified what
scope a *context-selected* (non-scope-selector) profile is contributed from
(prototype README judgment-call #1). Decision 1 resolves the *authority* axis
(source-derived), but the plan must pin the **schema**: is source-scope ever
authored on the unit, or always derived from `ref.source`? (Decision 1 implies
always-derived; confirm no authored override survives.)

**Q3 — Lock syntax surface.** The prototype encodes locks as strings
(`tools.deny:{Edit,Write}@role:reviewer`). The plan must decide the authored lock
schema (string DSL vs structured object) and how value-locks (`model=X`) are
expressed alongside deny-locks, given Decision 4 forbids force-allow.

**Q4 — Named replace-mode surface (Decision 3).** What is the authored marker for
"this policy replaces rather than narrows"? Where does it live and how does
`config explain` surface that a replace happened?

**Q5 — Projection targets per harness.** R8 says profiles project into
platform files. The exact projection per harness (Claude subagent frontmatter
fields, hook data file shape, MCP inclusion, settings) is a plan/per-harness
matter; the spec only requires that projection exists and is drift-checked (R7).

**Q6 — Cross-source authority-delegation registry shape.** Decision 1 says
delegation lives in the §15 source registry. The exact registry field/grant shape
(which source may carry which scope's authority) must be specified in the plan and
reconciled with the §15 source-registry schema.

---

## 7. Done criteria

Verifiable, tracing to the decisions and requirements above:

1. **One engine.** `app_type`, `stage`, and `agent-capability` profiles all
   resolve through a single two-phase selector-merge resolver (R1); there is no
   second resolution path. *Verify:* code has one resolver entry point; all three
   kinds route through it.

2. **Zero-diff migration (central).** For every context the legacy `app_type` /
   `stage` paths handle, the unified resolver's effective bundle is structurally
   and digest-identical to the legacy output (§5.2). *Verify:* a matrix test
   resolving each context through both paths from independent parses asserts
   equality — the way the prototype proves H7.

3. **No `extends`, order-independent.** No profile→profile edge exists; resolution
   is deterministic under input reordering (§2.1, H1). *Verify:* the type carries
   no inheritance field; a shuffle test holds the digest constant.

4. **Authority is source-derived, not selector-derived or self-declared**
   (Decision 1). *Verify:* a profile in an org source selected for `scope:project`
   resolves with org authority; a profile attempting to self-declare a higher
   scope is a validation error.

5. **Lock asymmetry holds** (Decision 4). *Verify:* deny-lock and value-lock are
   supported and absolute (permission never beats a lock); a force-allow lock is a
   validation error; a lower-scope grant cannot punch through a higher-scope deny
   — the H8(a)/H8(b) mutation-sensitive proofs, ported to the production resolver.

6. **Permission states distinguished** (Decision 2). *Verify:* omitted,
   explicit-non-empty (allowlist), and explicit-empty (lockdown) produce three
   distinct resolution behaviors.

7. **Reproducible digest** (Decision 7). *Verify:* same source-set + lockfile
   digest ⇒ identical bundle; the digest changes when contributing refs or the
   effective policy version change even if bundle values do not.

8. **`config explain` readback** (R6). *Verify:* `da config explain
   --role/--app-type/--stage/--harness` shows effective values, all contributing
   refs (both on a same-scope conflict), binding locks with owning scope, the
   permission map, and the digest; `--json` works.

9. **Drift detection** (R7). *Verify:* `da config verify` / `da doctor` flags a
   projection (e.g. `AGENT.md` allowlist) that does not match the resolved-bundle
   digest.

10. **Orchestrator dogfood** (§5.3). *Verify:* the orchestrator's permitted Skill
    set is the projected output of a resolved `agent-capability` profile, not a
    hand-authored inline list, with R7 guarding the projection; the
    `fix/orchestrator-skill-scoping` stopgap is retired into this projection.

---

## 8. Deferred

Explicitly out of scope for this spec / v1:

- **Public-source / supply-chain hardening.** Trust, signing, discovery, and
  caching for `public` (npm-like) profile sources. This spec is *designed to be
  safe to extend there* (no-`extends`, no force-allow, source-derived authority),
  but the public-source backend, trust model, and registry hardening are a
  separate spec (mirrors scoped-knowledge-graphs deferring its `public` backend).

- **Glob / list / negative selectors** (Decision 5 is exact-match v1). A richer
  selector algebra is a later version.

- **Trace-derived relevance auto-proposals** (originating proposal option D).
  Trace/scoring data may *propose* capability changes ("orchestrator used
  plan-wave-picker in 12 successful runs; promote to preload") but must produce a
  reviewed config-change proposal, never auto-mutate a resolved profile. Future
  automation, not v1.

- **Full agent-capability rollout.** Beyond the orchestrator dogfood (§5.3),
  rolling capability profiles out to all agents (loop-worker, every reviewer,
  cross-harness reviewer) waits until the unified model is accepted and the
  orchestrator dogfood is proven (originating-proposal disposition: "do not
  broaden all agents until this design is accepted").

- **Cross-harness projection completeness.** Projection into non-Claude harness
  config (Codex, Copilot, Cursor) beyond the minimal surface needed for the
  dogfood is a per-harness follow-on (Q5).

---

## 9. Relationship to other specs

- **config-distribution-model (§15)** — *substrate.* Profiles are §15 `units` of
  `kind: profile`, identified by `<source>:<name>`, locked in `.agentsrc.lock`,
  read back via `config explain`. This spec adds the new kind and its resolver;
  it does **not** re-define scope/source/kind/lock/digest, which §15 owns. The
  authority-derivation (Decision 1) extends §15's source registry; the digest
  (Decision 7) and reproducibility (§2.4) extend §15's content-hash staleness
  model.

- **app-type-profiles** — *subsumed kind.* Its named/versioned/composable bundle
  becomes the `app_type` profile-kind under the shared engine, migrated zero-diff
  (§5.1). Its `composes:` union semantics survive as multi-fragment selector
  matching, not a profile→profile edge (§2.1).

- **stage-profile-and-routing-consolidation** — *subsumed kind.* The shipped
  `stage_profiles` primitive becomes the `stage` profile-kind under the shared
  engine, migrated zero-diff (§5.1). Its consolidation of verifier/reviewer
  routing is preserved; only the *resolution path* unifies.

- **scoped-knowledge-graphs** — *scope-chain + public-source precedent.* Supplies
  the `repo → user → team → org → public` chain this model resolves against (Q1
  reconciles it with §15's chain) and the precedent that `public` is read-only and
  deferred. Its driver-event staleness model is the conceptual sibling of the
  reproducibility digest.

- **org-config-resolution** — *generalized.* Its layer-precedence and
  merge-category rules are the seed of the Phase-1 policy resolution; this spec
  generalizes them into a scope-attachable `layering_policy` unit with locks and
  permission states.

---

*Empirical appendix:* the prototype (`proto/config-profile-resolver`, PR #181)
validated H1 (no dependency hell / order-independence), H7 (reproducibility
across clones), and H8 (policy-governed merge: org-lock-wins, team-lock-holds,
precedence-swap-changes-resolution) — all PASS and mutation-sensitive
(`mutation_check.sh`). The three judgment calls its README flagged
(source-scope vs selector-scope, `override_permissions` empty-map semantics,
lock-vs-permission ordering) are resolved by Decisions 1, 2, and 4 respectively.
