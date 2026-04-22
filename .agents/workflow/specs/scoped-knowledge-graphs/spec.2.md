# Scoped Knowledge Graphs — Product Contract (v2-pre-fix snapshot, SUPERSEDED)

**Status:** SUPERSEDED — historical snapshot, do not build against this file.
**Superseded by:** `design.md` (canonical event-driven contract, post-fix).
**Captured:** 2026-04-21
**Why retained:** audit trail of the first event-driven rewrite *before*
two contract-level defects were caught in adversarial review:

1. **Legacy config silently dropped staleness.** §3.1 (line ~190) in this
   snapshot said legacy `kg.graph_home` / `kg.backend` configs "continue to
   work as a single-scope `repo` chain" but did not commit to what
   staleness behavior that carries. Combined with §5.3's "a scope with no
   drivers produces no staleness signals," an untouched legacy config
   would appear compatible while silently providing no invalidation.
   `design.md` §3.1 / §5.13 now make the absence explicit and require a
   mandatory runtime diagnostic.

2. **Cross-scope contradictions were modeled inconsistently.** §2.5 driver
   4 and §3.5 in this snapshot treated `contradiction` as a stale reason
   and required contradictions to surface across scopes at read time, while
   §5.6 also committed to write-time-only propagation. A cross-scope
   disagreement has no write event to stamp `fired_at` on. `design.md`
   §2.5 / §3.2 / §3.5 / §5.12 now split the behavior: same-scope
   contradictions fire a write-time driver; cross-scope disagreements are
   read-time metadata in `contradictions` only and do **not** emit a
   `stale` tag.

> **Canonical contract lives in `design.md`.** Do not revise this file to
> reflect later decisions — edit `design.md` instead. This snapshot's body
> is preserved verbatim below as the evidence of what the defects looked
> like in the contract that was being reviewed.

---

## 0. What changed from v1

v1 treated staleness as a *freshness policy* with TTL, source-hash, and ETag
as interchangeable candidates. That framing was wrong. Domain knowledge does
not decay on a clock — a fact stays true until something drives a change.

v2 rebuilds §2.5 and §5 around **driver events**: an entry is fresh by default
and becomes stale only when an identifiable event invalidates it. Time-based
signals are demoted from "staleness" to **review-nudges** — "I haven't seen
evidence this is still valid, flag for human confirmation" — which is a
separate dimension from truth.

Everything else (scope chain, provenance, write routing, propagation along
derivation edges, resolver purity) is unchanged from v1.

---

## 1. Problem statement

Today `AgentsRC.KG` describes a single knowledge graph: one `graph_home`, one
`backend`, one bridge. In practice a developer sits at the intersection of
several KG audiences:

- **repo** — facts only true inside this checkout.
- **user** — facts true across everything this human works on.
- **team** — facts shared across a working group.
- **org** — facts true across the company.
- **public** *(optional, read-only)* — upstream package docs, published
  design records, community-curated entities.

With a single KG we must either pollute the repo graph with cross-cutting
facts or lose them entirely. We cannot answer "where did this fact come
from?" which makes trust and revocation impossible.

Beyond provenance, single-KG thinking has led us into a second trap:
**treating staleness as elapsed time.** Repo facts should invalidate when
HEAD moves. Team conventions should invalidate when the team revokes them.
Library assumptions should invalidate when the library releases a breaking
change. None of those are clock events. A KG that flags entries as stale
just because they are old is noisy in exactly the cases where it should
be quiet, and silent in exactly the cases where it should alarm.

This spec defines the behavioral contract for a multi-scope KG, with scope
provenance on every result and an **event-driven** staleness model.

---

## 2. Decisions

### 2.1 Scopes are an ordered chain, not a flat set

A project configures an **ordered list** of scopes. Precedence runs
most-local → most-global: `repo → user → team → org → public`. Order is
explicit in config.

**Why ordered:** every query must produce a single answer per node id.
Ordering makes "repo overrides org" a config fact, not a tie-breaker buried
in query code.

