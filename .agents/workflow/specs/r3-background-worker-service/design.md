# Spec — r3-background-worker-service

**Status:** draft (designer-B pass, 2026-05-27). Promotes the existing
`.agents/workflow/plans/r3-background-worker-service/design.md` into the
canonical spec form and adds the contract sections (open questions, deferred,
relationship) the in-plan document elided.

## 1. Problem statement and goals

Today, agent-run telemetry is produced by hook-and-CLI invocations: hooks
write `.agents/active/iteration-log/iter-N.yaml`, then a human (or scheduled
loop) runs `da score run` to materialize sidecar scores. Two consequences:

1. The score sidecars are stale until somebody runs the CLI. Anything that
   wants to watch agent runs in real time (R2 dashboard, R5 review queue,
   R4 eval harness) has to poll the filesystem or re-run scoring on every
   read.
2. Rubric/version bumps don't auto-propagate; existing sidecars stay on the
   old rubric until an operator remembers to rescore.

**Goal:** stand up a long-running service that holds the moving parts which
must outlive a single CLI invocation — a watcher on the iter-log directory,
a rescore loop driven by the rubric-version constant, and the HTTP surface
that R2 and R5 will mount onto. The service is the single in-process host
for these jobs and the bus that lets them notify subscribers.

**Non-goals (v1).** Distributed job execution, durable job queues, multi-
machine fan-out, RBAC, frontend code, daemonization tooling. Those are
either downstream plans (R5 owns RBAC) or operator concerns (systemd /
launchd own daemonization).

## 2. Explicit decisions with rationale

### D1 — Hosting model: `da service` cobra subcommand

R3 ships as `da service run` (plus `da service status`, `da service stop`),
a long-running subcommand of the existing `da` binary. The service runs in
the foreground by default; it can be daemonized via `da service run -d`
(alias `--detach`), which self-backgrounds the process, writes a pidfile,
and detaches. This self-backgrounding mode enables the "daemon" form
without requiring separate tooling. Alternatively, operators can supervise
the foreground mode via systemd/launchd (both forms coexist). No second binary.

Rationale: every existing capability is a cobra subcommand, the service
reads in-repo state (`.agents/active/iteration-log/`, the KG sqlite under
`internal/graphstore/`), and co-locating it with the CLI avoids a cross-
binary contract. The `-d`/`--detach` mode handles self-daemonization; operator
supervision (systemd/launchd) handles the always-on deployment model.

Rejected: standalone `da-service` binary (cosmetic, second install target);
sidecar to CLI invocations (wrong lifetime for an ingester); embedding
into `da workflow watch` (conflates loop concerns with observability);
a separate `da daemon` command (no — daemon is a mode, not a command).

### D2 — Task framework: minimal in-process scheduler

Build a ~300-500 LoC scheduler in `internal/service/scheduler/` (see
`scheduler-core` task in TASKS.yaml for shape). Tasks are Go funcs
registered with a name, a trigger (interval or fsnotify), and a timeout.
Scheduler owns goroutine lifecycle, panic recovery, last-run/next-run/
last-error bookkeeping, and `Stop(timeout)` draining.

Rationale: the initial task set (2-4 tasks) all run in-process; the
distributed-queue use cases that `asynq`/`river` optimize for (Redis/
Postgres-backed durable queues, multi-machine fleets) are absent. Forcing
Redis or Postgres as a runtime dep just for a scheduler is unjustified
ops burden when the repo currently runs on sqlite only.

Rejected: `asynq` (requires Redis); `river` (requires Postgres at
runtime); `robfig/cron` alone (only handles cron, not fsnotify).

Re-evaluation trigger: if R4's eval harness grows multi-machine workers
or durable retries, revisit `river`. The framework boundary is internal
to `internal/service/scheduler/`, so swap cost is contained.

### D3 — Restart safety via per-task watermark sidecars (not a durable queue)

Each task owns one `.agents/active/service-state/<task-name>.watermark.yaml`
file, written atomically (temp+rename, mirroring `internal/scoring/persist.go
writeYAMLAtomic`). On startup each task reads its watermark; absent ⇒ start
from scratch.

