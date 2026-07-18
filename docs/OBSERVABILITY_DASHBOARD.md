---
title: Observability Dashboard
description: Run, configure, and troubleshoot the R2 workflow observability dashboard in standalone or R3-hosted mode.
---

# Observability Dashboard

The R2 observability dashboard is a read-only view of workflow runs, iterations,
outcome scores, rubric metadata, and live scoring updates. It reads the existing
iteration-log and score-sidecar files; it does not introduce another persistent
store. See the [Dashboard API reference](./DASHBOARD_API.md) for the HTTP and SSE
contracts.

## Direction

The commands below describe the dashboard as it ships today: a standalone
`da-dashboard` process for local use and a mount on the R3 runtime. The planned
direction in the `dashboard-subsystem-and-bus-security` spec consolidates local
use into a `da dashboard` subsystem and retires the separate `da-dashboard`
root. It also establishes a seam for a slim, separately deployable production
runtime; that runtime artifact is deferred and does not exist yet.

## Security posture

The current HTTP and SSE surface is read-only and binds to loopback by default.
The dashboard does not provide network authentication. Treat exposure beyond
loopback as fail-closed: do not expose the dashboard listener directly; opt in
only through a fronting reverse proxy or tunnel that owns authentication and
TLS. The planned capability model keeps read-only as the default and permits
future mutations only through explicit, allowlisted write-operation paths—there
is no ambient write capability.

## Standalone mode

The standalone entrypoint is [`cmd/da-dashboard/main.go`](../cmd/da-dashboard/main.go).
It owns the HTTP listener, static frontend, store, event broker, and filesystem
watcher.

For a production-like local run, build the frontend and serve that on-disk build:

```bash
pnpm install
pnpm --filter r2-observability-dashboard build
go run ./cmd/da-dashboard \
  --iter-log-dir .agents/active/iteration-log \
  --static-dir web/dashboard/dist
```

Open <http://127.0.0.1:7300>. The standalone process exposes a bare liveness
probe at `GET /api/health` and the versioned dashboard API under
`/api/v1/observability`.

The binary can also serve the bundle embedded in
[`internal/dashboard/server/dist`](../internal/dashboard/server/dist). Release
builds replace that directory with the compiled frontend; on a source checkout,
`--static-dir web/dashboard/dist` is the explicit way to serve a freshly built
bundle.

### Frontend hot reload

Run Vite and the Go server in separate terminals:

```bash
# Terminal 1
pnpm --filter r2-observability-dashboard dev -- --host 127.0.0.1

# Terminal 2
go run ./cmd/da-dashboard \
  --iter-log-dir .agents/active/iteration-log \
  --dev-asset-proxy http://127.0.0.1:5173
```

Browse through the Go server at <http://127.0.0.1:7300>, not directly through
Vite. The Go server retains `/api` and reverse-proxies every non-`/api` request
to Vite. The Vite config does not define a backend proxy. The frontend's
`web/dashboard/README.md` lists all frontend commands.

## R3-hosted mode

R3 integration uses [`server.Mount`](../internal/dashboard/server/r3mount.go)
rather than the standalone command:

```go
closer, err := dashboardserver.Mount(edge, dashboardserver.MountConfig{
    IterLogDirs:   roots,
    RepoDir:       repoDir,
    TranscriptDirs: transcriptDirs,
    Logger:        logger,
})
if err != nil {
    return err
}
defer closer.Close()
```

The `edge` must implement `RegisterMount` and expose the R3 `EventBus`. The mount
registers full-path REST and SSE routes under `/api`, while R3 continues to own
the listener, host health routes, static assets, and shutdown. There is no
separate dashboard address or dev-asset-proxy flag in this composition.

R3-hosted mode does not start the dashboard filesystem watcher. Its primary push
source is the R3 event bus: `iteration.scored` is translated to the dashboard
event of the same name, and `rescore.done` becomes `rubric.changed`. The returned
closer detaches the bridge, closes the broker, and disconnects SSE clients.

## Configuration

The standalone flags are defined in
[`cmd/da-dashboard/main.go`](../cmd/da-dashboard/main.go); defaults and routing
semantics live in
[`internal/dashboard/server/server.go`](../internal/dashboard/server/server.go).

| Flag | Default | Meaning |
|---|---:|---|
| `--iter-log-dir PATH` | none | Iter-log root to read and watch. Repeat the flag for multiple roots. |
| `--addr HOST:PORT` | `127.0.0.1:7300` | TCP listen address. Keep the loopback default unless a fronting reverse proxy or tunnel owns authentication and TLS; the dashboard itself provides no network authentication. |
| `--dev-asset-proxy URL` | unset | Proxy all non-`/api` requests to a Vite server. Takes precedence over static assets. |
| `--static-dir PATH` | embedded bundle | Serve a frontend build from disk instead of the embedded bundle. Ignored when `--dev-asset-proxy` is set. |