**Rejected — single merged store with tags:** writing to org from a repo
checkout is a permission problem, and tags don't give us per-scope backends.

### 2.2 Each scope is independently backed

A scope declares its own `backend` (`sqlite`, `postgres`, `http`) and
connection target. `repo` and `user` default to sqlite on local disk.
`team`/`org` default to postgres behind a shared URL. `public` is `http`
and always read-only.

### 2.3 Reads fan out; writes target exactly one scope

**Reads.** A query walks the scope chain; results carry their origin scope.
When the same node id appears in multiple scopes, most-local wins unless
`--merge=union` is passed.

**Writes.** Every write names a target scope. Default `repo`. Writes to
`team`/`org` require `writable: true` in config and backend auth.
`public` is never writable.

### 2.4 Every node and note carries origin scope

Non-negotiable. Without provenance we cannot answer "who said this" or
"revoke everything X contributed."

### 2.5 Staleness is event-driven, not time-driven

**An entry is fresh by default. It becomes stale only when a driver event
fires.** Time is never a primary staleness signal.

The drivers — taxonomy at §4.1, bounds at §5:

1. **Source mutation.** The thing this entry describes changed. For a repo
   note citing `FuncA`, the driver is "the stored hash of `FuncA` no longer
   matches the current hash." Detected at write time whenever a code-graph
   node is upserted with a different content hash.
2. **Derivation mutation.** Something this entry cites changed. Propagates
   from source mutations along `derived_from` and `NoteSymbolLink` edges.
   See §2.6.
3. **Explicit revocation.** A writer marks an entry revoked. Revocations
   are themselves stored entries so they carry provenance and can be
   appealed.
4. **Contradiction arrival.** A new entry asserts the opposite of an
   existing one. Detected by node-id collision across writes in the same
   scope, or across scopes at read time (surfaced as `contradictions`).
5. **Environmental trigger.** A declared dependency fired — a library
   version bumped, a schema migrated, an incident closed. Entries declare
   their environmental predicates at write time; a `kg trigger` command
   fires the driver when the predicate changes.

**Why not TTL:** a fact's truth does not depend on clock time. A decision
from 2022 ("we use postgres") is as true today as the day it was written
unless something happened to change it. Clock-based expiry marks valid
facts stale (noisy) and leaves invalidated facts fresh (silent) — exactly
the wrong error profile.

**Rejected — hybrid TTL + driver model.** A hybrid pushes the TTL bug down
one level: scopes without a driver default to TTL, so anyone who doesn't
declare a driver gets the wrong behavior by default. v2 takes the opposite
default: **no driver declared → no staleness signal at all.** The entry is
assumed fresh, and **review-nudges (§2.7)** — a separate dimension — handle
the case where humans want periodic confirmation.

### 2.6 Staleness propagates along derivation edges

Two kinds of staleness: **source-stale** (the thing you describe moved) and
**derivation-stale** (something you cited moved).

The substrate exists today: `NoteSymbolLink` connects notes to code symbols,
and the code graph has edges between symbols. Stored notes also implicitly
cite other node/note ids — v2 makes those cites **explicit and stored**.

When a driver fires on node X, staleness propagates outward along
derivation edges up to a bounded depth. Reachable entries are marked
`derivation-stale`, distinct from `source-stale`.

**Why mandatory:** if a rename of one function silently leaves fifty
decisions citing its old name as "fresh," the graph is actively misleading.

**Why bounded:** unbounded propagation taints half the graph on any edit.
Depth limit and edge-type allowlist live in scope config.

### 2.7 Review-nudges are a separate dimension from staleness

Time still has a legitimate role, but not as a staleness signal. A scope
may declare a **review-nudge** policy: "if no driver has fired on this
entry in N days, flag it for review."

A review-nudged entry is **still fresh**. It is not filtered, not tagged
`stale`, not excluded from queries. It carries a separate
`review_due: true` marker saying "a human hasn't confirmed this in a
while; consider verifying." The reader decides whether to act.

