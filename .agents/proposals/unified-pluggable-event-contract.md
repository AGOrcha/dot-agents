# Unified pluggable event-contract

**Status:** draft design proposal, project-local (`.agents/proposals/`) per `[[proposal-routing]]`.
**Created:** 2026-05-28
**Routing rationale:** alters dot-agents-local surfaces — the daemon event schema, the hook-sentinel
schema, and the registry-driven dispatch in `commands/` / `internal/` — so it is project-scope, not a
shared `~/.agents/` resource. Candidate to graduate to
`workflow/specs/workflow-orchestrator-daemon/design.md` once the contract stabilizes.
**Plan:** sibling task on `pr10-branch-split` (added 2026-05-28), surfaced by maintainer review #160
line 147 as the convergence target for #157 + #160.
**Parents / siblings:**
- `[[monitor-pr-review-comment-routing]]` §4 (event payload schemas) + §8.6 (the pluggable
  event-contract open item that names this task).
- `[[hook-schema-extension-mechanism]]` (#157 followup — the hook-sentinel side of the same request).
- #157 hook-sentinels-generic/custom + #160 event-contract — the two PRs whose convergence this
  proposal captures.

---

## §1. Problem — per-type rework cost

Today, adding a new "kind" to any of three surfaces forces a central code edit:

- **Daemon events** — `[[monitor-pr-review-comment-routing]]` §4 defines four event types
  (`review.submitted`, `review_comment.posted`, `issue_comment.posted`, `review_batch.posted`).
  Each new event type is a new shape the renderer + ingester must learn, currently by editing the
  dispatch code that switches on `type`.
- **Hook sentinels** — #157's hook-sentinels-generic/custom path lets hooks emit sentinel markers
  the orchestrator reacts to. Each new sentinel kind today is a code change at the sentinel
  parse/route site.
- **Future plug-ins** — any later surface that wants to emit a typed event/marker the orchestrator
  consumes inherits the same per-type edit cost.

Three surfaces, three near-identical "switch on a string tag, hand-edit a central dispatcher per new
kind" patterns. Maintainer review #160 line 147 named the convergence: dot-agents should NOT have to
rework code each time a new event / sentinel / hook type is added. The three surfaces want **one**
generic, pluggable shape.

## §2. Approach — a registry-driven, schema-additive contract

Adopt the `verifier_profiles` registry as the model: a typed kind is **declared in a registry**
(schema-additive, validated against the registry at dispatch — not enumerated in an `enum` at schema
time), and dispatch is **table-driven over the registry**, so adding a new kind is a registry/config
entry, never a central code edit.

The contract has three parts:

1. **A common envelope.** Every event/sentinel/hook payload shares a minimal envelope —
   `{ type, source, occurred_at, idempotency_key, payload }` — where `type` is the registered kind,
   `source` is the emitting surface (daemon, hook, sentinel, plug-in), and `payload` is the
   kind-specific body. The envelope is stable; only `payload` varies by kind.

2. **A kind registry.** Kinds are registered (config + discovered, mirroring `verifier_profiles`
   keying — an object keyed by kind name) rather than baked into a switch. A registered kind carries
   its `payload` shape (or a reference to it) and its routing/render hint. Unknown-but-registered
   kinds route through a generic handler; unknown-and-unregistered kinds are rejected/logged, not
   silently mis-dispatched.

3. **Table-driven dispatch.** Renderers, the ingester, and the orchestrator's reaction logic look up
   the handler for a `type` in the registry rather than `switch`-ing on it. A new event/sentinel/hook
   kind is added by registering it; no central dispatcher edit, no `enum` bump.

This is deliberately the same shape `verifier_profiles` already proves in `.agentsrc.json`:
schema-additive (`additionalProperties` accepts new keys, no `enum`), pluggable, validated at
dispatch against discovered/registered entries rather than at schema-load time. Per `[[schema-usage]]`,
any new top-level `.agentsrc.json` field added to carry the registry must touch all six sync points
(struct + `agentsRCCore` mirror + `UnmarshalJSON` + `MarshalJSON` + `agentsRCKnown` map +
`schemas/agentsrc.schema.json`), with `additionalProperties` left open (no enum) so the registry stays
extensible.

## §3. Surfaces this unifies

| Surface | Today (per-type edit) | Under the contract |
|---|---|---|
| **Daemon events** (`[[monitor-pr-review-comment-routing]]` §4) | each event type is a new shape the renderer/ingester switches on | `type` is a registered kind; the §4 shapes become the first registered consumers; new event types are registry entries |
| **Hook sentinels** (#157 / `[[hook-schema-extension-mechanism]]`) | each sentinel kind is a parse/route code change | sentinels emit the common envelope with a registered `type`; new sentinel kinds are registry entries |
| **Future plug-ins** | inherit the per-type edit cost | any plug-in emitting a registered kind routes through the same table-driven dispatch with zero central code edit |

The win is shared across all three: the central code (renderer, ingester, sentinel router, plug-in
host) is written once against the registry, and the per-kind cost collapses to a declaration.

## §4. Relationship to existing artifacts

- **`[[monitor-pr-review-comment-routing]]` §4 event schemas** — the four event payload shapes
  (`review.submitted`, `review_comment.posted`, `issue_comment.posted`, `review_batch.posted`) are
  the **first consumers** of this contract: each becomes a registered kind with its payload shape,
  rather than a hand-switched type. §8.6 of that proposal names this task as the convergence target;
  this proposal answers it.
- **Hook-sentinel schema (#157) / `[[hook-schema-extension-mechanism]]`** — the sentinel side of the
  same request. Sentinels adopt the common envelope so the orchestrator's sentinel router becomes
  table-driven over the same registry. The hook-schema-extension mechanism is how a hook *declares*
  the sentinel kind it emits; this contract is what the *consumer* binds to.
- **`verifier_profiles` (`.agentsrc.json`)** — the precedent model: object-keyed-by-name,
  schema-additive, no enum, validated at dispatch. The registry here mirrors it.
- **`[[schema-usage]]`** — the six-point AgentsRC field-sync discipline + the
  "`additionalProperties: false` to catch drift, open registries to stay extensible" guidance govern
  the `.agentsrc.json` registry field.

## §5. What this proposal does NOT do (scope)

This is a **design proposal**, not an implementation plan. It does not:

- Define the final envelope field names down to the wire (left to the spec graduation under
  `workflow/specs/workflow-orchestrator-daemon/design.md`).
- Enumerate the registry's storage location or the exact `.agentsrc.json` key name — that is a
  planning decision once the daemon + hook-sentinel work converges.
- Re-decide the §4 daemon event shapes or the #157 sentinel shapes — those stand; this proposal makes
  them registry-driven rather than hand-switched.
- Land code. The pr10-branch-split sibling task owns sequencing the implementation against whatever
  registry surface the daemon and hook-sentinel work ship.

## §6. Open questions

- **OQ-1 — One registry or per-surface registries?** A single unified kind-registry is simplest for
  the consumer but couples daemon-event kinds and hook-sentinel kinds in one namespace. Alternative:
  per-surface registries sharing the common envelope contract. Lean: one envelope, one registry,
  namespaced `type` (`event.*`, `sentinel.*`) so the surfaces stay distinguishable without separate
  registries.
- **OQ-2 — Registry location.** `.agentsrc.json` top-level field vs a dedicated registry file vs
  code-side registration. Mirroring `verifier_profiles` argues for `.agentsrc.json`; high-churn
  built-in kinds may argue for code-side defaults + config overrides.
- **OQ-3 — Validation timing.** Validate a `type` against the registry at emit time (fail-fast) or
  dispatch time (fail-soft to a generic handler)? Mirrors the `lens_modes` resolution debate in
  `[[lens-template-and-mode-skills]]` OQ-3 — lean fail-soft to a generic handler with a warning.

## §7. Cross-links

- `[[monitor-pr-review-comment-routing]]` §4 + §8.6 — event schemas + the open item naming this task.
- `[[hook-schema-extension-mechanism]]` — the hook-sentinel side of the same convergence (#157).
- `[[schema-usage]]` — AgentsRC field-sync + open-registry discipline.
- `[[workflow-orchestrator-daemon]]` — the spec this contract graduates into once stable.
