# Spec: multi-provider-wave-dispatch

**Status:** draft
**Owner:** workflow / wave-engine
**Spec id:** `multi-provider-wave-dispatch`
**Composes with:** `task-tier-model-suggestion` (model SIZE), `skill-tiering-contract` (tier vocabulary), `app-type-profiles` / `execution-profile` (routing primitive)

---

## 1. Problem statement

The ultracode wave engine fans every eligible task out to a single agent runtime:
Claude, via the hard-coded `agentType: 'loop-worker'` seam in
`.claude/workflows/ultracode-wave-engine.mjs` (line 305). Every executor, on
every wave, is the same provider thinking the same way. We want **diversity of
thinking** — let a wave dispatch some tasks to Codex agents and some to Cursor
agents alongside Claude — so that:

- independent implementations of contract-bounded slices catch each other's
  blind spots (a Codex executor verified by a Claude verifier, etc.),
- model/provider monoculture risk on autonomous (T2) tasks is reduced,
- cost/latency can be steered per task (cheap provider for mechanical T0 work,
  premium provider for high-autonomy T2 orchestration).

Today there is no way to carry a `(provider, model)` decision from config →
wave engine → worker, and no contract that says a non-Claude worker must honor
the same delegation bundle, stealth mandate, write_scope, and closeout protocol
that a Claude `loop-worker` honors. Without that, a Codex or Cursor worker is a
parallel codepath that drifts from the canonical lifecycle.

This spec defines **provider selection and the provider seam**. It is the
sibling of `task-tier-model-suggestion`, which defines **model size**. Together
they resolve a full `(provider, model)` pair per task. This spec owns
*provider*; the sibling owns *size*; §6.4 defines exactly how they compose.

---

## 2. Goals

- **G1 — Provider as a routed facet.** A `(provider, model)` hint is a
  first-class, scope-overridable facet of the execution profile, resolved per
  app_type × stage, with per-task override — never a hard-coded constant in the
  engine.
- **G2 — One worker contract.** Codex and Cursor workers consume the *same*
  `delegationBundleYAML` contract a Claude `loop-worker` consumes, and return
  the *same* merge-back shape (`MERGEBACK_SCHEMA`). Provider is an
  implementation of one interface, not a fork of the lifecycle.
- **G3 — Parity is enforced, not assumed.** Every provider worker upholds:
  write_scope, the stealth mandate, the closeout protocol
  (`bundle.closeout.worker_must`), and the PR-readiness loop (verify-before-open
  → open PR → `da workflow merge-back`). A worker that cannot uphold these is
  not dispatchable.
- **G4 — A generalizable `agentType` seam.** The engine's
  `agent(prompt, { agentType })` call selects the runtime from the resolved
  provider, mapping `claude → loop-worker`, `codex → codex:codex-rescue`,
  `cursor → <cursor-agent>` — without the engine knowing provider internals.
- **G5 — Verifiable diversity.** A single wave can dispatch a real Codex worker
  AND a real Cursor worker that each open a valid, green PR via the canonical
  merge-back path.

---

## 3. Decisions (with rationale)

### D1 — Provider lives as a new `ModelProvider` facet on `AppTypeProfile`, NOT a new top-level AgentsRC field

