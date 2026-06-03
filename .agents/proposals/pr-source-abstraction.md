# Proposal: PR state as the first unified pluggable event (engine + config + auth-proxy)

Status: PROMOTED 2026-05-31 → workflow/specs/pr-event-source/design.md (canonical); credential slice → external-agent-sources spec §4.1. This proposal retained for full rationale + examples.
Created: 2026-05-30
Context: the layered-pr-fanout poll-detector (§3.2/§3.4.5), the `pr-ci` verifier substrate
([[verifier-owns-ci-watch-shift-left]]), and the session coach/poller all need PR/CI state
platform-agnostically. This builds PR state on the **[[unified-pluggable-event-contract]]** (PR
events are the first registered `event.*` kinds) and routes outbound auth through the **`da service`
auth proxy/injector** ([[agorcha-public-vs-internal-and-obs-deploy]] §5.4–§5.5). PR state is the
**reference first implementation** — it exercises the whole contract (envelope, registry, dispatch,
pluggable producer, auth boundary) and de-risks every later surface (daemon review/comment events,
hook sentinels, observability ingest).

## 1. Key correction: no bespoke `internal/prsource` package

Pluggability lives at two separable layers; only one is config-driven:

- **Producer engine** (generic, code, shared) — envelope + kind registry + table-driven dispatch +
  the `exec`/`http` fetch+map engine + the JSONPath subset. Lives in **`internal/events`**. There is
  **no `internal/prsource/github.go` / `gitlab.go`** — a per-platform producer *file* is exactly
  what would defeat pluggability.
- **Reactions** (code, per kind, ship+evolve with dot-agents) — e.g. what `event.pr.merged` *does*
  (advance a task). Having reaction code for a built-in kind does NOT defeat producer-pluggability;
  a per-platform producer file would.

So "PR support" = three small things, none of them a new package:
1. a default **`gh` config block** (`exec` + field map) interpreted by the one generic engine,
2. the **`event.pr.*` kind registrations** (declarations, not switches), and
3. **consumer-side reactions** in the poll-detector / orchestrator.

A new platform (GitLab/Bitbucket) reuses the existing kinds via a config block (`exec`/`http` + field
map) — **zero reaction code, zero new Go**.

## 2. PR state = events on the common envelope

```jsonc
{ "type": "event.pr.ci_failed", "source": "producer:gh",
  "occurred_at": "...", "idempotency_key": "github.com/org/repo#214@<sha>:ci_failed:<check>",
  "payload": { /* canonical PR view, 2.2 */ } }
```

### 2.1 Registered PR kinds (first entries in the registry)

Map 1:1 onto the poll-detector transitions (§3.2) AND [[monitor-pr-review-comment-routing]] §4 —
unifying both prior consumers:

| Kind | Fires when | Reaction owner |
|---|---|---|
| `event.pr.opened` | new PR | coach/track |
| `event.pr.ci_green` | rollup GREEN + mergeable | orchestrator "ready" |
| `event.pr.ci_failed` | a check fails (carries check + reason) | orchestrator → re-engage worker |
| `event.pr.merged` | merged (merge SHA) | poll-detector `awaiting_owner_review→completed`; `task:` auto-resume |
| `event.pr.closed` | closed unmerged | cascade-block (§2.6) |
| `event.pr.force_rebased` | SHA1 set diff (§2.7) | enqueue rebase |
| `event.pr.review_requested_change` | owner request-changes | `awaiting_owner_review→in_progress` |
| `event.pr.comment_posted` | inline/issue comment | route to worker (= §4 `review_comment.posted`) |

### 2.2 Canonical PR payload (the fields we require)

```go
type PR struct { Number int; Title, Branch, BaseRef, State, Mergeable, HeadSHA, URL string; Rollup Rollup }
type Rollup struct { State string; Checks []Check }   // State: GREEN|FAILING|PENDING (one shared derive rule)
type Check  struct { Name, Status, Conclusion, Link string }
type Comment struct { Author, Body, Path string; Line int; CreatedAt time.Time }
```

## 3. The pluggable producer = config on the shared engine

### 3.1 Declarative producer (the only platform-specific part)

`.agentsrc.json` `pr_source` (resolves through config-v2 scope layers — org can set a default):

```jsonc
{ "pr_source": {
    "producer": "gh",                         // built-in default | "exec" | "http" | <registered>
    "list":     { "argv": ["gh","pr","list","--state","open","--json","number,title,headRefName,baseRefName,state,mergeable,statusCheckRollup,url"],
                  "each": ".", "map": { "number": ".number", "branch": ".headRefName", "mergeable": ".mergeable", "state": ".state", "checks": ".statusCheckRollup" } },
    "comments": { "argv": ["gh","pr","view","{number}","--json","comments"], "each": ".comments",
                  "map": { "author": ".author.login", "body": ".body", "path": ".path", "line": ".line" } },
    "poll_interval_s": 270 } }
```

