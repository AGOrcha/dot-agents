# Streaming verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **event-driven / duplex transport
verification** (SSE, WebSockets, chunked/long-poll, gRPC server-streaming): prove **behavior over
time**. `--kind test` or `custom`, `--verifier-type streaming`. Reserve `api` for single-shot HTTP;
use this kind when the proof is ordered events, heartbeats, backpressure, reconnect, idle timeouts, or
frame-level integrity.

## What to run

1. **Scoped first.** Exercise only the streams / channels / tagged scenarios that cover
   `write_scope_touched`.
   - Positive: happy-path event order, per-event schema, heartbeat/ping-pong, clean reconnect where
     the product defines it.
   - Negative: server-initiated close (expected code/reason), client idle timeout, malformed
     SSE line / partial WS frame, slow-consumer backpressure, dropped/duplicated frames where the
     contract allows it.
2. **Broader tiers (when in scope):** bounded soak/volume and fault injection after scoped green.

Treat timeouts as first-class evidence (client hang vs server stall vs CI clock skew); use explicit
deadlines, never unbounded waits. Assert the product's backpressure contract (drop/block/coalesce/
error). For best-effort ordering, assert statistical/idempotent properties unless strict ordering is
required.

## Record

`da workflow verify record --kind <test|custom> --verifier-type streaming` — status, command lines,
and `artifact_paths` (WS frame logs, SSE transcripts, traces, timelines). Use `ok-warning` only for
environmental flake (CI timing, broker contention), not ambiguous product semantics. Endpoints, auth,
and CI budgets come from the repo-local override.