`AppTypeProfile` already bundles three independently scope-overridable facets —
`Relevance`, `Topology`, `Lenses` (`internal/config/execution_profile.go:32–44`).
Provider routing is the same shape of decision ("for this app_type, in this
stage, what runtime"), so it becomes the **fourth facet**:

```go
type AppTypeProfile struct {
    Relevance     map[string]RelevanceClasses // facet 1
    Topology      Topology                    // facet 2
    Lenses        Lenses                      // facet 3
    ModelProvider ModelProvider               // facet 4  (this spec)
}
```

**Rationale:** the `execution_profile` key is already wired through
`CategoryMapMerge` (`resolver.go:40–47, 67`), so org→team→repo→project scope
layering and per-facet deep-merge apply to the new facet *for free* — a repo can
override provider while inheriting topology/lenses unchanged. Adding a separate
top-level field would require its own merge category and would not benefit from
the by_app_type routing that already dispatches verifier_sequence. The surface
findings explicitly identify this facet as "the cleanest home."

**Rejected alternative:** a standalone `default_model_provider` top-level field
keyed only by plan. Rejected because it cannot express per-app_type or
per-stage routing and bypasses the existing merge machinery.

### D2 — `ModelProvider` is a flat scalar object, resolved per-stage from `StageProfile` precedence, carried per-task in the bundle

```go
type ModelProvider struct {
    Provider string `json:"provider,omitempty"` // claude | codex | cursor
    Model    string `json:"model,omitempty"`    // optional explicit model id
}
```

It MUST stay a **scalar object (not an array)** so the `mergeMaps` per-key
deep-merge keeps merge-by-key semantics (`resolver.go:292–315`; surface risk).
Per-*verifier* provider selection (different provider for each entry in a
verifier_sequence) is explicitly deferred (§8) because it would force an array
and complicate the merge.

**Rationale for flat scalar:** the surface findings warn that any non-scalar
shape under `execution_profile` breaks `CategoryMapMerge`. A wave dispatches at
the granularity of one executor per task; one `(provider, model)` per task is
the natural unit.

### D3 — Per-stage routing reuses the four-stage `StageProfile` primitive; the engine dispatches the EXECUTOR stage

The four agentic stages — executor | verifier | reviewer | orchestrator — are
the uniform `StageProfile` primitive keyed by the outer map
(`agentsrc.go:490–502`; `StageProfile` carries only `Label` + `PromptFiles`
today). Provider can differ by stage: a Codex executor may still be verified by
a Claude verifier and reviewed by a Claude reviewer. The wave engine dispatches
**executors**, so the engine consumes the resolved *executor-stage* provider.
Verifier/reviewer provider routing is specified here for completeness but is
consumed by the staged-runtime (ISP), not the fan-out loop.

**Rationale:** keeping provider selection per-stage means "diversity of
thinking" can be applied surgically — e.g. always cross-provider-verify a
Codex-authored slice with a Claude verifier — and it reuses the existing stage
vocabulary rather than inventing a parallel one. The `StageProfile` struct has
no facet structure today (surface risk), so the per-stage provider default is
resolved from the `ModelProvider` facet keyed by stage in the relevance map
pattern (stage as map key), NOT by adding a field to `StageProfile` itself.

### D4 — The hint flows: ExecutionProfile (per-stage default) → TASKS.yaml per-task override → bundle.Verification.ModelProvider → worker

The carry path mirrors the proven `app_type` / `verifier_sequence` path exactly
(`delegation.go:1385–1405`, `delegation.go:1296–1309`):

1. **Config default.** `execution_profile.by_app_type[app_type].model_provider`
   (resolved through scope merge) is the per-app_type, per-stage default.
2. **Per-task override.** `CanonicalTask` gains optional `Provider` and `Model`
   fields (`commands/workflow/types.go:120`), inherited from
   `CanonicalPlan.DefaultProvider` / `DefaultModel` when unset — the same
   inheritance chain as `app_type` ← `default_app_type`
   (`delegation.go:1386–1389`).
3. **Bundle population.** `resolveFanoutVerifierDispatch` (or a sibling
   `resolveFanoutModelProvider`) resolves the effective `(provider, model)` and
   populates `bundle.Verification.ModelProvider`
   (`delegationBundleYAML.Verification`, `types.go:264–274`).
4. **Worker consumption.** The worker reads `bundle.verification.model_provider`
   and selects its runtime/model from it.

**Rationale:** this path already carries app_type and verifier_sequence end to
end; provider is one more sibling field in the same `Verification` block. No new
plumbing primitive is introduced.

### D5 — The engine's `agentType` seam is generalized via a provider→agentType map; provider is read from the SCOUT, not invented by the engine

`agent(prompt, { agentType })` (engine line 269/305) is the single dispatch
seam. Today `agentType` is the constant `'loop-worker'`. It becomes a lookup:

```js
const AGENT_TYPE_BY_PROVIDER = {
  claude: 'loop-worker',
  codex:  'codex:codex-rescue',
  cursor: '<cursor-agent-type>',  // see D6 / open question Q1
}
```

The scout already mirrors the `da config relevance` envelope per task (engine
lines 209–224). It is extended to also return the resolved
`model_provider` facet as a **sibling of `topology`** (identical nesting to the
CLI envelope and to the existing `matched`/`topology` split). The engine reads
`t.modelProvider.provider`, looks up `AGENT_TYPE_BY_PROVIDER`, and passes the
result as `agentType`. The engine does NOT know provider internals — Codex
internals live behind `codex:codex-rescue`, Cursor internals behind its agent.

**Rationale:** the `codex:codex-rescue` agent
(`.claude/plugins/.../agents/codex-rescue.md`) is already a *thin forwarding
wrapper* — `tools: Bash`, one call to `codex-companion.mjs task ...` — i.e. it
is already an `agentType` that accepts a prompt and runs work behind it. The
engine treats every provider uniformly: prompt in, merge-back out. This keeps
the engine's contract surface a single `agent(prompt, {agentType, schema})`
call regardless of provider.

### D6 — Codex consumes the bundle as-is; Cursor is dispatched through an analogous thin wrapper agent

- **Codex:** `codex:codex-rescue` already forwards an arbitrary prompt to the
  Codex runtime and is a no-judgment wrapper. The wave engine hands it the
  *same fan-out prompt* it hands a Claude worker (materialize bundle via
  `da workflow fanout`, implement write_scope, verify, open PR, merge-back) plus
  the bundle path. Codex executes the canonical lifecycle inside its runtime.
  Required parity is enforced by the prompt + bundle, not by Codex-specific
  logic. (The default codex-rescue is `model: sonnet`; the resolved
  `bundle.verification.model_provider.model` overrides via the wrapper's
  `--model` pass-through — see Q2.)
- **Cursor:** a new thin wrapper agent (analogous to `codex-rescue`) forwards
  the same prompt to the Cursor agent runtime and returns the merge-back
  envelope. It MUST NOT introduce a parallel contract.

**Rationale:** G2/G3 require one contract. The bundle + prompt ARE the contract;
each provider wrapper is responsible only for "run this prompt in my runtime and
return the merge-back JSON." This is exactly the `codex-rescue` design already
in the tree, generalized.

### D7 — Parity is contractual and pre-dispatch-gated

A provider is **dispatchable** only if its wrapper can demonstrably uphold all
four parity obligations (G3). The four obligations, restated as the worker
contract every provider inherits from the bundle:

| Obligation | Source of truth | Enforced by |
|---|---|---|
| write_scope | `bundle.scope.write_scope` | merge-back diff confined to scope |
| stealth mandate | engine prompt + global rule | no AI attribution in commit/PR/comments |
| closeout protocol | `bundle.closeout.worker_must` | `da workflow merge-back` records artifact |
| PR-readiness loop | engine prompt steps 4–5 | verify green → open PR → merge-back |

A provider that cannot author commits as the user (stealth), cannot confine to a
write_scope, or cannot run `da workflow merge-back` is **not added to
`AGENT_TYPE_BY_PROVIDER`**. This is the gate, not a runtime fallback.

**Rationale:** G3 says parity is enforced, not assumed. The map IS the
allowlist of provider runtimes proven to uphold the contract.

### D8 — Provider selection POLICY is config-declared, not hard-coded; the engine only resolves and dispatches

The selection policy is expressed in the `ModelProvider` facet via scope-layered
config, supporting three declarable strategies (the engine implements none of
them as logic — it reads the resolved result):

- **Per-stage / per-app_type fixed** (default): the facet names a provider for
  an app_type's executor stage (e.g. `go-cli` executors → claude, `ideation`
  executors → codex).
