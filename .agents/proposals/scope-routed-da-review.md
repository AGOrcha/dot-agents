# Proposal: Scope-Routed `da review`

## Problem

The repo now has a routing rule for proposals in `.agents/rules/dot-agents/proposal-routing.md`,
but that rule only answers **where** a proposal should land. It does not yet define the full
high-level contract for `da review` when proposal queues exist across project, user, team, and
org scope.

Today the shipped implementation is still effectively user-scope-first. The newer architecture
notes and research expect broader routing, but the actual command behavior, review semantics,
ambiguity handling, and durable-write rules are not yet described in one place.

That leaves an uncomfortable gap:

- the routing intent exists
- the product docs now point at scope-routed behavior
- but `da review` itself is not yet fully specified at the business-rules level

This proposal closes that gap at the right level of abstraction and explicitly requires a follow-on
spec and plan before implementation work proceeds.

---

## Root Cause

The current proposal-routing rule and the broader workflow docs have been carrying two different
responsibilities at once:

1. routing intent
2. `da review` command behavior

Those are not the same thing.

The routing rule should stay small and normative:

> Which scope owns the durable write?

But `da review` needs a separate contract covering:

- what queues are visible
- how scope is surfaced in list/show/approve/reject
- how approve/reject behave per scope
- how targets resolve and validate in each scope
- what happens when IDs collide across scopes
- what must remain transactional

Without that split, a later implementation agent would be forced to invent policy from scattered
notes.

---

## Proposed Solution

Adopt scope-routed proposal review as the intended product direction for `da review`.

At a high level:

- proposals remain YAML review artifacts
- routing is determined by durable write scope, not artifact type alone
- `da review` becomes the review surface for the routed proposal queues the operator can see
- approve/reject/archive behavior stays within the selected scope
- repo, user, team, and org proposals all follow the same lifecycle shape, even if their backing
  stores differ

This proposal is intentionally **not** the implementation plan. It defines the business-level
requirements that the later spec and plan must refine.

---

## High-Level `da review` Requirements

### 1. Routing follows durable write scope

`da review` must respect the routing rule in `.agents/rules/dot-agents/proposal-routing.md`.

- project-owned writes review in project scope
- user-owned writes review in user scope
- team-owned writes review in team scope
- org-owned writes review in org scope

Examples that should remain valid:

- team skill -> team
- user hook -> user
- project code or repo-local workflow work -> project
- project subagent -> project
- org plugin -> org
- org shared library metadata/config -> org
- team KG note -> team
- project lesson -> project
- user KG note -> user

### 2. Queue and archive are scope-local

Approving or rejecting a proposal must keep the proposal inside its own scope lifecycle.

- a project proposal archives in the project archive queue
- a user proposal archives in the user archive queue
- a team proposal archives in the team archive queue
- an org proposal archives in the org archive queue

Review must not silently move a proposal across scopes as part of approval.

### 3. Target resolution is scope-relative

`target` must resolve relative to the routed scope root.

- project scope resolves against the repo root
- user scope resolves against `~/.agents/`
- team/org resolve against their configured canonical stores

The same path-safety rules still apply:

- no absolute paths
- no parent-directory traversal
- no writes outside the routed scope root

### 4. Scope must be visible to the reviewer

If `da review` shows proposals from more than one scope, the command surface must make scope
visible rather than implicit.

At minimum the reviewer needs to know:

- which scope a proposal belongs to
- which queue/store it came from
- which scope root will be mutated on approval

### 5. ID ambiguity must be deterministic

If the same proposal ID exists in more than one visible scope, `da review show/approve/reject`
must not guess.

The eventual spec must define one deterministic rule, such as:

- explicit scope selection required on ambiguity, or
- a strict precedence order with a loud ambiguity error when multiple matches exist

But silent best-effort matching is not acceptable.

### 6. Approval remains transactional

The existing safety bar should carry forward.

- validate proposal shape
- validate target inside the routed scope root
- apply change in the routed scope
- run whatever refresh/reload behavior is required for that scope
- if apply or refresh fails, proposal remains pending in its original queue

The scope change must not weaken rollback expectations.

### 7. Common schema, different backing stores

The proposal YAML lifecycle should remain conceptually uniform across scopes even if storage is
not.

That means the same review semantics still apply across project/user/team/org even if team/org are
not simple local filesystem queues.

### 8. Project scope includes repo work, not only `.agents/`

Project-local review should be able to cover repo-owned durable outputs, not only repo-local agent
artifacts.

That includes cases like:

- repo-local `.agents/` artifacts
- project lessons
- project plans/spec companions
- repo-owned workflow defaults
- project work artifacts when the approved change is intentionally project-owned

This keeps project scope aligned to ownership rather than artificially limiting it to one
subdirectory.

---

## Required Follow-On Artifacts

Per `workflow-artifact-model.md`, this proposal should not be treated as sufficient implementation
guidance on its own.

### Required spec

Create a canonical spec for scope-routed `da review`.

That spec should own:

- business rules and reviewer-facing behavior
- exact review semantics for list/show/approve/reject
- visibility rules across scopes
- ambiguity handling
- transactional guarantees
- done criteria for the user-visible command behavior
- open questions that still need product decisions

The spec should **not** own module layout, function names, or task sequencing.

### Required plan

Create a plan only after the spec is stable enough.

That plan should own:

- architecture and module boundaries
- scope-store abstraction choices
- command wiring and config-layer integration points
- task breakdown and dependency order
- verification strategy and test slices
- task scheduling for implementation rollout

The plan should explicitly resolve how the implementation splits responsibilities between command
surface, proposal loading, target resolution, archive behavior, and scope-store adapters.

---

## What This Proposal Does Not Decide

- exact flag syntax for scope selection
- final module/function names
- final precedence rules when multiple queues are visible
- exact team/org storage APIs
- whether rollout lands all scopes at once or project+user first

Those are deliberately deferred to the spec and plan layers.

---

## Open Questions

1. Should `da review` default to showing every visible scope, or should some scopes require an
   explicit opt-in flag?
2. When proposal IDs collide across scopes, should the command require explicit scope selection,
   or is there an acceptable precedence order?
3. Should project-scope approval be allowed for general repo files immediately, or should the
   first rollout limit project proposals to a narrower artifact set and widen later?
4. What refresh/reload contract applies per scope after approval, especially for team/org-backed
   stores?
5. Does the current YAML proposal schema need an explicit `scope` field, or should scope remain
   purely a property of the queue/store that holds the proposal?