# PR event source — spec (PR state as the first unified pluggable event)

Status: spec (promoted from `.agents/proposals/pr-source-abstraction.md`, 2026-05-31).
Builds on: `unified-event-contract` spec (envelope/registry/dispatch/producer engine) and
`external-agent-sources` spec §4 (auth provider model + credential store). Implemented by
layered-pr-fanout `lpf-pr-producer`; consumed by `lpf-e-transitions-poll`, the orchestrator/coach,
and the `pr-ci` verifier.

## 1. Problem & goal

PR/CI state is read in ≥3 places (poll-detector, `pr-ci` verifier, coach). A bespoke PR interface
would be a *fourth* per-type dispatch surface and would re-roll auth. Goal: PR state is **events on
the unified contract**, produced by a **config-driven producer**, with auth owned by the
external-agent-sources credential model + `da service` proxy — so adding a platform is config, not
code, and auth is one shape.

## 2. Decisions (normative)

### D1 — PR state = `event.pr.*` kinds on the common envelope
First registered kinds: `event.pr.opened`, `ci_green`, `ci_failed`, `merged`, `closed`,
`force_rebased`, `review_requested_change`, `comment_posted`. They map 1:1 onto the layered-pr-fanout
poll-detector transitions (§3.2) AND the `[[monitor-pr-review-comment-routing]]` §4 review/comment
events — unifying both prior consumers. All carry **control-plane disposition** (reject-on-unknown,
per unified-event-contract D4).

### D2 — No `internal/prsource` package
"PR support" = (a) a default `gh` `pr_source` config block (exec + field map) on the generic producer
engine, (b) the `event.pr.*` kind registrations, (c) consumer-side reactions. **No per-platform
producer files.** Reaction code for a built-in kind does not defeat pluggability; a per-platform
producer file would. A new platform (GitLab/Bitbucket) = a `pr_source` config block, **zero Go**.

### D3 — Canonical PR payload
`PR{Number,Title,Branch,BaseRef,State,Mergeable,HeadSHA,URL,Rollup}`,
`Rollup{State: GREEN|FAILING|PENDING, Checks}`, `Check{Name,Status,Conclusion,Link}`,
`Comment{Author,Body,Path,Line,CreatedAt}`. `Rollup.State` derived by one shared rule
(any FAILURE/TIMED_OUT/CANCELLED → FAILING; else any non-COMPLETED → PENDING; else GREEN).

### D4 — `pr_source` config (config-v2 scoped)
A top-level `.agentsrc.json` `pr_source` field (`producer: gh|exec|http|<registered>`, `list`/`comments`
fetch + JSONPath field map, `poll_interval_s`), resolving through the config-v2 scope layers so an org
layer can set the default. `gh` is the zero-config default.

### D5 — Auth via the proxy injector + external-agent-sources credential model
The producer receives an `AuthRoundTripper` targeting `localhost:<port>/proxy/<host>` (the `da service`
injector attaches the credential per host, owns refresh) with a **direct-load fallback** when the
service isn't running. Credentials are `{"kind":"credential-ref","id":"..."}` per external-agent-sources
§4; the producer never sees raw secrets. No `pr_source`-specific auth loader. (Encrypted store + CI
loader order: external-agent-sources spec §4.)

## 3. Requirements

- R1: register the `event.pr.*` kinds (control-plane) on the `internal/events` registry.
- R2: the canonical PR payload + the shared `Rollup.State` derive rule.
- R3: the default `gh` `pr_source` config interpreted by the generic engine; the `pr_source` AgentsRC
  field (six-point sync, enum-free) + schema.
- R4: the `AuthRoundTripper` seam with proxy + direct-load fallback consuming the external-agent-sources
  credential loader.
- R5: consumers subscribe to `event.pr.*` (poll-detector transitions §3.2 + auto-resume §3.4.5;
  orchestrator/coach; `pr-ci` verifier) — no direct `gh` in any consumer.

## 4. Done criteria

- Adding GitLab PR support is a `pr_source` config block (exec/http + field map) with **no Go change**
  — proven by a config-driven producer test mapping a non-GitHub JSON shape to `event.pr.*`.
- The layered-pr-fanout poll-detector reacts to `event.pr.merged`/`closed`/`force_rebased`/
  `review_requested_change` via the registry with no `gh` call in `commands/workflow/`.
- A producer fetch carries auth via the injected `AuthRoundTripper`; with the service absent it falls
  back to the external-agent-sources loader — both covered by tests (hermetic auth seam).

## 5. Deferred / relationships

- `da service` proxy server side (`[[agorcha-public-vs-internal-and-obs-deploy]]` §5.5) — fallback works
  now; proxy later.
- SAST/coverage (`event.sast.*`) — out of scope; the `pr-ci` verifier owns SAST.
- Migrating the session coach/poller onto the producer — follow-up.
- Full rationale + examples: `.agents/proposals/pr-source-abstraction.md`.
