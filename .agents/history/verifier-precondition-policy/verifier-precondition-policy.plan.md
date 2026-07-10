# Plan: Configurable verifier precondition policy (lpf-e rework)

> **Status: SHIPPED (2026-06-22).** All four slices merged to master:
> Slice A/pa = PR #51, Slice B1/B2/pb = PR #56, Slice B3/B4/pc = PR #57,
> Slice B5/pd = PR #60. This `.plan.md` is the **canonical design doc** for the
> verifier-precondition-policy concern (there is no separate spec); the
> `precondition_policies` registry, lockfile resolution, and `da config verify`
> fail-closed validation described below are live and verified against the shipped
> tree. One reconciliation vs the design: the verifier *dispatch call site* shipped
> as the overridable `resolvePreconditionPolicy` package var with a documented
> future call site (lpf-e had no live dispatch caller), exactly as the Slice B3/B4
> allowance permitted — the `verifierTransition` rework itself (off the deleted
> `verifierPreconditions` struct) did ship.

Implements the `layered-pr-fanout` spec amendment §2.3/§3.2/§6.1 (configurable
precondition policy) + `unified-event-contract` D1–D3 + the stage-profile config
model. Supersedes the hardcoded `verifierPreconditions` struct (fold-back
`verifier-preconditions-should-be-configurable`).

**Contract recap:** the `in_progress → awaiting_agent_review` gate is a *policy*
of *predicates over the unified event contract*. A policy is a **named entry in
a top-level `precondition_policies` registry**, referenced by name from a stage
profile (`app_type`), and the verifier reads the **resolved policy from the
lockfile** (not raw `.agentsrc.json`). Built-in `default` policy = the historical
PR/go-cli gate, so behavior is unchanged until a project declares otherwise.

## Data model (canonical names — use exactly)

Go (Slice A, `commands/workflow/preconditions.go`):
```go
// A predicate over one registered event/signal kind.
type Predicate struct {
    Signal string            // registered kind, e.g. "event.pr.open", "gate.quality.sonar"
    Args   map[string]string // kind-specific, e.g. {"equals":"green"} / {"provider":"sonar"}
}
type PreconditionPolicy struct {
    Name       string
    Predicates []Predicate
}
// Observed signals the verifier evaluates against (producer fills it).
type SignalSnapshot map[string]string // kind -> observed value, e.g. "signal.ci.rollup":"green"
// Registry entry. reason is the operator hint when !ok.
type PredicateEvaluator func(p Predicate, snap SignalSnapshot) (ok bool, reason string)
```

Config (Slice B, `internal/config/agentsrc.go`) — mirrors the Go shape:
```go
type PredicateSpec struct { Signal string `json:"signal"`; Args map[string]string `json:"args,omitempty"` }
type PreconditionPolicySpec struct { Predicates []PredicateSpec `json:"predicates"` }
// top-level: PreconditionPolicies map[string]PreconditionPolicySpec `json:"precondition_policies,omitempty"`
// StageProfile gains: PreconditionPolicy string `json:"precondition_policy,omitempty"`
```

## Slice A — `pa-predicate-evaluator`

New file `commands/workflow/preconditions.go`:
1. The types above.
2. A registry `var predicateEvaluators = map[string]PredicateEvaluator{}` plus
   `registerPredicate(kind string, ev PredicateEvaluator)` and lookup
   `evaluatorFor(signal string) (PredicateEvaluator, bool)` — exact-match first,
   then longest registered **prefix** (so `gate.quality.sonar` resolves the
   `gate.quality.` evaluator). Reference event-kind constants from
   `internal/events` (do not hardcode raw strings where a constant exists).
3. Built-in evaluators (registered in `init()`):
   - `event.pr.open` — ok iff `snap["event.pr.open"]=="true"`.
   - `signal.ci.rollup` — ok iff `snap["signal.ci.rollup"]==p.Args["equals"]`
     (default expected `events.RollupGreen`).
   - `gate.quality.` (prefix) — ok iff `snap[p.Signal]=="pass"`; provider is the
     suffix (`sonar`, `codeql`, …), no privileged field.
   - `metric.new_code_issues` — ok iff `snap["metric.new_code_issues"]=="0"`
     (or `==p.Args["equals"]`).
