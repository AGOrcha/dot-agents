# Infra cluster design summary — designer-B pass (2026-05-27)

Three design docs landed in this pass. All three are infra/hygiene work
for the orchestration runtime + the code-quality gate.

## Specs

| Spec | Status | Owner of next action |
|---|---|---|
| `r3-background-worker-service` | draft (this pass) | orchestrator — answer OQ1-OQ4 before `scheduler-core` fanout |
| `coverage-gate-per-file` | accepted-direction (restructure this pass) | cg6b loop continues; B3 spawn-ready, B4-B6 candidates queued |
| `production-code-helper-extraction` | accepted (restructure this pass) | t3 (Group RP) is the recommended next batch |

Saved at:
- `.agents/workflow/specs/r3-background-worker-service/design.md`
- `.agents/workflow/specs/coverage-gate-per-file/design.md`
- `.agents/workflow/specs/production-code-helper-extraction/design.md`

## Shared infra touched

- **`internal/platform/`** — both `production-code-helper-extraction`
  (Group RP, Group MCP) and `coverage-gate-per-file` (cg6b B5
  candidate) touch the same cluster of files: `resource_plan.go`,
  `mcp_settings.go`, `plugins.go`, `cursor.go`, `copilot.go`,
  `opencode.go`, `render_manifest.go`. **Coordination required:**
  helper extraction changes the per-file statement denominator, so
  cg6b coverage work on those files MUST happen after extraction
  lands. See `production-code-helper-extraction` OQ2 and
  `coverage-gate-per-file` B5 entry.
- **`internal/graphstore/`** — `production-code-helper-extraction`
  Group GS landed (t2 completed); `coverage-gate-per-file` B4 (now-
  unfenced trio per 2026-05-26 update) is the next coverage batch on
  these files. Same ordering rule — extraction before coverage.
- **`internal/service/`** — new package introduced by
  `r3-background-worker-service`; no overlap with the other two
  specs. Will fall under the coverage gate as soon as it lands; no
  allowlist entries should be needed if r3's verification strategy
  holds.
- **`commands/internal/cmdutil/`** — new package introduced by
  `production-code-helper-extraction` t4. Layering audit at OQ3.
  Coverage gate will apply to the new package automatically.

## Sequencing recommendation

```
r3 (parallel track)              extraction → coverage (serial track)
─────────────────────            ──────────────────────────────────
scheduler-core           ┐       t3 (RP)
event-bus                │       t4 (CMDS) ──┐
http-server              │       t5 (MCP)   │
tasks-iterlog-ingester   │       t7 (archive PR)
tasks-rescore            │              ↓
service-runtime          │       cg6b B4 (graphstore trio)
cobra-surface            │       cg6b B5 (platform cluster)
docs-and-verification    │       cg6b B6 (platform-tag audit)
                         │              ↓
                         │       cg-verify-close (retire pkg gate)
                         │
                         └────────────── independent
```

**Rationale:**

1. r3 is independent of the other two — it adds new packages, does not
   modify existing files in the helper-extraction or coverage-gate
   scope. Can fanout immediately after OQ1-OQ4 are answered.
2. Helper extraction MUST precede coverage work on the same files
   (per the wire-format-watch contract in
   `production-code-helper-extraction` D6 and the denominator-shift
   issue in OQ2). Land t3-t5-t7 first, then cg6b loops on those files.
3. cg-verify-close (retire the package gate, archive coverage-gate
   plan) is the terminal gate for the coverage track and is unblocked
   only after the allowlist holds only genuinely-untestable code.

## Cross-cutting risks identified

- **`[[no-lazy-allowlist-tech-debt]]`** is the dominant risk on both
  the extraction track and the coverage track. Every allowlist entry
  must have a scheduled cg6b batch; every helper extraction must NOT
  add new allowlist entries as a side effect.
- **`[[validate-bundle-against-head]]`** applies to all three specs.
  R3 needs HEAD verification that `internal/service/` does not exist
  yet (confirmed 2026-05-27). Production-code-helper-extraction needs
  re-snapshot of Sonar dup counts before each task fanout. Coverage-
  gate-per-file needs re-measurement of file percentages before each
  cg6b batch.
- **`[[bundle-scope-via-code-graph]]`** applies to production-code-
  helper-extraction t4 in particular (multi-file write_scope across
  commands/* with private helpers); pre-flight with
  `mcp__code-review-graph__file_summary`.

## Sub-spawns used

0 (none — all three specs were within complexity budget for a single
research pass).
