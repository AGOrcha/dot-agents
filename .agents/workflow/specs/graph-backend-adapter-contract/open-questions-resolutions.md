# Graph Backend Adapter / KG — Open-Question Resolution Dossier (O1–O7)

**Status:** PROPOSAL — IDEATION artifact awaiting owner ratification. Nothing
here is normative until the maintainer ratifies it back into the owning spec.
**Posture:** dot-agents `ideation` app-type — explore the option space, weigh
trade-offs, commit to a recommendation with rationale, blast-radius, and what
it unblocks. Each recommendation is a **PROPOSAL**, not a decision.
**Written:** 2026-06-23
**Author:** Nikash Prakash
**Owning specs:**
- `graph-backend-adapter-contract/design.md` (O1, O2, O3, O6, O7)
- `scoped-knowledge-graphs/design.md` (O5)
- `work-tracking-storage-abstraction/design.md` (O4 schema sibling)
- `graphstore-concurrency-contract/design.md` (O7)
- `kg-command-surface-readiness/design.md` (context for O3/O6)

**Companion artifact:**
[`work-tracking-storage-abstraction/sdd-entity-kg-schema-draft.md`](../work-tracking-storage-abstraction/sdd-entity-kg-schema-draft.md)
— the concrete SDD-entity node/edge schema draft referenced by O4.

---

## How to read this dossier

Each OQ is rendered as:

1. **The question** — concrete restatement + source section(s).
2. **Options** — 2–3 viable resolutions with trade-offs.
3. **Recommendation + rationale** — the proposed answer and why.
4. **Blast radius / reversibility** — what changes if adopted; how hard to undo.
5. **What it unblocks** — the task/tier the resolution releases.
6. **Sequencing** — when it must land relative to the plan's task graph.

A summary table and a **safe-to-ratify vs contested** verdict close the dossier.

---

## O1 — `accepts_breaking_changes` opt-out vs the mechanical (no-ack) cutover gate

### The question

`graph-backend-adapter-contract` §14 Q2 asks whether a dependent view should be
allowed to declare `accepts_breaking_changes: true` to opt out of the
cross-adapter cutover gate — letting a dependee upgrade proceed and accepting
view-stale state until a manual rebuild. The §14 Q2 *lean* is "yes, opt-in only,
requires acknowledgement in lockfile."

That lean **directly contradicts the ratified mechanical-gate design** the spec
hardened across two revisions:

- §10.1.4: "There is intentionally **no** `da kg view ack-breaking-change` or
  similar — the cutover gate is mechanical per §10.3, not procedural."
- §10.3 / §10.3.2: "The gate is mechanical, not procedural. There is no human
  'ack' step… There is no operator override — fixing the broken queries is the
  unblocking action."
- §10.3.2 rationale: acks introduce two failure modes — (a) deadlock if the
  dependent is abandoned, (b) silent bypass if operators learn to ack
  reflexively.

So §14 Q2 is a *stale open question* whose lean was written before §10.3 was
hardened into the mechanical gate. The real question is: **do we keep the
mechanical gate for v1, or reopen an opt-out?**

### Options

**Option A — Keep the mechanical gate for v1; close Q2 as "no opt-out."**
The gate releases only on observable DSL-validation state. A dependee bump that
breaks a dependent view query parks the view in `dsl-update-required` and blocks
dependee activation until the dependent ships a fixed query.
- *Trade-off:* a private/abandoned dependent can wedge a public dependee upgrade
  locally. But this is **already bounded** by §10.3.3 fall-through-to-source-of-
  truth (available when validation *passes*, i.e. `pending-rebuild`) — the only
  truly-blocked case is a genuinely broken query, which *should* block.

**Option B — Add `accepts_breaking_changes: true` opt-out (the §14 Q2 lean).**
A view author declares it accepts post-bump stale state; dependee activation
proceeds; view is force-marked `pending-rebuild` (not `dsl-update-required`)
even when DSL validation fails.
- *Trade-off:* reintroduces exactly the silent-breakage failure mode §10.3.2
  rejected. A view whose compiled form references a renamed/removed field
  **cannot fall through** (§10.3.3) — so "accept breaking changes" means "serve
  errors or nothing until someone notices." This is the silent-loss anti-pattern
  scoped-KG §0 and §3.1 spent a whole revision eradicating.

**Option C — Narrow opt-out scoped to *backward-compatible* schema diffs only.**
Allow `accepts_breaking_changes` but the loader only honors it when the dependee
diff is additive (superset) — i.e. exactly the case §10.2 step 2 already handles
without a migration skill. For genuinely breaking diffs, the mechanical gate
still applies.
- *Trade-off:* this is *almost a no-op* — additive diffs already pass DSL
  validation and transition straight to `pending-rebuild`. The flag would only
  suppress the rebuild scheduling, a marginal win for real complexity.

### Recommendation + rationale — **PROPOSAL: Option A (keep mechanical for v1)**

Close §14 Q2 with "**no opt-out in v1**." The mechanical gate is not an
incidental implementation choice — it is a **ratified architectural decision**
(§2.3, §10.3) with a written rationale that the opt-out would reverse. The
§14 Q2 lean predates that hardening and should be marked superseded.

The opt-out's only legitimate motivator (faster dependee upgrades when the
dependent is slow/abandoned) is already served by two existing mechanisms:
(1) §10.3.3 fall-through-to-source-of-truth for the *recoverable* case; (2) the
fact that a *broken* query is precisely the case that *should* block, because
serving wrong/empty results from a stale view is the silent-loss failure mode
the whole spec is built to avoid. Adding an ack surface trades a loud, auditable,
reproducible block for a silent, operator-fatigue-prone bypass.

### Blast radius / reversibility

**Blast radius: near-zero (documentation only).** Adopting A means editing
§14 Q2 to "Resolved: no opt-out (mechanical gate retained per §10.3)" and
nothing in the task graph or code changes — the mechanical gate is already the
spec's normative design.

