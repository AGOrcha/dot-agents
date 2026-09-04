# Workspace Member Aggregation

**Spec ID**: workspace-member-aggregation
**Status:** ratified — decisions locked 2026-08-12 (all open questions resolved before
publication); **amended 2026-08-12** — a follow-up investigation into hooks/MCP/settings (never
classified in org-config-resolution's merge-category table, and consumed nowhere but `explain`)
produced a second ratification round replacing the original hooks/MCP boolean-mask model with a
uniform name-selection + exclusion-token model. See [Ratified Decisions](#ratified-decisions-2026-08-12).

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
- The general layer-merge model whose hooks/MCP/rules/settings merge-category omissions this
  spec's amendment round fills in is
  [org-config-resolution §7.2/§7.3](../org-config-resolution/design.md#72-proposed-merge-categories)
  — that section is amended to point back here rather than restate the semantics.

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
   execution-profile app types, features). `settings` is excluded by design — see
   [D5](#d5--per-kind-merge-semantics) — as a per-repo platform-preference toggle with no
   cross-repo meaning to aggregate.
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

- **Set kinds** (skills, agents, rules, hooks, MCP): union, order-stable, deduped by value, with
  exclusion-token masking. Rules, hooks, and MCP were originally drafted with bespoke or
  unspecified treatment; the amendment round folded all five into one uniform name-selection
  union model — see [D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model) (the
  selection-list model) and [D13](#d13--exclusion-tokens-the-uniform-masking-successor) (the
  masking mechanics). This union rule is **generalized to the layer-merge itself**, not confined
  to the workspace-aggregation combiner — every scope in the value-precedence chain merges these
  five kinds the same way.
- **Keyed maps** (stage profiles, execution-profile app-type facets, features): keyed
  union. Identical content across members dedupes silently. Differing content under the same key
  is last-declared-member-wins (by declaration order in the member list), with a collision
  warning surfaced in `explain`/`verify` naming every contributing member ([D8](#d8--provenance)).
- **Sources**: union by source identity (`id`), **with an identity check**. If the same source
  `id` resolves to a different `url`/`ref` across members (or between a member and the root
  itself), that is a **hard error**, not a last-wins pick — silently choosing one member's URL for
  a shared source id is a supply-chain risk the resolver must refuse to paper over.
- **MCP server definitions**: promoted from a whole-file link to ordinary mergeable keyed content
  — see [D15](#d15--mcp-server-definitions-are-resolvable-config-content) — merged by server name
  *before* the set-kind selection filter above applies to which server names are included.
- **Never aggregated**: repo identity, `extends`, packages/artifacts, KG config, observability
  config, work-tracking config, PR-source config, `settings`, and any authority surface. `settings`
  is a scalar per-repo platform-preference toggle with no natural name space (see
  [D16](#d16--legacy-grandfathering-and-deprecation-window) and Ratified Decisions RD13) — it is
  not shared capability content the way the five set-kinds above are, so cross-member aggregation
  does not apply to it, the same as it does not apply to repo identity or `extends`. These stay
  strictly per-repo; aggregating them would either duplicate identity, leak authority into a
  zero-authority tier ([D4](#d4--aggregate-tier-position-and-authority)), or aggregate a
  preference that has no cross-repo meaning.

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

### D12 — Hooks, MCP, and rules become a uniform name-selection model

Hooks and MCP are reclassified from their present ad hoc vocabulary into the same
**name-selection list** model skills and agents already use, and rules — which already carried a
name-selection *shape* but populated it with a stale scope-token artifact rather than real names —
is folded into the identical model:

- **Hooks** select **hook bundle names** (a named, versioned unit in the hooks store), not
  Claude-Code hook *event* names. Event names (`PreToolUse`, `PostToolUse`, …) are an
  implementation-internal firing vocabulary, not a stable cross-platform resource identifier —
  two unrelated bundles can fire on the same event, so an event name cannot serve as a selection
  key the way a bundle name can.
- **MCP** selects **server names** — this was already the right vocabulary in practice, it was
  simply never consumed for selection; see
  [D15](#d15--mcp-server-definitions-are-resolvable-config-content).
- **Rules** selects **rule names**, retiring the legacy scope-token artifact (a literal
  `"project"`/`"global"` string that leaked in from an early generator, never a real rule
  identifier).

**All five set-kinds — skills, agents, rules, hooks, MCP — merge via the same union rule at every
scope in the value-precedence chain**, not only inside workspace aggregation: `absent` defers to
lower scopes; an empty list `[]` is equivalent to `absent`, never a mask; a plain name adds to the
accumulated union; masking is expressed only via the exclusion tokens in
[D13](#d13--exclusion-tokens-the-uniform-masking-successor). This retires the original boolean
`true`/`false` masking model (kept below for history; see the superseded notice on
[RD3](#rd3--oq3-hooksmcp-union-semantics--generalized-and-away-from-the-draft-recommendation)).

**Consumption is default-on, with no flag gate — safe by construction.** The selection filter for
a given set-kind only ever activates once **at least one scope in the resolved chain declares a
list** for it; a project whose entire chain never declares hooks/MCP/rules at all sees unchanged,
filesystem-driven projection behavior, identical to today. Because the filter is opt-in-by-declaration
rather than opt-in-by-flag, there is no behavior to gate: nothing changes for any project until an
operator actually writes a list somewhere in that project's chain. This is what let the earlier
flag-gated-enforcement design (kept as history in RD3/RD9) be retired — the flag existed only
because an explicit `false` could silently mask content a project relied on without the project
ever having declared anything; `false` as a masking primitive is retired by D13, so there is
nothing left for a flag to guard against.

*Rationale.* org-config-resolution §7.2/§7.3 never actually classified hooks/mcp/settings as a
merge category — it was an omission, not a considered "replace" choice — and only skills/agents
are consumed at project-install time today. Generalizing all five set-kinds to one uniform model
closes that gap with a single rule instead of five bespoke ones, and gives hooks/rules a selection
vocabulary that is genuinely stable across platforms (a bundle/rule name) instead of an event name
or a stray scope token that happened to leak into the field.

*Rejected:* keeping hooks keyed by event name and rules keyed by scope token — both are accidents
of how those fields were first generated, not deliberate selection vocabularies, and neither
identifies a specific, addressable resource the way a name does.

### D13 — Exclusion tokens: the uniform masking successor

Every set-kind list (skills, agents, rules, hooks, MCP) may contain, alongside plain names, two
token forms:

- `!name` — **excludes** that specific name.
- `!*` — **masks**: clears every name accumulated so far from lower-precedence scopes.

This is the direct successor to explicit boolean `false` masking, generalized uniformly across all
five kinds instead of being specific to hooks/MCP, and made composable with continued inclusion
instead of being a dead end.

**Evaluation order.** Scopes are processed in the existing value-precedence order (lowest to
highest — product → user-local → org → team → repo-imported → repo-local → project-local overlay
→ runtime, per
[config-distribution-model §15 D1a](../config-distribution-model/design.md#d1a--scope-carries-two-orderings-authority-rank--value-precedence-plus-source-and-kind)),
carrying forward one accumulated set. At each scope, in order:

1. If that scope's own list contains `!*`, clear the accumulated set (drop everything carried in
   from lower scopes).
2. Add every plain name that scope declares to the accumulated set.
3. Remove every `!name` that scope declares from the accumulated set.

The result after processing a scope becomes the input carried into the next (higher) scope. This
means: a higher scope's `!name` always wins over a lower scope's inclusion (step 3 runs after
every lower scope's step 2 has already contributed); a higher scope can always **re-include** a
name a lower scope excluded — via `!*` or via `!name` — simply by listing the plain name in its
own step 2, because nothing downstream of that scope removes it unless an even-higher scope
excludes it again. `!*` followed by plain names *within the same scope's own list* reads naturally
as "reset to exactly this list" — the closest analog to the old `false`-then-re-`extends` pattern,
but expressible in one declaration instead of two.

**Same-scope contradiction is a validation error.** A single scope's own list declaring both
`name` and `!name` for the same literal name is rejected at validation time, not silently resolved
by an implicit tie-break — an author must pick one.

*Rejected:* letting a bare `false` continue to serve as the mask token — `false` cannot compose
with "and also add these back," which is exactly the dead end the hooks/MCP audit surfaced (a
project relying on a higher layer's `false` today has no way to further layer additions on top of
it). `!*` is a token *within* the same list type, so it composes.

### D14 — De-selection is filter-at-projection plus provenance-aware bucket pruning

De-selecting a name (via the mechanisms in D13) has two effects, not one:

1. **Filter-at-projection** (always): a de-selected name stops being rendered into any projection
   target, immediately, regardless of origin.
2. **Bucket pruning** (conditional on provenance — see the table below): whether the underlying
   store copy is also deleted depends on where that copy actually came from.

**The governing predicate is provenance, not physical location.** Content whose canonical copy is
*machine-fetched and re-materializable* — recorded in a lock with a digest and a source it can
re-resolve from — may be pruned by de-selection; the next selection simply re-fetches it. Content
whose canonical copy is *human-provenance* — the store copy is the only copy, because a human
either authored it directly or moved it there — is **never** deleted by de-selection, no matter
which scope bucket it physically lives in. De-selecting human-provenance content only ever removes
it from *this* project's projection; the content stays in the store and `verify` reports it as
present-but-unselected so it is never silently invisible.

| Origin | Example | Only copy? | De-selection effect |
|---|---|---|---|
| Source-fetched / lock-pinned | a skill resolved from a declared remote source | No — re-resolvable from the lock's recorded source + digest | Filter, **and** prune the project-scope bucket copy; re-materializes automatically on re-selection |
| Promoted from a repo | a skill or agent moved into the store by the promote operation, which replaces the repo-local copy with a link back to the store | **Yes** — the repo-local copy is now only a link; the store copy is the only real content | Filter only. The store copy is never deleted by de-selection. `verify` reports it as promoted-and-unselected |
| Hand-authored directly in the store | a skill written straight into a global- or project-scope store directory, with no repo-local origin at all | **Yes** — there was never another copy | Filter only, same as promoted content |

**A second, orthogonal, hard invariant: pruning may only ever touch project-scope buckets, never
global-scope buckets, regardless of provenance.** A repo de-selecting a name must never delete
anything under a global-scope bucket, because other repos may depend on that same global content —
even if that global entry happens to be machine-fetched and in principle re-materializable, a
*different* project's de-selection is never the trigger that prunes it. Global-scope content is
removed, if ever, only by an operation scoped to global itself, never as a side effect of one
project's manifest edit.

*Rationale.* Source-fetched content is exactly the kind of state D11's cascade decision already
treats as safe to mutate automatically (lock-backed, re-materializable); promoted and
hand-authored content is exactly the kind of state the rest of this model treats as human-authored
and therefore untouchable by automated pruning ([D2](#d2--membership-is-repo-authored-only)
applies the same "human-authored is protected" posture to the member list itself). Unifying
promoted and hand-authored content under one "human-provenance, never pruned" rule — rather than
special-casing promote — keeps the invariant an operator has to remember simple: *pruning only
ever removes what could be re-fetched.*

*Rejected — move-back/archive semantics for promoted content on de-selection.* Physically
relocating a de-selected promoted resource (back into a repo, or into an archive location) was
considered and rejected: it reintroduces the same symlink-swap-with-rollback complexity the
promote operation itself already has to defend against (cross-filesystem rename failures,
partial-state recovery), for a case the simpler filter-only answer already fully protects.
Filter-plus-verify-report achieves the same safety with no new failure surface.

### D15 — MCP server definitions are resolvable config content

MCP server definitions are promoted to being ordinary resolvable config content, merged the same
way any other keyed value resolves: **scope files and any layer/source-contributed server
definitions merge by server name**, following the existing value-precedence chain (a
higher-precedence scope's definition for a given server name wins over a lower one, the same rule
other keyed maps already use). **Then** the selection list and its exclusion tokens
([D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model)/[D13](#d13--exclusion-tokens-the-uniform-masking-successor))
filter which of those merged server names are actually included. **Then** the filtered result
renders into the platform's MCP artifact.

This **retires the current whole-file, project-shadows-global behavior**: today, a project-scope
MCP file and a global-scope MCP file are never merged — the first one found (project first, global
second) is linked in its entirety, so a project file that lacks a server the global file defines
silently never sees that server. Under this decision, both contribute at the server-definition
level and the union (minus exclusions) renders as one artifact.

The same model extends to **every platform's MCP artifact**, not only the primary one — any
platform with its own MCP config path renders from the same merged-then-filtered resolution.

*Rationale.* Once hooks/rules/skills/agents are name-selectable content, treating MCP server
definitions as anything less — a single opaque file one scope happens to own — is the odd one out.
Server definitions are exactly the shape of content the rest of the model already knows how to
merge (keyed, mergeable, filterable); there is no reason MCP alone should stay a whole-file link.

*Rejected:* keeping the whole-file link and only adding selection filtering on top of whichever
file wins — this preserves the shadow bug (a project file silently hides global servers it doesn't
itself list) instead of fixing it, and selection filtering on top of a shadowed file would produce
confusing results (a server "selected" that the shadowed-in file never even defined).

### D16 — Legacy grandfathering and deprecation window

Two categories of existing, pre-amendment state are grandfathered rather than broken outright:

- **Legacy settings-backed hooks** (a hook expressed only inside a platform's own settings file,
  with no corresponding bundle name) remain **always-on** — exempt from the new selection filter —
  **only for a stated deprecation window**. After that window, a settings-backed hook must be
  converted to a named bundle to remain selectable; an unconverted one stops firing once the
  window closes, and `verify` surfaces every settings-backed hook with no corresponding bundle
  name well before the window closes so the conversion is never a surprise.
- **Deprecated bool forms** (`true`/`false` previously declared on hooks/MCP) are
  **accepted-with-warning indefinitely**, with no forced cutover date — matching the posture the
  existing config-migration path already takes toward other superseded manifest shapes: warn
  forever rather than hard-break an old manifest.

Migration of existing declared state follows a fixed, per-field table (idempotent — re-running
produces no further changes):

| Existing value | Migrates to | Warned? | Why |
|---|---|---|---|
| `true` (hooks or MCP) | absent | No | `true` meant "everything," which is exactly what `absent` also now means by default; behaviorally a no-op |
| `false` (hooks or MCP) | absent | **Yes** | `false` used to mask; `absent` does not. A stray `false` injected by the pre-amendment manifest-injection defect and a deliberate mask are indistinguishable after the fact, so migration cannot guess — it takes the safe (non-masking) reading and warns so a human can re-express a genuine mask as `!*` if that was the real intent |
| event-name list (hooks) | dropped tokens (empty/absent) | **Yes** | event names are not bundle names under the new vocabulary; keeping them would silently select nothing (near-certain to match no real bundle) |
| scope-token list (rules: `"project"`/`"global"`) | dropped tokens (empty/absent) | **Yes** | scope tokens are not rule names; same failure mode as event-name hooks |

**Sequencing constraint.** The existing scalar-scoped shadow-value verify check (the audit that
first surfaced stray injected `false` values) must run its audit **before** hooks/MCP flip from
scalar to the set-union model above — the migration table's `false → absent + warn` row depends on
that audit already having enumerated which repos carry a stray value, so the warning is accurate
and actionable at flip time rather than a blanket, unhelpful notice.

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

R7. Skills, agents, rules, hooks, and MCP merge via the same set-union-with-exclusion-tokens rule
    ([D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model),
    [D13](#d13--exclusion-tokens-the-uniform-masking-successor)) at every scope in the
    value-precedence chain, not only inside workspace aggregation: `absent` defers to lower
    scopes; `[]` is equivalent to `absent`, never a mask; plain names union; `!name` and `!*`
    exclude/mask per D13's evaluation order, and a higher scope can always re-include a name a
    lower scope excluded.

R8. Root sync defaults to root-only refresh plus a member-drift warning; an explicit flag cascades
    a member-lock refresh honoring every guardrail in [D11](#d11--member-lock-refresh-is-opt-in-not-cascaded-by-default)
    (locks-only, serial, per-member failure isolation, no cascade under `locked`/`frozen`).

R9. MCP server definitions merge by name across every contributing scope, layer, and source
    *before* the selection filter applies; the rendered MCP artifact is never a whole-file link,
    for any platform's MCP path ([D15](#d15--mcp-server-definitions-are-resolvable-config-content)).

R10. Projection is byte-identical to today's behavior for any project whose resolved chain never
     declares a hooks/MCP/rules list at any scope. The selection filter activates only once at
     least one scope opts in by declaring a list — this is what makes default-on enforcement
     behavior-preserving by construction, with no flag required
     ([D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model)).

R11. De-selection prunes only machine-fetched, re-materializable bucket content, and only from
     project-scope buckets; human-provenance content (promoted or hand-authored) and all
     global-scope content are never deleted by a de-selection — filter-only in both cases — and
     every filter-only outcome is reported by `verify`
     ([D14](#d14--de-selection-is-filter-at-projection-plus-provenance-aware-bucket-pruning)).

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

> **Superseded by RD9/RD10 in the amendment round below.** The boolean `true`/`false` masking
> model this entry describes, and the flag-gated-enforcement design it produced
> ([D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model)'s prior form), were
> retired by the follow-up investigation into hooks/MCP/settings. Kept verbatim for history; do
> not implement against this entry.

**Question raised:** should union-vs-replace semantics for hooks/MCP be confined to the
workspace-aggregation combiner (the draft's own recommendation), or generalized to the layer-merge
model as a whole?

**Ruled against the draft's recommendation:** generalized to the layer-merge itself. Every scope
in the value-precedence chain — not just workspace aggregation — now merges hooks/MCP by this
rule: list values union across scopes; an explicit boolean `false` at a scope **masks** (clears)
everything unioned in from lower-precedence scopes, while higher-precedence scopes can still add
after the mask; an explicit boolean `true` is a transparent union marker. See
[D5](#d5--per-kind-merge-semantics), [R7](#requirements), and [D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model).

**Migration note (required — see Done Criteria):** any existing stack where a higher layer's
scalar `false`/`true` today behaves as a **replace** (rather than the new mask-then-allow-further-union
semantics) will observe a behavior change once this ships. This is called out explicitly rather
than left implicit, because it is the one decision in this spec with a real backward-compatibility
edge even outside the workspace-aggregation feature this spec otherwise scopes to.

**Related finding folded in.** An audit of the shipped hooks/MCP/settings surface found that,
today, **nothing consumes** hooks/MCP at projection time — projection is filesystem-driven
directly from the agents-home scopes, the hooks/MCP flags affect only `explain` output, and prior
schema documentation incorrectly claimed an explicit `false` disables projection (it does not,
today). [D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model) defines flag consumption going
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

### Amendment round 2 — hooks/MCP/settings reclassification (2026-08-12, follow-up investigation)

A follow-up investigation, conducted after this spec's initial ratification, found that
hooks/mcp/settings had never actually been classified in org-config-resolution §7.2/§7.3; that
only skills/agents are consumed at project-install time today; that the existing hooks list
vocabulary means Claude-Code hook *event* names, not bundle names; and that MCP already means
server names but projects via a whole-file symlink that lets one scope's file shadow another's
entirely rather than merging. These findings were not anticipated by the original eight open
questions and produced their own maintainer rulings, recorded here as RD9–RD14.

#### RD9 — Hooks/MCP/rules become a uniform name-selection model (supersedes event-name/scope-token vocabulary)

**Ruled:** hooks and MCP are reclassified into the same name-selection list model skills/agents
already use — hooks select hook bundle names, MCP selects server names (already the right
vocabulary, just never consumed) — and rules, whose field already carried a name-selection *shape*
but was populated with a stale scope-token artifact, is folded into the identical model. All five
set-kinds merge `CategorySetUnion` at every scope in the value-precedence chain. See
[D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model).

**Rationale.** org-config-resolution §7.2/§7.3 never classified hooks/mcp/settings as a merge
category at all — an omission, not a deliberate "replace" choice. Only skills/agents are consumed
at install time; hooks/MCP/settings flags affect only `explain` output today. The hooks list's
existing content is literally Claude-Code hook *event* names (`PreToolUse`, `PostToolUse`, …),
surfaced from either a bundle-directory scan or, as a fallback, keys of a legacy settings-based
hooks map — neither a stable, cross-platform, addressable resource identifier. MCP already stores
server names in the field but the field was never consumed for selection.

**Rejected:** leaving hooks keyed by event name — an implementation-internal firing vocabulary
where two unrelated bundles can share an event, so it cannot serve as a selection key the way a
bundle name can; and leaving rules keyed by its legacy scope-token artifact, which never named an
actual rule to begin with.

#### RD10 — Exclusion tokens replace explicit `false` as the masking primitive, uniformly across all five set-kinds

**Ruled:** `!name` (targeted exclusion) and `!*` (full mask, the successor to explicit `false`)
apply uniformly to skills, agents, rules, hooks, and MCP. Precedence: scopes are evaluated
lowest-to-highest in value-precedence order, each scope first applying its own `!*` (if present)
to clear the running accumulation, then adding its own plain names, then removing its own
`!name` exclusions; a higher scope's exclusion always wins over a lower scope's inclusion, and a
higher scope can always re-include a name a lower scope excluded by simply listing it. See
[D13](#d13--exclusion-tokens-the-uniform-masking-successor) for the full algorithm and the
same-scope-contradiction validation rule.

**Rationale.** Explicit `false` could mask but could never compose with "and also add these back
at a higher scope" — a dead end the hooks/MCP audit surfaced directly. A token *within* the list
type composes naturally with continued inclusion, and generalizing it to all five set-kinds (not
just hooks/MCP) gives every set-kind the same masking vocabulary instead of a hooks/MCP-specific
special case.

**Rejected:** keeping `false` as the mask primitive and simply generalizing it to more fields —
rejected because the composability gap is the actual problem, not the field count; generalizing a
broken primitive would have generalized the dead end too.

#### RD11 — De-selection: filter-at-projection plus provenance-aware bucket pruning, with a human-provenance carve-out

**Ruled:** de-selecting a name always filters it out of projection. Whether the store copy is also
pruned depends on provenance: source-fetched/lock-pinned content (re-materializable from the lock)
is prunable; content promoted from a repo or hand-authored directly in the store is filter-only
and never destroyed by de-selection, because in both cases the store copy is the only copy. As an
orthogonal hard invariant, pruning may only ever touch project-scope buckets — never global-scope
buckets — regardless of provenance, because a global entry may be depended on by repos other than
the one doing the de-selecting. See
[D14](#d14--de-selection-is-filter-at-projection-plus-provenance-aware-bucket-pruning) for the full
per-origin table.

**Rationale.** The maintainer's own framing: source-derived content is cache/lock-backed and
re-materializable, so pruning it loses nothing. That framing does not extend to promoted content —
the promote operation replaces a repo-local copy with a link back to the store, making the store
copy the only real copy, so pruning it would be a genuine, unrecoverable data loss, not a cache
eviction. Hand-authored store content has the same only-copy property by construction. Both are
folded under one "human-provenance, never pruned" rule rather than two special cases.

**Rejected — move-back/archive semantics for promoted content.** Considered and rejected in favor
of filter-only: physically relocating a de-selected promoted resource on de-selection would
reintroduce the same symlink-swap-with-rollback failure surface (cross-filesystem rename failures,
partial-state recovery) the promote operation itself already has to defend against, for a case
filter-only already fully protects with no new machinery.

#### RD12 — MCP renders from the full effective resolution, not a whole-file link

**Ruled:** MCP server definitions become resolvable config content — scope files and any
layer/source-contributed server definitions merge by server name, following the standard
value-precedence chain, **then** the selection list and its exclusion tokens filter which merged
server names are included, **then** the filtered result renders into the platform's MCP artifact.
This retires the current project-shadows-global whole-file link behavior, and the same model
extends to every platform's MCP path (not only the primary one). See
[D15](#d15--mcp-server-definitions-are-resolvable-config-content).

**Rationale.** Once every other set-kind is name-selectable, mergeable content, a single opaque
whole-file link for MCP is the odd one out — and the existing whole-file-link behavior has a real
shadow bug (a project-scope file that omits a server the global file defines silently never sees
that server), which a full-resolution merge fixes as a side effect of making MCP consistent with
the rest of the model.

#### RD13 — Settings stays scalar in v1

**Ruled (default accepted):** `settings` remains a plain `*bool` — the only field in this model
with no natural name space. `settings` is corrected out of D5's keyed-maps bullet (where it was
listed in error) and into D5's never-aggregated bucket instead: a per-repo platform-preference
toggle is not shared capability content, so cross-member aggregation does not apply to it, the
same as it does not apply to repo identity. Listifying `settings` (e.g. selecting which
platform-setting bundles apply) is deferred until a real need for it appears. See
[D5](#d5--per-kind-merge-semantics) and [D16](#d16--legacy-grandfathering-and-deprecation-window).

#### RD14 — Legacy grandfathering and deprecation window

**Ruled (defaults taken):** legacy settings-backed hooks (no corresponding bundle name) are
grandfathered as always-on, exempt from the new selection filter, **only for a stated deprecation
window** — after which conversion to a named `HOOK.yaml`-style bundle is required to stay
selectable. Deprecated bool forms (`true`/`false` on hooks/MCP) are accepted-with-warning
**indefinitely**, with no forced cutover date, matching the existing config-migration posture of
warning forever rather than hard-breaking an old manifest. A sequencing constraint governs the
rollout: the existing scalar-scoped shadow-value verify check must run its audit **before**
hooks/MCP flip from scalar to the set-union model, so the `false → absent + warn` migration path
has already-enumerated, per-repo data to warn against rather than a blanket notice. See
[D16](#d16--legacy-grandfathering-and-deprecation-window) for the full per-value migration table.

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

DC6. Hooks, MCP, and rules ship as the uniform name-selection model with exclusion-token masking
     ([D12](#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model),
     [D13](#d13--exclusion-tokens-the-uniform-masking-successor)); the boolean `true`/`false`
     masking model is fully retired from the effective schema — a legacy `false` no longer masks
     anything at resolution time beyond its documented D16 migration.

DC7. A default root sync provably never mutates a member's lockfile (only warns on drift); an
     explicit cascade flag provably does, and only through each member's standard resolve path,
     serially, with per-member failure isolation and a summary report, and never under `locked`/
     `frozen` (D11, R8).

DC8. Projection output is byte-identical, before and after the amendment ships, for every project
     whose resolved chain declares no hooks/MCP/rules list at any scope — verified across a corpus
     of projects with zero declarations (R10).

DC9. Migration of existing hooks/MCP/rules values is idempotent and produces a per-key report
     matching [D16](#d16--legacy-grandfathering-and-deprecation-window)'s table (`true` → absent,
     silent; `false` → absent, warned; event-name/scope-token lists → dropped, warned); running
     migration a second time yields no further changes.

DC10. The scalar-scoped shadow-value verify check runs and enumerates every repo whose projection
      would change once hooks/MCP flip to the set-union model, and this audit completes before the
      flip ships as the effective default (D16's sequencing constraint).

DC11. The provenance-prune invariant is directly tested: de-selecting a source-fetched resource
      prunes its project-scope bucket copy and the resource re-materializes correctly on
      re-selection; de-selecting a promoted or hand-authored resource never deletes its bucket
      copy (filter-only, reported by `verify`); no de-selection, under any origin, ever prunes a
      global-scope bucket entry (D14).

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
- Adding a name space to `settings` (e.g. selecting which platform-setting bundles apply) — stays
  a plain scalar in v1; deferred until a real need appears (RD13).

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
- [org-config-resolution §7.2/§7.3](../org-config-resolution/design.md#72-proposed-merge-categories) —
  amended alongside this spec to add hooks/mcp/rules (set-union) and settings (scalar) to the
  merge-category table, pointing back here for the full semantics.
