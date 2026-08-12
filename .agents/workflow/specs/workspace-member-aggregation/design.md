# Workspace Member Aggregation

**Spec ID**: workspace-member-aggregation
**Status:** ratified — decisions locked 2026-08-12 (all open questions resolved before
publication; see [Ratified Decisions](#ratified-decisions-2026-08-12))

**Purpose:** define how a workspace root directory — one that contains independently-managed
member repositories, each with its own identity, sources, and lock — can compute the union of
its members' effective agentic configuration, without requiring the root to hand-mirror that
union or the members to change how they resolve on their own.

**Upstream context:**
- The scope/source/kind/lock model this spec's aggregate tier plugs into is defined in
  [config-distribution-model §15](../config-distribution-model/design.md#15-coherence-model-v2-scopes-sources-units-and-the-lock).
- Workspace-as-optional-convenience and repo-local operability are design principles inherited
  from [org-config-resolution §3.1, §3.3, §9](../org-config-resolution/design.md#9-workspace-model).
- The canonical scope-chain / authority-rank ordering this spec deliberately does **not** extend
  is [unified-config-profiles Decision 1 / Q1](../unified-config-profiles/design.md#decision-1--source-authority-is-derived-never-self-declared).
- The machine-local project identity registry this spec is a distinct, non-overlapping concept
  from is [home-config-portability FORK-4](../home-config-portability/design.md#fork-4--relationship-to-15-q5-workspace-lockfile--owner-ratified-2026-06-27--keep-separate-forward-compat).
- The transitive-pin question this spec's lock model bounds is
  [distributable-config-manifest F5](../distributable-config-manifest/design.md#f5--versioning--pinning-does-a-pinned-manifest-pin-its-referenced-units-transitively).

---

## Problem Statement

A workspace root directory containing independently-managed member repos has no way to obtain
the union of its members' agentic configuration. Today the root manually mirrors an
approximation via explicit layer `extends` (the `payout`-style pattern: a dozen-plus hand-listed
layers), which drifts silently when members change, misses member repo-local declarations, and
over-includes layers no member actually uses. Sessions started at the workspace root therefore
lack — or mis-state — capability the members already have.

`config-distribution-model` §14 Q5 raised this as an open question ("should there be a
workspace-level lockfile that aggregates resolved SHAs across repos") and left it an
either-or sketch, never implemented. This spec answers it directly and supersedes that sketch
(see [D6](#d6--lock-model-member-locks-stay-authoritative-root-references-them)).

## Goals

1. A root manifest can declare its member projects; root resolution computes the superset of the
   members' effective agentic configuration (skills, agents, rules, hooks, MCP, stage profiles,
   execution-profile app types, settings, features).
2. Aggregation is additive: the root's own `extends`/skills/etc. continue to function, layered as
   today; root-explicit declarations win conflicts, mirroring the existing repo-over-layer
   precedence.
3. A member's own resolution is byte-identically unaffected by being listed in a workspace, and
   resolves identically whether checked out alone or inside the workspace — the repo-local
   operability principle in [org-config-resolution §3.3](../org-config-resolution/design.md#33-repo-local-operability)
   extends to workspace membership.

## Non-Goals

- Auto-discovery of nested manifests. Membership is explicit, identity-based inheritance only —
  no filesystem walking, no globs.
- The workspace as a policy or authority scope. The aggregate carries no locks, grants, or
  policy of its own (see [D4](#d4--aggregate-tier-position-and-authority)).
- Aggregating executable packages/artifacts. Deferred (see
  [Ratified Decisions — RD6](#rd6--oq6-packages-aggregation)).
- KG code-graph fan-in across workspace members. Tracked separately by the
  submodule-blindness observation on the code graph build surface, which should treat the
  membership declaration introduced here as its input rather than re-deriving membership on its
  own.

## Decisions

### D1 — Explicit, ordered membership; recursive through nested workspaces

A root declares its members as an explicit, ordered list of relative member identities, each
optionally flagged (e.g. optional-if-absent). No globs, no filesystem discovery. A suggestion-only
discovery aid may *propose* candidate members for the operator to confirm; it never writes an
unconfirmed member into the list itself.

A member that is itself a workspace root (its own member list is non-empty) is aggregated
**recursively** — its own members' effective config folds transitively into what the outer root
sees, following the same per-kind merge semantics ([D5](#d5--per-kind-merge-semantics)) at each
level. Cycle detection is mandatory (see [D10](#d10--failure-modes)); a cycle in the membership
graph is a hard error, never a silent truncation.

*Rejected:* implicit discovery by walking the filesystem for nested manifests — reintroduces the
same locality-based inheritance [org-config-resolution §3.1](../org-config-resolution/design.md#31-explicit-not-implicit)
already rejects for the non-workspace case.

### D2 — Membership is repo-authored only

The member list is protected exactly like repo identity: it may only be set by the root's own
repo-local, committed declaration. An imported layer (org/team/whatever the root itself extends)
cannot inject or modify the member list. This keeps "which repos this root aggregates" an
auditable, repo-owned fact rather than something a shared upstream layer can silently expand.

### D3 — Aggregation input is each member's effective, lock-resolved config

The root aggregates the **result** of each member's own resolution — its effective, lock-resolved
config — never the member's raw manifest re-interpreted by the root. This is member sovereignty:
the root never re-parses or re-applies a member's `extends` chain itself; it consumes what the
member itself already produced. This is also what makes Goal 3 (member resolution unaffected by
membership) mechanically true — the root is a pure downstream consumer, never an upstream input,
of a member's resolution.

*Rejected:* having the root re-resolve each member's full layer stack itself — doubles resolution
work, and risks the root and the member disagreeing about what "effective" means for that member
if their resolver versions or caches ever diverge.

### D4 — Aggregate tier position and authority

A single synthetic members-aggregate tier sits in the value-precedence ordering **above the
root's own imported `extends` layers and below the root's repo-local manifest** — root-explicit
declarations win conflicts with anything member-sourced, matching Goal 2's repo-over-layer
precedence.

The aggregate tier carries **zero authority**. It is a value-precedence-only slot, not a new rung
on the [config-distribution-model §15 D1a](../config-distribution-model/design.md#d1a--scope-carries-two-orderings-authority-rank--value-precedence-plus-source-and-kind)
AUTHORITY-RANK ordering. No member gains org/team-level authority merely by being aggregated into
a root; a member's own authority, if it has any, remains exactly what its own source-authority
derivation already grants it, unaffected by workspace membership. This keeps
[unified-config-profiles Q1](../unified-config-profiles/design.md#q1--scope-chain-reconciliation)
(the canonical scope-chain reconciliation) closed — the aggregate does not become a new entry in
that chain and does not need to be reconciled into it.

### D5 — Per-kind merge semantics

Aggregation applies different merge rules by kind:

- **Set kinds** (skills, agents, rules): union, order-stable, deduped by value.
- **Keyed maps** (stage profiles, execution-profile app-type facets, settings, features): keyed
  union. Identical content across members dedupes silently. Differing content under the same key
  is last-declared-member-wins (by declaration order in the member list), with a collision
  warning surfaced in `explain`/`verify` naming every contributing member ([D8](#d8--provenance)).
- **Sources**: union by source identity (`id`), **with an identity check**. If the same source
  `id` resolves to a different `url`/`ref` across members (or between a member and the root
  itself), that is a **hard error**, not a last-wins pick — silently choosing one member's URL for
  a shared source id is a supply-chain risk the resolver must refuse to paper over.
- **Hooks and MCP**: union-merge semantics are **generalized to the layer-merge itself**, not
  confined to the workspace-aggregation combiner — see the dedicated treatment in
  [Ratified Decisions — RD3](#rd3--oq3-hooksmcp-union-semantics--generalized-and-away-from-the-draft-recommendation)
  and [D12](#d12--hooksmcp-flag-consumption-at-projection-time).
- **Never aggregated**: repo identity, `extends`, packages/artifacts, KG config, observability
  config, work-tracking config, PR-source config, and any authority surface. These stay strictly
  per-repo; aggregating them would either duplicate identity or leak authority into a
  zero-authority tier ([D4](#d4--aggregate-tier-position-and-authority)).

### D6 — Lock model: member locks stay authoritative; root references them

Each member's own lock remains the **sole authority** for that member's resolved state. The root
lock never embeds, re-writes, or takes ownership of a member's lock content. Instead, the root
lock carries a **per-member unit** — one entry per declared member, recording that member's
identity and a digest of that member's own current lock state — and that per-member digest
participates in the root's `inputs_digest` exactly like any other local input (per
[config-distribution-model §15 D4/D5](../config-distribution-model/design.md#d4--staleness-is-content-hash-driver-events-ttl-becomes-a-review-nudge)).

Root staleness therefore has a first-class, distinguishable reason — **member drift** — separate
from a root manifest change or the root's own layer drift: "member `<x>`'s lock changed since the
root last resolved" is reported distinctly from "the root's own `inputs_digest` changed."

**This supersedes the §14 Q5 gitdir-pointer either-or sketch.** That sketch proposed a strict
either-or, git-`.git`-file style: a member resolved alone owns a real lock, but inside a workspace
the workspace "manages the aggregate" on the member's behalf — implying the workspace could
supersede or hold a member's lock state. That model is rejected: it would let the root
re-interpret or own member state, breaking member sovereignty ([D3](#d3--aggregation-input-is-each-members-effective-lock-resolved-config))
and the repo-local operability guarantee ([org-config-resolution §3.3](../org-config-resolution/design.md#33-repo-local-operability)
— a member checked out alone must resolve identically to the same member inside a workspace,
which is only true if the workspace never owns or substitutes for the member's own lock).
`config-distribution-model` §14 Q5 is annotated to point here rather than restate this decision.

### D7 — Offline contract preserved

The root reconstructs the aggregate from the root's own lock, the referenced member locks, and
the machine-local cache — no network required. `locked`/`frozen` resolution modes fail loudly on
detected member drift rather than silently resolving against stale member state, consistent with
the offline/reproducibility contract the rest of the lock model already provides.

### D8 — Provenance

Every member-sourced entry in the aggregate is attributable, in `explain` output, to the specific
member(s) that contributed it. Where two members collide on the same key under D5's keyed-map
rule, `explain` names both contributing members and the winner, not just the winner — the same
provenance discipline `explain` already provides for layer-stack field resolution elsewhere in the
config model.

### D9 — Projection scope

The root renders its enriched effective config into the root's own projection targets only —
member projections are untouched by root aggregation. The root's exact/prune projection behavior
and any config-scanning surface exclude member subtrees, so a root scan never treats a member's
own projected outputs as stray root-managed files.

A separate, broader **machine-wide refresh** (one that walks every project on a machine, not just
one workspace root) is itself an **explicit, opt-in invocation** — not the default scope of an
ordinary refresh — consistent with the sibling decision that reversed refresh's default from
machine-wide fan-out to current-project-only. When a machine-wide refresh *is* explicitly invoked
and it encounters a workspace root, it processes that root's members before aggregating the root,
in deterministic (declaration) order, so the root's own refresh within that pass sees already-fresh
member state.

### D10 — Failure modes

- **Absent member directory**: warn and resolve without that member; `verify` reports it. A
  freshly cloned root with no members checked out yet is an expected, non-fatal state.
- **Invalid member manifest**: fatal, unless that member entry is marked optional.
- **Duplicate membership**: error.
- **Membership cycle** (a member that is itself a workspace root whose member graph, followed
  recursively per [D1](#d1--explicit-ordered-membership-recursive-through-nested-workspaces),
  loops back to an ancestor root): error. A cycle is never silently truncated or deduplicated away.

### D11 — Member lock refresh is opt-in, not cascaded by default

A root sync, by default, refreshes **only the root itself** — its own imported layers and its own
manifest — and, if it detects member drift ([D6](#d6--lock-model-member-locks-stay-authoritative-root-references-them)),
**warns** about it. It does not touch any member's lockfile by default.

Cascading a member-lock refresh requires an **explicit flag** on the root sync invocation (e.g. a
`--members`-style opt-in). When passed, the same guardrails always apply:

- the cascade touches member **lockfiles only**, via each member's own standard, centralized
  resolve path — it never touches a member's manifest and never touches a member's projection;
- members are processed **serially**, in declaration order (the same order as
  [D1](#d1--explicit-ordered-membership-recursive-through-nested-workspaces)'s member list) — no
  parallel fan-out across members;
- **per-member failure isolation**: a member whose resolve fails during the cascade emits a
  warning and does not halt the rest of the root sync; the sync completes and reports a summary
  of per-member outcomes;
- `locked`/`frozen` modes never cascade, regardless of the flag — they are no-network,
  use-what's-recorded modes by definition.

*Rationale.* Cross-repo effects must be explicit in the invocation that causes them, mirroring the
concurrent decision to reverse the general refresh command's default from machine-wide fan-out to
current-project-only-by-default with an explicit all-projects opt-in ([D9](#d9--projection-scope)).
A root-scoped sync command silently reaching out and rewriting every member's lockfile is a
surprising, wide-blast-radius default for a command whose default scope is "this project." Making
the cascade explicit keeps the default narrow and predictable while still offering the
one-command, whole-workspace refresh as a deliberate, opt-in action.

See [Ratified Decisions — RD5](#rd5--oq5-root-sync-cascade-behavior-revised-during-authoring)
for the two rejected alternatives and the full history of this decision (it was revised once
during spec authoring).

### D12 — Hooks/MCP flag consumption at projection time

Projection **honors** the effective union/mask result computed by the generalized hooks/MCP
union-merge semantics ([RD3](#rd3--oq3-hooksmcp-union-semantics--generalized-and-away-from-the-draft-recommendation)).
This closes the gap where, today, nothing consumes hooks/MCP/settings flags at projection time —
projection is filesystem-driven from the agents-home scopes, and the flags affect only `explain`
output.

Enforcement is **opt-in and flag-gated in v1** — default off, matching today's filesystem-driven
projection behavior exactly until an operator opts in. A ratified path to default-on exists, but
flipping the default is gated on two prerequisites, not a target date:

1. a shadow-value verify check confirming no repo in scope carries a stray, injected explicit
   `false` left over from the manifest-injection defect that predates this spec (such a value
   would silently mask hooks/MCP a repo actually relies on, the moment enforcement turned on for
   that repo); and
2. migration guidance (the existing config-migration tooling) for cleaning any such stray values
   before that repo's enforcement flips on.

*Rationale.* The payout org-rollout use case this spec targets is exactly "org sets `hooks: true`,
every repo inherits it" — that payoff only lands if projection actually consumes the union result,
so defining consumption now (rather than deferring it to a follow-up spec) captures real value.
But flipping enforcement on by default immediately would silently change projection behavior for
any repo still carrying a stray legacy `false` from the pre-fix injection defect — hence opt-in v1
with an explicit, prerequisite-gated path to default-on, not an open-ended "later."

## Requirements

R1. When a root's member list is empty or absent, root resolution produces byte-identical output
    to non-aggregated resolution today.

R2. A member's own resolution is unaffected by workspace membership — identical whether the
    member is checked out alone or listed in a root's member list.

R3. Aggregate staleness is driven only by a change to a member's manifest, a change to a member's
    lock, or a change to the root's declared member list — never by a clock.

R4. `explain` attributes every member-sourced entry to its contributing member(s) and shows both
    parties on a collision; `verify` reports membership-level issues (absent member, invalid
    manifest, duplicate membership, membership cycle, member drift).

R5. Root projection never double-projects a member-sourced entry; the merge result is
    deterministic given the same member and root inputs.

R6. All lock mutations remain centralized through each unit's standard resolve path; `locked`/
    `frozen` resolution is byte-identical across repeated runs with no member or root drift.

R7. Hooks/MCP union-merge semantics ([D5](#d5--per-kind-merge-semantics),
    [RD3](#rd3--oq3-hooksmcp-union-semantics--generalized-and-away-from-the-draft-recommendation))
    apply uniformly at every scope in the value-precedence chain, not only at the
    workspace-aggregation combiner: list values union; an explicit scoped `false` masks everything
    unioned from lower-precedence scopes while higher scopes can still add after the mask; an
    explicit scoped `true` is a transparent union marker with no masking effect.

R8. Root sync defaults to root-only refresh plus a member-drift warning; an explicit flag cascades
    a member-lock refresh honoring every guardrail in [D11](#d11--member-lock-refresh-is-opt-in-not-cascaded-by-default)
    (locks-only, serial, per-member failure isolation, no cascade under `locked`/`frozen`).

## Ratified Decisions (2026-08-12)

The design investigation that produced this spec's base decisions (D1–D10 in their original
draft form) left eight questions open. The maintainer ruled on all eight before this spec was
published; this section is the record of those rulings, replacing what would otherwise be an
Open Questions section. Where a ruling changed the draft's own recommendation or a draft decision's
text, both the final ruling and the rejected alternative are recorded here; the corresponding
Decision above already carries the ruled, final text.

### RD1 — OQ1: root/member lock relationship

**Question raised:** should the root own a workspace-level lockfile that aggregates resolved
state across members, or should each repo own its lock exclusively — and if a workspace exists,
does it manage the aggregate the way a git worktree's `.git` file points at the parent's real
`.git` directory (the `config-distribution-model` §14 Q5 framing)?

**Ruled:** member locks stay authoritative. The root lock references them via a per-member unit
plus a member-drift staleness reason ([D6](#d6--lock-model-member-locks-stay-authoritative-root-references-them)).
This **supersedes** the §14 Q5 gitdir-pointer either-or sketch outright, rather than choosing one
side of that either-or — the gitdir analogy implied the workspace could take over a member's lock,
which member sovereignty ([D3](#d3--aggregation-input-is-each-members-effective-lock-resolved-config))
forecloses. `config-distribution-model` §14 Q5 is annotated to point here.

### RD2 — OQ2: aggregate tier position

**Ruled (default accepted):** the aggregate tier sits above the root's imported `extends` layers
and below the root's repo-local manifest ([D4](#d4--aggregate-tier-position-and-authority)). No
alternative was seriously contested — this is exactly what the design investigation's draft
already proposed, and it is the placement that makes Goal 2 (root-explicit wins conflicts) true
by construction.

### RD3 — OQ3: hooks/MCP union semantics — generalized, and away from the draft recommendation

**Question raised:** should union-vs-replace semantics for hooks/MCP be confined to the
workspace-aggregation combiner (the draft's own recommendation), or generalized to the layer-merge
model as a whole?

**Ruled against the draft's recommendation:** generalized to the layer-merge itself. Every scope
in the value-precedence chain — not just workspace aggregation — now merges hooks/MCP by this
rule: list values union across scopes; an explicit boolean `false` at a scope **masks** (clears)
everything unioned in from lower-precedence scopes, while higher-precedence scopes can still add
after the mask; an explicit boolean `true` is a transparent union marker. See
[D5](#d5--per-kind-merge-semantics), [R7](#requirements), and [D12](#d12--hooksmcp-flag-consumption-at-projection-time).

**Migration note (required — see Done Criteria):** any existing stack where a higher layer's
scalar `false`/`true` today behaves as a **replace** (rather than the new mask-then-allow-further-union
semantics) will observe a behavior change once this ships. This is called out explicitly rather
than left implicit, because it is the one decision in this spec with a real backward-compatibility
edge even outside the workspace-aggregation feature this spec otherwise scopes to.

**Related finding folded in.** An audit of the shipped hooks/MCP/settings surface found that,
today, **nothing consumes** hooks/MCP at projection time — projection is filesystem-driven
directly from the agents-home scopes, the hooks/MCP flags affect only `explain` output, and prior
schema documentation incorrectly claimed an explicit `false` disables projection (it does not,
today). [D12](#d12--hooksmcp-flag-consumption-at-projection-time) defines flag consumption going
forward rather than deferring it to a follow-up spec, on the maintainer's recommendation that the
org-rollout payoff this spec targets requires it — but enforcement ships opt-in/flag-gated in v1,
with a prerequisite-gated (not date-gated) path to default-on, precisely because of the injection
defect this finding surfaced.

### RD4 — OQ4: source declaration aggregation

**Ruled:** member-declared sources union into the aggregate **with an identity check** — the same
source `id` resolving to a different `url`/`ref` across members (or against the root) is a hard
error, not a last-wins pick. See [D5](#d5--per-kind-merge-semantics). Rationale: sources are
supply-chain-sensitive; silently letting one member's URL win for a shared id would let a
compromised or merely mistaken member redirect what every other member (and the root) resolves
against.

### RD5 — OQ5: root sync cascade behavior (revised during authoring)

**Question raised:** when a root syncs, should it cascade a refresh of every member's lock as
part of one workspace-wide operation, or leave members alone?

**First ruling (superseded during spec authoring):** always cascade by default, with an opt-out
flag and config knob.

**Final ruling:** cascade is **opt-in**, not opt-out. Root sync by default refreshes only the
root and warns on member drift; an explicit flag (e.g. `--members`) triggers the cascade, with
guardrails unchanged from the first ruling (member lockfiles only, via the centralized resolve
path; serial, declaration-order processing; per-member failure isolation with a summary;
`locked`/`frozen` never cascade). See [D11](#d11--member-lock-refresh-is-opt-in-not-cascaded-by-default).

**Rationale for the revision:** cross-repo effects must be explicit in the invocation that causes
them — the same principle behind a concurrent, separate decision that reversed the general
machine-wide refresh command's default from fan-out-across-every-project to current-project-only,
requiring an explicit all-projects flag. A root sync silently touching every member's lockfile is
the same shape of surprising default this principle rules out.

**Rejected alternatives (two, both considered and both rejected):**
1. *Always-cascade-by-default with opt-out* (the first ruling above) — rejected on reflection
   because it makes an ordinarily root-scoped command implicitly cross-repo unless the operator
   remembers to opt out, which is exactly the surprising-default shape the revision rejects.
2. *Read-only-always, no cascade capability at all* — rejected because it fully forecloses the
   one-command, whole-workspace refresh convenience that motivated this spec; the maintainer wants
   that capability available as a deliberate, explicit action, not removed entirely.

**Why the cascade is safe to offer at all, even opt-in.** Locks are, today, the only
machine-written file in the resolved-config model (manifests are always human/repo-authored).
An explicit cascade can therefore only ever refresh regenerable, machine-derived state — it can
never corrupt a human-authored manifest. That bounded blast radius is why the capability is safe
to expose as an explicit, opt-in action even though the earlier always-on framing was rejected as
a *default*.

### RD6 — OQ6: packages aggregation

**Ruled (default accepted):** deferred. Aggregating executable packages/artifacts across members
is out of scope for this spec (see [Non-Goals](#non-goals)); the per-kind merge model in
[D5](#d5--per-kind-merge-semantics) explicitly excludes packages from what gets aggregated.

### RD7 — OQ7: nested workspaces

**Ruled (default accepted):** recursive, with mandatory cycle detection. A member that is itself
a workspace root has its own members folded in transitively; a membership-graph cycle is a hard
error. See [D1](#d1--explicit-ordered-membership-recursive-through-nested-workspaces) and
[D10](#d10--failure-modes).

### RD8 — OQ8: aggregate authority

**Ruled (default accepted):** the aggregate is value-tier only, with zero authority — it is not a
new rung on the AUTHORITY-RANK ordering, and [unified-config-profiles Q1](../unified-config-profiles/design.md#q1--scope-chain-reconciliation)
stays closed; nothing in this spec reopens that reconciliation. See
[D4](#d4--aggregate-tier-position-and-authority).

## Done Criteria

DC1. A root with an empty or absent member list produces resolution output byte-identical to
     today's non-aggregated resolution (R1).

DC2. A member repo's resolved effective config is identical whether resolved standalone or as a
     listed member of a root workspace (R2).

DC3. Aggregate staleness reacts to a member manifest change, a member lock change, and a root
     member-list change — and does not react to elapsed time alone (R3).

DC4. `explain` shows per-entry member attribution and both parties on a collision; `verify`
     surfaces every membership failure mode in [D10](#d10--failure-modes), including member drift
     and membership cycles (R4).

DC5. Repeated root resolution against unchanged root and member inputs is deterministic —
     including under `locked`/`frozen` — with no double-projection of member-sourced entries
     (R5, R6).

DC6. The hooks/MCP generalized union-merge ships with its documented migration note (RD3): any
     stack relying on the old scalar-replace behavior is identifiable before the change lands, and
     flag-consumption enforcement ([D12](#d12--hooksmcp-flag-consumption-at-projection-time)) is
     verifiably off by default, with the shadow-value verify check and migration guidance in place
     as the named prerequisites for any later default-on flip.

DC7. A default root sync provably never mutates a member's lockfile (only warns on drift); an
     explicit cascade flag provably does, and only through each member's standard resolve path,
     serially, with per-member failure isolation and a summary report, and never under `locked`/
     `frozen` (D11, R8).

## Deferred

- Executable package/artifact aggregation across members (RD6).
- Any change to the AUTHORITY-RANK ordering or the canonical scope-chain reconciliation — this
  spec deliberately keeps that surface untouched (RD8).
- Folding the machine-local project identity registry ([home-config-portability](../home-config-portability/design.md))
  and this spec's member-lock referencing model into one artifact. They remain distinct concepts;
  `home-config-portability` FORK-4 already frames the identity registry as forward-compatible with
  a future workspace aggregate, but no unification is a deliverable here.
- The exact transitivity boundary a *pinned manifest* implies for a workspace root's member set
  (`distributable-config-manifest` F5) — that spec's own transitive-pin question is bounded by,
  but not resolved by, this spec's per-member lock-referencing model in
  [D6](#d6--lock-model-member-locks-stay-authoritative-root-references-them).

## Appendix: payout migration sketch

The `payout`-style root that motivated this spec currently mirrors its members' capability via a
hand-maintained set of app-type layers extended one by one. Under this spec, that root:

- drops its hand-listed app-type layers entirely;
- keeps only its baseline org `extends` entry (the fresh-clone baseline every repo already
  inherits);
- adds one ordered member list naming its member projects.

Member repos require zero changes — D3's member-sovereignty guarantee means their own resolution
is untouched. The transition is additively safe: any layer a member already pulled in via the
root's old hand-listed set simply dedupes against the same content now arriving through
aggregation, per D5's set-union and keyed-union rules.

## Related

- [config-distribution-model §15](../config-distribution-model/design.md#15-coherence-model-v2-scopes-sources-units-and-the-lock) —
  the units/scope/source/kind/lock model this spec's aggregate tier and per-member lock
  referencing extend.
- [config-distribution-model §14 Q5](../config-distribution-model/design.md#q5-lockfile-for-workspace-level-installs) —
  superseded by this spec (see RD1); annotated to point here.
- [org-config-resolution §3, §9](../org-config-resolution/design.md#3-design-principles) —
  the explicit/identity-based/repo-local-operability/optional-workspace principles this spec's
  membership model, member sovereignty, and non-authority stance all derive from.
- [unified-config-profiles Decision 1, Q1](../unified-config-profiles/design.md#decision-1--source-authority-is-derived-never-self-declared) —
  the authority-derivation model this spec's zero-authority aggregate tier stays outside of.
- [home-config-portability FORK-4](../home-config-portability/design.md#fork-4--relationship-to-15-q5-workspace-lockfile--owner-ratified-2026-06-27--keep-separate-forward-compat) —
  the machine-local identity registry this spec's member-lock referencing model stays distinct
  from, by design.
- [distributable-config-manifest F5](../distributable-config-manifest/design.md#f5--versioning--pinning-does-a-pinned-manifest-pin-its-referenced-units-transitively) —
  the transitive-pin question this spec's per-member lock referencing bounds without resolving.
