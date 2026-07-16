---
schema_version: 1
task_id: t13-r3-mount-integration
parent_plan_id: r2-observability-dashboard
title: Replace fswatch primary with R3 publish primitive; mount handlers in R3 service
summary: 'R3 publish primitive present (internal/service/events EventBus). Added events.Broker.AttachR3Bus (binds the EventBus INTERFACE, subscribes R3 topics iteration.scored/rescore.done, translates to the dashboard taxonomy; task.error + off-contract dropped) and server.Mount (composes store+recompute -> broker -> handlers, RegisterMounts under the R3 mux, attaches the R3 bus with a disk-backed session resolver, starts NO watcher — fswatch demoted to the standalone binary fallback). Real end-to-end test proves an R3 bus publish surfaces on the dashboard SSE stream.'
files_changed:
    - internal/dashboard/events/r3bridge.go
    - internal/dashboard/events/r3bridge_test.go
    - internal/dashboard/server/r3mount.go
    - internal/dashboard/server/r3mount_test.go
verification_result:
    status: pass
    summary: 'Delegate + parent: go test ./internal/dashboard/events/... ./internal/dashboard/server/... green; delegate also ran -race green + vet clean. Scope = exactly the 4 declared files (+841). Binds the EventBus interface (not the concrete Bus). feedback_goal YES — TestMountBridgesR3PublishToSSE proves R3 publish -> translated SSE frame; fswatch no longer primary.'
integration_notes: 'Branch feat/r2-t13-r3-mount @ 3dc59463 → PR to master. Depends on internal/service/events (R3) API already on master; interface-bound so resilient to R3 internals churn.'
created_at: "2026-07-16T05:20:00Z"
---

## Summary
R3 publish primitive found (`internal/service/events` EventBus). `AttachR3Bus` bridges + translates R3 topics into the dashboard taxonomy; `Mount` wires store→broker→handlers with the R3 bus as the sole/primary source (no watcher). fswatch demoted to the standalone binary fallback.

## Verification
- `go test ./internal/dashboard/events/... ./internal/dashboard/server/...` → ok (parent re-run); delegate `-race` green, `vet` clean.
- Scope: exactly the 4 declared files.
- feedback_goal: YES (end-to-end R3-publish→SSE test).

## Follow-ups
None. (Interface-bound to `EventBus`, so R3-internal changes don't break the bridge.)