**Why separate:** mixing the two axes is exactly the bug v1 had. An entry
can be (fresh, review-due), (stale, review-due), (fresh, not-due), or
(stale, not-due) — four distinct states, not one collapsed "stale" flag.

**Why optional:** review-nudges are noise for scopes where facts are
self-verifying (repo scope: drivers fire on every commit). They are
valuable for scopes where drivers are rare or unreliable (user scope:
personal conventions drift without leaving a trail).

### 2.8 Scope resolution is a pure function of config + query

Given `(agentsrc.kg.scopes, query)` the resolver produces the same result
regardless of caller (CLI, MCP tool, hook). No implicit env lookups, no
per-command defaults. Callers that want a subset pass `--scopes=repo,user`.

**Why:** today's single-KG path has hidden fallbacks (`KG_HOME` env,
`~/.knowledge-graph` default) that make "what graph did this query hit"
unanswerable. Scoped KGs would multiply that problem.

---

## 3. Requirements

### 3.1 Configuration

- `agentsrc.kg.scopes` is an ordered list. Each entry declares: `name`,
  `backend`, connection target, `writable`, `drivers` (which of §2.5's
  drivers are active for this scope), optional `review_nudge`.
- Legacy `agentsrc.kg.graph_home` / `agentsrc.kg.backend` continue to work
  as a single-scope `repo` chain.
- A scope may be declared `read_only: true`. `public` must be read-only.

### 3.2 Query behavior

- All KG read tools accept `--scopes=<list>`. Default: all configured.
- Every result row carries origin `scope`.
- Precedence collisions surface as `[scope-overrides: N]` warning.
- Empty results surface as `[scopes-empty: repo,user,team]` warning
  listing which scopes were consulted.
- Contradictions across scopes surface in a `contradictions` field
  even when precedence already picked a winner.

### 3.3 Write behavior

- Every write names its target scope (`--scope`, default `repo`).
- Writes to a non-writable scope fail loud with the scope name.
- Auth failures do not silently fall back to a local scope.
- Every write captures `derived_from` cites or the explicit
  `derivation: untracked` marker.

### 3.4 Provenance and status

- Every returned node/note carries `scope: <name>`.
- Every stored note carries `scope: <name>` in its persisted form.
- `kg scopes status` reports per scope: backend, target, writable,
  note count, drivers enabled, review-nudge policy, health.

### 3.5 Staleness surface

- An entry invalidated by a driver is returned with
  `stale: { reason: "source" | "derivation" | "revocation" |
  "contradiction" | "environmental", because: [<id>, ...], fired_at: <ts> }`.
- An entry with no driver fired but past its review-nudge threshold is
  returned with `review_due: { since: <ts>, days_unreviewed: N }` —
  separate from `stale`.
- A consumer can opt into `--hide-stale` or `--hide-review-due`;
  defaults tag rather than hide.

### 3.6 Health and failure modes

- Unreachable scope backends degrade gracefully: remaining scopes answer,
  response carries `[scope-degraded: team]`.
- `workflow graph health` reports per scope independently.

---

## 4. Open questions

Must be resolved by the plan.

### 4.1 Driver taxonomy — what counts, what fires

The plan must pin down:

- **Source mutation.** What "change" means for a cited symbol. Options:
  any upsert, signature-only change, content-hash change. Recommended:
  content-hash change — a body edit that doesn't alter the hash
  (formatting) should not invalidate.
- **Derivation mutation.** See §4.2.
- **Revocation.** How a revocation is recorded (new note with
  `revokes: <id>` vs. in-place flag). Recommended: new note, so
  revocations have provenance and are themselves reversible.
- **Contradiction.** Whether automatic contradiction detection runs at
  write (same scope) and/or at read (cross-scope). Recommended: both,
  with cross-scope surfaced as `contradictions`, same-scope surfaced
  as a write-time warning the writer can override.
- **Environmental trigger.** Predicate language for env triggers
  (semver range? regex on a changelog? webhook?). Start small: an entry
  declares `env_predicates: [{kind: "module_version", module: "foo",
  range: "<2.0"}]`, and `kg trigger --env module_version foo=2.0.1`
  fires drivers on entries whose range no longer matches.