- **Tier-aware** (composes with `skill-tiering-contract`): provider can be
  steered by the task's declared tier — e.g. route T2 (high-autonomy) tasks to a
  diversity provider so monoculture risk is highest where reliability is lowest
  (T2 ~60–70% ceiling per Shiv). Tier→provider is a config rule, analogous to
  the sibling's tier→model-size rule.
- **Round-robin** (opt-in, deferred mechanism §8): rotate providers across
  eligible executors in a wave for maximal diversity. Specified as a policy
  *name*; the rotation mechanism is fast-follow.

**Rationale:** the tiering findings warn against code-hardcoding model/provider
mappings ("Consider config-layer defaults rather than code-hardcoding") because
model/provider availability churns. Policy in config + a dumb engine keeps the
engine stable across provider catalog changes.

---

## 4. Requirements (behavioral)

- **R1.** A `(provider, model)` pair MUST be resolvable for any eligible task,
  with this precedence: explicit per-task `provider`/`model` →
  plan `default_provider`/`default_model` → resolved
  `execution_profile.by_app_type[app_type].model_provider` (executor stage) →
  built-in default (`claude` / unset model).
- **R2.** The resolved provider facet MUST survive org→team→repo→project scope
  merge with per-facet independence: overriding provider MUST NOT disturb
  topology, lenses, or relevance for that app_type.