Rationale: the v1 tasks are *idempotent watchers* (re-scoring an iter is a
no-op if the watermark says it's already done), not transactional units of
work. A durable queue would be a heavier abstraction than the actual
restart semantics require. The watermark is also human-readable, which is
worth the cost over a binary checkpoint.

### D4 — Event bus behind a pluggable interface; in-process is the v1 builtin default

**The R3 scheduler and event surfaces depend on an `EventBus` *interface*,
not on the concrete channel implementation.** v1 ships exactly one
implementation — in-process channels, zero external deps — but it is wired
in behind the interface from day one so an org's existing messaging system
(Kafka, NATS, Redis) can be swapped in later as a config-selected adapter
**without a rewrite of any subscriber or publisher**. This mirrors the
`graph_backend` adapter pattern (`[[graph-backend-adapter-contract]]`):
a stable contract, a builtin default, and config-selected external
backends behind the same contract. §D4 is the **canonical home for the
transport seam** (the `EventBus` interface + the delivery/ordering/
backpressure guarantees it bounds); see §D4.0 for why the seam, not the
payload, lives here.

#### D4.0 — Where the transport seam is canonical (single-source)

There are **two distinct contracts** and they live in two places — do not
conflate them:

| Contract | Owns | Canonical home |
|---|---|---|
| **`EventBus` transport seam** — *how* a payload travels: `Publish`/`Subscribe`, topic semantics, delivery + ordering + backpressure/drop guarantees, and the config-selected builtin-vs-external backend | the *transport* | **this spec, §D4** |
| **Unified pluggable event-contract** — *what* travels: the common `{ type, source, occurred_at, idempotency_key, payload }` envelope + the typed-kind registry + table-driven dispatch | the *payload schema + registry* | `[[unified-pluggable-event-contract]]` |

The proposal's own scope note (§5) already disclaims the transport ("does
not define the wire" / "graduates into `r3-background-worker-service`") and
its concurrency note already cross-refs §D4.5 for the bus impl — so the
`EventBus` interface is single-sourced **here**, and the proposal
**references** §D4 for the bus. The envelope payloads ride *over* this
transport: the registry decides what a `payload any` is; the `EventBus`
decides how it's published and delivered. Neither doc re-states the other.

#### D4.1 — The `EventBus` interface (canonical contract)

Subscribers and publishers (the scheduler, R2's SSE fan-out, R5's queue)
bind to this interface, never to `*events.channelBus`:

```go
// internal/service/events: the transport seam.
type EventBus interface {
    // Publish delivers payload to all subscribers of topic. Non-blocking
    // for the publisher (§D4.2 guarantee G3); MAY drop for a slow
    // subscriber per the bounded-buffer / drop-oldest policy.
    Publish(topic string, payload any) error
    // Subscribe returns a receive-only stream for topic plus an
    // unsubscribe func. Buffer is bounded; drop-oldest on overflow.
    Subscribe(topic string) (<-chan Event, func(), error)
    // Close drains and releases backend resources.
    Close() error
}
```

`Event` carries the unified envelope as its `payload` (the
`[[unified-pluggable-event-contract]]` registry decides the typed shape);
the bus treats it as opaque `any` and never inspects `type`.

#### D4.2 — Contract guarantees the interface bounds (so app code is backend-agnostic)

The interface promises **only** these, and app code MUST NOT depend on any
stronger guarantee a particular backend happens to provide:

- **G1 — At-most-once, best-effort delivery.** Events are "wake up and look
  at the new state on disk," not authoritative deliveries. A missed event
  is recoverable by re-reading the canonical sidecar (§D3). Backends that
  *can* offer at-least-once or exactly-once (Kafka, JetStream, Redis
  Streams) do not change this floor — the contract ceiling stays
  at-most-once so a subscriber written against the builtin keeps working
  against any backend.
- **G2 — Per-topic ordering is best-effort, not guaranteed.** The in-process
  builtin delivers in publish order per topic; a partitioned/sharded
  external backend (Kafka partitions, NATS queue groups) MAY reorder across
  partitions. App code that needs strict order re-derives it from the
  sidecar state, not from arrival order.
- **G3 — Backpressure = bounded buffer, drop-oldest; publishers never
  block.** (R8: "slow consumers do not block publishers"; r2 OQ5 "bounded
  buffer, disconnect-on-overflow".) External backends map this to their own
  flow control (consumer-group lag, max-in-flight); the *app-visible*
  contract is unchanged: a slow subscriber loses old events, never stalls
  a publisher.
- **G4 — No cross-publish atomicity / no transactions.** A `Publish` is a
  single fire-and-forget; there is no multi-event transaction even on a
  backend that supports one.

A backend adapter that cannot meet *or exceed* G1–G4 (e.g. cannot offer
non-blocking publish) is non-conformant. Backends are free to be
*stronger*; the contract is the floor app code may assume.

#### D4.3 — Builtin default: in-process channels (`dotagents-builtin:eventbus/inproc@^1.0`)

The v1 builtin implementation is `internal/service/events`'s channel-based
bus: bounded buffer per subscriber, drop-oldest on slow consumer, no
durable storage (consumers re-read canonical state from sidecars if they
missed an event), no cross-process IPC. It is the default backend when no
`event_bus` adapter is configured, and it is the only backend that ships in
v1. Rationale: events are notifications, not authoritative deliveries; R2
subscribes via SSE; R5 mounts an HTTP handler that doesn't even need bus
access for v1. Keeping the builtin tiny preserves the cross-plan contract
surface — and putting it behind the interface means "tiny v1" and
"swappable later" are not in tension.

#### D4.4 — Pluggable external backends (config-selected adapters; post-v1, demand-gated)

External backends are config-selected adapters behind the §D4.1 interface,
selected exactly like `graph_backend`'s adapter-ref — a
`source-id:domain/name@version` reference resolved through the existing
`sources` + lockfile machinery, with builtins under the synthetic
`dotagents-builtin` source. The selection field is `event_bus` (absent ⇒
the §D4.3 builtin):

```yaml
# .agentsrc.json (illustrative)
event_bus: dotagents-builtin:eventbus/inproc@^1.0      # default; omit ⇒ this
# event_bus: acme-config:eventbus/nats@^1.0            # org-provided adapter
```

The adapter ref pins in the **`adapters` section of the shared
`.agentsrc.lock`** (the same section and shared-writer discipline the
graph-adapter contract uses, `[[graph-backend-adapter-contract]]` §10.1) —
not a separate lockfile. Candidate external backends and the
delivery/ordering/durability semantics each *adds* (all bounded down to
G1–G4 at the app boundary per §D4.2):

| Backend | Modes | Adds over the builtin | Bounded by the contract to |
|---|---|---|---|
| **Kafka** | partitioned log | durable replay, at-least-once, partition-keyed order | at-most-once, best-effort per-topic order (G1/G2) — durability is invisible to app code unless it opts into replay out-of-band |
| **NATS** | core pub/sub + JetStream streams | core = fire-and-forget (near-builtin); JetStream = durable, replayable, at-least-once | same floor; JetStream's durability is a backend capability, not an app guarantee |
| **Redis** | pub/sub + Streams | pub/sub = fire-and-forget; Streams = durable, consumer-group lag-based backpressure | same floor; Streams' consumer-group ack model maps onto G3's drop-oldest at the app edge |

Because app code binds to the interface and may assume only G1–G4, swapping
the builtin for any of these is a **config + adapter install, never a
subscriber/publisher rewrite** — the whole point of seaming the bus on day
one. An adapter author MUST document, per backend, how that backend's
native semantics are clamped to G1–G4 (e.g. how Kafka's at-least-once is
presented as at-most-once at the subscriber, how JetStream durability is
*not* surfaced as an app guarantee).

#### D4.5 — Concurrency: per-topic locking (deferred optimization, decision recorded)

The v1 bus (shipped via the event-bus PR) guards the whole
`subscribers map[string][]*subscriber` + the fan-out with a **single
`sync.Mutex`**. Correct and low-contention at the current scale (2-4
internal topics, fast non-blocking drop-oldest delivery, lock held only
for the synchronous fan-out). NOTE: as topic count grows — and it will,
once the `[[unified-pluggable-event-contract]]` lands many event/sentinel
types — a single global mutex serializes publish/subscribe **across
unrelated topics** (publishing to topic A briefly blocks topic B).

**Decision (deferred to implement, recorded now): move to per-topic
locking when topic count or cross-topic publish contention warrants.**
The shape is NOT "replace the mutex" — it is two-level:
- An `RWMutex` (or a sharded lock) on the **registry** (the map itself),
  taken only for add-topic / remove-topic / subscribe / unsubscribe.
- A **per-topic mutex** guarding that topic's subscriber slice + its
  fan-out, so `Publish` to different topics runs concurrently.

Trigger to implement: the unified-pluggable-event-contract surface
landing (many topics) OR measured cross-topic publish contention. Until
then the single mutex stays — do not pre-optimize a tiny low-contention
bus. Per-topic delivery goroutines are a further option if ordering
guarantees per topic are needed independently. (Per-subscriber buffer
strategy — currently bounded drop-oldest — is a separate, already-decided
axis; revisit only if a different back-pressure policy is wanted.)

### D5 — HTTP surface is reservation, not implementation

`internal/service/http` exposes `RegisterMount(prefix, handler)`. R3 only
ships `/healthz` and `/api/tasks` (the scheduler.State() projection). R2's
`/api/*` and R5's `/api/reviews/*` are mounted by their own plans through
the same registration call. R3 ships stub `RegisterR2Mount(srv, nil)`-style
no-op test wiring to prove the contract.

### D6 — Service lifecycle: optional now, value-gated scaling

**`da service` is OPTIONAL — never required. The CLI core works fully
standalone with no daemon dependency.**

Phased adoption strategy:

- **v1 (now): optional, never required.** The CLI core (`init`, `add`,
  `refresh`, `rules`, `skills`, `workflow`-reads, `config explain`) works
  fully standalone with NO daemon dependency. Current + near-term features
  don't need a running service, so don't force one.
- **`da service run` foreground + `da service run -d` / `da service install`
  (systemd/launchd)** = opt-in always-on. Deliberate opt-in, like
  `systemctl enable` — NOT auto-started at `da install`.
- **Graceful degradation / CLI-routes-when-present:** commands that benefit
  (warm-state `eligible`/`orient`, anything needing the auth-proxy or
  event-stream) detect a running service and route through it; if absent,
  fall back to direct cold-file operation. The service is an **accelerator/
  enabler when present, never a requirement.**
- **Value scales with responsibilities:** as the observability dashboard
  (R2), auth-proxy injector (agorcha §5.5), scheduler tasks (dream-cycle/
  rescore/iterlog), and orchestrator watch-loop land on `da service`,
  always-on becomes the compelling single ops primitive (one health/metrics/
  event/auth surface). It MAY graduate to always-on-recommended (still not
  forced) once enough hangs off it — that decision is triggered by impl
  pace of sibling plans, not a fixed date.
- **Bonus rationale:** an always-on service owning warm runtime state also
  mitigates stale-local-checkout bugs (`eligible` queries the live source-
  of-truth instead of cold-reading possibly-stale files).

Framing: `da`'s core is git-like (pure CLI, no daemon), its background/team
features are docker-like (want a daemon) — so the service is optional-but-
able-to-be-always-on, value-gated on obs/service features landing.

## 2A. Transport & protocol (single source for the R-series)

**This section is the single source of truth for how the service moves bytes
on each surface.** R2 (`r2-observability-dashboard`) and the `streaming` /
`http-service` app-type profiles reference it rather than re-deciding
transport per surface. The governing principle is **not HTTP-by-default**:
HTTP-over-TCP has genuine benefits *and* genuine drawbacks, and we pay them
deliberately only where the benefit applies.

### Why "not HTTP everywhere"

HTTP-over-TCP buys us browser reach, proxy traversal, a ubiquitous client
ecosystem, and a familiar request/response shape — real benefits on the
edges that face browsers, other machines, or external tools. But on a
same-host control path it is mostly cost: TCP connection + TLS handshake
setup latency, header/framing overhead on every request, head-of-line
blocking under HTTP/1.1 (and even HOL within a TCP stream under HTTP/2),
ephemeral-port and bind-address management, and a request/response shape
that fits poll-style reads better than dispatch or streaming. R3 already
leans **loopback-only** (OQ3 default `127.0.0.1`, OQ4's `da service stop`
locked to loopback) — so the cross-machine justification for HTTP is largely
*absent* on the local control plane. Defaulting that path to HTTP would pay
TCP/TLS/HTTP costs for a benefit (remote reach) we have explicitly scoped out.

### Surface → transport map

| Surface | Transport | Rationale |
|---|---|---|
| **Browser / dashboard** (R2 SPA, `/api/*`, the R2 push channel) | **HTTP + SSE** | Browsers speak HTTP natively; SSE is R2's server-push primitive (r2 §D2.2 "server-pushed, not polled"; survives reverse proxies without a special handshake). Genuinely fits — keep here. |
| **Cross-machine / external-tool-consumed** (anything bound beyond loopback via the explicit `--addr` of OQ3; future external API clients) | **HTTP** (+ TLS / RBAC when R5 lands) | The one place HTTP-over-TCP's benefits (proxy traversal, ubiquitous clients) actually apply. Network exposure is gated behind OQ3's explicit opt-in and R5's RBAC. |
| **Local control plane** (`da service status`/`stop`, `da` CLI→service routing per §D6 "CLI-routes-when-present", OQ4's loopback-only stop) | **Unix-domain socket** (named pipe on Windows); optionally gRPC-over-UDS or a length-prefixed framed protocol | Same-host only. UDS removes TCP/TLS/HTTP overhead and port management, and gives **peer-credential passing** (`SO_PEERCRED` / `LOCAL_PEERCRED`) — which makes OQ4's "loopback-only stop" a *filesystem-permission + peer-uid* check instead of a network-ACL guess. |
| **Worker dispatch** (scheduler → task RunFns, §D2) | **In-process Go call** (no transport); UDS only if a task is ever hosted out-of-process | The v1 tasks are co-located goroutines (§D2, §D3). There is no wire here at all in v1; introducing one would be pure overhead. UDS is the escape hatch if/when D2's `river` re-evaluation moves a worker out-of-process. |
| **Streaming / event bus** (§D4 `Publish(topic, payload)` → subscribers; R2's push fan-out) | **In-process channels internally** (§D4 is already in-process pub/sub); **SSE at the browser edge**; evaluate WebSocket / a binary framed stream only if a non-browser consumer needs bidirectional or binary framing | The bus itself never crosses a process boundary in v1 (§D4). The only externalized stream is R2's browser push, which SSE already covers. SSE vs WebSocket vs binary-framed is decided against the **backpressure / slow-consumer-drop** policy already specified (r2 OQ5 "bounded buffer, disconnect-on-overflow"; r3 R8 "slow consumers do not block publishers") — SSE's unidirectional server→client shape matches that drop-on-overflow model; WebSocket's bidirectionality buys nothing for a read-only v1 (r2 §D2.2). |

### Local-host transport vs the pluggable event-bus backend (boundary call-out)

Two distinct seams are easy to conflate; keep them apart:

- **Local FS-projection / same-host control** (the "Local control plane" and
  "Worker dispatch" rows above) stays **in-process + UDS / named pipe**. This
  is the §2A transport layer (`http-server` task under OQ5 option B). It is
  *not* pluggable per-org — it is dot-agents' own same-host plumbing and has
  no reason to become Kafka.
- **External org integration of the event stream** is the **`EventBus`
  backend seam** (§D4.1 / §D4.4). When an org wants events to flow through
  *their* Kafka/NATS/Redis, that is a config-selected `event_bus` adapter
  behind the §D4.1 interface — **not** a change to the §2A local transport.

**The bus seam (§D4) is precisely what enables org-system plug-in.** The §2A
"In-process channels internally / SSE at the browser edge" row describes the
*builtin* backend's wire (§D4.3); swapping that backend for an org's system
swaps the bus adapter, leaving the local control plane (UDS) and the browser
edge (SSE) untouched. A non-builtin backend may move events across a process
or machine boundary, but the §D4.2 G1–G4 contract keeps every subscriber
backend-agnostic — so the boundary an org plugs into is the `EventBus`
interface, never the §2A local transport.

### The selection rule (so future surfaces choose deliberately)

> **Use HTTP for a surface when it is: browser-facing, OR cross-machine, OR
> consumed by an external tool that speaks HTTP. Otherwise use the local
> high-efficiency transport (Unix-domain socket on POSIX, named pipe on
> Windows; or an in-process call when the peer is co-located).**

Any new surface added to the service states which arm of this rule it falls
under, so the transport is a recorded decision, not an accident of "the
HTTP server was already there."

### Cross-platform note

The local high-efficiency transport is a **Unix-domain socket on POSIX** and a
**named pipe** (`\\.\pipe\...`) on Windows — Go's `net.Listen("unix", ...)`
vs `winio`/named-pipe equivalent behind one small dialer abstraction. Because
the platform split is exactly where the prior **agentslock / TCC** failures
bit (a same-host IPC path with OS-specific permission semantics), the named-
pipe path **must be exercised on the Windows box**, not assumed to mirror the
UDS path. Peer-credential checks differ too (`SO_PEERCRED` on Linux,
`LOCAL_PEERCRED` on macOS, the named-pipe client token on Windows) — the
loopback-only-stop guarantee (OQ4) is only as portable as that check.

### Tie to the app-type profiles

The `http-service` and `streaming` app-type profiles
(`app-type-profiles` §8A.2 / §8A.3) attach verifier chains per surface, and
those choices must reflect **which transport the surface actually uses**:

- A surface that resolves to **HTTP/SSE** (browser/dashboard, external bind)
  takes the `http-service` chain — `api-contract` (wire schema doesn't drift,
  r2 DC5) + `integration` (route served by a started host).
- A surface that resolves to the **local high-efficiency transport / event
  bus** takes the `streaming` chain — `race` (`go test -race` over the
  goroutine bus, §D4.5) + `stream-replay`/idempotence + `backpressure`
  (slow-consumer drop, r2 OQ5 / r3 R8). A UDS control endpoint is verified by
  `integration` against a *socket* listener, not a TCP one; an
  in-process-only path needs neither `api-contract` nor a network
  `integration` verifier — only `unit` + `race`.

In other words: the verifier chain is selected by the surface's transport (via
the selection rule above), not by the binary's name.

## 3. Requirements (behavioral, not implementation)

1. The service runs continuously in the foreground and exits cleanly on
   SIGINT/SIGTERM within a bounded shutdown timeout.
2. ≥2 background tasks run on independent triggers: an fsnotify-driven
   iter-log ingester and an interval-driven rescore-on-rubric-bump task.
3. After a restart, no task re-processes work it has already acknowledged
   via its watermark.
4. Task health (last-run-at, next-run-at, last-error, consecutive-failures)
   is observable via `da service status` and a JSON HTTP endpoint.
5. Other plans (R2, R5) can mount HTTP handlers under arbitrary prefixes
   without modifying R3 code.
6. Other plans can subscribe to an in-process event bus without modifying
   R3 code; slow consumers do not block publishers.
7. A panic in any task's RunFn is recovered, recorded as last-error, and
   does not bring down the scheduler.

## 4. Open questions

- **OQ1 — KG-staleness-refresh task in v1?** Plan summary mentions it; in-
  plan design.md defers it pending `graphstore-concurrency-contract`
  settle. That plan is now archived (per the cg6b note in
  coverage-gate-per-file TASKS.yaml). **Recommendation:** still defer to a
  v1.1 slice; v1 should ship the two tasks already specified to keep the
  surface small. Decision belongs to the orchestrator at materialization.
- **OQ2 — Concurrency limit per task?** Scheduler today implies one
  goroutine per task. If a task's RunFn takes longer than its trigger
  interval (rescore on a big run), does the next tick block, queue, or
  drop? Default proposal: drop with a counter; document. Confirm before
  `scheduler-core` lands.
- **OQ3 — Configurable bind address vs loopback-only by default?** Plan
  notes default `:7878` for the cobra surface. With R5 introducing RBAC
  later, defaulting to loopback (`127.0.0.1:7878`) and requiring an
  explicit `--addr 0.0.0.0:...` to bind externally is the safer default.
  Confirm before `cobra-surface` lands.
- **OQ4 — `da service stop` mechanism.** TASKS.yaml notes "POST
  /admin/stop ... gated on loopback only" — this is the only state-
  changing endpoint on the v1 server. Lock down to loopback even if D5
  later allows external bind. Tracked here because it cuts across `http-
  server` and `cobra-surface`. **Note:** §2A makes this cleaner — if the
  control plane is on a UDS, `stop` is gated by socket file permission +
  peer-uid (`SO_PEERCRED`) rather than a network-ACL check.
- **OQ5 — Transport for the local control plane: HTTP-everywhere vs
  UDS-first-with-HTTP-edge?** This is an **architecture decision and an
  owner-decision** (see §2A for the full map and selection rule). The two
  candidate shapes:
  - *(A) HTTP-everywhere:* one TCP/HTTP listener serves browser, external,
    *and* local control. Simplest single surface; pays TCP/TLS/HTTP cost on
    the local path for a remote benefit R3 has scoped out (loopback-only
    default, OQ3/OQ4).
  - *(B) UDS-first + HTTP edge (recommended):* the local control plane and
    `da`-routes-when-present (§D6) ride a Unix-domain socket (named pipe on
    Windows); HTTP/SSE is mounted **only** on the browser/dashboard + the
    explicit external-bind edge. Removes TCP/TLS/HTTP overhead, removes port
    management, and turns OQ4's loopback-stop into a peer-credential check.
  - **Recommendation: (B).** It follows directly from R3's existing
    loopback-only posture and from the selection rule in §2A; HTTP stays
    where it genuinely fits (R2's browser surface, external bind) and the
    high-throughput same-host path drops the HTTP tax. The cost is one small
    cross-platform dialer abstraction (UDS / named-pipe) that must be tested
    on the Windows box (agentslock / TCC lesson). **Owner must rule** before
    `http-server` lands, because the ruling determines what that task *is*
    (see the frontier-task note below).
- **OQ6 — Event-bus backend seam: ship a reference external adapter now, or
  interface-only?** §D4 wires the `EventBus` behind a pluggable interface
  from day one (forward-evolvability: an org's existing Kafka/NATS/Redis may
  be requested "relatively soon"), but the *external adapters themselves* are
  post-v1 work. The two candidate shapes:
  - *(A) Interface-only in v1 (recommended):* v1 ships **only** the in-process
    builtin (§D4.3) behind the §D4.1 interface. No external adapter, no new
    runtime dep, no Kafka/NATS/Redis client in the tree. The seam is proven by
    a second in-tree fake-backend in tests (so "swappable" is demonstrated,
    not just asserted). External adapters land when an org actually requests
    one, scoped against that org's real semantics.
  - *(B) Spec + ship a reference NATS adapter in v1:* validates the seam
    against a real external backend's quirks (JetStream durability, queue-group
    reordering) before declaring the interface stable, at the cost of a new
    dependency and ops surface v1 has no consumer for.
  - **Recommendation: (A).** It matches D2's "don't force Redis/Postgres for a
    use case that's absent" posture and the project's no-pre-optimization
    stance, while still satisfying the maintainer forward-evolvability
    requirement — the interface and the G1–G4 contract (§D4.1/§D4.2) are the
    deliverable, and they make (B) a config + adapter install later rather than
    a rewrite. **Owner-decision flag:** confirm (A) interface-only before the
    `event-bus` surface is considered closed; if the org Kafka/NATS request is
    already concrete, escalate to (B) and scope a reference adapter against
    that specific backend.

## 5. Done criteria

- `da service run` starts in foreground, registers the two v1 tasks, serves
  `/healthz` + `/api/tasks`, and exits cleanly on SIGINT within 5s.
- `da service run -d` (or `--detach`) self-backgrounds, writes a pidfile,
  and allows the command to return immediately.
- Integration test: write `iter-N.yaml` to a temp iter-log dir under a
  running service; assert the score sidecar appears AND an
  `iteration.scored` event lands on a test subscriber within 2s.
- Restart test: run service, write iter-1+iter-2, kill, restart, write
  iter-3 — assert only iter-3 is re-processed.
- Rescore test: start service with rubric version A, advance the version
  to B, assert one rescore pass runs and a `rescore.done` event publishes.
- Mount test: a fake R2 handler registered via `RegisterMount("/api/test",
  ...)` is reachable; conflicting prefixes are rejected deterministically.
- HTTP smoke: `curl /api/tasks` returns the registered task list with
  last-run/next-run/last-error fields populated.
- `docs/SERVICE.md` documents start/stop, systemd + launchd examples,
  task list, mount contract, watermark layout, and troubleshooting.

These trace back to umbrella spec
`agent-run-scoring-observability-platform`'s D1-D5 cross-cutting decisions.

## 6. Deferred items

- KG-staleness-refresh task (OQ1).
- Distributed/multi-machine execution; if needed, swap scheduler for
  `river` per D2's re-evaluation trigger.
- RBAC on HTTP endpoints — R5's plan owns this.
- Frontend (R2 plan owns it).
- Log rotation / structured logging beyond stderr (operator concern).
- **External `EventBus` backends (Kafka / NATS / Redis adapters)** — the
  seam is in v1 (§D4.1 interface + §D4.4 selection model), the adapters are
  not. Demand-gated post-v1 work per OQ6; v1 ships interface-only with the
  in-process builtin (§D4.3). This subsumes the older "second sibling
  Publisher interface for async/queue-backed events" follow-up note — that
  capability is now expressed as a config-selected `event_bus` adapter behind
  the one `EventBus` interface, not a parallel publisher type.

## 7. Relationship to other specs and plans

- **Umbrella:** `agent-run-scoring-observability-platform` owns D1-D5
  cross-cutting decisions. R3 implements D2 (long-running host) and D4
  (push events). Don't re-litigate those here.
- **Sibling R-plans:**
  - `r1-outcome-scoring` ships the sidecar shape this service ingests
    (`scoring.ScoreIteration`, `scoring.WriteIterationScoreWithRecord`).
    R3 calls into `internal/scoring`; it does NOT duplicate scoring.
  - `r2-observability-dashboard` consumes `/api/*` mount + event-bus
    subscription. R3 ships the mount machinery + bus; R2 ships the
    handler.
  - `r5-review-labeling-access` mounts `/api/reviews/*` and owns RBAC.
  - `r4-code-task-generation-eval` may grow into a workload that
    triggers D2's `river` re-evaluation.
- **Codex staged-dispatch model:** the codex-companion runtime already
  has its own per-stage agent dispatch; R3's scheduler is for in-repo
  background jobs, not LLM dispatch. The two surfaces don't overlap.
- **`[[verifier-owns-ci-watch-shift-left]]`:** existing verifier agents
  poll CI independently and are not run as scheduler tasks. R3's
  scheduler is for repo-local file watchers and idempotent rescores,
  not for tasks that need their own polling loop.
- **`[[validate-bundle-against-head]]`:** the 2026-05-27 note asserted
  `internal/service/` did not exist on HEAD. STALE as of 2026-06-25:
  `internal/service/scheduler/` (commit 0a57f0b6) and
  `internal/service/events/` (PR #172) have since shipped — those two
  tasks are now `completed`. The remaining tasks add
  `internal/service/{http,tasks,state}` + `commands/service/`
  (HEAD-verified absent 2026-06-25); re-validate write_scope before each
  remaining fanout.

## Candidate canonical-plan tasks (appendix)

These already exist in `TASKS.yaml` (HEAD-verified 2026-05-27 — task IDs
match). This appendix is descriptive, not authoritative; `TASKS.yaml` is
the single source for status. (Status column refreshed 2026-06-25.)

| Task ID | Status | Notes |
|---|---|---|
| design-doc | completed | Replaced by this spec; in-plan design.md retained for archive context |
| scheduler-core | completed | Shipped 2026-06-25 (commit 0a57f0b6); `fsnotify` dep added |
| event-bus | completed | Shipped via PR #172; surface is `Publish(topic, payload)` + `Subscribe` per D4 |
| http-server | pending | OQ3 (loopback default) + OQ4 (`/admin/stop` lockdown) + **OQ5 (transport)** must be answered before fanout. OQ5 reframes this task: under the recommended option (B), it is not an "HTTP server" but a **transport layer** — a UDS/named-pipe control listener (local plane + `da`-routes-when-present) with an **HTTP/SSE edge** mounted only for the browser/dashboard + explicit external bind. Under option (A) it stays a single HTTP listener. Owner ruling on OQ5 decides which. |
| tasks-iterlog-ingester | pending | Must call into `internal/scoring`, not duplicate |
| tasks-rescore | pending | Idempotent — no-op when rubric version unchanged |
| service-runtime | pending | Composes scheduler+http+bus; integration test is the umbrella verifier |
| cobra-surface | pending | Mirrors `commands/score.go` shape; OQ3+OQ4 propagate here |
| docs-and-verification | pending | `docs/SERVICE.md` + umbrella E2E integration test |

Cross-plan ordering: nothing in this plan depends on anything outside the
plan. R2 and R5 plans depend on `http-server` + `event-bus`, not the
other way.