4. `var defaultPreconditionPolicy = PreconditionPolicy{Name:"default", Predicates: …}`
   = the four above (this reproduces today's gate exactly).
5. `func evaluatePolicy(policy PreconditionPolicy, snap SignalSnapshot) (ok bool, reason string)`:
   if `len(policy.Predicates)==0` → evaluate `defaultPreconditionPolicy` instead
   (NEVER an empty/open gate). For each predicate: look up evaluator; **unknown
   kind ⇒ fail-closed** `ok=false, reason="unregistered precondition signal kind <x>"`.
   First failing predicate's reason is returned. All ok ⇒ `true,""`.

Edit `commands/workflow/transitions.go`:
- DELETE `verifierPreconditions` struct + its `met()`.
- Change `verifierTransition(taskID, from string, pre verifierPreconditions)` →
  `verifierTransition(taskID, from string, policy PreconditionPolicy, snap SignalSnapshot)`.
  Keep the §6.1 ownership guard (refuse `from != in_progress`); replace the
  `pre.met()` call with `evaluatePolicy(policy, snap)`; on `!ok` return the same
  shape of error using `reason`.

Tests (`preconditions_test.go` new; update `transitions_test.go`): table-driven
per built-in predicate (satisfied + each unsatisfied-with-reason); unknown-kind
fail-closed; empty-policy → default fallback; full default policy met/unmet;
verifierTransition new-signature happy/ownership-guard/precondition-fail paths.

## Slice B1/B2 — `pb-config-registry`

`internal/config/agentsrc.go`, all **6 sync points** per `[[schema-usage]]`:
1. `AgentsRC` struct: `PreconditionPolicies map[string]PreconditionPolicySpec \`json:"precondition_policies,omitempty"\``.
2. `agentsRCCore` mirror: same field+tag.
3. `UnmarshalJSON`: `a.PreconditionPolicies = core.PreconditionPolicies`.
4. `MarshalJSON`: `PreconditionPolicies: a.PreconditionPolicies` in the core literal.
5. `agentsRCKnown`: add `"precondition_policies": true`.
6. `schemas/agentsrc.schema.json`: `precondition_policies` object property,
   `additionalProperties` = the policy object (`predicates` array of
   `{signal, args}`), **no `enum`**, nested `additionalProperties:false` on the
   predicate object.
Also add `PreconditionPolicy string \`json:"precondition_policy,omitempty"\`` to
`StageProfile` (agentsrc.go:497) + its schema entry. Tests: round-trip
(parse→marshal stable), unknown-key rejection still works, schema validates a
sample.

## Slice B3/B4 — `pc-lockfile-resolution`

New `internal/config/precondition_resolve.go`:
```go
func ResolvePreconditionPolicy(projectPath, appType string) (workflowPolicy, error)
```
- Read the **resolved** config from the lockfile via `ResolveLocked(projectPath)
  → *Snapshot` (the merged/locked state — NOT `LoadAgentsRC`). Confirm the
  Snapshot exposes `precondition_policies` + the resolved `stage_profiles`; if it
  does not yet, extend the Snapshot projection (that extension is in-scope here
  and is the config-v2 lock-surface dependency — coordinate with the config-v2
  resolver owners).
- app_type → stage profile → `precondition_policy` name → named entry in the
  resolved `precondition_policies` → convert `PreconditionPolicySpec`→ the
  `commands/workflow` `PreconditionPolicy`. Unset name OR unset registry ⇒
  return the built-in default (do not error).
- Return type note: to avoid an import cycle, return a small config-side struct
  and convert at the workflow call site, OR move the shared `Predicate`/`Policy`
  types to a leaf package both import. Decide at impl time; prefer the leaf
  package if a cycle appears.

Wire the verifier dispatch call site (wherever `verifierTransition` is invoked)
to call `ResolvePreconditionPolicy(projectPath, appType)` and pass the policy +
the producer's `SignalSnapshot`. (lpf-e currently has no live dispatch caller;
if still absent, leave a clearly-marked TODO at the future call site and cover
the resolver with unit tests.)

Tests: synthetic lock with app_type→profile→named policy resolves correctly;
unset→default; a profile naming a missing policy surfaces the Slice-B5 error.

## Slice B5 — `pd-validation`

Fail-closed validation surfaced by `da config verify` / lint:
- a `StageProfile.precondition_policy` naming a key absent from
  `precondition_policies` → error (with the profile + name).
- a `PredicateSpec.Signal` whose kind is not registered (reuse
  `evaluatorFor`) → error (with the policy + signal).
Wire into the existing verify path (`internal/config/verify_layers.go` /
`commands/config/verify.go`). Tests for both error cases + a clean pass.

## Ordering & non-goals
- Order: pa → pb → (pc, pd). pc/pd depend on pb; pc also on pa.
- **Non-goal:** new predicate KINDS beyond the four built-ins (docs/dataset
  signals are future registrations — the registry makes them additive).
- **Non-goal:** the event *producers* that fill `SignalSnapshot` (owned by the
  lpf PR-producer / poll-detector work); this plan consumes the snapshot.
- Land on the `feature/lpf-e-transitions-poll` branch (updates PR #51) for
  Slice A; Slices B may be separate PRs stacked on it.