- **R3.** The scout MUST return the resolved `model_provider` per task as a
  sibling of `topology`, mirroring the CLI envelope nesting (never folded into
  `topology`).
- **R4.** The engine MUST map the resolved provider to an `agentType` via
  `AGENT_TYPE_BY_PROVIDER` and pass it to `agent(prompt, { agentType })`. An
  unknown/unmapped provider MUST fall back to `claude`/`loop-worker` with a
  logged warning — never silently skip the task.
- **R5.** Every provider worker MUST consume the identical `delegationBundleYAML`
  produced by `da workflow fanout` and MUST return a merge-back conforming to
  `MERGEBACK_SCHEMA` (`{plan, task, branch, pr_url, status, summary}`).
- **R6.** Every provider worker MUST: confine its diff to
  `bundle.scope.write_scope`; leave no Claude/AI attribution in commits, PR
  title/body, or comments; run the closeout
  (`da workflow merge-back`) so the task advances to
  `awaiting_owner_review` and the slot frees; and open a PR only after
  verification is green.
- **R7.** A provider absent from `AGENT_TYPE_BY_PROVIDER` MUST NOT be dispatched
  (it has not been proven to uphold R6).
- **R8.** The four-place AgentsRC schema contract MUST be upheld for the new
  facet: struct field, `agentsRCCore` mirror, custom Marshal/UnmarshalJSON,
  `agentsRCKnown` map, and `schemas/agentsrc.schema.json` updated atomically
  (per schema-usage). The bundle struct and
  `schemas/workflow-delegation-bundle.schema.json` MUST stay mirrored.
- **R9.** `(provider, model)` composition with the sibling spec MUST be
  deterministic: provider from this spec, size from
  `task-tier-model-suggestion`; when both resolve, the worker dispatches to
  `provider` with `model` = explicit model if set, else the tier→size default
  for that provider (§6.4).

---

## 5. Architecture / carry path (informative)