`exec` runs `argv` (`{number}` substituted); `http` swaps `argv` for `{url, method}`. GitLab MR =
`map.number=".iid"`, `branch=".source_branch"`, `mergeable=".merge_status"`. The engine derives
`Rollup.State`, diffs vs last cycle, emits envelopes.

### 3.2 JSONPath: a ~100-line built-in subset, no external dep

The maps are trivial (`.number`, `.headRefName`, `.statusCheckRollup`). A built-in subset — `.a.b`,
array index, maybe one equality filter — covers it, tuned and dependency-free. **No jq, and not
mikefarah/yq's expression engine either**: pulling a heavy general evaluator into a *foundational
contract* for this need couples the contract to a big dep. (If we later consolidate all our
YAML/JSON path-processing onto one library, that's a deliberate, separate decision — this contract
shouldn't smuggle it in.)

### 3.3 Code producer — only for shapes the engine can't express

Pagination/GraphQL/multi-call → register a code producer (`events.RegisterProducer(name, factory)`).
`gh` is the built-in default and is itself just the §3.1 config baked in. This is rare; the
declarative path is the norm.

## 4. Kind registry + validation disposition (control-plane vs observational)

Kinds are **registered** (mirroring `verifier_profiles`: object-keyed, schema-additive, no enum),
dispatched table-driven. The registry carries a **disposition per kind / per namespace**:

- **control-plane kinds → reject-on-unknown, emit-time fail-fast (loud).** `event.pr.*`,
  `sentinel.*`, `lens.*`, the §3.4.5 auto-resume predicate kinds — these *drive behavior* (a status
  transition, a lens dispatch). An unregistered/misnamed control kind must NOT silently route to a
  generic handler: that produces a missed `merged`, a skipped lens — wrong-or-no behavior, invisible.
  Same reason an unknown `verifier_profile` should error, not no-op.
- **observational kinds → fail-soft to a generic handler.** `event.metric.*`, display/render-only
  kinds — a dashboard rendering an unknown event as raw JSON is harmless.
- **Default: reject.** Opt into fail-soft per namespace.

This corrects [[unified-pluggable-event-contract]] OQ-3's blanket "fail-soft" lean: fail-soft is only
safe for observational kinds.

### 4.1 High-churn built-in kinds (code-side defaults + config overrides)

These ship and evolve with dot-agents (their *reactions* are behavior in the codebase), so they are
code-side built-in registrations, overridable by config:

- `event.pr.*` (merged / ci_failed / force_rebased / review_requested_change) — orchestrator state
  transitions, tied to the layered-pr-fanout state machine.
- `sentinel.*` (#157 hook sentinels) — orchestrator reacts; ship with the hook system.
- daemon review/comment events (`review.submitted`, `review_comment.posted`, `issue_comment.posted`,
  `review_batch.posted`) — renderer/router reacts ([[monitor-pr-review-comment-routing]] §4).
- auto-resume predicate kinds (`task:` / `secret:` / `decision:` / `condition:`, §3.4.5) — code evaluators.
- workflow/iteration lifecycle (`iteration.scored`, `checkpoint`, `verify.record`) — feed observability ingest.

**Pure config kinds are the inverse:** project/org-defined kinds that only route to a generic /
observational handler — nothing in core code branches on them.

## 5. Auth via the proxy injector + encrypted local store

A producer's outbound calls route through the **`da service` auth proxy/injector**
([[agorcha-public-vs-internal-and-obs-deploy]] §5.5): the producer gets an `AuthRoundTripper`
targeting `localhost:<port>/proxy/<host>`; the service attaches the credential for the host and owns
refresh/rotation — the producer is credential-unaware. Credentials follow the external-agent-sources
shape (`{"kind":"credential-ref","id":"..."}`); the producer never sees raw secrets.

### 5.1 Encrypted local credential store

`~/.config/da/credentials.json` is **encrypted at rest**, not plaintext 0600:

- On first use, `da` generates a data key, stores it in the **OS keychain via a credential helper**
  (macOS Keychain / Windows Credential Manager / Secret Service on Linux), and encrypts the
  credential file with it. Reads/writes go through the helper to unwrap the key.
- This upgrades §5.4's "secrets never in repo files" to "secrets encrypted at rest locally," so a
  stolen disk image doesn't leak tokens.

### 5.2 CI credential injection — brainstorm (make it easy for us + them)

CI runners usually have **no OS keychain / credential helper**, so the encrypted-file+keychain path
doesn't fit. The credential loader resolves in this order, so each environment uses the easiest path:

1. **Env-var direct (recommended CI default).** `da` checks `DA_CREDENTIAL_<ID>` (or a single
   `DA_CREDENTIALS` JSON blob) *before* the file. CI injects the secret as a masked env var; `da`
   uses it in-memory, **writes nothing to disk, needs no cleanup, no keychain**. Works on every CI
   (GH Actions/GitLab CI/CircleCI secrets). This is the path we'd document first.
2. **Ephemeral plaintext file injection.** CI writes a temp plaintext cred file from a secret at job
   start (`$RUNNER_TEMP`, 0600) and points `DA_CREDENTIALS_FILE` at it; an `always()`/`trap` step
   removes it at job end. Use when a tool insists on a file path. The risk is cleanup discipline —
   env-var avoids it.
3. **OIDC / workload-identity exchange (advanced, no static secret).** On platforms with OIDC (GH
   Actions `id-token`), `da` mints a short-lived token at runtime by exchanging the OIDC JWT with the
   target — no long-lived secret stored anywhere. Best security, platform-specific; defer past v1 but
   design the loader so it's a pluggable resolver alongside env-var/file/keychain.

**Loader resolution order:** `DA_CREDENTIAL_*` env → `DA_CREDENTIALS_FILE` (plaintext, ephemeral) →
`~/.config/da/credentials.json` (encrypted, keychain-unwrapped) → (optional) OIDC resolver. The
`da service` proxy, when running, sits in front of all of these; when absent, the loader runs inline
(§5.5 bootstrap-fallback). So local dev gets the encrypted store, CI gets env-var injection, neither
needs the other's machinery.

## 6. Consumers (table-driven over the registry)

Consumers subscribe to `event.pr.*` kinds, not a PR interface: the poll-detector
(`merged`/`closed`/`force_rebased`/`review_requested_change` → §3.2 transitions + §3.4.5 auto-resume),
the orchestrator/coach (`ci_green`/`ci_failed`/`opened`/`comment_posted`), the `pr-ci` verifier
(`ci_failed`). Adding a consumer or kind is a registration, not a central edit.

## 7. Plan integration (intertwined — see §8)

Rework the layered-pr-fanout plan so the foundation precedes its consumer, and wire cross-plan deps
so the related proposals land together:

- **`lpf-event-foundation`** (dep-less) — `internal/events`: common envelope + kind registry (schema-
  additive, no enum, per-kind/namespace **disposition** reject|soft) + table-driven dispatch + the
  `exec`/`http` producer engine + the ~100-line JSONPath subset. Implements the
  [[unified-pluggable-event-contract]] core.
- **`lpf-pr-producer`** (deps: `lpf-event-foundation`) — the default `gh` `pr_source` config + the
  `event.pr.*` kind registrations (control-plane disposition) + the canonical `PR` payload + the
  `AuthRoundTripper` seam. **No `internal/prsource` package** — config + registrations on the engine.
- **`cred-store-loader`** (dep-less; shared) — the §5.1 encrypted store (keychain helper) + §5.2
  loader resolution order (env → file → encrypted → OIDC-stub). Cross-plan dep target for
  `lpf-pr-producer`, observability ingest, and `sources:`. Lives in external-agent-sources territory.
- **retarget `lpf-e-transitions-poll`** (deps: `lpf-pr-producer`) — poll-detector consumes
  `event.pr.*` + the auto-resume predicate evaluators; no direct `gh`.
- **Follow-ups:** swap the session coach/poller onto the engine; the `pr-ci` verifier prompt
  references the same `pr_source` config; the `da service` proxy (agorcha §5.5) implements the
  `AuthRoundTripper` server side later (fallback works now).

## 8. Intertwining the related proposals (so the work completes properly)

These three proposals are one program; they must be accepted + planned together, not piecemeal:

| Proposal | Graduates to | This work's relationship |
|---|---|---|
| [[unified-pluggable-event-contract]] | r3-background-worker-service spec (or sibling event-contract spec) | `lpf-event-foundation` is its first implementation; the disposition rule (§4) is a refinement to fold back into it |
| this (PR-as-first-event) | layered-pr-fanout spec §§3–6 | `lpf-pr-producer` + retargeted `lpf-e` |
| [[agorcha-public-vs-internal-and-obs-deploy]] §5.4–§5.5 (auth proxy + cred store) | a `da service` / credential-store spec | `cred-store-loader` is the shareable slice; the proxy server is later, fallback now |

Concretely: keep `lpf-event-foundation` as the shared core both the event-contract and PR work
depend on; make `lpf-pr-producer` and the future daemon-event/sentinel work both depend on it
(cross-plan `depends_on`), so `da workflow eligible` interleaves them and the envelope/registry is
implemented exactly once. The cred-store is the third leg, shared by PR auth + observability + sources.

## 9. Open questions

- Exact `internal/events` package boundary vs `r3-background-worker-service` ownership (the contract
  graduates there) — lean: land `internal/events` now, the service consumes it.
- OIDC resolver shape for CI (§5.2 option 3) — design the loader pluggable now, implement later.
- Whether `cred-store-loader` lives in a new plan or folds into an external-agent-sources plan —
  decide when wiring deps (§7).