### 4.2 Derivation propagation — shape and bounds

- **What counts as a derivation edge.** Cumulative:
  - `NoteSymbolLink` rows with `LinkKind` in a load-bearing set
    (`documents`, `implements`, `decided_on`). Exclude weak kinds like
    `mentions`.
  - Explicit `derived_from` cites stored on the written entry.
  - Code-graph edges between cited symbols, bounded (§depth).
- **Depth limit.** Recommended: **1** for code-graph hops; **unbounded
  within note→note chains** (if A cites B cites C and C mutates, A is
  tainted). Plan must commit.
- **Edge-type allowlist.** Which `LinkKind`s propagate.
- **Taint decay.** Once marked derivation-stale, does the tag persist
  until explicit refresh, or fade on re-verification?

### 4.3 Review-nudge policy expression

How a scope declares review-nudge. Candidates:

- **Global per-scope window.** `review_nudge: 90d` — all entries in the
  scope are nudged after 90 days without a driver.
- **Per-note-type window.** `review_nudge: { decision: 180d, entity: 30d,
  runbook: 14d }` — different note types decay trust at different rates.
- **Per-entry override.** Writer declares `review_nudge: 7d` on a
  specific entry (useful for "I'm not sure about this, check back").

Recommended: per-note-type with per-entry override. The plan must commit.

### 4.4 Stale-read semantics

Three options for what happens when a stale entry is returned:

- **Hide.** Filter out. Too aggressive — hides entries consumers may
  still want (e.g., a revoked decision is evidence when auditing
  history).
- **Tag (default).** Return with the structured `stale` payload.
  Consumers decide.
- **Refresh on read.** Opt-in flag that triggers re-verification.
  Expensive; never default.

Recommended: tag by default; `--hide-stale` opt-in; refresh-on-read is
its own command, not a query flag.

### 4.5 Public scope — deferred

Model it from day one (so provenance and resolver cover it); first
implementation plan does not build a public-scope backend. A second
spec will cover discovery, trust, and caching. `public` drivers will
likely lean on HTTP `ETag`/`Last-Modified` as source-mutation signals.

### 4.6 Semantic (non-structural) propagation — deferred