**Reversibility: trivial.** If post-v1 dogfood shows the gate is too rigid, an
opt-out can be added as a v1.5 amendment (§0 revision entry) with the
fall-through semantics already in place. Keeping v1 strict is the conservative,
easily-loosened choice; shipping the opt-out and removing it later is a
contract break.

### What it unblocks

Unblocks **t5-cross-adapter-reads-from** by removing ambiguity about whether the
lockfile state machine needs an `accepts_breaking_changes` field and an
ack-command surface. t5's hard test (bump CRG incompatibly → view enters
`dsl-update-required` and blocks) is exactly the mechanical-gate behavior; the
opt-out would have required t5 to also build the override path. Closing Q2 as
"no opt-out" keeps t5's write scope (`internal/kg/dsl/cross-namespace/`) tight.

### Sequencing

Resolve **before t5 begins** (t5 depends on t2 + t4). No dependency on t1.
Lowest-risk to ratify first since it's documentation-only and aligns the stale
open question with the already-ratified body.

---

## O2 — Namespace-token signing scheme + key management

### The question

Conformance test **N13** (§8.2.1) requires: "Adapter MCP server attempting to
forge a token… rejected at storage layer (token signature validation)." But the
spec defines the token *shape* (§8.2) and the *enforcement obligation* (§8.2,
§8.4) **without specifying any signing scheme or key management**. There is no
answer to: who signs, with what key, how the storage layer verifies, and where
the key lives. N13 is therefore currently unimplementable as written.

### Context that constrains the answer

The token is **short-lived, single-operation** (§8.2: `expires_at` "short-lived;
one operation"). The issuer and the verifier are **the same trust domain**:
`da` core issues the token (§8.2 "da core grants…"), and the storage layer
inside the same `da` process (or the same daemon, per graphstore-concurrency
Path B) verifies it. The adversary is an **adapter-owned MCP server** (§8.5)
that runs in-process-adjacent and could try to bypass `da-adapter-sdk` and forge
a wider grant (N11, N13). This is an *intra-host capability-confinement* problem,
not a cross-network/cross-org trust problem — at least for v1.

### Options

**Option A — In-process HMAC with a per-process ephemeral key (v1).**
At `da` startup (or daemon start), generate a random 256-bit key held only in
`da` core's memory. Token = `HMAC-SHA256(key, canonical_token_bytes)`. The
storage layer (same process / same daemon) verifies with the same in-memory key.
The key is never written to disk and never handed to adapter code or MCP servers.
- *Trade-off:* only defends within a single host/process boundary. An adapter
  MCP server in the **same** process could in principle read the key from memory
  — but that is out of scope for v1's threat model (adapter code is sandboxed
  per §8.4; the SDK is the only sanctioned issuer path, and the storage layer
  is the enforcement chokepoint regardless of token validity via N8/N9 defense-
  in-depth). For the cross-process MCP-server case (the real N11/N13 surface),
  the server cannot mint a valid HMAC without the key, which it never receives.

**Option B — Asymmetric signing (Ed25519): `da` core holds the private key,
verifiers hold the public key.**
`da` core signs tokens with a private key; any verifier (including a remote
`http` backend per §8.1) validates with the public key.
- *Trade-off:* the only scenario this buys over HMAC is **multi-party verifiers
  that should not be able to mint tokens** — e.g. a remote `http` storage backend
  operated by a different trust domain. That scenario is real for the `http`
  backend row in §8.1, but v1's built-in adapters (`none`, `compliance`, `crg`,
  `crg-bridge`) and the SQLite/Postgres backends are all same-trust-domain.
  Asymmetric brings key-distribution, rotation, and PKI overhead with no v1
  payoff.

**Option C — No cryptographic signing; rely purely on the storage-layer
namespace check (N6–N9) as the enforcement boundary.**
Treat the token as an unsigned capability descriptor and lean entirely on the
storage layer validating *every namespace in the compiled plan* against the
token's `authorized` set (§8.2 "Compiler obligation" + N9 defense-in-depth).
- *Trade-off:* fails N13 by definition — a forged token granting wider access
  would pass an unsigned check. N13 is a normative conformance test, so C is
  non-compliant unless N13 is *also* deleted, which weakens the contract's
  stated trust boundary.

### Recommendation + rationale — **PROPOSAL: Option A (in-process HMAC v1, defer asymmetric to the `http`-backend milestone)**

Ship **in-process HMAC with a per-process/per-daemon ephemeral key** for v1, and
**defer asymmetric signing** until the `http` backend (the only multi-trust-
domain verifier in §8.1) is actually built. Rationale:

1. **HMAC satisfies N13 for v1's real threat model.** Every v1 verifier
   (SQLite, Postgres, the in-process storage layer) is in `da` core's trust
   domain. A forged token from an adapter MCP server that bypasses the SDK
   (N11/N13) cannot produce a valid MAC without the key it never receives. N13
   passes.
2. **Asymmetric is YAGNI until `http`.** The single scenario justifying
   asymmetric — a verifier that must validate but must not mint — does not exist
   among v1's built-in adapters and backends. Building Ed25519 + key
   distribution + rotation now is speculative PKI the spec's own §12
   evidence-budget posture argues against.
3. **The storage-layer namespace check (N6–N9) remains the primary boundary
   regardless.** Signing defends *token integrity*; the per-namespace plan
   validation defends *capability scope* even against a valid token referencing
   an unauthorized namespace (N9). Both layers stay; HMAC just makes N13's
   integrity check real without overbuilding.

Document the upgrade path explicitly: the token's `authorized`/`issued_for`/
`expires_at` shape is signing-scheme-agnostic, so swapping HMAC → Ed25519 when
the `http` backend lands is a verifier-side change behind the same token shape —
exactly the "executor tier swap" pattern §2.7 establishes for the engine.

### Blast radius / reversibility