An empty root list is legal and produces empty collections. Relative paths are
resolved by the process working directory. With multiple roots, the first is the
active root used by `GET /iterations/{n}` when `iter_log_dir` is omitted. A
non-empty `iter_log_dir` query value must match one configured root after path
normalization; it cannot select an arbitrary directory. These rules come from
[`internal/dashboard/store/store.go`](../internal/dashboard/store/store.go).

The R3 mount accepts the same root list through `MountConfig.IterLogDirs`, plus
`RepoDir` and optional `TranscriptDirs` for recompute-on-miss. Those latter two
fields are library configuration; the standalone `da-dashboard` command does
not expose flags for them and uses the server defaults.

## Data and push architecture

The read path is defined in [`internal/dashboard/store`](../internal/dashboard/store):

1. The disk store scans each configured root for `iter-N.yaml`,
   `iter-N.score.yaml`, and `session-<id>.score.yaml`.
2. Parsed snapshots are cached in-process. Corrupt or unreadable individual
   files are logged and skipped rather than failing a list request.
3. Iteration detail is decorated by recompute-on-miss. A missing, corrupt, or
   stale iteration score sidecar triggers synchronous scoring; persistence of a
   successful recomputation is best effort in the background.

The push path is deliberately an invalidation channel, not a second data store:

1. Standalone mode uses `fsnotify` plus a one-second mtime poll over configured
   roots. Both feed one per-file deduplication table
   ([`internal/dashboard/watch/watch.go`](../internal/dashboard/watch/watch.go)).
   R3-hosted mode instead bridges selected in-process R3 events
   ([`internal/dashboard/events/r3bridge.go`](../internal/dashboard/events/r3bridge.go)).
2. Before fan-out, the broker evicts the affected root's store cache when the
   payload identifies a root, or the whole cache otherwise.
3. Each SSE client has a bounded 64-event buffer. Publish never blocks. An
   overflow closes that client instead of silently dropping an event.
4. `GET /api/v1/observability/events` writes typed SSE frames and a typed
   heartbeat every 15 seconds. Sequence numbers start at zero per connection.
5. There is no replay or durable cursor. After any disconnect, the client
   reconnects and refetches HTTP resources.

The broker and backpressure behavior are defined in
[`internal/dashboard/events/broker.go`](../internal/dashboard/events/broker.go),
and the wire framing is defined in
[`internal/dashboard/handlers/stream.go`](../internal/dashboard/handlers/stream.go).

## Troubleshooting

### The page loads but shows no runs

Check the rich health endpoint and its configured roots:

```bash
curl -s http://127.0.0.1:7300/api/v1/observability/health
```

`data.roots` should contain the intended directories, and `data.run_count` and
`data.iteration_count` should be nonzero when records are present. The store does
not discover roots automatically. Session views also require a non-empty
`agent.session_id`; legacy flat records without one are not addressable as runs.

### Iteration detail returns `bad_request` for `iter_log_dir`

Use the exact configured root shown by `data.roots` in the rich health response
and URL-encode it as a query value. The store allows only configured roots after
path normalization. Omit the parameter to use the first configured root.

### The UI has no live updates

Inspect the SSE stream directly:

```bash
curl -N http://127.0.0.1:7300/api/v1/observability/events
```

A successful connection starts with a large comment used to flush proxy buffers,
then receives typed heartbeat frames every 15 seconds. In standalone mode, check
server warnings for roots that cannot be watched; the one-second poll remains a
fallback even if `fsnotify` cannot register a root. In R3-hosted mode, verify that
the host edge has a non-nil event bus and that it publishes one of the bridged R3
topics. `task.error` is intentionally not exposed on the dashboard stream.

A disconnect is recoverable and expected under backpressure: reconnect and
refetch, because the server does not replay missed events.

### Vite works on port 5173 but API requests fail

Open port 7300 and start the Go server with `--dev-asset-proxy`. Vite serves only
the frontend; the Go process owns `/api`. If the Go process rejects the proxy
configuration at startup, use a complete URL such as
`http://127.0.0.1:5173`.

### A built frontend is not being served

Run the frontend build, then pass the resulting directory explicitly:

```bash
pnpm --filter r2-observability-dashboard build
go run ./cmd/da-dashboard \
  --iter-log-dir .agents/active/iteration-log \
  --static-dir web/dashboard/dist
```

`--dev-asset-proxy` takes precedence over `--static-dir`, so remove it when
checking the on-disk build.