Two notes can be about the same idea without a stored edge. Embedding-
similarity propagation ("when X changes, tag entries with cosine > 0.9
to X as review-due") is out of scope for v1. The `stale.reason` enum
must be extensible to add `"semantic"` later without breaking clients.

### 4.7 Cross-scope write routing — deferred

Default write target is the configured `default_write_scope` (usually
`repo`). Heuristics for auto-routing ("this smells like a user fact")
are deferred.

### 4.8 Revocation-of-revocation

If a revocation note is itself later revoked, does the original entry
become fresh again? The plan should pick: yes (revocations are
first-class reversible events) or no (revocation is terminal, use a
new assertion). Recommended: yes — revocation notes are regular
entries and the staleness graph walks them like any other driver.

---

## 5. Staleness management — design commitments

These hold regardless of how §4 resolves.

### 5.1 Event-driven, not clock-driven

An entry is fresh until a driver fires. No background job expires
entries because they are old. Time-based signals exist only in the
separate `review_due` dimension (§5.7).

### 5.2 Staleness is visible, not silent

Driver-invalidated entries are returned tagged, never quietly dropped,
unless the consumer explicitly opts into `--hide-stale`.

### 5.3 Staleness is per-scope

No global policy. Each scope declares which drivers are active and
how they're detected. A scope that has no drivers enabled produces no
staleness signals — entries are simply fresh.

### 5.4 Staleness carries a reason and a cause

```
stale: {
  reason: "source" | "derivation" | "revocation" | "contradiction" | "environmental",
  because: [<node_id or note_id or trigger_id>, ...],
  fired_at: <timestamp>
}
```

A consumer must be able to tell *why* an entry is stale and *what*
tainted it.

### 5.5 Evidence is stored at write time

Every entry records, at write time, the facts drivers need later:
source hash of cited symbols, `derived_from` cites, environmental
predicates. Read time compares stored evidence against current state
or against a stored "already-fired" flag. Read time does not walk
derivation graphs or recompute hashes.

### 5.6 Propagation is write-time, not read-time

When a driver fires, the write pipeline walks derivation edges and
stamps reachable entries `derivation-stale`. Reads filter on stored
flags. Walking at read time is prohibitively expensive and makes stale
status depend on read latency, not truth.

### 5.7 Review-nudges are a separate axis

An entry can carry `review_due` without carrying `stale`, and vice
versa. Consumers that want "show me everything that hasn't been
verified in 90 days" ask for `review_due`. Consumers that want
"show me everything known-bad" ask for `stale`. These are different
questions and get different answers.

### 5.8 Derivation cites are first-class

Every written note/decision records the node/note ids its claim rests
on. Without stored cites, propagation has nothing to walk. Writers that
cannot produce cites are allowed but are tagged `derivation: untracked`
so consumers know propagation won't protect them.

### 5.9 Propagation is bounded

Configured depth limit and edge-type allowlist prevent one symbol
rename from tainting half the graph. Bounds live in scope config.

### 5.10 Re-verification is explicit

`kg refresh --scope=<name>` re-runs a scope's index pipeline and
clears drivers whose stored evidence matches current state. Staleness
tags are hints that a refresh is needed, not triggers that run one
automatically.

### 5.11 Revocation is a first-class note

Revocations are stored entries with their own scope, provenance, and
`revokes: <id>` reference — not in-place flags. This lets revocations
be audited, appealed (revoked), and propagated like any other driver.

---

## 6. Done criteria

- `.agentsrc.json` with an ordered `kg.scopes` list validates and
  round-trips.
- A legacy `.agentsrc.json` with only `kg.graph_home` still works
  without edits.
- A KG query against a repo with `repo + user + org` scopes returns
  results annotated with origin scope.
- A `team` scope with a bad DSN returns `[scope-degraded]` and does
  not fail the whole query.
- A write with `--scope=team` against a non-writable scope fails loud.
- `kg scopes status` reports per-scope backend, drivers, health, and
  review-nudge policy.
- **No entry is marked stale by elapsed time alone.** Staleness
  requires a named driver firing.
- When a cited node's content hash changes, every entry reachable
  within the configured derivation bound returns on the next read
  with `stale: { reason: "derivation", because: [<id>] }`.
- A written revocation note invalidates its target with
  `stale: { reason: "revocation", because: [<revocation-id>] }`.
- A cross-scope node collision with opposing claims returns in
  `contradictions` even after precedence picks a winner.
- An entry past its scope's review-nudge window with no driver fired
  returns with `review_due: { ... }` and **without** `stale`.
- Every written note carries structured `derived_from` cites or the
  explicit `derivation: untracked` marker.

---

## 7. Deferred

- Public-scope backend (discovery, trust, caching, ETag drivers).
- Write-routing heuristics.
- Cross-scope merge UIs beyond the `contradictions` field.
- Per-scope access control beyond `read_only`.
- Scope inheritance / composition (team inherits from org).
- Semantic-similarity-based propagation. v1 handles structural only.
- Automatic re-verification of derivation-stale entries. v1 tags; a
  human or explicit `kg refresh` resolves.
- Rich environmental trigger predicate language beyond a small set of
  kinds (`module_version`, `schema_version`, explicit webhook id).

---

## 8. Relationship to other specs

- **graph-bridge-contract** — single-KG bridge surface this spec
  generalizes. `[bridge-sparse]` is the pattern for `[scope-degraded]`,
  `[scopes-empty]`, `[scope-overrides]`.
- **kg-command-surface-readiness** — `kg` command surface that must
  grow `--scope`, `kg scopes status`, `kg refresh --scope`, and
  `kg trigger --env`.
- **org-config-resolution** — how org-level config reaches a repo;
  the org scope's DSN likely resolves through the same mechanism.