**Blast radius: scoped to t1's token machinery.** O2 lands inside
`internal/kg/registry/` / `internal/kg/lockfile/` token issuance and the storage-
layer verifier — both already in **t1**'s write scope (`internal/kg/registry/`,
`internal/kg/lockfile/`). No schema change, no DSL change, no lockfile-format
change (the MAC is computed over the in-memory token, not persisted).

**Reversibility: high.** HMAC is an internal verification detail behind the
fixed token shape. Replacing it with Ed25519 later touches only the issuer and
verifier, not adapter authors or the token format. The decision is a
provider-internal mechanism, not a contract surface.

### What it unblocks

Unblocks **t1-none-adapter-end-to-end** directly: t1 delivers "Namespace token
issuance and storage validation (§8.2)" and must pass token conformance tests.
N13 was unimplementable without a signing scheme — O2 makes t1's N11–N13 rows
achievable. Transitively unblocks t2 (env_predicates/DSL ride the same token
path) and t5 (multi-namespace token derivation N3–N4, N6–N9).

### Sequencing

Resolve **before t1 implementation** (t1 is the keystone, depends on nothing).
This is the highest-priority resolution because t1 blocks every other adapter
task and t1 cannot deliver N13 without it.

---

## O3 — The missing app-type-profiles wiring task

### The question

`graph-backend-adapter-contract` §9 (Distribution and tiers) + §15 (Completeness
note) establish that **backend selection happens in app-type-profiles** —
`graph_backend: <adapter-ref>` is the field a profile sets, and §15 states
explicitly: "This spec is **not** auto-applied to `app-type-profiles/design.md`.
The edits… need to be applied as a separate commit… the app-type-profiles edits
are the **wiring**." The §1 problem statement is literally "app-type-profiles
§2.6 currently treats `graph_backend` as a closed enum" — fixing that enum →
adapter-ref is the wiring.

**There is no task in `TASKS.yaml` that does this wiring.** t1–t6 build adapters,
DSL, cross-adapter reads, and CRG migration; `release-minor` bumps VERSION. None
of them edit `app-type-profiles/design.md` or wire `graph_backend` resolution
through the profile-selection path. The contract is buildable end-to-end with no
profile able to *select* a backend.

### Options

**Option A — Add a dedicated `t7-app-type-profiles-wiring` task.**
A task whose write scope is the app-type-profiles spec edits + the
`graph_backend` field resolution in the profile loader, depends on **t1**
(needs the adapter-ref resolution + lockfile machinery the `none` adapter
proves), and **blocks `release-minor`** (you cannot ship the minor with an
unwired selection surface).
- *Trade-off:* adds a task to the graph. But it is genuinely separate work
  (config/profile surface, not adapter internals) with a distinct write scope.

**Option B — Fold the wiring into t1.**
Extend t1's scope to also do the app-type-profiles edits.
- *Trade-off:* t1 is already the keystone with the largest write scope
  (`internal/adapters/builtin/none/`, `internal/kg/registry/`,
  `internal/kg/lockfile/`, `cmd/da/commands/kg/`). Adding cross-spec wiring +
  profile-loader changes overloads the fanout-amenable keystone and muddies its
  "smallest possible adapter, no domain logic" anti-scope. The wiring touches
  `app-type-profiles` and the profile resolver — a different subsystem.

**Option C — Defer wiring to `release-minor` / leave implicit.**
Treat the wiring as part of release finalization.
- *Trade-off:* `release-minor`'s scope is `VERSION` + `CHANGELOG.md` only.
  Wiring is feature work, not release bookkeeping; smuggling it in violates that
  task's clean scope and hides a real integration step behind a version bump.

### Recommendation + rationale — **PROPOSAL: Option A — add `t7-app-type-profiles-wiring`, `depends_on: [t1]`, `blocks: [release-minor]`**

Add the explicit task. §15's own language ("a separate commit," "the wiring")
asks for a discrete unit. The work is real and currently unowned: without it,
every adapter t1–t6 builds is unreachable by any profile, and `release-minor`
would ship a backend system no profile can select — a hollow minor.

Proposed task shape (PROPOSAL for `TASKS.yaml`):

```yaml
- id: t7-app-type-profiles-wiring
  title: Wire graph_backend adapter-ref selection into app-type-profiles
  status: pending
  depends_on:
    - t1-none-adapter-end-to-end
  blocks:
    - release-minor
  owner: dot-agents
  write_scope:
    - .agents/workflow/specs/app-type-profiles/design.md   # §2.6/§3.1/§7.3/§8 edits
    - internal/config/                                     # graph_backend field resolution
    - cmd/da/commands/                                     # profile-select path
  verification_required: true
  notes: |-
    Apply the app-type-profiles edits §15 of graph-backend-adapter-contract
    defers (the "wiring" commit): replace the closed graph_backend enum
    (crg|citation-graph|document-cross-ref|none) with an adapter-ref that
    resolves via sources/extends and pins in the lockfile (§9). Wire the
    profile-selection path so a profile declaring
    `graph_backend: dotagents-builtin:graph/none@^1.0` resolves to the t1
    none adapter end-to-end.
    Hard test: a profile selecting the none adapter ref resolves, registers
    in the lockfile, and the no-op impact_radius runs through the profile
    path (not just the direct adapter path t1 exercised).
    Anti-scope: no new adapters; uses the t1 none adapter as the proof.
```

And update `release-minor.depends_on` to include `t7-app-type-profiles-wiring`.

### Blast radius / reversibility

**Blast radius: one new task + one edited dependency edge + the
app-type-profiles spec edits.** The code surface (`internal/config/` profile
resolution, `cmd/da/commands/` select path) is additive — turning a closed enum
into an adapter-ref resolver. Existing profiles that named the old enum values
need a one-time migration to refs (the same `dotagents-builtin:graph/<name>@^1.0`
form §9 already uses), which is mechanical.

**Reversibility: moderate.** The task itself is trivially removable from the
plan if the maintainer disagrees. The spec edit (enum → ref) is the substantive
part; reverting it would re-close the extension point §1 set out to open, so
once shipped it's load-bearing — but that is the *intended* direction, not a
regret-risk.