```
                   .agentsrc.json execution_profile.by_app_type[T].model_provider   (D1, scope-merged)
                                        │  per-stage (executor) default
                                        ▼
   TASKS.yaml task.provider/model  ──►  resolveFanoutModelProvider  ◄── plan.default_provider/model   (D4)
                                        │
                                        ▼
                          bundle.verification.model_provider = {provider, model}     (D4, types.go:264–274)
                                        │
   scout returns model_provider  ◄──────┤  (sibling of topology, mirrors CLI envelope)   (D5, R3)
   as sibling of topology                │
                                        ▼
   engine: agentType = AGENT_TYPE_BY_PROVIDER[provider]                              (D5)
                                        │
            ┌───────────────────────────┼───────────────────────────┐
            ▼                           ▼                           ▼
      loop-worker (claude)      codex:codex-rescue (codex)   cursor-agent (cursor)
            │                           │                           │
            └──── same prompt: fanout → implement write_scope → verify → PR → merge-back ────┘
                                        │
                                        ▼
                          MERGEBACK_SCHEMA {plan,task,branch,pr_url,status,summary}   (R5)
```

---

## 6. Key interactions

### 6.1 Provider seam in the engine (D5, R4)

`agentType` is the only line that changes per task. The fan-out call becomes
`agentType: AGENT_TYPE_BY_PROVIDER[t.modelProvider?.provider] ?? 'loop-worker'`.
The prompt body is provider-agnostic — the existing fan-out prompt already
encodes the full lifecycle (steps 1–5, stealth mandate, merge-back). Codex and
Cursor receive it verbatim plus their resolved model.

### 6.2 Bundle parity (G2, R5, R6)

No provider-specific bundle exists. `da workflow fanout` emits one
`delegationBundleYAML`; the only provider-aware field is
`verification.model_provider`. The worker reads `scope.write_scope`,
`closeout.worker_must`, `verification.verifier_sequence`, and
`verification.model_provider` from the same document.

### 6.3 Verifier/reviewer provider (D3)

Executor provider is consumed by the wave engine. Verifier and reviewer provider
(if a stage routes to a non-Claude runtime) is consumed by the staged-runtime
(ISP) reading the same `model_provider` facet keyed by the `verify` / `review`
stage. This spec defines the data; ISP consumption is in scope for the plan but
the wave engine's verification path (§7) exercises only the executor seam.

### 6.4 Composition with `task-tier-model-suggestion` (R9, D8)

This spec resolves **provider**. The sibling resolves **model size** from tier
(T0→haiku, T1→sonnet, T2→opus). They compose at bundle-population time:

- If `task.model` (explicit) is set → use it as-is with the resolved provider.
- Else the sibling supplies a *size* (haiku/sonnet/opus tier) and this spec
  supplies a *provider*; the worker maps the size to that provider's nearest
  model class (a provider-local size table, e.g. codex small/medium/large).
- Tier-aware provider policy (D8) and tier→size (sibling) read the **same** task
  `tier` field — neither spec owns tier; both consume `skill-tiering-contract`.

The two specs are intentionally separable: provider can ship with model size
left at provider-default, and vice versa.

---

## 7. Done criteria (verifiable)

- **DC1.** `execution_profile.by_app_type[T].model_provider` round-trips through
  load → scope-merge → marshal with `additionalProperties:false` intact, and a
  repo-scope provider override does not perturb topology/lenses (unit test on
  `CategoryMapMerge` for the new facet).
- **DC2.** `da workflow fanout` for a task whose resolved provider is `codex`
  emits a bundle with `verification.model_provider.provider: codex`, validated
  against `schemas/workflow-delegation-bundle.schema.json`.
- **DC3.** Per-task override beats plan default beats config default beats
  built-in (`claude`) — table-driven test on `resolveFanoutModelProvider`.
- **DC4.** Engine maps `codex`→`codex:codex-rescue` and `cursor`→cursor-agent;
  an unknown provider falls back to `loop-worker` with a warning (engine unit
  test in `ultracode-wave-engine.test.mjs`, the existing pure-helper test seam).
- **DC5 (the headline verification).** A single live wave dispatches a **real
  Codex worker AND a real Cursor worker** on two distinct eligible tasks; each
  consumes its bundle, confines to write_scope, opens a **valid, green PR** with
  no AI attribution, and returns a `MERGEBACK_SCHEMA` envelope that advances its
  task to `awaiting_owner_review`. Both PRs are inspectable on the `org` remote.
