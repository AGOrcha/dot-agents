# Dashboard subsystem consolidation + event-bus security posture

**Spec ID**: dashboard-subsystem-and-bus-security
**KG Briefing**: Generated 2026-07-18 — 5 prior decisions, 3 research findings, 1 contradiction
**Status**: draft

## Problem Statement

The R2 observability dashboard ships today as its own root command tree (a standalone
`da-dashboard` binary) alongside an R3-mounted mode. This drifts from two ratified
decisions — `r3-background-worker-service` **D1** ("no second binary"; the runtime is a
`da service` subcommand) and `r2-observability-dashboard` **Goal 5** ("same binary … no
separate deploy"). The separate root costs discoverability, duplicates CLI plumbing, and
forks the dev and prod composition paths.

Separately, the dashboard's push channel is a **high-frequency, high-bandwidth event bus**
fanning internal workflow events out to browser clients. Its security posture is only
partially specified: the existing slow-consumer backpressure (bounded buffer,
drop⇒disconnect, per OQ5) is sound, but concurrent-subscriber bounds, high-frequency
coalescing, the exposure/auth model, the capability model, and payload hygiene are
unspecified. As the bus graduates onto the shared `unified-event-contract` and the R3
runtime, that posture must be made explicit **before** any exposure beyond loopback.

The framing is drawn from container-tooling separation (a thin client + build/packaging
tooling are dev-time concerns; the runtime is a distinct, minimal, separately-shipped
thing) and from a daemonless/rootless, least-privilege security stance (no ambient
privileged surface; explicit, narrowly-scoped capabilities; deny-by-default network
exposure).

## Goals

- Make the dashboard a first-class subsystem of the one `da` command tree for dev/local
  use — not a separate command tree.
- Keep a single shared dashboard core consumed by both the dev CLI and the prod runtime.
- Establish a forward door for a **slim, separately-deployable prod runtime** whose
  network-facing surface excludes the broader agent toolchain — without building that
  artifact until a prod-deploy driver exists.
- Define the event bus's security posture as normative requirements: least-privilege
  capability model, exposure discipline, DoS governance, payload hygiene, and a deferred
  multi-user authorization precondition.
- Retire the drift: the standalone separate-root path is superseded by the
  subsystem + runtime-mount composition.

## Decisions

### D1 — Dashboard is a `da` subsystem; retire the separate root
Invoked as a subcommand of the primary `da` root (dev/local: serve, open, status),
sharing global flags, config resolution, and output conventions. The prior standalone
separate-root binary is retired as a distinct entry point; its behavior is preserved as
the dev subcommand's local-serve mode. Aligns with r3 D1 and r2 Goal 5.
*Rejected:* two roots (discoverability + duplicated plumbing); a thin alias over the old
binary (keeps the two-root problem).

### D2 — One shared core, two composition targets: dev CLI + runtime mount
A single dashboard core (store → recompute → broker → REST/SSE surface) composes two
ways: (a) the dev subcommand runs it in-process against local iter-log roots with the
filesystem watcher as push source; (b) the R3 runtime mounts it onto its HTTP/SSE edge
and bridges the runtime's in-process event bus as the push source. The core is
host- and source-agnostic; neither composition owns hosting.
*Rejected:* forking dev and prod into divergent implementations.

### D3 — Slim separately-deployable prod runtime as a forward door (seam now, artifact later)
The spec commits to an enforced import/build boundary so the network-facing runtime can
later be cut as a slim, separately-installable deployment artifact whose dependency graph
**excludes** the fat agent command surface (kg, worktree, config mutation, etc.). This is
the client↔runtime separation of container toolchains applied here. The actual slim
artifact and its packaging are **deferred** until a concrete prod-deploy driver exists;
the seam must exist now so that cut requires no re-architecture.
*Rejected now:* building the slim artifact/module immediately (no driver yet; premature);
abandoning separate-deployability (forecloses the prod posture and keeps a network
service carrying the whole toolchain).

### D4 — Event bus is egress-only and one-way from the runtime
The push channel is server→client only (per r2 D2.2). The runtime→broker bridge is
strictly one-directional: the dashboard consumes runtime events and cannot publish or
inject upstream. Ingress is read-only request handling; the bus exposes no mutation
endpoints. Inherits `unified-event-contract` D1–D4 (envelope, kind registry,
table-driven dispatch, control-plane fail-closed).

### D5 — Capability model: deny-by-default, explicit strict write-operation paths
The runtime is read-only by default, but **not** a permanently, blanket read-only plane.
Specific control/mutation operations may be added over time — only as explicit,
individually-cased, **allowlisted** write-operation paths, each with a narrow documented
justification and its own gate. There is **no ambient write capability**; state changes
only through a registered write-operation path. Runtime-level enforcement of that
allowlist (making unlisted writes structurally impossible) is a forward requirement to
bake in as the set of sanctioned operations is discovered; until then, the allowlist
discipline is a normative design rule applied to every new path.
*Rejected:* a hard, permanent read-only lock (forecloses legitimate future control ops);
open/ambient write capability (defeats least privilege).

### D6 — Exposure discipline: loopback default, fail-closed beyond
The runtime binds loopback by default and never binds a public interface implicitly. Any
beyond-loopback exposure is an explicit opt-in routed through a fronting reverse proxy or
tunnel that owns authentication, and must satisfy the fail-closed exposure checklist
(decode+normalize before path classification; allowlist not carve-out; HTTPS-only
credentials; pinned token algorithms). The bus service itself implements no network
authn/z in this spec.

### D7 — DoS governance for a high-frequency/high-bandwidth bus
Beyond the existing bounded-buffer drop⇒disconnect slow-consumer policy, the bus bounds
concurrent subscribers and coalesces/debounces bursty high-frequency source events so a
burst cannot saturate egress. Backpressure never blocks the publisher and never buffers
unboundedly.

### D8 — Payload hygiene
Events crossing to browser clients carry no secrets, tokens, or sensitive identifiers;
payloads are shaped and audited at the bridge boundary.

## Requirements

- The dashboard is reachable as a `da` subsystem with dev subcommands for local
  serve/open/status; no separate root binary is a supported entry point.
- A single dashboard core serves both the dev in-process mode (filesystem-watch source)
  and the runtime-mounted mode (runtime event-bus source); switching host/source
  requires no change to the core's behavior.
- A build/import boundary lets the runtime be built without the broader agent command
  surface in its dependency graph; a guard fails the build if the runtime target imports
  outside the permitted set.
- The push channel is server→client only; the runtime→broker bridge cannot inject
  upstream.
- No state mutation occurs except through an explicitly registered, individually-justified
  write-operation path; the default surface is read-only; unlisted write attempts are
  refused.
- The runtime binds loopback by default; beyond-loopback exposure is opt-in and fails
  closed per the exposure checklist.
- The bus bounds concurrent subscribers and coalesces bursty events; a slow or abusive
  client cannot exhaust runtime memory or saturate egress for other clients.
- Browser-bound event payloads contain no secrets, tokens, or PII.

## Open Questions

- What is the concrete boundary mechanism for D3 — build tags, a separate module, or a
  dedicated minimal entrypoint — and which minimizes both attack surface and maintenance
  cost? (candidate for an empirical prototype under the fidelity gate)
- What are the initial concurrent-subscriber bound and the coalescing/debounce policy
  (per-topic vs global; time-window vs count) appropriate to expected event rates?
  (needs a rate measurement, not a guess)
- Which control/mutation operations, if any, become the first sanctioned write-operation
  paths under D5, and what gates each? (discovered incrementally; none in scope now)
- When the prod driver arrives, does the slim runtime deploy as an OCI artifact via the
  existing packaging path or as a plain binary?

## Done Criteria

- The dashboard is invocable via the `da` root subsystem, and the standalone
  separate-root entry point is no longer a supported/tested path (its tests target the
  subsystem/runtime composition).
- A build/import guard exists and fails when the runtime target's dependency graph
  includes the disallowed fat-command surface.
- The bus refuses any mutation attempt that is not a registered write-operation path
  (default-deny), covered by a test.
- The bus enforces a documented concurrent-subscriber cap and event coalescing, verified
  by a burst/slow-client test that neither blocks the publisher nor grows memory
  unboundedly.
- Loopback-default bind is verified; a test asserts no public bind occurs absent the
  explicit opt-in.
- A payload-hygiene check verifies browser-bound events omit sensitive fields.

## Deferred

- Building the slim separately-deployable runtime artifact and its packaging (until a
  prod-deploy driver exists; the seam is in scope, the artifact is not).
- First-class in-service network authentication/authorization (a fronting proxy owns it;
  RBAC is owned by `r5-review-labeling-access`).
- Multi-user stream authorization (per-subscriber event visibility) — recorded as a HARD
  precondition before any multi-user exposure; implementation deferred to r5.
- Concrete write-operation paths under D5 (none now; each added case is its own scoped
  decision).
- Distributed/multi-machine bus fan-out and durable event replay (r2 D2.2 already forbids
  replay in v1).

## Related

- Prior specs: `r3-background-worker-service` (D1 hosting), `r2-observability-dashboard`
  (D2.1/D2.2/Goal 4/Goal 5, OQ5), `unified-event-contract` (D1–D4),
  `agent-run-scoring-observability-platform` (umbrella), `pr-event-source`.
- Lessons: `cf-worker-auth-gate-fail-closed` (D6 exposure checklist),
  `verify-plan-readiness-against-canonical-ref` (reconcile r3 notes vs canonical spec),
  `prototype-experiment-fidelity-gate` (D3 boundary + D7 coalescing prototypes).
- Amends: `r2-observability-dashboard` Goal 5 and `r3-background-worker-service` D1
  ("no separate deploy / no second binary") — narrowed to "one dev entry + one shared
  core + a seam for a slim prod runtime."