### What it unblocks

Unblocks **`release-minor`** legitimately: the minor can only claim "graph
backend adapter contract shipped" once a profile can actually select a backend.
Also makes t2/t3/t4's adapters *reachable* — the compliance/citation/document-
cross-ref/crg adapters are referenced by the `research`/`resume-ideation`/review
profiles (§13.4/§13.5), which can't bind them without the wiring.

### Sequencing

`depends_on: [t1]` (needs the adapter-ref resolution + lockfile registration the
`none` adapter proves) and `blocks: [release-minor]` (gates the release). Runs in
parallel with t2/t3/t4 once t1 lands. Ratifying O3 is a **plan edit**
(`da workflow` task add), so it should be applied to `TASKS.yaml`/`PLAN.yaml` via
the workflow CLI, not hand-edited.

---

## O4 — SDD-entity KG schema: reuse `KGNote` vs typed nodes

### The question

`work-tracking-storage-abstraction` §6 open question: "**KG schema for SDD
entities + correlation edges** — node/edge types for Plan/Task/Spec/Proposal and
the result→`stage_profiles`/skill/rule/agent/hook edges; how much reuses the
existing `KGNote` shape vs new typed nodes (see schema-usage `KGNote` field
caveats)." This is the schema design for the **KG-as-SOT** direction (D1′, §3A):
the graph store becomes the source of truth for SDD artifact *structure + state*,
with results correlated to the operational/semantic nodes that produced them.

The `KGNote` shape (`internal/graphstore/store.go:104`) is a flat note record:
`ID, Title, NoteType, Status, Summary, FilePath, Version, ArchivedAt, IndexedAt`.

### Options

**Option A — Reuse `KGNote` with a `NoteType` discriminator for everything.**
Model Plan/Task/Spec/Proposal/result/stage_profile as `KGNote` rows distinguished
by `NoteType`, with structured fields shoved into `Summary`/frontmatter and
relationships expressed via `NoteSymbolLink` + ad-hoc note→note cites.
- *Trade-off:* zero new storage types, but the structured coordination state
  D2/D5 require (status transitions, leases, PR linkage, `depends_on`,
  `write_scope`, `verifier_sequence`) has nowhere typed to live — it degrades to
  unindexed blobs in `Summary`. The wave-engine re-dispatch fix (D5, the whole
  motivating failure) needs *typed, queryable, atomically-transitionable* status;
  a `Status` string on a flat note can't carry a lease or a CAS transition. This
  is exactly the "`KGNote` field caveats" the open question flags.