- **DC6.** Stealth audit: `git log` and the two PR bodies/comments contain no
  Claude/Codex/Cursor/AI-attribution trace (parity obligation R6).

---

## 8. Deferred (explicitly out of scope)

- **Per-verifier provider selection** (different provider per verifier_sequence
  entry) — requires an array shape under `model_provider`, which breaks
  `CategoryMapMerge` merge-by-key (D2 risk). Single executor-provider per task
  only, for now.
- **Round-robin rotation MECHANISM** (D8) — the policy *name* is declarable; the
  per-wave rotation logic is a fast-follow once fixed per-app_type routing is
  proven.
- **Provider-local model size tables** beyond a minimal small/medium/large
  mapping — full per-provider catalog management is deferred to the sibling
  spec's rollout.
- **Cursor wrapper agent implementation details** — this spec mandates a thin
  wrapper analogous to `codex-rescue`; the exact CLI/runtime invocation is a
  plan-level decision (Q1).
- **Cost/latency-optimizing scheduler** — choosing provider to minimize $ or
  wall-clock is an optimization layer above the policy facet.
- **Rewriting existing Claude-only history/PRs** — applies going forward only.

---

## 9. Open questions (must be resolved in the plan)

- **Q1.** What is the concrete Cursor `agentType` / wrapper? Is there a Cursor
  agent runtime callable the way `codex:codex-rescue` shells to
  `codex-companion.mjs`, or must a new wrapper agent + runtime script be
  authored? (Blocks DC5 Cursor half.)
- **Q2.** How does the resolved `model` reach the Codex/Cursor runtime — via the
  wrapper's `--model` pass-through (codex-rescue leaves model unset by default),
  via the prompt, or via the bundle alone? Define the model-injection path per
  wrapper.
- **Q3.** Does the per-stage provider key live as a stage-keyed map inside the
  `ModelProvider` facet (mirroring `Relevance map[string]RelevanceClasses`), or
  is the facet a single executor-stage scalar with verifier/reviewer provider
  deferred? (D3 leans stage-keyed; confirm before schema lands.)
- **Q4.** Should `CanonicalTask`/bundle also gain a `tier` field now (shared with
  the sibling), or does this spec consume `tier` read-only and leave the field
  addition to `task-tier-model-suggestion`? Avoid double-adding the same field.
- **Q5.** Parity proof gating (D7/R7): is the dispatchability gate a manual
  allowlist (the map) or an automated pre-dispatch capability probe? For v1 the
  map is the gate; confirm no automated probe is required.

---

## 10. Relationships

- **`task-tier-model-suggestion` (sibling, model SIZE):** complementary half of
  the `(provider, model)` decision. Composition defined in §6.4 / R9. Both
  consume `tier`; neither owns it. Ship-independent.
- **`skill-tiering-contract` (`design.md` §3–4):** supplies the tier vocabulary
  (T0–T4) that the tier-aware provider policy (D8) keys on. Read-only consumer.
- **`app-type-profiles` / `execution-profile`:** the routing primitive this spec
  extends. The `ModelProvider` facet is the 4th sibling of Relevance/Topology/
  Lenses; it inherits the by_app_type routing and `CategoryMapMerge` scope
  layering already proven for `verifier_sequence`.
- **Wave engine (`.claude/workflows/ultracode-wave-engine.mjs`):** the consumer.
  This spec generalizes its `agentType` seam (line 305) and extends its scout
  envelope (lines 209–224) and fan-out prompt — without changing its
  topology-driven admission logic.
- **`codex` plugin (`codex:codex-rescue`):** the existing thin-wrapper agent
  reused as the `codex` `agentType` (D5/D6). Establishes the wrapper pattern the
  Cursor agent copies.