**Option B — Pure typed nodes; `KGNote` is irrelevant to SDD entities.**
Define first-class `Plan`, `Task`, `Spec`, `Proposal`, `Result`, `StageProfile`,
`Skill`, `Rule`, `Agent`, `Hook` node types with typed fields and typed edges,
ignoring `KGNote` entirely.
- *Trade-off:* cleanest model, but throws away the existing `KGNote` /
  `NoteSymbolLink` / semantic-view machinery (search, FilePath projection,
  prose-body indexing) that D1′ tier 3 explicitly *wants* ("SDD artifact prose
  body… authored in files… indexed into the KG semantic view; the KG node
  references the file as its text source"). A spec's prose body genuinely is a
  note-shaped, file-backed, searchable artifact — discarding `KGNote` for it
  re-implements what already exists.

**Option C — Typed nodes for structure/state + a `KGNote` projection for prose.**
First-class typed nodes carry the *structure and coordination state* (tier 2,
KG-canonical per D1′); each artifact that has a prose body also has (or
references) a `KGNote` row for the *narrative text* (tier 3, file-backed +
semantic-indexed). The typed node references its `KGNote` via a `has_prose` edge
(or a `prose_note_id` field). Results are typed `Result` nodes with correlation
edges to operational/semantic nodes.
- *Trade-off:* two shapes to keep coherent (typed node ↔ its prose note), but
  this is **exactly the D1′ three-tier split** made concrete: code stays git,
  structure/state is the typed graph, prose stays a file indexed as a note. The
  coherence cost is the natural cost of the tier model the spec already ratified.

### Recommendation + rationale — **PROPOSAL: Option C — typed nodes for structure/state + `KGNote` projection for prose**

Adopt **typed nodes + KGNote projection**. This is the only option that honors
D1′'s already-ratified tier split:

- **Tier 2 (structure + state) → typed nodes.** Status, leases, PR linkage,
  `depends_on`, `write_scope`, `verifier_sequence`, spec↔plan↔task trace are
  *relationships and typed fields*, not free text. They need typed nodes/edges so
  D5's atomic status transitions and D-§3A's correlation queries are expressible.
  A flat `KGNote.Status` string cannot carry a CAS lease or answer "which
  verifier sequence regresses spec X."
- **Tier 3 (prose) → `KGNote`.** A spec/plan/lesson narrative *is* a note-shaped,
  file-backed, searchable artifact. `KGNote`'s `FilePath`/`Summary`/`IndexedAt`
  fields are the right home for it, and the existing semantic-view machinery
  already indexes it. Reusing `KGNote` here is not legacy debt — it is the
  correct shape for that tier.
- **Correlation edges (the feedback loop) → typed edges from `Result` nodes.**
  §3A's "correlation = edges, and that is the feedback loop" requires typed
  `produced_under` / `exercised` / `implements` edges from a `Result` node to
  `StageProfile`/`Skill`/`Rule`/`Agent`/`Hook`/`Spec`/`Plan`/`Task`. These have
  no honest representation as `KGNote` rows.

The full node/edge catalog is drafted in the companion artifact
[`sdd-entity-kg-schema-draft.md`](../work-tracking-storage-abstraction/sdd-entity-kg-schema-draft.md).

### Blast radius / reversibility

**Blast radius: large but deferred.** This schema is the foundation of the
KG-as-SOT cutover, which `work-tracking-storage-abstraction` §6/§7 explicitly
**gates on `graph-backend-adapter-contract` landing**. So O4 is a *design*
commitment now and a *build* commitment later. Adopting the typed-node design
shapes the eventual `WorkStore` `kg` implementation and the typed-view services,
but lands no code until the work-tracking plan is created.

**Reversibility: moderate at design stage, low after build.** As a draft schema
it is freely revisable. Once the `kg` `WorkStore` and projection machinery are
built against it, changing node/edge types becomes a schema migration (§10.2 of
the adapter contract applies if SDD entities ride an adapter). The
recommendation keeps reversibility *higher* than Option B by preserving the
`KGNote` semantic-view path, so prose handling never has to be re-migrated.

### What it unblocks

Unblocks the **`work-tracking-storage-abstraction` plan** (not yet created) —
specifically D3 (`WorkStore` over typed entities), D5 (atomic status transitions
that kill the wave-engine re-dispatch storm), and §3A (the queryable
self-improvement feedback loop). It is a prerequisite *design* for that plan's
done-criterion #5 ("SDD artifacts are graph-canonical… a result node carries the
correlation edges"). Within *this* contract's plan, O4 unblocks nothing — it is
the sibling spec's foundation.

### Sequencing

O4 is a **design-time** resolution with **no task in this plan's graph**. It
should land as the schema draft now (companion artifact) and feed the future
`work-tracking-storage-abstraction` plan, which is gated on
`graph-backend-adapter-contract` completing. Ratify the *design* now; defer the
*build* to that plan.

---

## O5 — Scoped-KG driver taxonomy + derivation-propagation depth/decay

### The question

`scoped-knowledge-graphs` §4.1 + §4.2 are flagged "**Must be resolved by the
plan**" and pin four sub-decisions the adapter contract's staleness machinery
(§7, §7.3) depends on:

- **§4.1 Source mutation** — what "change" means for a cited symbol (any upsert
  / signature-only / content-hash).
- **§4.1 Revocation** — recorded as a new note with `revokes: <id>` vs. an
  in-place flag.
- **§4.2 Derivation depth** — code-graph hop limit + note→note chain bound.
- **§4.2 Taint decay** — does a derivation-stale tag persist until explicit
  refresh, or fade on re-verification?
- **§4.2 Edge-type allowlist** — which `LinkKind`s propagate.

The spec already states recommended answers; the question is whether to commit
them as the resolution.

### Options (per sub-decision, with the consolidated lean)

**Source mutation = content-hash change.** (vs. any-upsert / signature-only)
- Content-hash is the spec's §4.1 recommendation and matches the gcc-shipped
  `GraphNode.FileHash` / `UpdatedAt` substrate. Any-upsert over-fires on
  formatting; signature-only under-fires on body-semantic changes. Content-hash
  is the right error profile (§2.5 "a body edit that doesn't alter the hash
  should not invalidate").

**Revocation = new note with `revokes: <id>`.** (vs. in-place flag)
- §4.1 + §5.11 already commit revocations as first-class reversible notes (for
  provenance + appeal + §4.8 revocation-of-revocation). An in-place flag loses
  provenance and can't be walked by the propagation graph.

**Derivation depth = 1 code-graph hop; unbounded within note→note chains.**
- §4.2 recommendation. Code-graph hops taint fast and wide (one rename → half the
  call graph), so cap at 1. Note→note cites are deliberate human assertions
  (A cites B cites C); tainting transitively up that chain is the *point* of
  derivation-staleness (§5.8). The adapter contract's §7.3 ref/edge `derivation:
  true` opt-in is the per-relationship gate that keeps even this bounded.

**Taint persists until explicit refresh.** (vs. fade on re-verification)
- §5.10 + §7 (Deferred): v1 *tags*; a human or `kg refresh --scope` clears
  drivers whose stored evidence matches current state. Auto-fade-on-read would
  violate §5.6 (write-time-only propagation) and §2.8 (resolver purity) — a read
  must not mutate stale state. Persist-until-refresh is the only option
  consistent with those commitments.

**Edge-type allowlist = load-bearing `LinkKind`s (`documents`, `implements`,
`decided_on`); exclude weak kinds (`mentions`).**
- §4.2 recommendation. `mentions` is incidental co-reference, not a derivation
  dependency; including it reintroduces the unbounded-taint failure mode.

### Recommendation + rationale — **PROPOSAL: adopt all five spec recommendations as the resolution**

Commit the consolidated lean exactly as the spec's own §4.1/§4.2 recommendations
state, because every one of them is **forced by an already-ratified §5 design
commitment**, not a free choice:

| Sub-decision | Resolution | Forced by |
|---|---|---|
| Source mutation | content-hash change | §2.5 error-profile + `GraphNode.FileHash` substrate |
| Revocation | new note `revokes: <id>` | §5.11 first-class reversible notes; §4.8 |
| Code-graph depth | 1 hop | §5.9 bounded propagation; §2.6 "why bounded" |
| Note→note chains | unbounded (opt-in via `derivation: true`) | §5.8 cites are first-class; adapter §7.3 opt-in gate |
| Taint decay | persist until explicit `kg refresh` | §5.6 write-time-only; §2.8 resolver purity; §5.10 |
| Edge-type allowlist | `documents`/`implements`/`decided_on`; not `mentions` | §5.9 bounded; §2.6 unbounded-taint warning |

These are not genuinely open — they are the **only answers consistent with §5's
design commitments**, which were hardened post-adversarial-review (§0). The
"open question" framing in §4 is a request to *commit* them in the plan, which
this proposal does. The adapter contract's §7.3 propagation rule already assumes
exactly this taxonomy (content-hash `source_mutation`, `derivation: true` opt-in,
bounded propagation per scoped-KG §2.6).

### Blast radius / reversibility

**Blast radius: shapes the staleness-driver implementation** that t2 (compliance
adapter exercises all five drivers) and t4 (CRG `source_mutation` on note-hash
change) build against. No new storage types; the driver logic routes through the
existing `FileHash`/`UpdatedAt` + `NoteSymbolLink` substrate.

**Reversibility: high per-knob.** Each knob is a config/policy value, not a
contract surface. Depth limit and edge-type allowlist live in *scope config*
(§5.9), so a deployment can tune them; the *defaults* this proposal commits are
revisable per-scope without a contract change. Taint-decay is the one knob that
is load-bearing on §5.6/§2.8 — flipping to fade-on-read would require revisiting
those commitments, so it should stay persist-until-refresh.

### What it unblocks

Unblocks **t2-compliance-read-only** (exercises source/derivation/environmental/
revocation/contradiction drivers end-to-end per §13.2) and **t4-crg-dual-read**
(declares `staleness_drivers: [source_mutation]` firing on note-hash change).
Both need the driver semantics pinned — what counts as a mutation, how deep
taint walks, whether it persists — before they can write conformance tests.

### Sequencing

Resolve **before t2** (t2 depends on t1). The CRG `source_mutation` slice in t4
also needs it. This is a `scoped-knowledge-graphs`-spec resolution that the
adapter-contract plan *consumes*; ideally the maintainer ratifies it into
scoped-KG §4 and the adapter plan references it.

---

## O6 — CRG parity-matrix gaps A/C/D/G: fold A/C/D before t4?

### The question

`graph-backend-adapter-contract` v6.1 (§0) applied three of seven proposals from
`crg-dual-read-parity-surface-2026-05.md` (the wording fixes **E + F** that
unblocked the two payout review intents — edge-alias returns, `STARTS_WITH`).
The remaining four are deferred: "**The four parity-matrix proposals (A, C, D on
§11.1 testability gaps; G on SQL-callable views) are deferred to a separate spec
revision.**"

The question: should A/C/D (the §11.1 testability gaps) be folded into the spec
*before* **t4-crg-dual-read** builds the parity surface, or deferred further?
And G (SQL-callable views)?

### What A/C/D/G are (inferred from §11.1/§11.6 + the v6.1 framing)

The proposal file is referenced but its body lives under `.agents/proposals/`
(not in scope here). From the v6.1 classification ("A, C, D on §11.1 testability
gaps; G on SQL-callable views") and the §11.1/§11.6 parity matrix, A/C/D are
**testability/parity-criteria refinements** to the eight-row matrix (e.g.
tightening the `flows`/`communities`/`postprocess` pass criteria, ordering
semantics, or per-row corpus definitions), and **G proposes SQL-callable views**
— a raw-SQL surface that directly contradicts §2.2/§5.2's no-raw-SQL invariant.

### Options

**Option A (the lean) — Fold A/C/D into §11.1/§11.6 before t4; defer G.**
Apply the three testability-gap fixes so t4 builds the parity surface against the
*corrected* matrix; keep G out because SQL-callable views break the no-raw-SQL
contract.
- *Trade-off:* requires reading + ratifying A/C/D's specifics before t4. But t4's
  whole job is the parity surface (§11.2/§11.3/§11.6) — building it against a
  matrix with known testability gaps means t4's conformance tests bake in the
  gaps and t6 (decommission, which depends on the *full* matrix passing) inherits
  them.

**Option B — Defer all four (A/C/D/G) to a post-t4 spec revision.**
Build t4 against the current §11.1/§11.6; revisit A/C/D/G later.
- *Trade-off:* t4 + t6 are the multi-week parity gate (§11.4: "3 consecutive
  weeks of CI green"). Discovering testability gaps *after* t4 ships restarts that
  clock. The parity matrix is t4/t6's contract — fixing it after is expensive
  rework on the longest-pole tasks.

**Option C — Fold all four including G.**
Apply A/C/D *and* add SQL-callable views (G).
- *Trade-off:* G reintroduces a raw-SQL surface. §11.1's `flows` row already
  closed the door ("Named-query / MCP-server alternatives are **closed** — v1 DSL
  §5.1 RETURN has no `ORDER BY`"), and §2.2/§5.2 forbid raw SQL contract-wide.
  G is incompatible with the v1 contract; adopting it is a contract break, not a
  parity refinement.

### Recommendation + rationale — **PROPOSAL: Option A — fold A/C/D into §11.1/§11.6 before t4; defer G (SQL-callable views contradict no-raw-SQL)**

Fold the three testability-gap proposals (A/C/D) *before* t4, and **reject G for
v1** because SQL-callable views contradict the no-raw-SQL invariant (§2.2, §5.2,
§1.1 non-goal). Rationale:

1. **A/C/D harden the contract t4/t6 are tested against.** The parity matrix
   (§11.1/§11.6) *is* the decommissioning gate (§11.4). t4 builds the parity
   surface and t6's gate requires all eight rows green for three weeks. Folding
   testability fixes before t4 means the gate is sound from the start; folding
   them after means re-deriving conformance tests on the longest-pole tasks.
2. **G is a contract violation, not a parity gap.** SQL-callable views would
   reintroduce exactly the raw-SQL escape hatch the spec's prior-revision summary
   (§232–242) records as *dropped*. The `flows` row already documents the v1
   stance: where the DSL can't express something (no `ORDER BY`), use a
   materialized view's row order — *not* raw SQL. G belongs to a v1.5 DSL-richness
   discussion via the §12 evidence budget, if at all.
3. **This matches the spec's own split.** v6.1 already separated the *wording
   fixes* (E/F, applied) from the *parity-matrix* fixes (A/C/D, deferred) from the
   *SQL-views* proposal (G). The recommendation simply promotes A/C/D to
   "apply-before-t4" and confirms G stays out.

**Caveat (flagged for the maintainer):** the *specific content* of A/C/D lives in
`.agents/proposals/crg-dual-read-parity-surface-2026-05.md`, which was outside
the read set for this dossier. This recommendation is on the *sequencing/posture*
("fold the testability fixes before t4, reject the SQL-views one") — the exact
§11.1/§11.6 edits A/C/D imply must be reviewed against that proposal before
ratification. This is the one OQ where I cannot fully verify the option contents.

### Blast radius / reversibility

**Blast radius: §11.1/§11.6 spec edits + t4's conformance-test definitions.**
A/C/D are matrix/criteria refinements — they change what t4's parity tests assert,
not the adapter mechanism. No code-architecture change. Rejecting G changes
nothing (it keeps the existing invariant).

**Reversibility: high for A/C/D** (spec-text refinements, revisable before t4
locks them into tests). **G's rejection is the conservative default** — deferring
a raw-SQL surface is trivially reversible via the §12 budget later if a real
blocker emerges; shipping it and clawing it back is a contract break.

### What it unblocks

Unblocks **t4-crg-dual-read** (builds the parity surface against a sound matrix)
and, transitively, **t6-bridge-decommission** (whose §11.4 gate is the full
matrix passing). Getting the matrix right before t4 protects the multi-week
parity clock that t6 depends on.

### Sequencing

Fold A/C/D **before t4 begins** (t4 depends on t1). G requires no action (stays
deferred/rejected). Because A/C/D's exact content needs cross-checking against
the proposal file, this OQ should be ratified *after* the maintainer re-reads
that proposal — it is the least safe to ratify blind.

---

## O7 — Graphstore `Store` seam vs adapter executor seam reconciliation

### The question

Two specs define overlapping-but-distinct seams over the same storage:

- `graphstore-concurrency-contract` + `internal/graphstore/store.go` define the
  **`Store` seam**: a role-segregated (`CodeGraphReader`/`CodeGraphWriter`/
  `KGNoteStore`/`NoteSymbolLinkStore`/`Closer`) backend-agnostic contract with a
  provider that owns bounds/timeout/concurrency, swappable
  ephemeral→pooled→daemon (decision C-Hybrid).
- `graph-backend-adapter-contract` §2.7 defines the **executor tier**: the engine
  between the DSL front-end (§5) and the storage back-end, "free to evolve"
  (B-tree/recursive-traversal v1 → in-memory CSR v2), explicitly *over* the
  gcc-shipped `Store` family.

§2.7 already says the executor "reads and writes through the role-segregated
`Store` family already shipped by gcc1" and lists the exact roles. So the
relationship is *stated* but lives only in the adapter spec — the
`graphstore-concurrency-contract` / `CONTRACT.md` side doesn't acknowledge that
an executor tier sits above it. The question is how to reconcile the two so
neither spec implies it owns the other's concern.

### Options

**Option A — Executor sits strictly ABOVE `Store`; add a one-paragraph
cross-reference to both specs.**
Affirm the layering §2.7 already describes: DSL §5 → **executor tier** (algorithm
choice, traversal, namespace-token resolution at the boundary) → **`Store` seam**
(role-segregated backend contract, provider owns concurrency). Add a short
note to `graphstore-concurrency-contract` + `CONTRACT.md` pointing "up" to the
executor tier, and confirm in the adapter spec the executor never bypasses
`Store`.
- *Trade-off:* none of substance — it documents the relationship the code already
  embodies (the executor would call `CodeGraphReader.GetImpactRadius` etc.). Pure
  clarification.

**Option B — Merge the executor into the `Store` provider (executor IS the
provider).**
Treat the executor as just another `Store` provider variant (like `LazyStore`).
- *Trade-off:* conflates two genuinely different axes. The `Store` provider owns
  *connection/concurrency/bounds* (ephemeral vs pooled vs daemon); the executor
  owns *traversal algorithm + DSL lowering + token resolution* (B-tree vs CSR).
  §2.7's whole point is that these evolve *independently* — a CSR executor swap
  and an A→B daemon swap are orthogonal. Merging them couples two swap dimensions
  the specs deliberately separated.

**Option C — Executor owns its own storage primitives, parallel to `Store`.**
Let the executor read/write through a new executor-specific storage interface,
not the gcc `Store` family.
- *Trade-off:* directly contradicts §2.7's normative "The executor reads and
  writes through the role-segregated `Store` family already shipped by gcc1." It
  would fork storage access into two interfaces over the same tables, breaking
  the single-substrate commitment (§2.1) and the C-Hybrid transparent-swap
  guarantee.

### Recommendation + rationale — **PROPOSAL: Option A — executor sits ABOVE `Store`; add a one-paragraph reconciliation note to both specs**

Affirm the layering the adapter spec already states and make it bidirectional
with a short note. The two seams are **stacked, not competing**:

```
DSL §5 (front-end contract: schema + queries, adapter-author-facing)
        │
        ▼
Executor tier (§2.7: algorithm choice, traversal, ref-join lowering,
        namespace-token resolution-at-boundary; B-tree v1 → CSR v2)
        │   reads/writes exclusively through ↓
        ▼
Store seam (graphstore-concurrency-contract: role-segregated contract;
        provider owns bounds/timeout/concurrency; ephemeral→pooled→daemon)
        │
        ▼
Backend (SQLite Path A / Postgres / http)
```

Rationale:

1. **The code already embodies A.** §2.7 names the exact `Store` roles the
   executor uses (`CodeGraphReader`, `CodeGraphWriter`, `KGNoteStore`,
   `NoteSymbolLinkStore`, `Closer`). The executor calling
   `CodeGraphReader.GetImpactRadius`/`SearchNodes` *is* the layering. No code
   change — only a doc cross-reference so the `graphstore` side acknowledges the
   tier above it.
2. **The two swap dimensions are orthogonal and must stay so.** §2.7's CSR-vs-
   B-tree executor swap and graphstore-concurrency's A→B daemon swap are
   independent: you can swap the executor without touching the provider and vice
   versa. Option B couples them; A keeps them clean.
3. **`resolve-at-boundary` (§8.2 v6 supplement) is an executor concern, and that
   resolves the apparent token overlap.** Namespace tokens are resolved *once at
   the DSL boundary* by the executor (§8.2 v6 "the executor's inner loop sees a
   fixed, pre-resolved capability set"); the `Store` provider just enforces the
   per-namespace check the executor hands it. So even the token machinery has a
   clean owner per tier — executor resolves, storage enforces. The reconciliation
   note should say exactly this.

Proposed one-paragraph note (PROPOSAL text for both specs):

> **Executor tier vs `Store` seam.** The graph executor (adapter-contract §2.7)
> sits *above* this `Store` contract: it owns traversal algorithm, DSL ref-join
> lowering, and namespace-token resolution-at-boundary, and it reads/writes
> *exclusively* through the role-segregated `Store` roles — never bypassing them
> and never opening a backend directly. The `Store` provider owns the orthogonal
> concern of connection/concurrency/bounds (the ephemeral→pooled→daemon
> C-Hybrid swap). An executor swap (B-tree → CSR) and a provider swap (A → B
> daemon) are independent; neither is a contract change to the other.

### Blast radius / reversibility

**Blast radius: documentation only.** Two one-paragraph additions
(`graphstore-concurrency-contract/design.md` + `internal/graphstore/CONTRACT.md`,
and a confirming sentence in adapter-contract §2.7). No code, no interface, no
task scope change. The `Store` interface and the executor's use of it are already
as described.

**Reversibility: trivial.** It's a clarifying note. If the executor design later
needs a dedicated storage primitive (e.g. CSR materialization tier), that would
be a *new* `Store` role or provider variant — still within the C-Hybrid model —
and the note would be revised accordingly.

### What it unblocks

Unblocks **t1-none-adapter-end-to-end** by removing ambiguity about where the
executor's query routing (`§4` schema validation → query routing) plugs into the
gcc `Store` family — t1 delivers "query routing" and needs to know it routes
*through* `Store`, not around it. Also de-risks t4 (CRG adapter executor) and the
graphstore-concurrency Path B daemon work by confirming the executor doesn't
constrain the provider swap.

### Sequencing

Resolve **before t1 implementation** (clarifies t1's query-routing layering).
Documentation-only and low-controversy, so it can be ratified alongside O1 in the
first pass.

---

## Summary table

| OQ | One-line recommendation (PROPOSAL) | Unblocks | Sequence |
|---|---|---|---|
| **O1** | Keep the **mechanical (no-ack) cutover gate** for v1; close §14 Q2 as "no opt-out" (its lean is stale vs ratified §10.3). | t5 | before t5 |
| **O2** | **In-process HMAC** token signing (per-process ephemeral key) for v1; defer asymmetric to the `http`-backend milestone. | t1 (N13) | before t1 |
| **O3** | **Add `t7-app-type-profiles-wiring`** (`depends_on: [t1]`, `blocks: [release-minor]`) — the §15 "wiring" commit has no owning task. | release-minor | parallel w/ t2–t4 |
| **O4** | **Typed nodes for structure/state + `KGNote` projection for prose** (honors D1′ tiers); schema drafted in companion artifact. | work-tracking plan (future) | design now, build deferred |
| **O5** | Adopt all five spec recommendations: **content-hash mutation; new-note revocation; depth-1 code hops + unbounded note-chains; taint persists until `kg refresh`; load-bearing LinkKind allowlist**. | t2, t4 | before t2 |
| **O6** | **Fold A/C/D (testability gaps) into §11.1/§11.6 before t4; defer/reject G** (SQL-callable views break no-raw-SQL). | t4, t6 | before t4 (after re-reading the proposal) |
| **O7** | **Executor sits ABOVE the `Store` seam**; add a one-paragraph reconciliation note to both specs. Orthogonal swap dimensions. | t1 | before t1 |

---

## Safe-to-ratify-as-recommended vs genuinely contested

**Safe to ratify as recommended (low controversy; forced by already-ratified
design):**

- **O1** — the recommendation just realigns a stale open-question lean with the
  body's own ratified mechanical gate (§10.3). Documentation-only, zero blast
  radius.
- **O5** — every sub-decision is forced by a §5 design commitment that survived
  adversarial review; the "open question" is really "commit the recommendations."
- **O7** — affirms the layering the code and §2.7 already embody; a clarifying
  cross-reference, no code change.
- **O2** — HMAC-v1 is the conservative, YAGNI-respecting choice and is the only
  way to make N13 implementable for t1; the upgrade path to asymmetric is clean.
  Low controversy *given* v1's single-trust-domain threat model.

**Has a real decision to make, but the recommendation is well-grounded
(moderate):**

- **O3** — adding a task is a plan edit the maintainer should bless, but the gap
  is real and §15 explicitly asks for this "separate commit." The only judgment
  call is task granularity (separate t7 vs fold into t1), and the rationale for
  separate is strong (different subsystem, clean scope).
- **O4** — typed-nodes-vs-KGNote is a genuine design fork, but D1′'s ratified
  tier split forces the hybrid answer. Contested only in *schema detail* (the
  companion draft), not in the high-level posture. Build is deferred, so the cost
  of getting detail wrong now is low.

**Genuinely contested / cannot fully verify here:**

- **O6** — the *posture* (fold testability fixes before t4, reject SQL-views) is
  well-grounded, but the **specific content of proposals A/C/D lives in
  `.agents/proposals/crg-dual-read-parity-surface-2026-05.md`, which was outside
  this dossier's read set.** The exact §11.1/§11.6 edits must be reviewed against
  that proposal before ratification. This is the one OQ I'd flag as "ratify the
  direction, but verify the details against the source proposal first." G's
  rejection is safe regardless (it's a clear contract-invariant conflict).

---

*All recommendations above are PROPOSALS authored for the maintainer's morning
review. None is normative until ratified back into its owning spec via the
dot-agents proposal/review loop. Ratification of O3 is a `TASKS.yaml`/`PLAN.yaml`
edit and should go through `da workflow`, not a hand-edit.*
